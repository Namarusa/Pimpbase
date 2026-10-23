package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// StepOptions says which settings a provider takes in a flow's AI step, so
// the step's settings match what the provider understands.
type StepOptions struct {
	Temperature bool `json:"temperature"`
	TopP        bool `json:"topP"`
	Seed        bool `json:"seed"`
	Stop        bool `json:"stop"`
	// Effort lists the reasoning efforts the provider takes, if any.
	Effort      []string `json:"effort,omitempty"`
	EffortLabel string   `json:"effortLabel,omitempty"`
	EffortHelp  string   `json:"effortHelp,omitempty"`
	// Thinking lists Gemini's thinking settings.
	Thinking []string `json:"thinking,omitempty"`
	// Schema is true where the provider holds answers to a JSON schema;
	// elsewhere the fields are asked for in words and checked after.
	Schema bool `json:"schema"`
	// JSONMode is true where the provider can be asked for any JSON.
	JSONMode bool `json:"jsonMode"`
	// MaxTokens is the default length limit.
	MaxTokens int `json:"maxTokens"`
	// Notes are hints the step shows for this provider.
	Notes string `json:"notes,omitempty"`
}

var (
	anthropicStep = StepOptions{Stop: true, Schema: true, MaxTokens: 4096,
		Effort: []string{"low", "medium", "high", "xhigh", "max"}, EffortLabel: "Effort",
		EffortHelp: "How hard Claude thinks before answering. Lower is faster and cheaper; Claude Opus 5.5 uses medium unless set.",
		Notes:      "Claude models no longer take temperature or top P; effort steers the answer instead."}
	openaiStep = StepOptions{Temperature: true, TopP: true, Seed: true, Stop: true, Schema: true, JSONMode: true, MaxTokens: 4096,
		Effort: []string{"minimal", "low", "medium", "high"}, EffortLabel: "Reasoning effort",
		EffortHelp: "Only for reasoning models (gpt-5, o-series). Those do not take temperature or top P; leave them empty."}
	geminiStep = StepOptions{Temperature: true, TopP: true, Seed: true, Stop: true, Schema: true, JSONMode: true, MaxTokens: 4096,
		Thinking: []string{"off", "low", "high"}}
	chatStep     = StepOptions{Temperature: true, TopP: true, Seed: true, Stop: true, Schema: true, JSONMode: true, MaxTokens: 4096}
	deepseekStep = StepOptions{Temperature: true, TopP: true, Stop: true, JSONMode: true, MaxTokens: 4096,
		Notes: "DeepSeek has no JSON schemas: the fields are asked for in the instructions and checked on the answer."}
)

// Field is one value a structured answer holds.
type Field struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // text, number, boolean, list, object
	Description string `json:"description"`
}

// Generation is what a flow asks a model for: one answer, no tools.
type Generation struct {
	// Provider is empty for the one in use; Model empty for its saved one.
	Provider   string
	Model      string
	APIVersion string
	System     string
	Prompt     string
	MaxTokens  int64
	// Output is "text", "json" (any JSON) or "fields" (an object with
	// Fields).
	Output      string
	Fields      []Field
	Temperature *float64
	TopP        *float64
	Seed        *int64
	Stop        []string
	Effort      string
	Thinking    string
}

// Generated is a model's answer to a Generation.
type Generated struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Text     string `json:"text"`
	// Data is the parsed answer for JSON and fields output.
	Data      any   `json:"data,omitempty"`
	Truncated bool  `json:"truncated"`
	Usage     Usage `json:"usage"`
}

// Usage counts tokens, as the provider reported them.
type Usage struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

// genConfig is a Generation's settings, as the chat adapters read them.
type genConfig struct {
	temperature *float64
	topP        *float64
	seed        *int64
	stop        []string
	effort      string
	thinking    string
	// format is "", "json" or "schema"; schema is the JSON schema then.
	format string
	schema map[string]any
}

var fieldNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// Check validates a Generation's settings against its provider.
func (g *Generation) check(p Provider) error {
	switch g.Output {
	case "", "text":
		g.Output = "text"
	case "json", "fields":
	default:
		return &Error{"the answer is text, JSON or fields"}
	}
	if g.Output == "fields" {
		if len(g.Fields) == 0 {
			return &Error{"add the fields the answer holds"}
		}
		seen := map[string]bool{}
		for _, f := range g.Fields {
			if !fieldNameRe.MatchString(f.Name) {
				return &Error{fmt.Sprintf("the field name %q uses letters, digits and underscores and starts with a letter", f.Name)}
			}
			if seen[strings.ToLower(f.Name)] {
				return &Error{"the field " + f.Name + " is listed twice"}
			}
			seen[strings.ToLower(f.Name)] = true
		}
	}
	if strings.TrimSpace(g.Prompt) == "" {
		return &Error{"the prompt is empty"}
	}
	o := p.Step
	if g.Temperature != nil {
		if !o.Temperature {
			return &Error{p.Name + " does not take a temperature"}
		}
		if *g.Temperature < 0 || *g.Temperature > 2 {
			return &Error{"the temperature is between 0 and 2"}
		}
	}
	if g.TopP != nil {
		if !o.TopP {
			return &Error{p.Name + " does not take top P"}
		}
		if *g.TopP <= 0 || *g.TopP > 1 {
			return &Error{"top P is above 0 and at most 1"}
		}
	}
	if g.Seed != nil && !o.Seed {
		return &Error{p.Name + " does not take a seed"}
	}
	if len(g.Stop) > 4 {
		return &Error{"use at most 4 stop sequences"}
	}
	if len(g.Stop) > 0 && !o.Stop {
		return &Error{p.Name + " does not take stop sequences"}
	}
	if g.Effort != "" && !containsString(o.Effort, g.Effort) {
		if len(o.Effort) == 0 {
			return &Error{p.Name + " does not take an effort"}
		}
		return &Error{fmt.Sprintf("the effort is one of %s", strings.Join(o.Effort, ", "))}
	}
	if g.Thinking != "" && !containsString(o.Thinking, g.Thinking) {
		return &Error{p.Name + " does not take a thinking setting"}
	}
	if g.MaxTokens < 0 || g.MaxTokens > 128000 {
		return &Error{"the length limit is between 1 and 128000 tokens"}
	}
	if g.MaxTokens == 0 {
		g.MaxTokens = int64(o.MaxTokens)
	}
	return nil
}

// fieldSchema is the JSON schema of an answer with the given fields. All
// fields are required and no others are allowed, as strict schemas want.
func fieldSchema(fields []Field) map[string]any {
	props := map[string]any{}
	var required []string
	for _, f := range fields {
		var s map[string]any
		switch f.Type {
		case "number":
			s = map[string]any{"type": "number"}
		case "boolean":
			s = map[string]any{"type": "boolean"}
		case "list":
			s = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		default:
			s = map[string]any{"type": "string"}
		}
		if d := strings.TrimSpace(f.Description); d != "" {
			s["description"] = d
		}
		props[f.Name] = s
		required = append(required, f.Name)
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// Generate asks a model for one answer, as a flow's AI step does.
func (s *Service) Generate(ctx context.Context, g Generation) (*Generated, error) {
	id := g.Provider
	if id == "" {
		var err error
		if id, _, err = s.active(ctx); err != nil {
			return nil, err
		}
	}
	p, ok := provider(id)
	if !ok {
		return nil, &Error{fmt.Sprintf("there is no AI provider %q", id)}
	}
	if err := g.check(p); err != nil {
		return nil, err
	}
	st, cfg, err := s.providerStatus(ctx, p)
	if err != nil {
		return nil, err
	}
	if m := strings.TrimSpace(g.Model); m != "" {
		if p.ID == Anthropic && !strings.HasPrefix(m, "claude-") {
			return nil, &Error{fmt.Sprintf("%q is not a Claude model", m)}
		}
		cfg.Model = m
	}
	if v := strings.TrimSpace(g.APIVersion); v != "" && p.HasAPIVersion {
		cfg.APIVersion = v
	}
	switch {
	case p.NeedsKey && cfg.APIKey == "", cfg.BaseURL == "":
		return nil, &Error{p.Name + " is not set up: a workspace admin adds " + st.Missing + " in Users → AI assistant"}
	case cfg.Model == "":
		return nil, &Error{"choose the " + strings.ToLower(p.ModelLabel) + " to use"}
	}

	gc := &genConfig{temperature: g.Temperature, topP: g.TopP, seed: g.Seed, stop: g.Stop, effort: g.Effort, thinking: g.Thinking}
	system := strings.TrimSpace(g.System)
	switch g.Output {
	case "json":
		if p.Step.JSONMode {
			gc.format = "json"
		}
		system = strings.TrimSpace(system + "\n\nAnswer with JSON only: no words before or after it, and no code fences.")
	case "fields":
		schema := fieldSchema(g.Fields)
		if p.Step.Schema {
			gc.format, gc.schema = "schema", schema
		} else {
			if p.Step.JSONMode {
				gc.format = "json"
			}
			raw, _ := json.Marshal(schema)
			system = strings.TrimSpace(system + "\n\nAnswer with one JSON object that follows this JSON schema, and nothing else (no code fences):\n" + string(raw))
		}
	}
	c := s.newChat(p, cfg, request{system: system, prompt: g.Prompt, maxTokens: g.MaxTokens, gen: gc})
	r, err := c.send(ctx, nil, "")
	if err != nil {
		return nil, apiError(p, err)
	}
	if r.stop == stopRefusal {
		return nil, &Error{"the model declined this request"}
	}
	a := &Generated{Provider: p.ID, Model: cfg.Model, Text: r.text, Truncated: r.stop == stopMaxTokens, Usage: r.usage}
	if g.Output == "text" {
		return a, nil
	}
	data, err := parseJSON(r.text)
	if err != nil {
		if a.Truncated {
			return a, &Error{"the answer was cut off at the length limit before its JSON was complete; raise the limit"}
		}
		return a, &Error{"the answer is not valid JSON: " + err.Error()}
	}
	if g.Output == "fields" {
		obj, ok := data.(map[string]any)
		if !ok {
			return a, &Error{"the answer is not a JSON object with the fields"}
		}
		var missing []string
		for _, f := range g.Fields {
			if _, ok := obj[f.Name]; !ok {
				missing = append(missing, f.Name)
			}
		}
		if len(missing) > 0 {
			return a, &Error{"the answer lacks the fields " + strings.Join(missing, ", ")}
		}
	}
	a.Data = data
	return a, nil
}

var fence = regexp.MustCompile("(?s)^```[a-zA-Z]*\\s*(.*?)\\s*```$")

// parseJSON reads a model's JSON answer, which some models wrap in a code
// fence however they are asked.
func parseJSON(text string) (any, error) {
	t := strings.TrimSpace(text)
	if m := fence.FindStringSubmatch(t); m != nil {
		t = m[1]
	}
	var v any
	if err := json.Unmarshal([]byte(t), &v); err != nil {
		if se, ok := err.(*json.SyntaxError); ok {
			return nil, fmt.Errorf("%s at character %d", se.Error(), se.Offset)
		}
		return nil, err
	}
	return v, nil
}
