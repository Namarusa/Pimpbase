package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// openaiChat talks to the OpenAI chat completions API, which Azure OpenAI,
// Mistral and many other servers (Ollama, LM Studio, OpenRouter, Groq,
// vLLM…) also speak. flavor is the provider id; the differences are the
// address, the key header and a few fields.
type openaiChat struct {
	s        *Service
	flavor   string
	cfg      Config
	req      request
	messages []any
	ids      int
}

func (c *openaiChat) endpoint() string {
	base := strings.TrimRight(c.cfg.BaseURL, "/")
	if c.flavor != Azure {
		return base + "/chat/completions"
	}
	if c.azureV1() {
		if strings.HasSuffix(base, "/openai/v1") {
			return base + "/chat/completions"
		}
		return base + "/openai/v1/chat/completions"
	}
	return base + "/openai/deployments/" + url.PathEscape(c.cfg.Model) + "/chat/completions?api-version=" + url.QueryEscape(c.cfg.APIVersion)
}

// azureV1 is Azure's newer API, which takes the deployment as the model
// and no dated version.
func (c *openaiChat) azureV1() bool {
	return c.cfg.APIVersion == "v1" || strings.HasSuffix(strings.TrimRight(c.cfg.BaseURL, "/"), "/openai/v1")
}

func (c *openaiChat) send(ctx context.Context, results []toolResult, user string) (*reply, error) {
	if c.messages == nil {
		if c.req.system != "" || c.req.gen == nil {
			c.messages = append(c.messages, map[string]any{"role": "system", "content": c.req.system})
		}
		c.messages = append(c.messages, map[string]any{"role": "user", "content": c.req.prompt})
	}
	for _, r := range results {
		content := r.content
		if r.isError {
			content = "Error: " + content
		}
		m := map[string]any{"role": "tool", "tool_call_id": r.id, "content": content}
		if c.flavor == Mistral {
			m["name"] = r.name
		}
		c.messages = append(c.messages, m)
	}
	if user != "" {
		c.messages = append(c.messages, map[string]any{"role": "user", "content": user})
	}
	body := map[string]any{"messages": c.messages}
	if c.flavor != Azure || c.azureV1() {
		body["model"] = c.cfg.Model
	}
	// OpenAI's reasoning models take only max_completion_tokens; other
	// servers know max_tokens.
	if c.flavor == OpenAI || c.flavor == Azure {
		body["max_completion_tokens"] = c.req.maxTokens
	} else {
		body["max_tokens"] = c.req.maxTokens
	}
	if len(c.req.tools) > 0 {
		var tools []any
		for _, t := range c.req.tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": schema(t),
			}})
		}
		body["tools"] = tools
	}
	if g := c.req.gen; g != nil {
		c.settings(body, g)
	}
	header := http.Header{}
	if c.flavor == Azure {
		header.Set("api-key", c.cfg.APIKey)
	} else if c.cfg.APIKey != "" {
		header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content   json.RawMessage `json:"content"`
				Refusal   string          `json:"refusal"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := c.s.postJSON(ctx, c.endpoint(), header, body, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, &providerError{status: 502, message: "the answer has no choices"}
	}
	ch := out.Choices[0]
	r := &reply{stop: stopEnd, text: openaiText(ch.Message.Content), usage: Usage{Input: out.Usage.Prompt, Output: out.Usage.Completion}}
	switch {
	case ch.Message.Refusal != "":
		r.stop = stopRefusal
	case ch.FinishReason == "content_filter" && r.text == "" && len(ch.Message.ToolCalls) == 0:
		r.stop = stopRefusal
	case ch.FinishReason == "length":
		r.stop = stopMaxTokens
	}
	var calls []any
	for _, tc := range ch.Message.ToolCalls {
		id := tc.ID
		if id == "" {
			c.ids++
			id = fmt.Sprintf("c%08d", c.ids)
		}
		call := toolCall{id: id, name: tc.Function.Name}
		call.input, call.bad = toolArguments(tc.Function.Arguments)
		args := string(call.input)
		if call.bad != "" {
			args = "{}"
		}
		r.calls = append(r.calls, call)
		calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]any{"name": tc.Function.Name, "arguments": args}})
	}
	m := map[string]any{"role": "assistant", "content": r.text}
	if len(calls) > 0 {
		m["tool_calls"] = calls
	}
	c.messages = append(c.messages, m)
	return r, nil
}

// settings adds a flow step's settings to a request.
func (c *openaiChat) settings(body map[string]any, g *genConfig) {
	if g.temperature != nil {
		body["temperature"] = *g.temperature
	}
	if g.topP != nil {
		body["top_p"] = *g.topP
	}
	if g.seed != nil {
		// Mistral names the seed differently.
		if c.flavor == Mistral {
			body["random_seed"] = *g.seed
		} else {
			body["seed"] = *g.seed
		}
	}
	if len(g.stop) > 0 {
		body["stop"] = g.stop
	}
	if g.effort != "" {
		body["reasoning_effort"] = g.effort
	}
	switch g.format {
	case "json":
		body["response_format"] = map[string]any{"type": "json_object"}
	case "schema":
		body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "strict": true, "schema": g.schema}}
	}
}

// schema is a tool's input as JSON Schema.
func schema(t Tool) map[string]any {
	props := t.Schema
	if props == nil {
		props = map[string]any{}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(t.Required) > 0 {
		s["required"] = t.Required
	}
	return s
}

// toolArguments reads a call's arguments: a JSON string holding an object,
// as OpenAI sends them, or the object itself, as some servers do.
func toolArguments(raw json.RawMessage) (json.RawMessage, string) {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" || t == `""` {
		return json.RawMessage("{}"), ""
	}
	if strings.HasPrefix(t, `"`) {
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, "the arguments are not valid JSON; send them as one JSON object"
		}
		t = strings.TrimSpace(str)
		if t == "" {
			return json.RawMessage("{}"), ""
		}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(t), &obj); err != nil {
		return nil, "the arguments are not valid JSON; send them as one JSON object"
	}
	return json.RawMessage(t), ""
}

var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

// openaiText reads a message's content: a string, or a list of parts. Some
// open models put their reasoning in <think> tags, which are dropped.
func openaiText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		json.Unmarshal(raw, &parts)
		var b []string
		for _, p := range parts {
			if p.Type == "text" || p.Type == "output_text" {
				b = append(b, p.Text)
			}
		}
		text = strings.Join(b, "\n")
	}
	return strings.TrimSpace(thinkBlock.ReplaceAllString(text, ""))
}
