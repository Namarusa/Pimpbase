package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pimpbase/pimpbase/internal/ai"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// fakeModels answers each provider's model list the way the provider does.
func fakeModels(t *testing.T) (*httptest.Server, func() []*http.Request) {
	var mu sync.Mutex
	var seen []*http.Request
	h := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	h.HandleFunc("GET /anthropic/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after_id") == "" {
			reply(w, map[string]any{"data": []any{map[string]any{"id": "claude-opus-5-5", "display_name": "Claude Opus 5.5"}}, "has_more": true, "last_id": "claude-opus-5-5"})
			return
		}
		reply(w, map[string]any{"data": []any{map[string]any{"id": "claude-haiku-5-5", "display_name": "Claude Haiku 5.5"}}, "has_more": false})
	})
	h.HandleFunc("GET /openai/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-typed-not-saved-1234" {
			w.WriteHeader(401)
			reply(w, map[string]any{"error": map[string]any{"message": "Incorrect API key provided"}})
			return
		}
		reply(w, map[string]any{"data": []any{
			map[string]any{"id": "gpt-old", "created": 100},
			map[string]any{"id": "text-embedding-3-large", "created": 300},
			map[string]any{"id": "gpt-new", "created": 200},
			map[string]any{"id": "whisper-1", "created": 50},
			map[string]any{"id": "gpt-image-1", "created": 400},
		}})
	})
	h.HandleFunc("GET /mistral/v1/models", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"data": []any{
			map[string]any{"id": "mistral-large-latest", "capabilities": map[string]any{"completion_chat": true, "function_calling": true}},
			map[string]any{"id": "mistral-embed", "capabilities": map[string]any{"completion_chat": false, "function_calling": false}},
			map[string]any{"id": "mistral-ocr-latest", "capabilities": map[string]any{"completion_chat": false, "function_calling": false}},
			map[string]any{"id": "open-old", "capabilities": map[string]any{"completion_chat": true, "function_calling": false}},
		}})
	})
	h.HandleFunc("GET /ollama/v1/models", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"object": "list", "data": []any{map[string]any{"id": "qwen3:8b", "created": 2}, map[string]any{"id": "llama3.3:70b", "created": 1}, map[string]any{"id": "nomic-embed-text", "created": 3}}})
	})
	h.HandleFunc("GET /openrouter/v1/models", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"data": []any{
			map[string]any{"id": "acme/tooler", "name": "Acme: Tooler", "supported_parameters": []any{"tools", "max_tokens"}},
			map[string]any{"id": "acme/plain", "name": "Acme: Plain", "supported_parameters": []any{"max_tokens"}},
		}})
	})
	h.HandleFunc("GET /gemini/v1beta/models", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			reply(w, map[string]any{"models": []any{
				map[string]any{"name": "models/gemini-pro-x", "displayName": "Gemini Pro X", "supportedGenerationMethods": []any{"generateContent", "countTokens"}},
				map[string]any{"name": "models/text-embedding-004", "supportedGenerationMethods": []any{"embedContent"}},
			}, "nextPageToken": "p2"})
			return
		}
		reply(w, map[string]any{"models": []any{map[string]any{"name": "models/gemini-flash-y", "displayName": "Gemini Flash Y", "supportedGenerationMethods": []any{"generateContent"}}}})
	})
	h.HandleFunc("GET /azure/openai/deployments", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Api-Key") != "azure-key-0123456789" || r.URL.Query().Get("api-version") == "" {
			w.WriteHeader(401)
			return
		}
		reply(w, map[string]any{"data": []any{
			map[string]any{"id": "sales-gpt", "model": "gpt-5", "status": "succeeded"},
			map[string]any{"id": "pending-one", "model": "gpt-5", "status": "running"},
		}})
	})
	h.HandleFunc("GET /azure-new/openai/deployments", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		reply(w, map[string]any{"error": map[string]any{"code": "404", "message": "Resource not found"}})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Clone(r.Context()))
		mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []*http.Request { mu.Lock(); defer mu.Unlock(); return seen }
}

func ids(list map[string]any) string {
	var out []string
	for _, m := range list["models"].([]any) {
		out = append(out, m.(map[string]any)["id"].(string))
	}
	return strings.Join(out, ",")
}

func TestAIModelLists(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		srv.AI.RetryDelay = time.Millisecond
		fake, seen := fakeModels(t)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)

		// A key must be there, saved or typed.
		if e := owner.do("POST", "/api/ai/models", map[string]any{"provider": "openai"}, 400); !strings.Contains(e["error"].(string), "key first") {
			t.Errorf("no key = %v", e)
		}
		// A key typed but not saved is used, and stays unsaved.
		l := owner.do("POST", "/api/ai/models", map[string]any{"provider": "openai", "apiKey": "sk-typed-not-saved-1234", "baseUrl": fake.URL + "/openai/v1"}, 200)
		if ids(l) != "gpt-new,gpt-old" {
			t.Errorf("openai models = %s", ids(l))
		}
		if st := owner.do("GET", "/api/ai", nil, 200); strings.Contains(func() string { b, _ := json.Marshal(st); return string(b) }(), "typed") {
			t.Errorf("the typed key was saved")
		}
		if e := owner.do("POST", "/api/ai/models", map[string]any{"provider": "openai", "apiKey": "sk-wrong-key-123456", "baseUrl": fake.URL + "/openai/v1"}, 400); !strings.Contains(e["error"].(string), "OpenAI refused this key") {
			t.Errorf("bad key = %v", e)
		}

		// Each provider's own list.
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "anthropic", "apiKey": "sk-ant-api03-abcdefghijklmnop-WXYZ", "baseUrl": fake.URL + "/anthropic"}, 200)
		if l := owner.do("POST", "/api/ai/models", map[string]any{"provider": "anthropic"}, 200); ids(l) != "claude-opus-5-5,claude-haiku-5-5" {
			t.Errorf("anthropic models = %s", ids(l))
		}
		for _, r := range seen() {
			if strings.HasPrefix(r.URL.Path, "/anthropic") && (r.Header.Get("X-Api-Key") != "sk-ant-api03-abcdefghijklmnop-WXYZ" || r.Header.Get("Anthropic-Version") == "") {
				t.Errorf("anthropic headers = %v", r.Header)
			}
		}
		if l := owner.do("POST", "/api/ai/models", map[string]any{"provider": "mistral", "apiKey": "mistral-key-123456", "baseUrl": fake.URL + "/mistral/v1"}, 200); ids(l) != "mistral-large-latest" {
			t.Errorf("mistral models = %s", ids(l))
		}
		if l := owner.do("POST", "/api/ai/models", map[string]any{"provider": "openai_compatible", "baseUrl": fake.URL + "/ollama/v1"}, 200); ids(l) != "qwen3:8b,llama3.3:70b" {
			t.Errorf("ollama models = %s", ids(l))
		}
		if l := owner.do("POST", "/api/ai/models", map[string]any{"provider": "openai_compatible", "baseUrl": fake.URL + "/openrouter/v1"}, 200); ids(l) != "acme/tooler" {
			t.Errorf("openrouter models = %s", ids(l))
		}
		l = owner.do("POST", "/api/ai/models", map[string]any{"provider": "gemini", "apiKey": "AIzaSyabcdefghijklmnopGEMI", "baseUrl": fake.URL + "/gemini/v1beta"}, 200)
		if ids(l) != "gemini-pro-x,gemini-flash-y" || l["models"].([]any)[0].(map[string]any)["name"] != "Gemini Pro X" {
			t.Errorf("gemini models = %v", l)
		}
		l = owner.do("POST", "/api/ai/models", map[string]any{"provider": "azure", "apiKey": "azure-key-0123456789", "baseUrl": fake.URL + "/azure"}, 200)
		if ids(l) != "sales-gpt" || l["models"].([]any)[0].(map[string]any)["note"] != "gpt-5" {
			t.Errorf("azure deployments = %v", l)
		}
		// Where Azure does not list deployments, the builder says what to type.
		if e := owner.do("POST", "/api/ai/models", map[string]any{"provider": "azure", "apiKey": "azure-key-0123456789", "baseUrl": fake.URL + "/azure-new"}, 400); !strings.Contains(e["error"].(string), "type the deployment name") {
			t.Errorf("azure without listing = %v", e)
		}
		// A server that cannot be reached.
		if e := owner.do("POST", "/api/ai/models", map[string]any{"provider": "openai_compatible", "baseUrl": "http://127.0.0.1:1/v1"}, 400); !strings.Contains(e["error"].(string), "cannot reach the AI server") {
			t.Errorf("unreachable = %v", e)
		}

		// A key given to the server wins over one typed.
		srv.AI.Server[ai.OpenAI] = ai.Config{APIKey: "sk-typed-not-saved-1234"}
		owner.do("POST", "/api/ai/models", map[string]any{"provider": "openai", "apiKey": "sk-wrong-key-123456", "baseUrl": fake.URL + "/openai/v1"}, 200)

		// Builders list models with the saved settings, for flow AI steps;
		// only admins try settings that are not saved.
		owner.do("POST", "/api/users", map[string]any{"email": "bea@example.com", "password": "a long secret", "role": "builder"}, 201)
		bea := newClient(t, hs.URL)
		bea.do("POST", "/api/auth/login", map[string]any{"email": "bea@example.com", "password": "a long secret"}, 200)
		bea.do("POST", "/api/ai/models", map[string]any{"provider": "anthropic"}, 200)
		bea.do("POST", "/api/ai/models", map[string]any{"provider": "openai", "baseUrl": fake.URL + "/openai/v1"}, 403)
		bob, _ := member(t, owner, hs.URL, "", "bob@example.com", "")
		bob.do("POST", "/api/ai/models", map[string]any{"provider": "anthropic"}, 403)
	})
}
