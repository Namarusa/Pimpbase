package ai

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicChat talks to the Anthropic Messages API through its SDK.
type anthropicChat struct {
	client  anthropic.Client
	params  anthropic.BetaMessageNewParams
	started bool
}

func newAnthropicChat(s *Service, cfg Config, req request) *anthropicChat {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey), option.WithMaxRetries(2), option.WithRequestTimeout(5 * time.Minute)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if s.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(s.HTTPClient))
	}
	var defs []anthropic.BetaToolUnionParam
	for _, t := range req.tools {
		defs = append(defs, anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: t.Schema, Required: t.Required},
		}})
	}
	params := anthropic.BetaMessageNewParams{
		Model:        anthropic.Model(cfg.Model),
		MaxTokens:    req.maxTokens,
		System:       []anthropic.BetaTextBlockParam{{Text: req.system, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		Messages:     []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.prompt))},
		Tools:        defs,
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
	}
	// A request a safety check declines is answered by another model
	// instead of failing.
	if m := cfg.Model; strings.HasPrefix(m, "claude-opus-5") || strings.HasPrefix(m, "claude-fable-5") || m == "claude-sonnet-5-5" {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}
	if g := req.gen; g != nil {
		if g.effort != "" {
			params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(g.effort)
		} else {
			params.OutputConfig.Effort = ""
		}
		if g.format == "schema" {
			params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: g.schema}
		}
		params.StopSequences = g.stop
		if req.system == "" {
			params.System = nil
		}
	}
	return &anthropicChat{client: anthropic.NewClient(opts...), params: params}
}

func (c *anthropicChat) send(ctx context.Context, results []toolResult, user string) (*reply, error) {
	if c.started {
		var blocks []anthropic.BetaContentBlockParamUnion
		for _, r := range results {
			blocks = append(blocks, anthropic.NewBetaToolResultBlock(r.id, r.content, r.isError))
		}
		if user != "" {
			blocks = append(blocks, anthropic.NewBetaTextBlock(user))
		}
		c.params.Messages = append(c.params.Messages, anthropic.NewBetaUserMessage(blocks...))
	}
	c.started = true
	msg, err := c.client.Beta.Messages.New(ctx, c.params)
	if err != nil {
		return nil, err
	}
	c.params.Messages = append(c.params.Messages, msg.ToParam())
	r := &reply{stop: stopEnd, usage: Usage{Input: msg.Usage.InputTokens, Output: msg.Usage.OutputTokens}}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		r.stop = stopRefusal
	case anthropic.BetaStopReasonMaxTokens:
		r.stop = stopMaxTokens
	}
	var text []string
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.BetaTextBlock:
			text = append(text, b.Text)
		case anthropic.BetaToolUseBlock:
			input, _ := json.Marshal(b.Input)
			r.calls = append(r.calls, toolCall{id: b.ID, name: b.Name, input: input})
		}
	}
	r.text = strings.TrimSpace(strings.Join(text, "\n"))
	return r, nil
}
