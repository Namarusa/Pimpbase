package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// geminiChat talks to the Gemini API's generateContent. The model's turns
// go back exactly as they came, with their thought signatures.
type geminiChat struct {
	s        *Service
	cfg      Config
	req      request
	contents []any
	// sentIDs maps call ids to the ids Gemini gave, which may be none.
	sentIDs map[string]string
	ids     int
}

type geminiPart struct {
	Text         string `json:"text"`
	Thought      bool   `json:"thought"`
	FunctionCall *struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall"`
}

func (c *geminiChat) send(ctx context.Context, results []toolResult, user string) (*reply, error) {
	if c.contents == nil {
		c.sentIDs = map[string]string{}
		c.contents = []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": c.req.prompt}}}}
	}
	var parts []any
	for _, r := range results {
		resp := map[string]any{"name": r.name, "response": map[string]any{"result": r.content}}
		if r.isError {
			resp["response"] = map[string]any{"error": r.content}
		}
		if id := c.sentIDs[r.id]; id != "" {
			resp["id"] = id
		}
		parts = append(parts, map[string]any{"functionResponse": resp})
	}
	if user != "" {
		parts = append(parts, map[string]any{"text": user})
	}
	if len(parts) > 0 {
		c.contents = append(c.contents, map[string]any{"role": "user", "parts": parts})
	}
	gen := map[string]any{"maxOutputTokens": c.req.maxTokens}
	body := map[string]any{
		"contents":         c.contents,
		"generationConfig": gen,
	}
	if c.req.system != "" || c.req.gen == nil {
		body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": c.req.system}}}
	}
	if g := c.req.gen; g != nil {
		geminiSettings(gen, g, c.cfg.Model)
	}
	if len(c.req.tools) > 0 {
		var decls []any
		for _, t := range c.req.tools {
			decls = append(decls, map[string]any{"name": t.Name, "description": t.Description, "parametersJsonSchema": schema(t)})
		}
		body["tools"] = []any{map[string]any{"functionDeclarations": decls}}
	}
	model := strings.TrimPrefix(c.cfg.Model, "models/")
	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/models/" + url.PathEscape(model) + ":generateContent"
	header := http.Header{}
	header.Set("x-goog-api-key", c.cfg.APIKey)

	var out struct {
		Candidates []struct {
			Content      json.RawMessage `json:"content"`
			FinishReason string          `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		UsageMetadata struct {
			Prompt     int64 `json:"promptTokenCount"`
			Candidates int64 `json:"candidatesTokenCount"`
			Thoughts   int64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := c.s.postJSON(ctx, endpoint, header, body, &out); err != nil {
		return nil, err
	}
	if len(out.Candidates) == 0 {
		if out.PromptFeedback.BlockReason != "" {
			return &reply{stop: stopRefusal}, nil
		}
		return nil, &providerError{status: 502, message: "the answer has no candidates"}
	}
	cand := out.Candidates[0]
	var content struct {
		Parts []geminiPart `json:"parts"`
	}
	json.Unmarshal(cand.Content, &content)
	r := &reply{stop: stopEnd, usage: Usage{Input: out.UsageMetadata.Prompt, Output: out.UsageMetadata.Candidates + out.UsageMetadata.Thoughts}}
	switch cand.FinishReason {
	case "MAX_TOKENS":
		r.stop = stopMaxTokens
	case "SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII", "RECITATION", "IMAGE_SAFETY":
		if len(content.Parts) == 0 {
			r.stop = stopRefusal
		}
	}
	if len(content.Parts) == 0 {
		return r, nil
	}
	c.contents = append(c.contents, cand.Content)
	var text []string
	for _, p := range content.Parts {
		switch {
		case p.FunctionCall != nil:
			c.ids++
			id := fmt.Sprintf("g%d", c.ids)
			c.sentIDs[id] = p.FunctionCall.ID
			call := toolCall{id: id, name: p.FunctionCall.Name}
			call.input, call.bad = toolArguments(p.FunctionCall.Args)
			r.calls = append(r.calls, call)
		case p.Thought:
		case p.Text != "":
			text = append(text, p.Text)
		}
	}
	r.text = strings.TrimSpace(strings.Join(text, ""))
	return r, nil
}

// geminiSettings adds a flow step's settings to generationConfig. Gemini 3
// models take a thinking level; earlier ones a thinking budget.
func geminiSettings(gen map[string]any, g *genConfig, model string) {
	if g.temperature != nil {
		gen["temperature"] = *g.temperature
	}
	if g.topP != nil {
		gen["topP"] = *g.topP
	}
	if g.seed != nil {
		gen["seed"] = *g.seed
	}
	if len(g.stop) > 0 {
		gen["stopSequences"] = g.stop
	}
	switch g.format {
	case "json":
		gen["responseMimeType"] = "application/json"
	case "schema":
		gen["responseMimeType"] = "application/json"
		gen["responseJsonSchema"] = g.schema
	}
	if g.thinking == "" {
		return
	}
	m := strings.TrimPrefix(model, "models/")
	if strings.HasPrefix(m, "gemini-2") {
		budget := map[string]int{"off": 0, "low": 1024, "high": 16384}[g.thinking]
		gen["thinkingConfig"] = map[string]any{"thinkingBudget": budget}
		return
	}
	level := g.thinking
	if level == "off" {
		// Gemini 3 cannot stop thinking; minimal is the least it does.
		level = "minimal"
	}
	gen["thinkingConfig"] = map[string]any{"thinkingLevel": level}
}
