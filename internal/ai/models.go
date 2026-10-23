package ai

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Model is one model a provider offers.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Note adds a detail, e.g. the model behind an Azure deployment.
	Note string `json:"note,omitempty"`
}

// ModelList is what a provider listed.
type ModelList struct {
	Provider string  `json:"provider"`
	Models   []Model `json:"models"`
}

// ListModels asks a provider which models it offers, for picking one.
// Settings in draft that are not saved yet (a key just pasted, a new
// address) are used in place of the saved ones; the server's still win.
func (s *Service) ListModels(ctx context.Context, id string, draft Update) (*ModelList, error) {
	p, ok := provider(id)
	if !ok {
		return nil, &Error{"there is no AI provider " + strings.TrimSpace(id)}
	}
	_, cfg, err := s.providerStatus(ctx, p)
	if err != nil {
		return nil, err
	}
	srv := s.Server[id]
	use := func(server string, v *string, to *string) {
		if server == "" && v != nil && strings.TrimSpace(*v) != "" {
			*to = strings.TrimRight(strings.TrimSpace(*v), "/")
		}
	}
	use(srv.APIKey, draft.APIKey, &cfg.APIKey)
	use(srv.BaseURL, draft.BaseURL, &cfg.BaseURL)
	use(srv.APIVersion, draft.APIVersion, &cfg.APIVersion)
	if p.NeedsKey && cfg.APIKey == "" {
		return nil, &Error{"add the " + p.Name + " API key first, then load its models"}
	}
	if cfg.BaseURL == "" {
		return nil, &Error{"add the address first, then load its models"}
	}
	if v, err := url.Parse(cfg.BaseURL); err != nil || (v.Scheme != "http" && v.Scheme != "https") || v.Host == "" {
		return nil, &Error{"the address must be a full http:// or https:// URL"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var models []Model
	switch id {
	case Anthropic:
		models, err = s.anthropicModels(ctx, cfg)
	case Gemini:
		models, err = s.geminiModels(ctx, cfg)
	case Azure:
		models, err = s.azureDeployments(ctx, cfg)
	case TypeSafe:
		models, err = s.typesafeModels(ctx, cfg)
	default:
		models, err = s.openaiModels(ctx, id, cfg)
	}
	if err != nil {
		var pe *providerError
		if id == Azure && errors.As(err, &pe) && (pe.status == 404 || pe.status == 400) {
			return nil, &Error{"Azure did not list the deployments of this resource; type the deployment name from the Deployments page of Azure AI Foundry"}
		}
		if errors.As(err, &pe) && (pe.status == 401 || pe.status == 403) {
			return nil, &Error{p.Name + " refused this key; check that it is complete and still active"}
		}
		e := apiError(p, err)
		if ae, ok := e.(*Error); ok {
			ae.Message = strings.Replace(ae.Message, "; try again", "", 1)
		}
		return nil, e
	}
	if models == nil {
		models = []Model{}
	}
	return &ModelList{Provider: id, Models: models}, nil
}

func (s *Service) anthropicModels(ctx context.Context, cfg Config) ([]Model, error) {
	h := http.Header{}
	h.Set("x-api-key", cfg.APIKey)
	h.Set("anthropic-version", "2023-06-01")
	var out []Model
	after := ""
	for page := 0; page < 10; page++ {
		u := strings.TrimSuffix(cfg.BaseURL, "/v1") + "/v1/models?limit=1000"
		if after != "" {
			u += "&after_id=" + url.QueryEscape(after)
		}
		var res struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := s.getJSON(ctx, u, h, &res); err != nil {
			return nil, err
		}
		for _, m := range res.Data {
			out = append(out, Model{ID: m.ID, Name: m.DisplayName})
		}
		if !res.HasMore || res.LastID == "" {
			break
		}
		after = res.LastID
	}
	return out, nil
}

// notChat are parts of OpenAI model ids that cannot hold a conversation
// with tools.
var notChat = []string{"embed", "whisper", "tts", "dall-e", "davinci", "babbage", "moderation", "image", "realtime", "audio", "transcribe", "sora", "search", "computer-use"}

func (s *Service) openaiModels(ctx context.Context, id string, cfg Config) ([]Model, error) {
	h := http.Header{}
	if cfg.APIKey != "" {
		h.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	var res struct {
		Data []struct {
			ID           string   `json:"id"`
			Name         string   `json:"name"`
			Created      int64    `json:"created"`
			Supported    []string `json:"supported_parameters"`
			Capabilities *struct {
				Chat  *bool `json:"completion_chat"`
				Tools *bool `json:"function_calling"`
			} `json:"capabilities"`
			Deprecation string `json:"deprecation"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/models", h, &res); err != nil {
		return nil, err
	}
	sort.SliceStable(res.Data, func(i, j int) bool { return res.Data[i].Created > res.Data[j].Created })
	var out []Model
	seen := map[string]bool{}
	for _, m := range res.Data {
		lower := strings.ToLower(m.ID)
		skip := seen[m.ID] || strings.Contains(lower, "embed")
		if id == OpenAI {
			for _, x := range notChat {
				skip = skip || strings.Contains(lower, x)
			}
		}
		if c := m.Capabilities; c != nil && ((c.Chat != nil && !*c.Chat) || (c.Tools != nil && !*c.Tools)) {
			skip = true
		}
		if m.Supported != nil && !containsString(m.Supported, "tools") {
			skip = true
		}
		// System One models take no tools: they answer questions in flows.
		p, _ := provider(id)
		so := systemOne(p, m.ID)
		if skip && !(so && !seen[m.ID]) {
			continue
		}
		seen[m.ID] = true
		mm := Model{ID: m.ID}
		if so {
			mm.Note = "System One"
		}
		if m.Name != "" && m.Name != m.ID {
			mm.Name = m.Name
		}
		if m.Deprecation != "" {
			mm.Note = "deprecated"
		}
		out = append(out, mm)
	}
	// OpenRouter's alias for the newest Jev is not in its list.
	if id == OpenRouter && !seen["~typesafe/jev-latest"] {
		for _, m := range out {
			if strings.HasPrefix(m.ID, "typesafe/jev") {
				out = append([]Model{{ID: "~typesafe/jev-latest", Note: "System One, newest Jev"}}, out...)
				break
			}
		}
	}
	return out, nil
}

func (s *Service) geminiModels(ctx context.Context, cfg Config) ([]Model, error) {
	h := http.Header{}
	h.Set("x-goog-api-key", cfg.APIKey)
	var out []Model
	token := ""
	for page := 0; page < 10; page++ {
		u := strings.TrimRight(cfg.BaseURL, "/") + "/models?pageSize=1000"
		if token != "" {
			u += "&pageToken=" + url.QueryEscape(token)
		}
		var res struct {
			Models []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				Methods     []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			Next string `json:"nextPageToken"`
		}
		if err := s.getJSON(ctx, u, h, &res); err != nil {
			return nil, err
		}
		for _, m := range res.Models {
			id := strings.TrimPrefix(m.Name, "models/")
			lower := strings.ToLower(id)
			if !containsString(m.Methods, "generateContent") || strings.Contains(lower, "embedding") || strings.Contains(lower, "imagen") ||
				strings.Contains(lower, "-tts") || strings.Contains(lower, "image") || strings.HasPrefix(lower, "aqa") {
				continue
			}
			out = append(out, Model{ID: id, Name: m.DisplayName})
		}
		if res.Next == "" {
			break
		}
		token = res.Next
	}
	return out, nil
}

// azureDeployments lists a resource's deployments, which Azure requests
// name instead of a model.
func (s *Service) azureDeployments(ctx context.Context, cfg Config) ([]Model, error) {
	h := http.Header{}
	h.Set("api-key", cfg.APIKey)
	base := strings.TrimSuffix(strings.TrimRight(cfg.BaseURL, "/"), "/openai/v1")
	var res struct {
		Data []struct {
			ID     string `json:"id"`
			Model  string `json:"model"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, base+"/openai/deployments?api-version=2022-12-01", h, &res); err != nil {
		return nil, err
	}
	var out []Model
	for _, d := range res.Data {
		if d.Status != "" && d.Status != "succeeded" {
			continue
		}
		out = append(out, Model{ID: d.ID, Note: d.Model})
	}
	return out, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
