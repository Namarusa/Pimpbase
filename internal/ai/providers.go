package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Provider ids.
const (
	Anthropic  = "anthropic"
	OpenAI     = "openai"
	Gemini     = "gemini"
	Azure      = "azure"
	Mistral    = "mistral"
	Compatible = "openai_compatible"
)

// Provider describes an LLM vendor the assistant can use.
type Provider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// NeedsKey is false where a key is optional, such as a local server.
	NeedsKey bool `json:"needsKey"`
	// NeedsBaseURL is true where there is no default address.
	NeedsBaseURL   bool   `json:"needsBaseUrl"`
	DefaultBaseURL string `json:"defaultBaseUrl"`
	DefaultModel   string `json:"defaultModel"`
	// Models are known models, offered when the provider's list cannot be
	// fetched. The list is fetched from the provider (see ListModels).
	Models []string `json:"models"`
	// CanList is false where the provider may not list its models.
	CanList bool `json:"canList"`
	// ModelLabel names the model setting, e.g. "Deployment" for Azure.
	ModelLabel string `json:"modelLabel"`
	// HasAPIVersion is true for Azure, whose requests carry one.
	HasAPIVersion bool   `json:"hasApiVersion"`
	KeyPrefix     string `json:"keyPrefix"`
	KeyHelp       string `json:"keyHelp"`
}

// DefaultModel is the model used unless an admin picks another.
const DefaultModel = "claude-opus-5-5"

// DefaultAzureAPIVersion is the Azure OpenAI API version used unless one is set.
const DefaultAzureAPIVersion = "2024-10-21"

// Models are the Anthropic models offered when its list cannot be fetched.
var Models = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"}

// Providers lists the vendors, in the order the builder shows them.
var Providers = []Provider{
	{ID: Anthropic, Name: "Anthropic", NeedsKey: true, DefaultBaseURL: "https://api.anthropic.com", DefaultModel: DefaultModel, Models: Models, CanList: true, ModelLabel: "Model", KeyPrefix: "sk-ant-", KeyHelp: "Create one at console.anthropic.com."},
	{ID: OpenAI, Name: "OpenAI", NeedsKey: true, DefaultBaseURL: "https://api.openai.com/v1", CanList: true, ModelLabel: "Model", KeyPrefix: "sk-", KeyHelp: "Create one at platform.openai.com."},
	{ID: Gemini, Name: "Google Gemini", NeedsKey: true, DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta", CanList: true, ModelLabel: "Model", KeyHelp: "Create one in Google AI Studio (aistudio.google.com)."},
	{ID: Azure, Name: "Azure OpenAI", NeedsKey: true, NeedsBaseURL: true, CanList: true, ModelLabel: "Deployment", HasAPIVersion: true, KeyHelp: "Find the key and endpoint under the resource's Keys and Endpoint page in the Azure portal."},
	{ID: Mistral, Name: "Mistral", NeedsKey: true, DefaultBaseURL: "https://api.mistral.ai/v1", CanList: true, ModelLabel: "Model", KeyHelp: "Create one at console.mistral.ai."},
	{ID: Compatible, Name: "OpenAI-compatible", NeedsBaseURL: true, CanList: true, ModelLabel: "Model", KeyHelp: "Any server speaking the OpenAI chat completions API: Ollama (http://localhost:11434/v1), LM Studio (http://localhost:1234/v1), OpenRouter, Groq, Together, vLLM… Leave the key empty when the server needs none."},
}

func provider(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Config is one provider's settings. Empty fields fall back to the
// provider's defaults.
type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	APIVersion string
}

// ProviderStatus is what the builder may know about one provider. It never
// holds the key itself.
type ProviderStatus struct {
	Provider
	Configured bool `json:"configured"`
	// KeySource is "server" for a key given to the server, "workspace" for
	// a saved key, or "". The other sources say the same of their field.
	KeySource        string `json:"keySource"`
	KeyHint          string `json:"keyHint"`
	BaseURL          string `json:"baseUrl"`
	BaseURLSource    string `json:"baseUrlSource"`
	Model            string `json:"model"`
	ModelSource      string `json:"modelSource"`
	APIVersion       string `json:"apiVersion"`
	APIVersionSource string `json:"apiVersionSource"`
	// Missing says what is still needed, when not configured.
	Missing string `json:"missing,omitempty"`
}

// Status is the setup: the provider in use and every provider's settings.
type Status struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider"`
	// ProviderSource is "server" when the server picks the provider.
	ProviderSource string `json:"providerSource"`
	// Source, KeyHint, Model and Models describe the provider in use.
	Source    string           `json:"source"`
	KeyHint   string           `json:"keyHint"`
	Model     string           `json:"model"`
	Models    []string         `json:"models"`
	Providers []ProviderStatus `json:"providers"`
}

// settingKey names a provider setting. Anthropic keeps the names it had
// before there were other providers.
func settingKey(p, field string) string {
	if p == Anthropic && (field == "api_key" || field == "model") {
		return "ai." + field
	}
	return "ai." + p + "." + field
}

func pick(server, saved, def string) (string, string) {
	switch {
	case server != "":
		return server, "server"
	case saved != "":
		return saved, "workspace"
	case def != "":
		return def, "default"
	}
	return "", ""
}

// providerStatus resolves one provider's settings: the server's win over
// saved ones, which win over defaults.
func (s *Service) providerStatus(ctx context.Context, p Provider) (*ProviderStatus, Config, error) {
	srv := s.Server[p.ID]
	var saved Config
	for _, f := range []struct {
		name string
		to   *string
	}{{"api_key", &saved.APIKey}, {"base_url", &saved.BaseURL}, {"model", &saved.Model}, {"api_version", &saved.APIVersion}} {
		v, err := s.setting(ctx, settingKey(p.ID, f.name))
		if err != nil {
			return nil, Config{}, err
		}
		*f.to = v
	}
	var c Config
	st := &ProviderStatus{Provider: p}
	c.APIKey, st.KeySource = pick(srv.APIKey, saved.APIKey, "")
	if st.KeySource == "default" {
		st.KeySource = ""
	}
	c.BaseURL, st.BaseURLSource = pick(srv.BaseURL, saved.BaseURL, p.DefaultBaseURL)
	c.Model, st.ModelSource = pick(srv.Model, saved.Model, p.DefaultModel)
	if p.HasAPIVersion {
		c.APIVersion, st.APIVersionSource = pick(srv.APIVersion, saved.APIVersion, DefaultAzureAPIVersion)
	}
	if c.APIKey != "" {
		st.KeyHint = hint(c.APIKey)
	}
	st.BaseURL, st.Model, st.APIVersion = c.BaseURL, c.Model, c.APIVersion
	if p.Models == nil {
		st.Models = []string{}
	}
	switch {
	case p.NeedsKey && c.APIKey == "":
		st.Missing = "an API key"
	case c.BaseURL == "":
		st.Missing = "an address"
		if p.ID == Azure {
			st.Missing = "the resource endpoint"
		}
	case c.Model == "":
		st.Missing = "a " + strings.ToLower(p.ModelLabel)
	}
	st.Configured = st.Missing == ""
	return st, c, nil
}

// active is the provider in use: the server's choice, else the saved one,
// else the first that is set up, else Anthropic.
func (s *Service) active(ctx context.Context) (string, string, error) {
	if s.ServerProvider != "" {
		return s.ServerProvider, "server", nil
	}
	p, err := s.setting(ctx, "ai.provider")
	if err != nil {
		return "", "", err
	}
	if _, ok := provider(p); ok {
		return p, "workspace", nil
	}
	for _, pr := range Providers {
		st, _, err := s.providerStatus(ctx, pr)
		if err != nil {
			return "", "", err
		}
		if st.Configured {
			return pr.ID, "default", nil
		}
	}
	return Anthropic, "default", nil
}

// Status reports the setup.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	active, source, err := s.active(ctx)
	if err != nil {
		return nil, err
	}
	st := &Status{Provider: active, ProviderSource: source}
	for _, p := range Providers {
		ps, _, err := s.providerStatus(ctx, p)
		if err != nil {
			return nil, err
		}
		st.Providers = append(st.Providers, *ps)
		if p.ID == active {
			st.Configured, st.Source, st.KeyHint, st.Model, st.Models = ps.Configured, ps.KeySource, ps.KeyHint, ps.Model, ps.Models
		}
	}
	return st, nil
}

// Update changes settings. Nil fields stay as they are; "" removes a saved
// value.
type Update struct {
	// Provider is whose settings change; empty means the one in use.
	Provider   string  `json:"provider"`
	APIKey     *string `json:"apiKey"`
	BaseURL    *string `json:"baseUrl"`
	Model      *string `json:"model"`
	APIVersion *string `json:"apiVersion"`
	// Use makes Provider the one in use.
	Use bool `json:"use"`
}

// Update saves settings, refusing ones the server gives.
func (s *Service) Update(ctx context.Context, u Update) error {
	id := u.Provider
	if id == "" {
		var err error
		if id, _, err = s.active(ctx); err != nil {
			return err
		}
	}
	p, ok := provider(id)
	if !ok {
		return &Error{fmt.Sprintf("there is no AI provider %q", id)}
	}
	srv := s.Server[id]
	type change struct {
		field, value, server string
	}
	var changes []change
	if u.APIKey != nil {
		k := strings.TrimSpace(*u.APIKey)
		if k != "" {
			if err := checkKey(p, k); err != nil {
				return err
			}
		}
		changes = append(changes, change{"api_key", k, srv.APIKey})
	}
	if u.BaseURL != nil {
		b := strings.TrimRight(strings.TrimSpace(*u.BaseURL), "/")
		if b != "" {
			if v, err := url.Parse(b); err != nil || (v.Scheme != "http" && v.Scheme != "https") || v.Host == "" {
				return &Error{"the address must be a full http:// or https:// URL"}
			}
		}
		changes = append(changes, change{"base_url", b, srv.BaseURL})
	}
	if u.Model != nil {
		m := strings.TrimSpace(*u.Model)
		if m != "" {
			if strings.ContainsAny(m, " \t\r\n") || len(m) > 200 {
				return &Error{"a model name has no spaces"}
			}
			if p.ID == Anthropic && !strings.HasPrefix(m, "claude-") {
				return &Error{fmt.Sprintf("%q is not a Claude model", m)}
			}
		}
		changes = append(changes, change{"model", m, srv.Model})
	}
	if u.APIVersion != nil {
		changes = append(changes, change{"api_version", strings.TrimSpace(*u.APIVersion), srv.APIVersion})
	}
	for _, c := range changes {
		if c.server != "" {
			return &Error{"the server was started with this setting, which is used instead; change it there"}
		}
	}
	if u.Use && s.ServerProvider != "" && s.ServerProvider != id {
		return &Error{"the server was started with a provider, which is used instead; change it there"}
	}
	for _, c := range changes {
		if err := s.put(ctx, settingKey(id, c.field), c.value); err != nil {
			return err
		}
	}
	if u.Use {
		return s.put(ctx, "ai.provider", id)
	}
	return nil
}

func checkKey(p Provider, k string) error {
	if strings.ContainsAny(k, " \t\r\n") || len(k) < 8 {
		return &Error{"that does not look like an API key"}
	}
	if p.ID == Anthropic && (!strings.HasPrefix(k, "sk-ant-") || len(k) < 20) {
		return &Error{"that does not look like an Anthropic API key (they start with sk-ant-)"}
	}
	if p.ID == OpenAI && !strings.HasPrefix(k, "sk-") {
		return &Error{"that does not look like an OpenAI API key (they start with sk-)"}
	}
	return nil
}

// keyPrefixes are the public starts of keys, which a hint may show.
var keyPrefixes = []string{"sk-ant-", "sk-proj-", "sk-or-v1-", "sk-svcacct-", "sk-", "AIza", "gsk_"}

func hint(key string) string {
	if len(key) < 12 {
		return "••••"
	}
	for _, p := range keyPrefixes {
		if strings.HasPrefix(key, p) {
			return p + "…" + key[len(key)-4:]
		}
	}
	return "…" + key[len(key)-4:]
}

// Check is the outcome of a connection test.
type Check struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Reply    string `json:"reply"`
	Millis   int64  `json:"ms"`
}

// Test sends a tiny request with a provider's settings, to check them
// before or after they are used.
func (s *Service) Test(ctx context.Context, id string) (*Check, error) {
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
	st, cfg, err := s.providerStatus(ctx, p)
	if err != nil {
		return nil, err
	}
	if !st.Configured {
		return nil, &Error{p.Name + " still needs " + st.Missing}
	}
	start := s.now()
	echo := Tool{
		Name:        "report",
		Description: "Report that the connection works.",
		Schema:      map[string]any{"status": map[string]any{"type": "string", "description": "The word OK"}},
		Required:    []string{"status"},
		Run: func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			return "", true, nil
		},
	}
	res, err := s.run(ctx, p, cfg, "You check that a connection works. Call the report tool with status OK.", "Check the connection.", []Tool{echo}, 400, "")
	if err != nil {
		return nil, err
	}
	reply := res.Text
	if res.Final != nil {
		reply = "Called the report tool: " + string(res.Final)
	}
	return &Check{Provider: id, Model: cfg.Model, Reply: reply, Millis: s.now().Sub(start).Milliseconds()}, nil
}
