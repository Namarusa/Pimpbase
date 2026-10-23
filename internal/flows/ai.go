package flows

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/ai"
	"github.com/pimpbase/pimpbase/internal/interp"
)

// Generator answers AI steps; *ai.Service is one.
type Generator interface {
	Generate(ctx context.Context, g ai.Generation) (*ai.Generated, error)
}

// aiTimeout bounds one AI step.
const aiTimeout = 3 * time.Minute

// aiStep asks a model for one answer. Its settings are those of the
// provider it uses (see ai.StepOptions); empty ones keep the provider's
// defaults.
func (x *exec) aiStep(in *inputs) (any, error) {
	if x.e.AI == nil {
		return nil, errors.New("this server has no AI providers")
	}
	g := ai.Generation{
		Provider:   strings.TrimSpace(in.str("provider")),
		APIVersion: strings.TrimSpace(in.str("apiVersion")),
		Output:     in.str("output"),
		Effort:     in.str("effort"),
		Thinking:   in.str("thinking"),
	}
	for k, v := range map[string]string{"provider": g.Provider, "apiVersion": g.APIVersion, "output": g.Output, "effort": g.Effort, "thinking": g.Thinking} {
		if v != "" {
			in.values[k] = v
		}
	}
	var err error
	if g.Model, err = in.text("model", interp.Text); err != nil {
		return nil, err
	}
	g.Model = strings.TrimSpace(g.Model)
	if g.System, err = in.text("system", interp.Text); err != nil {
		return nil, err
	}
	if g.Prompt, err = in.text("prompt", interp.Text); err != nil {
		return nil, err
	}
	if g.Output == "fields" {
		list, _ := in.raw["fields"].([]any)
		for _, item := range list {
			m, _ := item.(map[string]any)
			f := ai.Field{Name: strings.TrimSpace(str(m, "name")), Type: str(m, "type"), Description: str(m, "description")}
			if f.Name != "" {
				g.Fields = append(g.Fields, f)
			}
		}
	}
	num := func(key string) (*float64, error) {
		s, err := in.text(key, interp.Text)
		if err != nil || strings.TrimSpace(s) == "" {
			return nil, err
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a number", key, s)
		}
		return &f, nil
	}
	if g.Temperature, err = num("temperature"); err != nil {
		return nil, err
	}
	if g.TopP, err = num("topP"); err != nil {
		return nil, err
	}
	n, err := num("maxTokens")
	if err != nil {
		return nil, err
	}
	if n != nil {
		if *n < 1 || *n != float64(int64(*n)) {
			return nil, errors.New("the length limit is a whole number of tokens")
		}
		g.MaxTokens = int64(*n)
	}
	seed, err := num("seed")
	if err != nil {
		return nil, err
	}
	if seed != nil {
		v := int64(*seed)
		g.Seed = &v
	}
	stop, err := in.text("stop", interp.Text)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(stop, "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			g.Stop = append(g.Stop, line)
		}
	}

	ctx, cancel := context.WithTimeout(x.ctx, aiTimeout)
	defer cancel()
	a, err := x.e.AI.Generate(ctx, g)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && x.ctx.Err() == nil {
			return nil, fmt.Errorf("no answer within %s", aiTimeout)
		}
		if a != nil && a.Text != "" {
			return nil, fmt.Errorf("%s; the answer began: %s", err, clip(a.Text, 300))
		}
		return nil, err
	}
	out := map[string]any{
		"text": a.Text, "provider": a.Provider, "model": a.Model, "truncated": a.Truncated,
		"usage": map[string]any{"input": a.Usage.Input, "output": a.Usage.Output},
	}
	if g.Output != "text" && g.Output != "" {
		out["data"] = a.Data
	}
	return out, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
