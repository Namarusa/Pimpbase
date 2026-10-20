package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Draft kinds.
const (
	KindTable  = "table"
	KindScreen = "screen"
	KindQuery  = "query"
	KindFlow   = "flow"
)

// DraftRequest asks for a draft of one kind.
type DraftRequest struct {
	Kind   string
	Prompt string
	// App describes the app: tables, fields, queries, flows, roles.
	App any
	// Catalog lists the building blocks the builder offers (screen
	// components, flow steps), as the builder describes them.
	Catalog json.RawMessage
	// Current is the thing being changed, if not a new one.
	Current json.RawMessage
	// Check validates a draft and may tidy it; its errors go back to the
	// model, which tries again.
	Check func(ctx context.Context, draft json.RawMessage) (json.RawMessage, error)
}

// Draft is a proposed change, to be reviewed before it is applied.
type Draft struct {
	Kind    string          `json:"kind"`
	Summary string          `json:"summary"`
	Changes []string        `json:"changes"`
	Draft   json.RawMessage `json:"draft"`
	Notes   string          `json:"notes,omitempty"`
}

const maxPrompt = 4000

func checkPrompt(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", &Error{"describe what you want"}
	}
	if len(p) > maxPrompt {
		return "", &Error{fmt.Sprintf("keep the description under %d characters", maxPrompt)}
	}
	return p, nil
}

// MakeDraft asks the model for a draft and checks it.
func (s *Service) MakeDraft(ctx context.Context, req DraftRequest) (*Draft, error) {
	prompt, err := checkPrompt(req.Prompt)
	if err != nil {
		return nil, err
	}
	guide, ok := guides[req.Kind]
	if !ok {
		return nil, &Error{"drafts are tables, screens, queries or flows"}
	}
	app, _ := json.Marshal(req.App)
	var b strings.Builder
	b.WriteString(baseGuide)
	b.WriteString("\n\n")
	b.WriteString(guide)
	b.WriteString("\n\n<app>\n")
	b.Write(app)
	b.WriteString("\n</app>\n")
	if len(req.Catalog) > 0 {
		b.WriteString("\n<catalog>\n")
		b.Write(req.Catalog)
		b.WriteString("\n</catalog>\n")
	}
	user := "<request>\n" + prompt + "\n</request>"
	if len(req.Current) > 0 && string(req.Current) != "null" {
		user = "Change this existing " + req.Kind + ". Keep everything the request does not ask to change, including ids.\n<current>\n" + string(req.Current) + "\n</current>\n\n" + user
	}

	var out *Draft
	tool := Tool{
		Name:        "propose_draft",
		Description: "Propose the draft for the builder to review. Call it exactly once when the draft is ready; if it reports problems, fix them and call it again.",
		Schema: map[string]any{
			"summary": map[string]any{"type": "string", "description": "One sentence on what the draft is."},
			"changes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Each change in a few plain words, e.g. 'New table Invoices with 6 fields'."},
			"notes":   map[string]any{"type": "string", "description": "Anything the builder must do or decide themselves, such as a secret to add. Empty when nothing."},
			"draft":   map[string]any{"type": "object", "description": "The draft itself, in the shape the guide gives."},
		},
		Required: []string{"summary", "changes", "draft"},
		Run: func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			var d Draft
			if err := json.Unmarshal(input, &d); err != nil || len(d.Draft) == 0 {
				return "", false, errors.New("the input needs summary, changes and draft")
			}
			checked, err := req.Check(ctx, d.Draft)
			if err != nil {
				return "", false, fmt.Errorf("the draft has a problem: %s", err.Error())
			}
			d.Kind, d.Draft = req.Kind, checked
			if d.Changes == nil {
				d.Changes = []string{}
			}
			out = &d
			return "", true, nil
		},
	}
	res, err := s.converse(ctx, b.String(), user, []Tool{tool}, 32000)
	if err != nil {
		return nil, err
	}
	if out == nil {
		msg := "the assistant did not make a draft"
		if res.Text != "" {
			msg += ": " + res.Text
		}
		return nil, &Error{msg}
	}
	return out, nil
}

// FormulaRequest asks for a formula over a table.
type FormulaRequest struct {
	Prompt string
	// Table is the table's name and fields.
	Table any
	// Functions lists the formula functions.
	Functions any
	// Purpose is "field", "filter" or "rule".
	Purpose string
	Current string
	// Check compiles a formula and gives its result type.
	Check func(ctx context.Context, src string) (string, error)
}

// Formula is a proposed formula.
type Formula struct {
	Formula     string `json:"formula"`
	Explanation string `json:"explanation"`
	Type        string `json:"type"`
}

// MakeFormula writes a formula from a description and checks that it
// compiles.
func (s *Service) MakeFormula(ctx context.Context, req FormulaRequest) (*Formula, error) {
	prompt, err := checkPrompt(req.Prompt)
	if err != nil {
		return nil, err
	}
	table, _ := json.Marshal(req.Table)
	fns, _ := json.Marshal(req.Functions)
	purpose := map[string]string{
		"field":  "a formula field: its value is computed for each row",
		"filter": "a filter: it must give true or false for each row",
		"rule":   "a row rule: it must give true for the rows a role may see. Current.User.Email, Current.User.Id, Current.User.Name, Current.User.Role, Current.User.Roles and Current.User.Groups describe the person reading",
	}[req.Purpose]
	if purpose == "" {
		purpose = "a formula field: its value is computed for each row"
	}
	system := formulaGuide + "\n\nThe formula is " + purpose + ".\n\n<table>\n" + string(table) + "\n</table>\n\n<functions>\n" + string(fns) + "\n</functions>"
	user := "<request>\n" + prompt + "\n</request>"
	if strings.TrimSpace(req.Current) != "" {
		user = "The current formula is:\n<current>\n" + req.Current + "\n</current>\n\n" + user
	}
	var out *Formula
	tool := Tool{
		Name:        "propose_formula",
		Description: "Propose the formula. It is compiled; if it fails, you get the error and should fix it and call again.",
		Schema: map[string]any{
			"formula":     map[string]any{"type": "string"},
			"explanation": map[string]any{"type": "string", "description": "One or two plain sentences on what the formula does."},
		},
		Required: []string{"formula", "explanation"},
		Run: func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			var f Formula
			if err := json.Unmarshal(input, &f); err != nil || strings.TrimSpace(f.Formula) == "" {
				return "", false, errors.New("give the formula and an explanation")
			}
			f.Formula = strings.TrimSpace(f.Formula)
			typ, err := req.Check(ctx, f.Formula)
			if err != nil {
				return "", false, err
			}
			if (req.Purpose == "filter" || req.Purpose == "rule") && typ != "boolean" && typ != "any" {
				return "", false, fmt.Errorf("this must give true or false, but it gives %s", typ)
			}
			f.Type = typ
			out = &f
			return "", true, nil
		},
	}
	res, err := s.converse(ctx, system, user, []Tool{tool}, 8000)
	if err != nil {
		return nil, err
	}
	if out == nil {
		msg := "the assistant did not write a formula"
		if res.Text != "" {
			msg += ": " + res.Text
		}
		return nil, &Error{msg}
	}
	return out, nil
}

// AskRequest is a question about an app's data.
type AskRequest struct {
	Question string
	// Tables are the tables the asker may read, with their fields.
	Tables any
	// Who describes whose access the answer uses.
	Who string
	// Tools read the data with the asker's access.
	Tools []Tool
}

// Answer answers a question about data.
type Answer struct {
	Answer string `json:"answer"`
	Calls  []Call `json:"calls"`
}

// Ask answers a question by reading the data through tools that keep to
// the asker's access.
func (s *Service) Ask(ctx context.Context, req AskRequest) (*Answer, error) {
	q, err := checkPrompt(req.Question)
	if err != nil {
		return nil, err
	}
	tables, _ := json.Marshal(req.Tables)
	system := askGuide + "\n\nYou answer for " + req.Who + ". You see only what they may see.\n\n<tables>\n" + string(tables) + "\n</tables>"
	res, err := s.converse(ctx, system, q, req.Tools, 8000)
	if err != nil {
		return nil, err
	}
	calls := res.Calls
	if calls == nil {
		calls = []Call{}
	}
	if res.Text == "" {
		return nil, &Error{"the assistant found no answer"}
	}
	return &Answer{Answer: res.Text, Calls: calls}, nil
}
