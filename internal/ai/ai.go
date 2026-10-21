// Package ai is the builder's assistant. It drafts tables, screens, API
// queries and flows from a description, writes formulas, and answers
// questions about an app's data. It calls a model from one of several
// providers (Anthropic, OpenAI, Google Gemini, Azure OpenAI, Mistral, or any
// OpenAI-compatible server) with settings kept on the server: from the
// environment, or saved by a workspace admin.
// Nothing it makes is applied by itself; drafts go back to the builder to
// be reviewed.
package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/bubibase/bubibase/internal/store"
)

// ErrNoKey is returned when the provider in use is not set up.
var ErrNoKey = errors.New("the AI assistant is not set up: a workspace admin picks a provider and adds its key in Users → AI assistant, or the server is started with one")

// Error messages are safe to show.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// Service holds the assistant's settings and calls the model.
type Service struct {
	db *store.DB
	// Server holds settings given to the server (flags or environment),
	// per provider. They win over saved ones and cannot be changed from
	// the builder.
	Server map[string]Config
	// ServerProvider, when set, is the provider in use.
	ServerProvider string
	// HTTPClient replaces the client used to call providers, for tests.
	HTTPClient *http.Client
	// RetryDelay is the wait before the first retry of a failed call; it
	// doubles for the second.
	RetryDelay time.Duration
	now        func() time.Time
}

func NewService(s *store.Store) *Service {
	return &Service{db: s.Meta, Server: map[string]Config{}, now: time.Now, RetryDelay: time.Second}
}

func (s *Service) setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRow(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Service) put(ctx context.Context, key, value string) error {
	if value == "" {
		_, err := s.db.Exec(ctx, `DELETE FROM settings WHERE key = ?`, key)
		return err
	}
	_, err := s.db.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, s.now().UTC().Format(time.RFC3339))
	return err
}

// Tool is something the model may call. Run returns the text sent back to
// the model; done ends the conversation with that call's input as the
// result. An error goes back to the model so it can correct itself.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Required    []string
	Run         func(ctx context.Context, input json.RawMessage) (result string, done bool, err error)
}

// Result is how a conversation ended.
type Result struct {
	// Text is the model's last words.
	Text string
	// Final is the input of the tool call that ended the conversation.
	Final json.RawMessage
	// Calls lists the tools the model called, in order.
	Calls []Call
}

// Call is one tool call, for showing how an answer was found.
type Call struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	Error string          `json:"error,omitempty"`
}

// maxTurns caps the round trips of one conversation.
const maxTurns = 12

// converse runs a conversation with tools, using the provider in use,
// until the model stops or a tool ends it. finish names the tool that
// must end it, if any: a model that stops without calling it is reminded
// once.
func (s *Service) converse(ctx context.Context, system, prompt string, tools []Tool, maxTokens int64, finish string) (*Result, error) {
	id, _, err := s.active(ctx)
	if err != nil {
		return nil, err
	}
	p, _ := provider(id)
	st, cfg, err := s.providerStatus(ctx, p)
	if err != nil {
		return nil, err
	}
	if !st.Configured {
		return nil, ErrNoKey
	}
	return s.run(ctx, p, cfg, system, prompt, tools, maxTokens, finish)
}

func (s *Service) run(ctx context.Context, p Provider, cfg Config, system, prompt string, tools []Tool, maxTokens int64, finish string) (*Result, error) {
	c := s.newChat(p, cfg, request{system: system, prompt: prompt, tools: tools, maxTokens: maxTokens})
	byName := map[string]Tool{}
	for _, t := range tools {
		byName[t.Name] = t
	}
	res := &Result{}
	var results []toolResult
	nudge, nudged := "", false
	for turn := 0; turn < maxTurns; turn++ {
		r, err := c.send(ctx, results, nudge)
		if err != nil {
			return nil, apiError(p, err)
		}
		if r.stop == stopRefusal {
			return nil, &Error{"the model declined this request; try wording it differently"}
		}
		results, nudge = nil, ""
		res.Text = r.text
		for _, tc := range r.calls {
			call := Call{Tool: tc.name, Input: tc.input}
			if call.Input == nil {
				call.Input = json.RawMessage("{}")
			}
			t, ok := byName[tc.name]
			switch {
			case tc.bad != "":
				call.Error = tc.bad
			case !ok:
				call.Error = "there is no tool named " + tc.name
			}
			if call.Error != "" {
				res.Calls = append(res.Calls, call)
				results = append(results, toolResult{id: tc.id, name: tc.name, content: call.Error, isError: true})
				continue
			}
			out, done, err := t.Run(ctx, call.Input)
			if err != nil {
				call.Error = err.Error()
				res.Calls = append(res.Calls, call)
				results = append(results, toolResult{id: tc.id, name: tc.name, content: err.Error(), isError: true})
				continue
			}
			res.Calls = append(res.Calls, call)
			if done {
				res.Final = call.Input
				return res, nil
			}
			results = append(results, toolResult{id: tc.id, name: tc.name, content: out})
		}
		if len(results) > 0 {
			continue
		}
		if r.stop == stopMaxTokens {
			return nil, &Error{"the answer was too long and was cut off; ask for something smaller"}
		}
		if finish == "" || nudged {
			return res, nil
		}
		nudged = true
		nudge = "You have not called the " + finish + " tool. Call it now with your result; reply in words only if the request cannot be done."
	}
	return nil, &Error{"the assistant took too many steps without finishing; try a narrower request"}
}

// request is what a conversation starts with.
type request struct {
	system    string
	prompt    string
	tools     []Tool
	maxTokens int64
}

// A chat is one conversation with one provider. It keeps the history in
// the provider's own format, so whatever the provider needs back (thinking
// blocks, signatures) goes back unchanged.
type chat interface {
	// send sends the results of the last reply's tool calls (none at
	// first) and a user message, if any, and returns the next reply.
	send(ctx context.Context, results []toolResult, user string) (*reply, error)
}

type toolCall struct {
	id, name string
	input    json.RawMessage
	// bad says why the call cannot run, e.g. its arguments are not JSON.
	bad string
}

type toolResult struct {
	id, name, content string
	isError           bool
}

const (
	stopEnd       = "end"
	stopMaxTokens = "max_tokens"
	stopRefusal   = "refusal"
)

type reply struct {
	text  string
	calls []toolCall
	stop  string
}

func (s *Service) newChat(p Provider, cfg Config, req request) chat {
	switch p.ID {
	case Gemini:
		return &geminiChat{s: s, cfg: cfg, req: req}
	case OpenAI, Azure, Mistral, Compatible:
		return &openaiChat{s: s, flavor: p.ID, cfg: cfg, req: req}
	}
	return newAnthropicChat(s, cfg, req)
}

// providerError is a provider's HTTP error.
type providerError struct {
	status     int
	message    string
	retryAfter time.Duration
}

func (e *providerError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("status %d", e.status)
	}
	return e.message
}

// apiError turns API failures into messages builders can act on.
func apiError(p Provider, err error) error {
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	status, detail := 0, ""
	var apiErr *anthropic.Error
	var pe *providerError
	switch {
	case errors.As(err, &apiErr):
		status, detail = apiErr.StatusCode, apiErr.Error()
	case errors.As(err, &pe):
		status, detail = pe.status, pe.message
	}
	name := "the " + p.Name + " API"
	if p.ID == Compatible {
		name = "the AI server"
	}
	switch status {
	case 0:
	case 401, 403:
		return &Error{name + " refused the key; check it in Users → AI assistant"}
	case 429:
		return &Error{name + " is rate limiting this key; try again in a minute"}
	case 400, 404, 422:
		return &Error{name + " rejected the request: " + detail}
	default:
		return &Error{fmt.Sprintf("%s failed (%d); try again", name, status)}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{"the assistant took too long; try again"}
	}
	return &Error{"cannot reach " + name + ": " + err.Error()}
}
