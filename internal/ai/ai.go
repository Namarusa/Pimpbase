// Package ai is the builder's assistant. It drafts tables, screens, API
// queries and flows from a description, writes formulas, and answers
// questions about an app's data. It calls the Anthropic API with a key
// kept on the server: from the environment, or saved by a workspace admin.
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
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/bubibase/bubibase/internal/store"
)

// DefaultModel is the model used unless an admin picks another.
const DefaultModel = "claude-opus-5-5"

// Models are the models an admin can pick from.
var Models = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"}

// ErrNoKey is returned when no API key is set up.
var ErrNoKey = errors.New("the AI assistant is not set up: a workspace admin adds an Anthropic API key in Users → AI assistant, or the server is started with --anthropic-key")

// Error messages are safe to show.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// Service holds the assistant's settings and calls the model.
type Service struct {
	db *store.DB
	// EnvKey is a key given to the server (flag or environment). It wins
	// over a saved key and cannot be changed from the builder.
	EnvKey string
	// BaseURL and HTTPClient point the client elsewhere, for tests.
	BaseURL    string
	HTTPClient *http.Client
	now        func() time.Time
}

func NewService(s *store.Store) *Service {
	return &Service{db: s.Meta, now: time.Now}
}

// Status is what the builder may know about the setup. It never holds
// the key itself.
type Status struct {
	Configured bool `json:"configured"`
	// Source is "server" for a key given to the server, "workspace" for a
	// saved key, or "".
	Source  string   `json:"source"`
	KeyHint string   `json:"keyHint"`
	Model   string   `json:"model"`
	Models  []string `json:"models"`
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

func hint(key string) string {
	if len(key) < 12 {
		return "••••"
	}
	return key[:7] + "…" + key[len(key)-4:]
}

// Status reports the setup.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	key, source, err := s.key(ctx)
	if err != nil {
		return nil, err
	}
	model, err := s.model(ctx)
	if err != nil {
		return nil, err
	}
	st := &Status{Configured: key != "", Source: source, Model: model, Models: Models}
	if key != "" {
		st.KeyHint = hint(key)
	}
	return st, nil
}

func (s *Service) key(ctx context.Context) (string, string, error) {
	if s.EnvKey != "" {
		return s.EnvKey, "server", nil
	}
	k, err := s.setting(ctx, "ai.api_key")
	if err != nil || k == "" {
		return "", "", err
	}
	return k, "workspace", nil
}

func (s *Service) model(ctx context.Context) (string, error) {
	m, err := s.setting(ctx, "ai.model")
	if m == "" {
		m = DefaultModel
	}
	return m, err
}

// Update changes the saved key (nil leaves it, "" removes it) and the model.
func (s *Service) Update(ctx context.Context, apiKey *string, model string) error {
	if apiKey != nil {
		k := strings.TrimSpace(*apiKey)
		if k != "" && (!strings.HasPrefix(k, "sk-ant-") || len(k) < 20 || strings.ContainsAny(k, " \t\r\n")) {
			return &Error{"that does not look like an Anthropic API key (they start with sk-ant-)"}
		}
		if err := s.put(ctx, "ai.api_key", k); err != nil {
			return err
		}
	}
	if model != "" {
		ok := false
		for _, m := range Models {
			ok = ok || m == model
		}
		if !ok {
			return &Error{fmt.Sprintf("the model %q is not one the assistant offers", model)}
		}
		if err := s.put(ctx, "ai.model", model); err != nil {
			return err
		}
	}
	return nil
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

// converse runs a conversation with tools until the model stops or a tool
// ends it.
func (s *Service) converse(ctx context.Context, system, prompt string, tools []Tool, maxTokens int64) (*Result, error) {
	key, _, err := s.key(ctx)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrNoKey
	}
	model, err := s.model(ctx)
	if err != nil {
		return nil, err
	}
	opts := []option.RequestOption{option.WithAPIKey(key), option.WithMaxRetries(2), option.WithRequestTimeout(5 * time.Minute)}
	if s.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(s.BaseURL))
	}
	if s.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(s.HTTPClient))
	}
	client := anthropic.NewClient(opts...)

	byName := map[string]Tool{}
	var defs []anthropic.BetaToolUnionParam
	for _, t := range tools {
		byName[t.Name] = t
		defs = append(defs, anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: t.Schema, Required: t.Required},
		}})
	}
	params := anthropic.BetaMessageNewParams{
		Model:        anthropic.Model(model),
		MaxTokens:    maxTokens,
		System:       []anthropic.BetaTextBlockParam{{Text: system, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		Messages:     []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(prompt))},
		Tools:        defs,
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
	}
	// A request a safety check declines is answered by another model
	// instead of failing.
	if strings.HasPrefix(model, "claude-opus-5") || strings.HasPrefix(model, "claude-fable-5") || model == "claude-sonnet-5-5" {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}
	res := &Result{}
	for turn := 0; turn < maxTurns; turn++ {
		msg, err := client.Beta.Messages.New(ctx, params)
		if err != nil {
			return nil, apiError(err)
		}
		if msg.StopReason == anthropic.BetaStopReasonRefusal {
			return nil, &Error{"the model declined this request; try wording it differently"}
		}
		params.Messages = append(params.Messages, msg.ToParam())
		var results []anthropic.BetaContentBlockParamUnion
		var text []string
		for _, block := range msg.Content {
			switch b := block.AsAny().(type) {
			case anthropic.BetaTextBlock:
				text = append(text, b.Text)
			case anthropic.BetaToolUseBlock:
				input, _ := json.Marshal(b.Input)
				call := Call{Tool: b.Name, Input: input}
				t, ok := byName[b.Name]
				if !ok {
					call.Error = "there is no tool named " + b.Name
					res.Calls = append(res.Calls, call)
					results = append(results, anthropic.NewBetaToolResultBlock(b.ID, call.Error, true))
					continue
				}
				out, done, err := t.Run(ctx, input)
				if err != nil {
					call.Error = err.Error()
					res.Calls = append(res.Calls, call)
					results = append(results, anthropic.NewBetaToolResultBlock(b.ID, err.Error(), true))
					continue
				}
				res.Calls = append(res.Calls, call)
				if done {
					res.Final = input
					res.Text = strings.TrimSpace(strings.Join(text, "\n"))
					return res, nil
				}
				results = append(results, anthropic.NewBetaToolResultBlock(b.ID, out, false))
			}
		}
		res.Text = strings.TrimSpace(strings.Join(text, "\n"))
		if len(results) == 0 {
			if msg.StopReason == anthropic.BetaStopReasonMaxTokens {
				return nil, &Error{"the answer was too long and was cut off; ask for something smaller"}
			}
			return res, nil
		}
		params.Messages = append(params.Messages, anthropic.NewBetaUserMessage(results...))
	}
	return nil, &Error{"the assistant took too many steps without finishing; try a narrower request"}
}

// apiError turns API failures into messages builders can act on.
func apiError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401, 403:
			return &Error{"the Anthropic API refused the key; check it in Users → AI assistant"}
		case 429:
			return &Error{"the Anthropic API is rate limiting this key; try again in a minute"}
		case 400, 404:
			return &Error{"the Anthropic API rejected the request: " + apiErr.Error()}
		}
		return &Error{fmt.Sprintf("the Anthropic API failed (%d); try again", apiErr.StatusCode)}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{"the assistant took too long; try again"}
	}
	return &Error{"cannot reach the Anthropic API: " + err.Error()}
}
