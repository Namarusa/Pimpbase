package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bubibase/bubibase/internal/ai"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// fakeClaude stands in for the Anthropic API: each reply comes from the
// script, which sees the request and how many came before.
type fakeClaude struct {
	mu       sync.Mutex
	requests []map[string]any
	keys     []string
	script   func(n int, req map[string]any) map[string]any
}

func (f *fakeClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &req)
	f.mu.Lock()
	n := len(f.requests)
	f.requests = append(f.requests, req)
	f.keys = append(f.keys, r.Header.Get("X-Api-Key"))
	script := f.script
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(script(n, req))
}

func (f *fakeClaude) reset(script func(n int, req map[string]any) map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests, f.keys, f.script = nil, nil, script
}

func toolUse(name string, input any) map[string]any {
	return map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "stop_reason": "tool_use",
		"content": []any{map[string]any{"type": "tool_use", "id": "toolu_1", "name": name, "input": input}},
		"usage":   map[string]any{"input_tokens": 10, "output_tokens": 10},
	}
}

func say(text string) map[string]any {
	return map[string]any{
		"id": "msg_2", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "stop_reason": "end_turn",
		"content": []any{map[string]any{"type": "text", "text": text}},
		"usage":   map[string]any{"input_tokens": 10, "output_tokens": 10},
	}
}

// lastToolResult is the text of the tool result the server sent back.
func lastToolResult(req map[string]any) (string, bool) {
	msgs := req["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	for _, b := range last["content"].([]any) {
		m := b.(map[string]any)
		if m["type"] == "tool_result" {
			isErr, _ := m["is_error"].(bool)
			var text string
			switch c := m["content"].(type) {
			case string:
				text = c
			case []any:
				for _, x := range c {
					text += x.(map[string]any)["text"].(string)
				}
			}
			return text, isErr
		}
	}
	return "", false
}

func systemText(req map[string]any) string {
	var out string
	for _, b := range req["system"].([]any) {
		out += b.(map[string]any)["text"].(string)
	}
	return out
}

func TestAIAssistant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		fake := &fakeClaude{}
		claude := httptest.NewServer(fake)
		defer claude.Close()
		srv.AI.Server[ai.Anthropic] = ai.Config{BaseURL: claude.URL}

		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Sales"}, 201)
		base := "/api/apps/sales"
		deals := owner.do("POST", base+"/tables", map[string]any{"name": "Deals", "fields": []any{
			map[string]any{"name": "Title", "type": "text"},
			map[string]any{"name": "Owner", "type": "text"},
			map[string]any{"name": "Region", "type": "single_select", "options": []any{"North", "South"}},
			map[string]any{"name": "Amount", "type": "number"},
		}}, 201)
		dt := base + "/tables/" + deals["id"].(string)
		for _, r := range []map[string]any{
			{"Title": "Acme", "Owner": "ada@example.com", "Region": "North", "Amount": 100},
			{"Title": "Globex", "Owner": "cy@example.com", "Region": "South", "Amount": 200},
			{"Title": "Initech", "Owner": "ada@example.com", "Region": "South", "Amount": 300},
		} {
			owner.do("POST", dt+"/rows", r, 201)
		}
		owner.do("PATCH", dt, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "BASIC", "delete": "BASIC",
			"rows": []any{map[string]any{"role": "BASIC", "filter": "[Owner] = Current.User.Email"}}}}, 200)
		secret := owner.do("POST", base+"/tables", map[string]any{"name": "Salaries", "fields": []any{map[string]any{"name": "Person", "type": "text"}}}, 201)
		owner.do("PATCH", base+"/tables/"+secret["id"].(string), map[string]any{"permissions": map[string]any{"read": "ADMIN", "write": "ADMIN", "delete": "ADMIN"}}, 200)
		owner.do("PUT", base+"/variables/API_TOKEN", map[string]any{"secret": true, "development": "dev-s3cret", "production": "prod-s3cret"}, 200)

		// Without a key the assistant says how to set it up.
		if st := owner.do("GET", "/api/ai", nil, 200); st["configured"] != false || st["model"] != "claude-opus-5-5" {
			t.Errorf("status = %v", st)
		}
		if e := owner.do("POST", base+"/ai/draft", map[string]any{"kind": "table", "prompt": "x"}, 409); e["code"] != "ai_not_configured" {
			t.Errorf("no key = %v", e)
		}
		owner.do("PATCH", "/api/ai", map[string]any{"apiKey": "not-a-key"}, 400)
		owner.do("PATCH", "/api/ai", map[string]any{"model": "gpt-4"}, 400)
		key := "sk-ant-api03-abcdefghijklmnop-WXYZ"
		st := owner.do("PATCH", "/api/ai", map[string]any{"apiKey": key, "model": "claude-sonnet-5-5"}, 200)
		raw, _ := json.Marshal(st)
		if st["configured"] != true || st["source"] != "workspace" || strings.Contains(string(raw), "abcdefghijklmnop") || st["keyHint"] != "sk-ant-…WXYZ" {
			t.Errorf("status = %s", raw)
		}
		owner.do("PATCH", "/api/ai", map[string]any{"model": "claude-opus-5-5"}, 200)
		// Builders may use it, only admins set it up; others cannot reach it.
		bob, _ := member(t, owner, hs.URL, "sales", "bob@example.com", "BASIC")
		bob.do("GET", "/api/ai", nil, 403)
		bob.do("POST", base+"/ai/ask", map[string]any{"question": "?"}, 403)
		owner.do("POST", "/api/users", map[string]any{"email": "bea@example.com", "password": "a long secret", "role": "builder"}, 201)
		bea := newClient(t, hs.URL)
		bea.do("POST", "/api/auth/login", map[string]any{"email": "bea@example.com", "password": "a long secret"}, 200)
		bea.do("GET", "/api/ai", nil, 200)
		bea.do("PATCH", "/api/ai", map[string]any{"model": "claude-haiku-5-5"}, 403)

		// A table draft: the first try is wrong, the error goes back, the
		// second is checked and returned. Nothing is created.
		fake.reset(func(n int, req map[string]any) map[string]any {
			if n == 0 {
				return toolUse("propose_draft", map[string]any{"summary": "s", "changes": []any{}, "draft": map[string]any{"tables": []any{
					map[string]any{"name": "Invoices", "fields": []any{map[string]any{"name": "Total", "type": "money"}}},
				}}})
			}
			return toolUse("propose_draft", map[string]any{"summary": "Invoices for deals", "changes": []any{"New table Invoices", "New table Payments", "Deals gets Paid"}, "draft": map[string]any{"tables": []any{
				map[string]any{"name": "Invoices", "fields": []any{
					map[string]any{"name": "Number", "type": "text"},
					map[string]any{"name": "Deal", "type": "link", "linkTable": "Deals"},
					map[string]any{"name": "Net", "type": "number"},
					map[string]any{"name": "Gross", "type": "formula", "formula": "ROUND([Net] * 1.2, 2)"},
				}},
				map[string]any{"name": "Payments", "fields": []any{
					map[string]any{"name": "Ref", "type": "text"},
					map[string]any{"name": "Invoice", "type": "link", "linkTable": "Invoices"},
				}},
				map[string]any{"name": "Deals", "existing": true, "fields": []any{map[string]any{"name": "Paid", "type": "boolean"}}},
			}}})
		})
		d := owner.do("POST", base+"/ai/draft", map[string]any{"kind": "table", "prompt": "Invoices and payments for deals"}, 200)
		if len(fake.requests) != 2 {
			t.Fatalf("model calls = %d", len(fake.requests))
		}
		if msg, isErr := lastToolResult(fake.requests[1]); !isErr || !strings.Contains(msg, "money") {
			t.Errorf("error sent back = %q %v", msg, isErr)
		}
		if fake.keys[0] != key {
			t.Errorf("key sent = %q", fake.keys[0])
		}
		sys := systemText(fake.requests[0])
		if !strings.Contains(sys, `"Deals"`) || !strings.Contains(sys, "API_TOKEN") || strings.Contains(sys, "s3cret") {
			t.Errorf("system prompt leaks or lacks context: %s", sys)
		}
		tl := d["draft"].(map[string]any)["tables"].([]any)
		inv := tl[0].(map[string]any)["fields"].([]any)
		if inv[1].(map[string]any)["linkTable"] != deals["id"] || tl[1].(map[string]any)["fields"].([]any)[1].(map[string]any)["linkTable"] != "draft:invoices" || tl[2].(map[string]any)["id"] != deals["id"] {
			t.Errorf("draft = %v", d["draft"])
		}
		if n := len(owner.do("GET", base+"/tables", nil, 200)["list"].([]any)); n != 2 {
			t.Errorf("tables after a draft = %d", n)
		}

		// A model that keeps failing gets a clear error, not a draft.
		fake.reset(func(n int, req map[string]any) map[string]any {
			if n < 2 {
				return toolUse("propose_draft", map[string]any{"summary": "s", "changes": []any{}, "draft": map[string]any{"name": "Bad", "trigger": map[string]any{"type": "schedule", "cron": "nope"}}})
			}
			return say("I could not make a valid schedule.")
		})
		if e := owner.do("POST", base+"/ai/draft", map[string]any{"kind": "flow", "prompt": "every morning"}, 400); !strings.Contains(e["error"].(string), "could not make") {
			t.Errorf("failed draft = %v", e)
		}

		// A flow draft is checked like a saved flow and starts switched off.
		fake.reset(func(n int, req map[string]any) map[string]any {
			return toolUse("propose_draft", map[string]any{"summary": "Flag big deals", "changes": []any{"New flow"}, "draft": map[string]any{
				"name": "Big deals", "enabled": true, "trigger": map[string]any{"type": "row_created", "table": deals["id"]},
				"steps": []any{map[string]any{"id": "s1", "name": "Check", "type": "condition", "inputs": map[string]any{"left": "{{ trigger.row.Amount }}", "operator": "gt", "right": "250"},
					"then": []any{map[string]any{"id": "s2", "name": "Mark", "type": "update_row", "inputs": map[string]any{"table": deals["id"], "rowId": "{{ trigger.row._id }}", "values": []any{map[string]any{"field": "Title", "value": "{{ trigger.row.Title }} (big)"}}}}}}},
			}})
		})
		d = owner.do("POST", base+"/ai/draft", map[string]any{"kind": "flow", "prompt": "flag big deals"}, 200)
		if f := d["draft"].(map[string]any); f["enabled"] != false || f["runAs"] != "flow" {
			t.Errorf("flow draft = %v", f)
		}

		// A screen draft may use only the components the builder offers.
		catalog := map[string]any{"components": []any{
			map[string]any{"type": "container", "container": true}, map[string]any{"type": "heading"},
			map[string]any{"type": "table"}, map[string]any{"type": "tabs", "container": true}, map[string]any{"type": "tab", "container": true, "parents": []any{"tabs"}},
		}}
		fake.reset(func(n int, req map[string]any) map[string]any {
			root := map[string]any{"id": "root", "type": "container", "props": map[string]any{}, "children": []any{
				map[string]any{"id": "h", "type": "heading", "props": map[string]any{"text": "Deals"}},
				map[string]any{"id": "t", "type": "table", "props": map[string]any{"table": deals["id"]}},
			}}
			if n == 0 {
				root["children"] = append(root["children"].([]any), map[string]any{"id": "x", "type": "kanban", "props": map[string]any{}})
			}
			if n == 1 {
				root["children"] = append(root["children"].([]any), map[string]any{"id": "tb", "type": "tab", "props": map[string]any{}})
			}
			return toolUse("propose_draft", map[string]any{"summary": "Deals list", "changes": []any{"New screen"}, "draft": map[string]any{"name": "Deals", "route": "deals", "role": "basic", "definition": root}})
		})
		owner.do("POST", base+"/ai/draft", map[string]any{"kind": "screen", "prompt": "a deals list", "catalog": catalog}, 200)
		if !strings.Contains(systemText(fake.requests[0]), `"tabs"`) {
			t.Errorf("the catalog did not reach the model")
		}
		if msg, _ := lastToolResult(fake.requests[1]); !strings.Contains(msg, "kanban") {
			t.Errorf("unknown component error = %q", msg)
		}
		if msg, _ := lastToolResult(fake.requests[2]); !strings.Contains(msg, "inside tabs") {
			t.Errorf("parent error = %q", msg)
		}
		// Routes and roles are normalised.
		fake.reset(func(n int, req map[string]any) map[string]any {
			return toolUse("propose_draft", map[string]any{"summary": "Deals list", "changes": []any{"New screen"}, "draft": map[string]any{"name": "Deals", "route": "deals/list", "role": "basic", "definition": map[string]any{
				"id": "root", "type": "container", "props": map[string]any{}, "children": []any{map[string]any{"id": "t", "type": "table", "props": map[string]any{"table": deals["id"]}}}}}})
		})
		d = owner.do("POST", base+"/ai/draft", map[string]any{"kind": "screen", "prompt": "a deals list", "catalog": catalog}, 200)
		if sd := d["draft"].(map[string]any); sd["route"] != "/deals/list" || sd["role"] != "BASIC" {
			t.Errorf("screen draft = %v", sd)
		}

		// Formulas are compiled before they are offered.
		fake.reset(func(n int, req map[string]any) map[string]any {
			if n == 0 {
				return toolUse("propose_formula", map[string]any{"formula": "[Amout] > 100", "explanation": "x"})
			}
			return toolUse("propose_formula", map[string]any{"formula": `[Amount] > 100 AND [Region] = "South"`, "explanation": "Big southern deals."})
		})
		f := owner.do("POST", base+"/ai/formula", map[string]any{"table": deals["id"], "prompt": "big deals in the south", "purpose": "filter"}, 200)
		if f["type"] != "boolean" || !strings.Contains(f["formula"].(string), "South") {
			t.Errorf("formula = %v", f)
		}
		if msg, _ := lastToolResult(fake.requests[1]); !strings.Contains(msg, "Amout") {
			t.Errorf("compile error = %q", msg)
		}

		// Questions are answered with the access of the role asked about.
		var rowsSeen string
		fake.reset(func(n int, req map[string]any) map[string]any {
			switch n {
			case 0:
				return toolUse("query_rows", map[string]any{"table": "Deals", "fields": []any{"Title", "Amount"}})
			case 1:
				rowsSeen, _ = lastToolResult(req)
				return toolUse("summarize", map[string]any{"table": "Deals", "op": "sum", "field": "Amount", "groupBy": "Region"})
			case 2:
				res, _ := lastToolResult(req)
				return say("Your deals total 400: " + res)
			}
			return say("?")
		})
		ans := owner.do("POST", base+"/ai/ask", map[string]any{"question": "How much are my deals worth?", "as": "BASIC"}, 200)
		if strings.Contains(rowsSeen, "Globex") || !strings.Contains(rowsSeen, "Initech") {
			t.Errorf("rows seen as BASIC = %s", rowsSeen)
		}
		if a := ans["answer"].(string); !strings.Contains(a, `"sum":100`) || !strings.Contains(a, `"sum":300`) || strings.Contains(a, "200") {
			t.Errorf("answer = %s", a)
		}
		if calls := ans["calls"].([]any); len(calls) != 2 {
			t.Errorf("calls = %v", calls)
		}
		sys = systemText(fake.requests[0])
		if strings.Contains(sys, "Salaries") || !strings.Contains(sys, "BASIC") {
			t.Errorf("tables for BASIC: %s", sys)
		}
		// Asking as a member applies their own role and email.
		fake.reset(func(n int, req map[string]any) map[string]any {
			if n == 0 {
				return toolUse("query_rows", map[string]any{"table": "Deals", "fields": []any{"Title"}})
			}
			res, _ := lastToolResult(req)
			return say(res)
		})
		bobID := ""
		for _, m := range owner.do("GET", base+"/members", nil, 200)["list"].([]any) {
			if m.(map[string]any)["email"] == "bob@example.com" {
				bobID = m.(map[string]any)["userId"].(string)
			}
		}
		owner.do("POST", dt+"/rows", map[string]any{"Title": "Bob's", "Owner": "bob@example.com", "Region": "North", "Amount": 5}, 201)
		if a := owner.do("POST", base+"/ai/ask", map[string]any{"question": "mine?", "as": "user:" + bobID}, 200); !strings.Contains(a["answer"].(string), "Bob's") || strings.Contains(a["answer"].(string), "Acme") {
			t.Errorf("as bob = %v", a)
		}
		if !strings.Contains(systemText(fake.requests[0]), "bob@example.com") {
			t.Errorf("who is not named")
		}

		// A builder sees everything.
		salaries := func(n int, req map[string]any) map[string]any {
			if n == 0 {
				return toolUse("query_rows", map[string]any{"table": "Salaries"})
			}
			res, isErr := lastToolResult(req)
			if isErr {
				return say("error " + res)
			}
			return say("ok")
		}
		fake.reset(salaries)
		if a := owner.do("POST", base+"/ai/ask", map[string]any{"question": "salaries?"}, 200); a["answer"] != "ok" {
			t.Errorf("builder answer = %v", a)
		}
		fake.reset(salaries)
		if a := owner.do("POST", base+"/ai/ask", map[string]any{"question": "salaries?", "as": "POWER"}, 200); !strings.Contains(a["answer"].(string), "no table") {
			t.Errorf("power answer = %v", a)
		}
	})
}
