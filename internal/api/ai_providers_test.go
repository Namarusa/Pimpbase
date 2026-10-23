package api

import (
	"encoding/json"
	"io"
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

// fakeLLM stands in for a provider's HTTP API. It records each request and
// answers from a script.
type fakeLLM struct {
	mu     sync.Mutex
	seen   []seenRequest
	script func(n int, req map[string]any) (int, any)
}

type seenRequest struct {
	path, query string
	header      http.Header
	body        map[string]any
}

func (f *fakeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	json.Unmarshal(raw, &body)
	f.mu.Lock()
	n := len(f.seen)
	f.seen = append(f.seen, seenRequest{r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body})
	script := f.script
	f.mu.Unlock()
	status, out := script(n, body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(out)
}

func (f *fakeLLM) reset(script func(n int, req map[string]any) (int, any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen, f.script = nil, script
}

// A wire speaks one provider protocol for the scripts.
type wire struct {
	call       func(name string, args any) any
	say        func(text string) any
	lastResult func(req map[string]any) (string, bool)
	system     func(req map[string]any) string
}

var openaiWire = wire{
	call: func(name string, args any) any {
		a, _ := json.Marshal(args)
		return map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "content": nil,
			"tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": name, "arguments": string(a)}}}}}}}
	},
	say: func(text string) any {
		return map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": text}}}}
	},
	lastResult: func(req map[string]any) (string, bool) {
		msgs := req["messages"].([]any)
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i].(map[string]any)
			if m["role"] == "tool" {
				c := m["content"].(string)
				return strings.TrimPrefix(c, "Error: "), strings.HasPrefix(c, "Error: ")
			}
		}
		return "", false
	},
	system: func(req map[string]any) string {
		return req["messages"].([]any)[0].(map[string]any)["content"].(string)
	},
}

var geminiWire = wire{
	call: func(name string, args any) any {
		return map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"role": "model", "parts": []any{
			map[string]any{"text": "thinking about it", "thought": true},
			map[string]any{"functionCall": map[string]any{"name": name, "args": args}, "thoughtSignature": "sig-" + name},
		}}}}}
	},
	say: func(text string) any {
		return map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}}}}}
	},
	lastResult: func(req map[string]any) (string, bool) {
		cs := req["contents"].([]any)
		last := cs[len(cs)-1].(map[string]any)
		for _, p := range last["parts"].([]any) {
			if fr, ok := p.(map[string]any)["functionResponse"].(map[string]any); ok {
				resp := fr["response"].(map[string]any)
				if e, ok := resp["error"].(string); ok {
					return e, true
				}
				return resp["result"].(string), false
			}
		}
		return "", false
	},
	system: func(req map[string]any) string {
		return req["systemInstruction"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string)
	},
}

func TestAIProviders(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		srv.AI.RetryDelay = time.Millisecond
		fake := &fakeLLM{}
		llm := httptest.NewServer(fake)
		defer llm.Close()

		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Sales"}, 201)
		base := "/api/apps/sales"
		deals := owner.do("POST", base+"/tables", map[string]any{"name": "Deals", "fields": []any{
			map[string]any{"name": "Title", "type": "text"},
			map[string]any{"name": "Owner", "type": "text"},
			map[string]any{"name": "Amount", "type": "number"},
		}}, 201)
		dt := base + "/tables/" + deals["id"].(string)
		for _, r := range []map[string]any{
			{"Title": "Acme", "Owner": "ada@example.com", "Amount": 100},
			{"Title": "Globex", "Owner": "cy@example.com", "Amount": 200},
		} {
			owner.do("POST", dt+"/rows", r, 201)
		}
		owner.do("PATCH", dt, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "BASIC", "delete": "BASIC",
			"rows": []any{map[string]any{"role": "BASIC", "filter": "[Owner] = Current.User.Email"}}}}, 200)

		// Every provider is listed; Anthropic is used until another is picked.
		st := owner.do("GET", "/api/ai", nil, 200)
		if st["provider"] != "anthropic" || len(st["providers"].([]any)) != 11 {
			t.Fatalf("status = %v", st)
		}
		// Settings are checked per provider.
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "openai", "apiKey": "not-a-key"}, 400)
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "openai_compatible", "baseUrl": "localhost:11434"}, 400)
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "openai", "model": "gpt 5"}, 400)
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "nope", "use": true}, 400)
		// An unfinished provider can be picked but is not configured.
		st = owner.do("PATCH", "/api/ai", map[string]any{"provider": "azure", "use": true}, 200)
		if st["provider"] != "azure" || st["configured"] != false {
			t.Errorf("azure without settings = %v", st)
		}
		if e := owner.do("POST", base+"/ai/formula", map[string]any{"table": deals["id"], "prompt": "x"}, 409); e["code"] != "ai_not_configured" {
			t.Errorf("unconfigured = %v", e)
		}
		if e := owner.do("POST", "/api/ai/test", map[string]any{"provider": "azure"}, 400); !strings.Contains(e["error"].(string), "Azure OpenAI still needs an API key") {
			t.Errorf("test unconfigured = %v", e)
		}

		// Builders may use it; only admins change or test it.
		owner.do("POST", "/api/users", map[string]any{"email": "bea@example.com", "password": "a long secret", "role": "builder"}, 201)
		bea := newClient(t, hs.URL)
		bea.do("POST", "/api/auth/login", map[string]any{"email": "bea@example.com", "password": "a long secret"}, 200)
		bea.do("PATCH", "/api/ai", map[string]any{"provider": "openai", "use": true}, 403)
		bea.do("POST", "/api/ai/test", map[string]any{}, 403)
		bob, _ := member(t, owner, hs.URL, "sales", "bob@example.com", "BASIC")
		bob.do("GET", "/api/ai", nil, 403)

		type setup struct {
			provider string
			settings map[string]any
			wire     wire
			// check looks at the first request of a conversation.
			check func(t *testing.T, r seenRequest)
		}
		cases := []setup{
			{"openai", map[string]any{"apiKey": "sk-proj-abcdefghijklmnop-OPEN", "baseUrl": llm.URL + "/v1", "model": "gpt-5.5"}, openaiWire, func(t *testing.T, r seenRequest) {
				if r.path != "/v1/chat/completions" || r.header.Get("Authorization") != "Bearer sk-proj-abcdefghijklmnop-OPEN" || r.body["model"] != "gpt-5.5" || r.body["max_completion_tokens"] == nil {
					t.Errorf("openai request = %s %v %v", r.path, r.header, r.body)
				}
				fn := r.body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
				if fn["name"] != "propose_formula" && fn["name"] != "query_rows" || fn["parameters"].(map[string]any)["type"] != "object" {
					t.Errorf("openai tool = %v", fn)
				}
			}},
			{"azure", map[string]any{"apiKey": "0123456789abcdef0123456789abcdef", "baseUrl": llm.URL, "model": "my-gpt"}, openaiWire, func(t *testing.T, r seenRequest) {
				if r.path != "/openai/deployments/my-gpt/chat/completions" || r.query != "api-version="+ai.DefaultAzureAPIVersion || r.header.Get("Api-Key") != "0123456789abcdef0123456789abcdef" || r.header.Get("Authorization") != "" {
					t.Errorf("azure request = %s?%s %v", r.path, r.query, r.header)
				}
			}},
			{"azure", map[string]any{"apiVersion": "v1"}, openaiWire, func(t *testing.T, r seenRequest) {
				if r.path != "/openai/v1/chat/completions" || r.body["model"] != "my-gpt" {
					t.Errorf("azure v1 request = %s %v", r.path, r.body["model"])
				}
			}},
			{"mistral", map[string]any{"apiKey": "mistral-abcdefghijkl", "baseUrl": llm.URL + "/v1", "model": "mistral-large-latest"}, openaiWire, func(t *testing.T, r seenRequest) {
				if r.path != "/v1/chat/completions" || r.body["model"] != "mistral-large-latest" || r.body["max_tokens"] == nil || r.header.Get("Authorization") != "Bearer mistral-abcdefghijkl" {
					t.Errorf("mistral request = %s %v", r.path, r.body)
				}
			}},
			{"openai_compatible", map[string]any{"baseUrl": llm.URL + "/v1/", "model": "qwen3:8b"}, openaiWire, func(t *testing.T, r seenRequest) {
				if r.path != "/v1/chat/completions" || r.body["model"] != "qwen3:8b" || r.header.Get("Authorization") != "" {
					t.Errorf("compatible request = %s %v %v", r.path, r.header, r.body)
				}
			}},
			{"gemini", map[string]any{"apiKey": "AIzaSyabcdefghijklmnopGEMI", "baseUrl": llm.URL + "/v1beta", "model": "gemini-3.1-pro-preview"}, geminiWire, func(t *testing.T, r seenRequest) {
				if r.path != "/v1beta/models/gemini-3.1-pro-preview:generateContent" || r.header.Get("X-Goog-Api-Key") != "AIzaSyabcdefghijklmnopGEMI" {
					t.Errorf("gemini request = %s %v", r.path, r.header)
				}
				decl := r.body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
				if decl["parametersJsonSchema"].(map[string]any)["type"] != "object" {
					t.Errorf("gemini tool = %v", decl)
				}
			}},
		}
		for _, c := range cases {
			name := c.provider
			if v, ok := c.settings["apiVersion"]; ok {
				name += " " + v.(string)
			}
			t.Run(name, func(t *testing.T) {
				p := map[string]any{"provider": c.provider, "use": true}
				for k, v := range c.settings {
					p[k] = v
				}
				st := owner.do("PATCH", "/api/ai", p, 200)
				raw, _ := json.Marshal(st)
				if st["provider"] != c.provider || st["configured"] != true {
					t.Fatalf("status = %s", raw)
				}
				if k, ok := c.settings["apiKey"].(string); ok && strings.Contains(string(raw), k[4:len(k)-4]) {
					t.Errorf("the key leaks: %s", raw)
				}

				// Formula help: the compile error goes back, the second try is used.
				fake.reset(func(n int, req map[string]any) (int, any) {
					if n == 0 {
						return 200, c.wire.call("propose_formula", map[string]any{"formula": "[Amout] > 100", "explanation": "x"})
					}
					return 200, c.wire.call("propose_formula", map[string]any{"formula": "[Amount] > 100", "explanation": "Big deals."})
				})
				f := owner.do("POST", base+"/ai/formula", map[string]any{"table": deals["id"], "prompt": "big deals", "purpose": "filter"}, 200)
				if f["type"] != "boolean" || f["formula"] != "[Amount] > 100" || len(fake.seen) != 2 {
					t.Fatalf("formula = %v after %d calls", f, len(fake.seen))
				}
				c.check(t, fake.seen[0])
				if msg, isErr := c.wire.lastResult(fake.seen[1].body); !isErr || !strings.Contains(msg, "Amout") {
					t.Errorf("error sent back = %q %v", msg, isErr)
				}
				if !strings.Contains(c.wire.system(fake.seen[0].body), "Amount") {
					t.Errorf("the table is not described")
				}

				// Questions keep to the access of the role asked about.
				var rowsSeen string
				fake.reset(func(n int, req map[string]any) (int, any) {
					if n == 0 {
						return 200, c.wire.call("query_rows", map[string]any{"table": "Deals", "fields": []any{"Title"}})
					}
					rowsSeen, _ = c.wire.lastResult(req)
					return 200, c.wire.say("You have Acme.")
				})
				a := owner.do("POST", base+"/ai/ask", map[string]any{"question": "my deals?", "as": "BASIC"}, 200)
				if a["answer"] != "You have Acme." || !strings.Contains(rowsSeen, "Acme") || strings.Contains(rowsSeen, "Globex") {
					t.Errorf("answer = %v, rows seen = %s", a, rowsSeen)
				}
				if !strings.Contains(c.wire.system(fake.seen[0].body), "BASIC") {
					t.Errorf("who is not named")
				}

				// The connection test.
				fake.reset(func(n int, req map[string]any) (int, any) {
					return 200, c.wire.call("report", map[string]any{"status": "OK"})
				})
				if ck := owner.do("POST", "/api/ai/test", map[string]any{}, 200); ck["provider"] != c.provider || !strings.Contains(ck["reply"].(string), "OK") {
					t.Errorf("test = %v", ck)
				}
			})
		}

		// Gemini gets its turns back as they were, thought signatures included.
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "gemini", "use": true}, 200)
		fake.reset(func(n int, req map[string]any) (int, any) {
			if n == 0 {
				return 200, geminiWire.call("query_rows", map[string]any{"table": "Deals"})
			}
			return 200, geminiWire.say("done")
		})
		owner.do("POST", base+"/ai/ask", map[string]any{"question": "deals?"}, 200)
		turn := fake.seen[1].body["contents"].([]any)[1].(map[string]any)
		if js, _ := json.Marshal(turn); turn["role"] != "model" || !strings.Contains(string(js), "sig-query_rows") {
			t.Errorf("model turn sent back = %s", js)
		}

		// A model that answers in words instead of calling the tool is
		// reminded once.
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "openai_compatible", "use": true}, 200)
		fake.reset(func(n int, req map[string]any) (int, any) {
			if n == 0 {
				return 200, openaiWire.say("<think>hmm</think>The formula is [Amount] > 100")
			}
			msgs := req["messages"].([]any)
			if last := msgs[len(msgs)-1].(map[string]any); last["role"] != "user" || !strings.Contains(last["content"].(string), "propose_formula") {
				t.Errorf("no reminder: %v", last)
			}
			if prev := msgs[len(msgs)-2].(map[string]any); prev["content"] != "The formula is [Amount] > 100" {
				t.Errorf("think tags kept: %v", prev)
			}
			return 200, openaiWire.call("propose_formula", map[string]any{"formula": "[Amount] > 100", "explanation": "Big."})
		})
		if f := owner.do("POST", base+"/ai/formula", map[string]any{"table": deals["id"], "prompt": "big deals", "purpose": "filter"}, 200); f["formula"] != "[Amount] > 100" {
			t.Errorf("after reminder = %v", f)
		}

		// Arguments that are not JSON go back as an error.
		fake.reset(func(n int, req map[string]any) (int, any) {
			if n == 0 {
				return 200, map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant",
					"tool_calls": []any{map[string]any{"function": map[string]any{"name": "propose_formula", "arguments": "{formula: oops"}}}}}}}
			}
			if msg, isErr := openaiWire.lastResult(req); !isErr || !strings.Contains(msg, "not valid JSON") {
				t.Errorf("bad arguments result = %q", msg)
			}
			return 200, openaiWire.call("propose_formula", map[string]any{"formula": "[Amount] > 100", "explanation": "Big."})
		})
		owner.do("POST", base+"/ai/formula", map[string]any{"table": deals["id"], "prompt": "big deals", "purpose": "filter"}, 200)

		// Provider errors become messages to act on; rate limits are retried.
		fake.reset(func(n int, req map[string]any) (int, any) {
			return 401, map[string]any{"error": map[string]any{"message": "Incorrect API key provided"}}
		})
		if e := owner.do("POST", base+"/ai/ask", map[string]any{"question": "?"}, 400); !strings.Contains(e["error"].(string), "refused the key") || len(fake.seen) != 1 {
			t.Errorf("401 = %v after %d", e, len(fake.seen))
		}
		fake.reset(func(n int, req map[string]any) (int, any) {
			if n < 2 {
				return 429, map[string]any{"error": "slow down"}
			}
			return 200, openaiWire.say("fine")
		})
		if a := owner.do("POST", base+"/ai/ask", map[string]any{"question": "?"}, 200); a["answer"] != "fine" || len(fake.seen) != 3 {
			t.Errorf("after 429s = %v, %d calls", a, len(fake.seen))
		}
		fake.reset(func(n int, req map[string]any) (int, any) {
			return 400, map[string]any{"message": "model 'qwen3:8b' not found"}
		})
		if e := owner.do("POST", base+"/ai/ask", map[string]any{"question": "?"}, 400); !strings.Contains(e["error"].(string), "not found") {
			t.Errorf("400 = %v", e)
		}

		// Settings given to the server win and are locked.
		srv.AI.Server[ai.Mistral] = ai.Config{APIKey: "server-mistral-key-1234"}
		srv.AI.ServerProvider = ai.Mistral
		st = owner.do("GET", "/api/ai", nil, 200)
		var ms map[string]any
		for _, p := range st["providers"].([]any) {
			if p.(map[string]any)["id"] == "mistral" {
				ms = p.(map[string]any)
			}
		}
		if st["provider"] != "mistral" || st["providerSource"] != "server" || ms["keySource"] != "server" || ms["keyHint"] != "…1234" {
			t.Errorf("server settings = %v", st)
		}
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "mistral", "apiKey": "another-mistral-key"}, 400)
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "openai", "use": true}, 400)
		// Other settings of that provider can still be saved.
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "mistral", "model": "mistral-small-latest"}, 200)
	})
}
