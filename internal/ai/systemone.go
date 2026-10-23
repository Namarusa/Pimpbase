package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// System One models, such as TypeSafe's Jev, do not write text: they are
// given a state (text or JSON) and typed questions about it, and answer
// each with a value and its probabilities. TypeSafe serves them at
// /v1/systemone, and OpenRouter at the same path for its typesafe/ models.

// Question is one System One question.
type Question struct {
	Name         string `json:"name"`
	Type         string `json:"type"` // choice, score or noul
	Instructions string `json:"instructions"`
	// Options are a choice's answers, with what each means.
	Options []Field `json:"options,omitempty"`
	// Levels are a score's levels, lowest first; the score is a position.
	Levels []string `json:"levels,omitempty"`
	// True and False say what a noul's answers mean.
	True  string `json:"true,omitempty"`
	False string `json:"false,omitempty"`
}

// systemOne is true when a provider's model answers System One questions.
func systemOne(p Provider, model string) bool {
	for _, prefix := range p.SystemOne {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

// IsSystemOne says whether a provider's model is a System One model.
func IsSystemOne(id, model string) bool {
	p, ok := provider(id)
	return ok && systemOne(p, model)
}

const maxQuestions = 32

func checkQuestions(qs []Question) error {
	if len(qs) == 0 {
		return &Error{"add at least one question for the System One model"}
	}
	if len(qs) > maxQuestions {
		return &Error{fmt.Sprintf("ask at most %d questions in one step", maxQuestions)}
	}
	seen := map[string]bool{}
	for _, q := range qs {
		if !fieldNameRe.MatchString(q.Name) {
			return &Error{fmt.Sprintf("the question name %q uses letters, digits and underscores and starts with a letter", q.Name)}
		}
		if seen[strings.ToLower(q.Name)] {
			return &Error{"the question " + q.Name + " is listed twice"}
		}
		seen[strings.ToLower(q.Name)] = true
		if strings.TrimSpace(q.Instructions) == "" {
			return &Error{q.Name + ": say what the question asks"}
		}
		switch q.Type {
		case "choice":
			if len(q.Options) < 2 || len(q.Options) > 255 {
				return &Error{q.Name + ": a choice has 2 to 255 options"}
			}
			names := map[string]bool{}
			for _, o := range q.Options {
				if strings.TrimSpace(o.Name) == "" || names[o.Name] {
					return &Error{q.Name + ": each option needs its own name"}
				}
				names[o.Name] = true
			}
		case "score":
			if len(q.Levels) < 2 || len(q.Levels) > 10 {
				return &Error{q.Name + ": a score has 2 to 10 levels"}
			}
			for _, l := range q.Levels {
				if strings.TrimSpace(l) == "" {
					return &Error{q.Name + ": describe every level"}
				}
			}
		case "noul":
		default:
			return &Error{q.Name + ": a question is a choice, a score or a yes/no (noul)"}
		}
	}
	return nil
}

// wire is a question as the API takes it.
func (q Question) wire(openRouter bool) map[string]any {
	m := map[string]any{"type": q.Type, "instructions": q.Instructions}
	switch q.Type {
	case "choice":
		c := map[string]any{}
		for _, o := range q.Options {
			var d any
			if s := strings.TrimSpace(o.Description); s != "" {
				d = s
			}
			c[strings.TrimSpace(o.Name)] = d
		}
		m["criteria"] = c
	case "score":
		m["criteria"] = q.Levels
	case "noul":
		t, f := strings.TrimSpace(q.True), strings.TrimSpace(q.False)
		// OpenRouter wants both; TypeSafe takes either or none.
		if openRouter {
			if t == "" {
				t = "Yes"
			}
			if f == "" {
				f = "No"
			}
		}
		c := map[string]any{}
		if t != "" {
			c["true"] = t
		}
		if f != "" {
			c["false"] = f
		}
		if len(c) > 0 {
			m["criteria"] = c
		}
	}
	return m
}

// askSystemOne sends the questions and returns the answers keyed by
// question name, as the API gives them.
func (s *Service) askSystemOne(ctx context.Context, p Provider, cfg Config, state any, qs []Question) (*Generated, error) {
	if state == nil || state == "" {
		return nil, &Error{"the state is empty: give the model the data the questions are about"}
	}
	questions := map[string]any{}
	for _, q := range qs {
		questions[q.Name] = q.wire(p.ID == OpenRouter)
	}
	body := map[string]any{"model": cfg.Model, "state": state, "questions": questions}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+cfg.APIKey)
	if p.ID == OpenRouter {
		header.Set("X-Title", "Pimpbase")
	}
	var out struct {
		Model   string                    `json:"model"`
		Answers map[string]map[string]any `json:"answers"`
		Usage   struct {
			Input  int64   `json:"input_tokens"`
			Output int64   `json:"output_tokens"`
			Cost   float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := s.postJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/systemone", header, body, &out); err != nil {
		return nil, apiError(p, err)
	}
	if out.Answers == nil {
		return nil, &Error{p.Name + " sent no answers"}
	}
	data := map[string]any{}
	var lines []string
	for _, q := range qs {
		a, ok := out.Answers[q.Name]
		if !ok {
			return nil, &Error{p.Name + " did not answer " + q.Name}
		}
		delete(a, "type")
		data[q.Name] = a
		switch q.Type {
		case "choice":
			lines = append(lines, fmt.Sprintf("%s: %v", q.Name, a["choice"]))
		case "score":
			lines = append(lines, fmt.Sprintf("%s: %v", q.Name, a["score"]))
		case "noul":
			lines = append(lines, fmt.Sprintf("%s: %v", q.Name, a["noul"]))
		}
	}
	model := out.Model
	if model == "" {
		model = cfg.Model
	}
	return &Generated{Provider: p.ID, Model: model, Text: strings.Join(lines, "\n"), Data: data,
		Usage: Usage{Input: out.Usage.Input, Output: out.Usage.Output, Cost: out.Usage.Cost}}, nil
}

// testSystemOne checks a System One provider with one yes/no question.
func (s *Service) testSystemOne(ctx context.Context, p Provider, cfg Config) (*Check, error) {
	start := s.now()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	g, err := s.askSystemOne(ctx, p, cfg, "The sky is blue.", []Question{{Name: "ok", Type: "noul", Instructions: "Is the statement true?"}})
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(g.Data)
	return &Check{Provider: p.ID, Model: g.Model, Reply: "Answered a yes/no question: " + string(raw), Millis: s.now().Sub(start).Milliseconds()}, nil
}

// typesafeModels lists TypeSafe's models and aliases.
func (s *Service) typesafeModels(ctx context.Context, cfg Config) ([]Model, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+cfg.APIKey)
	var res struct {
		Models []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Released    string `json:"release_date"`
		} `json:"models"`
	}
	if err := s.getJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/models", h, &res); err != nil {
		return nil, err
	}
	var out []Model
	for _, m := range res.Models {
		out = append(out, Model{ID: m.Name, Note: strings.TrimSpace(m.Released)})
	}
	return out, nil
}
