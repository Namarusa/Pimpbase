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

// TestFlowAIStep runs AI steps against stand-ins for each protocol and
// checks that each provider gets its own settings.
func TestFlowAIStep(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		srv.AI.RetryDelay = time.Millisecond
		var mu sync.Mutex
		seen := map[string]map[string]any{}
		answer := `{"sentiment": "positive", "score": 0.9, "tags": ["fast", "kind"]}`
		llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &body)
			mu.Lock()
			seen[r.URL.Path] = body
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.HasSuffix(r.URL.Path, "/v1/messages"):
				json.NewEncoder(w).Encode(map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "model": body["model"], "stop_reason": "end_turn",
					"content": []any{map[string]any{"type": "text", "text": answer}}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 7}})
			case strings.HasSuffix(r.URL.Path, ":generateContent"):
				json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"role": "model",
					"parts": []any{map[string]any{"text": "thinking", "thought": true}, map[string]any{"text": "```json\n" + answer + "\n```"}}}}},
					"usageMetadata": map[string]any{"promptTokenCount": 5, "candidatesTokenCount": 6, "thoughtsTokenCount": 4}})
			default:
				text := answer
				if strings.Contains(r.URL.Path, "/plain/") {
					text = "Hello Ada!"
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": text}}},
					"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 4}})
			}
		}))
		defer llm.Close()
		srv.AI.Server[ai.Anthropic] = ai.Config{APIKey: "sk-ant-test-key-123456789", BaseURL: llm.URL}
		srv.AI.Server[ai.OpenAI] = ai.Config{APIKey: "sk-test-openai-1234", BaseURL: llm.URL + "/openai/v1"}
		srv.AI.Server[ai.Gemini] = ai.Config{APIKey: "AIza-test-gemini-1234", BaseURL: llm.URL + "/gemini"}
		srv.AI.Server[ai.DeepSeek] = ai.Config{APIKey: "sk-test-deepseek-1234", BaseURL: llm.URL + "/deepseek/v1"}
		srv.AI.Server[ai.XAI] = ai.Config{APIKey: "xai-test-key-123456", BaseURL: llm.URL + "/plain/xai"}
		srv.AI.Server[ai.Azure] = ai.Config{APIKey: "azure-test-key-1234", BaseURL: llm.URL + "/azure"}

		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Desk"}, 201)
		base := "/api/apps/desk"

		st := owner.do("GET", "/api/ai", nil, 200)
		if len(st["providers"].([]any)) != 11 {
			t.Fatalf("providers = %d", len(st["providers"].([]any)))
		}
		for _, p := range st["providers"].([]any) {
			m := p.(map[string]any)
			if m["id"] == "anthropic" && m["step"].(map[string]any)["temperature"] != false {
				t.Errorf("anthropic step options = %v", m["step"])
			}
		}

		fields := []any{
			map[string]any{"name": "sentiment", "type": "text", "description": "positive, neutral or negative"},
			map[string]any{"name": "score", "type": "number"},
			map[string]any{"name": "tags", "type": "list"},
		}
		prompt := "Rate this review: {{ trigger.params.review }}"
		f := owner.do("POST", base+"/flows", map[string]any{
			"name":    "Triage",
			"trigger": map[string]any{"type": "manual", "parameters": []any{map[string]any{"name": "review"}}},
			"steps": []any{
				map[string]any{"name": "Claude", "type": "ai", "inputs": map[string]any{"provider": "anthropic", "model": "claude-haiku-5-5", "system": "You triage reviews.",
					"prompt": prompt, "output": "fields", "fields": fields, "effort": "low", "maxTokens": "500"}},
				map[string]any{"name": "Gpt", "type": "ai", "inputs": map[string]any{"provider": "openai", "model": "gpt-5-mini", "prompt": prompt, "output": "fields", "fields": fields,
					"effort": "minimal", "seed": "7", "stop": "END\nSTOP"}},
				map[string]any{"name": "Gem", "type": "ai", "inputs": map[string]any{"provider": "gemini", "model": "gemini-2.5-flash", "prompt": prompt, "output": "json",
					"temperature": "0.2", "topP": "0.9", "thinking": "off"}},
				map[string]any{"name": "Deep", "type": "ai", "inputs": map[string]any{"provider": "deepseek", "model": "deepseek-chat", "prompt": prompt, "output": "fields", "fields": fields}},
				map[string]any{"name": "Grok", "type": "ai", "inputs": map[string]any{"provider": "xai", "model": "grok-4", "prompt": "Greet {{ steps.Claude.data.sentiment }} people"}},
				map[string]any{"name": "Az", "type": "ai", "inputs": map[string]any{"provider": "azure", "model": "my-gpt", "apiVersion": "2025-04-01-preview", "prompt": prompt, "output": "json"}},
			},
		}, 201)
		fb := base + "/flows/" + f["id"].(string)
		run := owner.do("POST", fb+"/test", map[string]any{"trigger": map[string]any{"params": map[string]any{"review": "Quick and kind"}}}, 200)
		if run["status"] != "succeeded" {
			t.Fatalf("run = %v", run)
		}
		claude := stepByName(run, "Claude")["output"].(map[string]any)
		if claude["data"].(map[string]any)["score"] != 0.9 || claude["model"] != "claude-haiku-5-5" || claude["usage"].(map[string]any)["output"] != float64(7) {
			t.Errorf("claude output = %v", claude)
		}
		if gem := stepByName(run, "Gem")["output"].(map[string]any); gem["data"].(map[string]any)["sentiment"] != "positive" || gem["usage"].(map[string]any)["output"] != float64(10) {
			t.Errorf("gemini output = %v", gem)
		}
		if grok := stepByName(run, "Grok")["output"].(map[string]any); grok["text"] != "Hello Ada!" || grok["data"] != nil {
			t.Errorf("grok output = %v", grok)
		}

		mu.Lock()
		defer mu.Unlock()
		a := seen["/v1/messages"]
		oc, _ := a["output_config"].(map[string]any)
		if a["model"] != "claude-haiku-5-5" || a["max_tokens"] != float64(500) || oc["effort"] != "low" || oc["format"].(map[string]any)["type"] != "json_schema" {
			t.Errorf("anthropic request = %v", a)
		}
		if _, ok := a["temperature"]; ok {
			t.Errorf("anthropic got a temperature")
		}
		if !strings.Contains(jsonOf(a["messages"]), "Quick and kind") {
			t.Errorf("anthropic prompt = %v", a["messages"])
		}
		o := seen["/openai/v1/chat/completions"]
		rf := o["response_format"].(map[string]any)
		if o["reasoning_effort"] != "minimal" || o["seed"] != float64(7) || jsonOf(o["stop"]) != `["END","STOP"]` || rf["type"] != "json_schema" ||
			rf["json_schema"].(map[string]any)["strict"] != true {
			t.Errorf("openai request = %v", o)
		}
		gc := seen["/gemini/models/gemini-2.5-flash:generateContent"]["generationConfig"].(map[string]any)
		if gc["temperature"] != 0.2 || gc["topP"] != 0.9 || gc["responseMimeType"] != "application/json" || gc["thinkingConfig"].(map[string]any)["thinkingBudget"] != float64(0) {
			t.Errorf("gemini request = %v", gc)
		}
		d := seen["/deepseek/v1/chat/completions"]
		if d["response_format"].(map[string]any)["type"] != "json_object" || !strings.Contains(jsonOf(d["messages"]), "JSON schema") {
			t.Errorf("deepseek request = %v", d)
		}
		if x := seen["/plain/xai/chat/completions"]; !strings.Contains(jsonOf(x["messages"]), "Greet positive people") || len(x["messages"].([]any)) != 1 {
			t.Errorf("xai request = %v", x)
		}
		if _, ok := seen["/azure/openai/deployments/my-gpt/chat/completions"]; !ok {
			t.Errorf("azure was not called with the deployment: %v", keys(seen))
		}
	})
}

// TestFlowAIStepErrors checks settings a provider does not take and
// answers that are not what was asked for.
func TestFlowAIStepErrors(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		srv.AI.RetryDelay = time.Millisecond
		llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": `{"sentiment": "ok"`}}}})
		}))
		defer llm.Close()
		srv.AI.Server[ai.Anthropic] = ai.Config{APIKey: "sk-ant-test-key-123456789", BaseURL: llm.URL}
		srv.AI.Server[ai.Groq] = ai.Config{APIKey: "gsk_test-key-1234", BaseURL: llm.URL}

		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Desk"}, 201)
		base := "/api/apps/desk"
		try := func(inputs map[string]any) map[string]any {
			f := owner.do("POST", base+"/flows", map[string]any{"name": "T", "steps": []any{map[string]any{"name": "Ask", "type": "ai", "inputs": inputs}}}, 201)
			defer owner.do("DELETE", base+"/flows/"+f["id"].(string), nil, 200)
			run := owner.do("POST", base+"/flows/"+f["id"].(string)+"/test", nil, 200)
			return stepByName(run, "Ask")
		}
		for _, c := range []struct {
			inputs map[string]any
			want   string
		}{
			{map[string]any{"provider": "anthropic"}, "the prompt is empty"},
			{map[string]any{"provider": "anthropic", "prompt": "hi", "temperature": "0.5"}, "does not take a temperature"},
			{map[string]any{"provider": "anthropic", "prompt": "hi", "model": "gpt-5"}, "not a Claude model"},
			{map[string]any{"provider": "groq", "prompt": "hi"}, "choose the model"},
			{map[string]any{"provider": "groq", "model": "llama", "prompt": "hi", "temperature": "hot"}, "not a number"},
			{map[string]any{"provider": "groq", "model": "llama", "prompt": "hi", "effort": "high"}, "does not take an effort"},
			{map[string]any{"provider": "groq", "model": "llama", "prompt": "hi", "output": "json"}, "not valid JSON"},
			{map[string]any{"provider": "openai", "model": "gpt-5", "prompt": "hi"}, "not set up"},
		} {
			step := try(c.inputs)
			if step["status"] != "failed" || !strings.Contains(step["error"].(string), c.want) {
				t.Errorf("%v: step = %v, want %q", c.inputs, step, c.want)
			}
		}
	})
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func keys(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestFlowSystemOne asks typed questions of a System One model, at
// TypeSafe and through OpenRouter.
func TestFlowSystemOne(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		srv.AI.RetryDelay = time.Millisecond
		var mu sync.Mutex
		seen := map[string]map[string]any{}
		auth := map[string]string{}
		llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &body)
			mu.Lock()
			seen[r.URL.Path], auth[r.URL.Path] = body, r.Header.Get("Authorization")
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/ts/v1/models":
				json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "jev-latest", "description": "General-purpose system one model.", "release_date": "2026-09-15"}}})
			case "/or/api/v1/models":
				json.NewEncoder(w).Encode(map[string]any{"data": []any{
					map[string]any{"id": "typesafe/jev-1.14", "supported_parameters": []string{}},
					map[string]any{"id": "anthropic/claude-sonnet-5-5", "supported_parameters": []string{"tools"}},
					map[string]any{"id": "some/no-tools", "supported_parameters": []string{"temperature"}},
				}})
			case "/ts/v1/systemone", "/or/api/v1/systemone":
				json.NewEncoder(w).Encode(map[string]any{"model": "jev-1.14-20261001", "answers": map[string]any{
					"team":   map[string]any{"type": "choice", "choice": "payments", "confidence": 0.75, "probabilities": map[string]any{"account": 0.0, "payments": 0.84, "frontend": 0.16}},
					"urgent": map[string]any{"type": "score", "score": 1.99, "confidence": 0.99, "legend": map[string]any{"0": "Later", "1": "This week", "2": "Now"}, "probabilities": map[string]any{"0": 0, "1": 0.01, "2": 0.99}},
					"is_bug": map[string]any{"type": "noul", "noul": 0.96}, "ok": map[string]any{"type": "noul", "noul": 0.99},
				}, "usage": map[string]any{"input_tokens": 476, "output_tokens": 70, "cost": 0.00002}})
			default:
				w.WriteHeader(404)
			}
		}))
		defer llm.Close()
		srv.AI.Server[ai.TypeSafe] = ai.Config{APIKey: "ts-test-key-123456", BaseURL: llm.URL + "/ts/v1"}
		srv.AI.Server[ai.OpenRouter] = ai.Config{APIKey: "sk-or-v1-test-123456", BaseURL: llm.URL + "/or/api/v1"}

		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Desk"}, 201)
		base := "/api/apps/desk"

		// TypeSafe serves flows, not the assistant.
		owner.do("PATCH", "/api/ai", map[string]any{"provider": "typesafe", "use": true}, 400)
		if st := owner.do("GET", "/api/ai", nil, 200); st["provider"] == "typesafe" {
			t.Errorf("typesafe became the assistant's provider")
		}
		if c := owner.do("POST", "/api/ai/test", map[string]any{"provider": "typesafe"}, 200); !strings.Contains(c["reply"].(string), "yes/no") {
			t.Errorf("test = %v", c)
		}
		if m := owner.do("POST", "/api/ai/models", map[string]any{"provider": "typesafe"}, 200); jsonOf(m["models"]) != `[{"id":"jev-latest","note":"2026-09-15"}]` {
			t.Errorf("typesafe models = %v", m)
		}
		// OpenRouter lists Jev though it takes no tools, with its alias.
		m := jsonOf(owner.do("POST", "/api/ai/models", map[string]any{"provider": "openrouter"}, 200)["models"])
		if !strings.Contains(m, `"~typesafe/jev-latest"`) || !strings.Contains(m, `"typesafe/jev-1.14","note":"System One"`) || strings.Contains(m, "no-tools") {
			t.Errorf("openrouter models = %s", m)
		}

		qs := []any{
			map[string]any{"name": "team", "type": "choice", "instructions": "Which team should own this ticket?", "options": []any{
				map[string]any{"name": "account", "description": "Login issues"}, map[string]any{"name": "payments", "description": "Billing"}, map[string]any{"name": "frontend"}}},
			map[string]any{"name": "urgent", "type": "score", "instructions": "How urgent is it for {{ trigger.params.tier }}?", "levels": []any{"Later", "This week", "Now"}},
			map[string]any{"name": "is_bug", "type": "noul", "instructions": "Is it a defect?"},
		}
		f := owner.do("POST", base+"/flows", map[string]any{
			"name":    "Route",
			"trigger": map[string]any{"type": "manual", "parameters": []any{map[string]any{"name": "ticket", "type": "json"}, map[string]any{"name": "tier"}}},
			"steps": []any{
				map[string]any{"name": "Jev", "type": "ai", "inputs": map[string]any{"provider": "typesafe", "model": "jev-latest", "state": "{{ trigger.params.ticket }}", "questions": qs, "prompt": "ignored"}},
				map[string]any{"name": "Or", "type": "ai", "inputs": map[string]any{"provider": "openrouter", "model": "typesafe/jev-1.14", "state": "Ticket: {{ trigger.params.ticket.text }}", "questions": qs}},
				map[string]any{"name": "Gate", "type": "condition", "inputs": map[string]any{"left": "{{ steps.Jev.data.team.confidence }}", "operator": "gte", "right": "0.7"},
					"then": []any{map[string]any{"name": "Pick", "type": "set_variable", "inputs": map[string]any{"name": "team", "value": "{{ steps.Jev.data.team.choice }}"}}}},
			},
		}, 201)
		run := owner.do("POST", base+"/flows/"+f["id"].(string)+"/test", map[string]any{"trigger": map[string]any{"params": map[string]any{
			"ticket": map[string]any{"text": "Checkout shows a blank page", "customer": "Acme"}, "tier": "enterprise"}}}, 200)
		if run["status"] != "succeeded" {
			t.Fatalf("run = %v", run)
		}
		jev := stepByName(run, "Jev")["output"].(map[string]any)
		data := jev["data"].(map[string]any)
		if data["team"].(map[string]any)["choice"] != "payments" || data["is_bug"].(map[string]any)["noul"] != 0.96 || jev["model"] != "jev-1.14-20261001" || jev["usage"].(map[string]any)["cost"] != 0.00002 {
			t.Errorf("jev output = %v", jev)
		}
		if p := stepByName(run, "Pick")["output"].(map[string]any); p["value"] != "payments" {
			t.Errorf("gate = %v", p)
		}

		mu.Lock()
		defer mu.Unlock()
		ts := seen["/ts/v1/systemone"]
		if auth["/ts/v1/systemone"] != "Bearer ts-test-key-123456" || jsonOf(ts["state"]) != `{"customer":"Acme","text":"Checkout shows a blank page"}` {
			t.Errorf("typesafe request = %v", ts)
		}
		q := ts["questions"].(map[string]any)
		if jsonOf(q["team"].(map[string]any)["criteria"]) != `{"account":"Login issues","frontend":null,"payments":"Billing"}` ||
			q["urgent"].(map[string]any)["instructions"] != "How urgent is it for enterprise?" || q["is_bug"].(map[string]any)["criteria"] != nil {
			t.Errorf("typesafe questions = %v", jsonOf(q))
		}
		or := seen["/or/api/v1/systemone"]
		if or["model"] != "typesafe/jev-1.14" || or["state"] != "Ticket: Checkout shows a blank page" ||
			jsonOf(or["questions"].(map[string]any)["is_bug"].(map[string]any)["criteria"]) != `{"false":"No","true":"Yes"}` {
			t.Errorf("openrouter request = %v", or)
		}
	})
}
