package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bubibase/bubibase/internal/flows"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// waitRuns polls a flow's runs until n of them have finished.
func waitRuns(t *testing.T, c *client, path string, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		list := c.do("GET", path+"/runs", nil, 200)["list"].([]any)
		done := 0
		var out []map[string]any
		for _, r := range list {
			m := r.(map[string]any)
			if m["status"] != "running" {
				done++
			}
			out = append(out, m)
		}
		if done >= n && done == len(list) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d of %d runs finished: %v", path, done, n, list)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func stepByName(run map[string]any, name string) map[string]any {
	for _, s := range run["steps"].([]any) {
		if m := s.(map[string]any); m["name"] == name {
			return m
		}
	}
	return nil
}

func TestFlows(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		var hits atomic.Int32
		up := upstream(t, &hits)
		var echoed []map[string]any
		var mu sync.Mutex
		hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			echoed = append(echoed, map[string]any{"body": body, "key": r.Header.Get("X-Api-Key")})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"received":true}`))
		}))
		defer hook.Close()

		api := New(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		mailer := &flows.LogMailer{From: "shop@example.com"}
		api.Engine.Mailer = mailer
		srv := httptest.NewServer(api.Handler())
		defer srv.Close()
		defer api.Engine.Stop(t.Context())

		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
		base := "/api/apps/shop"
		orders := owner.do("POST", base+"/tables", map[string]any{"name": "Orders", "fields": []any{
			map[string]any{"name": "Customer", "type": "text"},
			map[string]any{"name": "Email", "type": "text"},
			map[string]any{"name": "Amount", "type": "number"},
			map[string]any{"name": "Status", "type": "text"},
			map[string]any{"name": "Touched", "type": "number"},
			map[string]any{"name": "Invoice", "type": "attachment"},
		}}, 201)
		ordersID := orders["id"].(string)
		ob := base + "/tables/" + ordersID
		customers := owner.do("POST", base+"/tables", map[string]any{"name": "Customers", "fields": []any{
			map[string]any{"name": "Name", "type": "text"},
			map[string]any{"name": "Plan", "type": "text"},
		}}, 201)
		owner.do("PUT", base+"/variables/HOOK_KEY", map[string]any{"secret": true, "production": "s3cret-prod-key"}, 200)
		ds := owner.do("POST", base+"/datasources", map[string]any{"name": "Up", "config": map[string]any{"baseUrl": up.URL}}, 201)
		owner.do("POST", base+"/queries", map[string]any{"name": "Echo", "datasourceId": ds["id"], "method": "POST", "path": "/echo", "bodyType": "json", "body": `{"order": {{ params.order }}}`,
			"parameters": []any{map[string]any{"name": "order", "type": "number"}}}, 201)
		owner.do("POST", base+"/queries", map[string]any{"name": "Missing", "datasourceId": ds["id"], "path": "/nothing-here"}, 201)

		// Bad definitions are refused with the step at fault.
		bad := owner.do("POST", base+"/flows", map[string]any{"name": "Bad", "trigger": map[string]any{"type": "schedule", "cron": "99 * * * *"}}, 400)
		if !strings.Contains(bad["error"].(string), "minute") {
			t.Errorf("bad cron = %v", bad)
		}
		bad = owner.do("POST", base+"/flows", map[string]any{"name": "Bad", "steps": []any{map[string]any{"id": "x1", "name": "A", "type": "set_variable", "inputs": map[string]any{"value": "{{ oops"}}}}, 400)
		if bad["step"] != "x1" {
			t.Errorf("bad step = %v", bad)
		}

		// A new order: check it with an API query, branch on the amount,
		// make an invoice PDF, attach it, mail it and call a webhook.
		invoice := owner.do("POST", base+"/flows", map[string]any{
			"name":    "Invoice new orders",
			"trigger": map[string]any{"type": "row_created", "table": ordersID},
			"steps": []any{
				map[string]any{"name": "Check", "type": "run_query", "inputs": map[string]any{"query": "Echo", "params": map[string]any{"order": "{{ trigger.row._id }}"}}},
				map[string]any{"name": "Optional", "type": "run_query", "continueOnError": true, "inputs": map[string]any{"query": "Missing"}},
				map[string]any{"name": "Big", "type": "condition", "inputs": map[string]any{"left": "{{ trigger.row.Amount }}", "operator": "gt", "right": "100"},
					"then": []any{map[string]any{"name": "SetBig", "type": "set_variable", "inputs": map[string]any{"name": "tier", "value": "big"}}},
					"else": []any{map[string]any{"name": "SetSmall", "type": "set_variable", "inputs": map[string]any{"name": "tier", "value": "small"}}}},
				map[string]any{"name": "Pdf", "type": "generate_pdf", "inputs": map[string]any{
					"table": ordersID, "rowId": "{{ trigger.row._id }}", "field": "Invoice",
					"fileName": "Invoice {{ trigger.row._id }}", "title": "Invoice for {{ trigger.row.Customer }}",
					"content": "| Item | Amount |\n|---|---:|\n| Order {{ steps.Check.body.body }} | {{ trigger.row.Amount | number:2 }} |\n\nTier: {{ vars.tier }}",
				}},
				map[string]any{"name": "Mail", "type": "send_email", "inputs": map[string]any{
					"to": "{{ trigger.row.Email }}", "subject": "Your invoice, {{ trigger.row.Customer }}",
					"body": "{{#if vars.tier}}Tier {{ vars.tier }}.{{/if}} See {{ steps.Pdf.file.name }}.", "attachments": "{{ steps.Pdf.file }}",
				}},
				map[string]any{"name": "Notify", "type": "http_request", "inputs": map[string]any{
					"method": "POST", "url": hook.URL + "/orders", "bodyType": "json",
					"body":    `{"order": {{ trigger.row._id }}, "tier": "{{ vars.tier }}", "customer": "{{ trigger.row.Customer }}"}`,
					"headers": []any{map[string]any{"key": "X-Api-Key", "value": "{{ env.HOOK_KEY }}"}},
				}},
				map[string]any{"name": "Mark", "type": "update_row", "inputs": map[string]any{"table": ordersID, "rowId": "{{ trigger.row._id }}",
					"values": []any{map[string]any{"field": "Status", "value": "invoiced {{ steps.Notify.body.received }}"}}}},
			},
		}, 201)
		if invoice["enabled"] != false || invoice["hookToken"] == "" {
			t.Fatalf("new flow = %v", invoice)
		}
		fb := base + "/flows/" + invoice["id"].(string)

		// Switched off, a new row starts nothing.
		owner.do("POST", ob+"/rows", map[string]any{"Customer": "Early", "Amount": 1}, 201)
		time.Sleep(100 * time.Millisecond)
		if runs := owner.do("GET", fb+"/runs", nil, 200)["list"].([]any); len(runs) != 0 {
			t.Fatalf("a switched-off flow ran: %v", runs)
		}
		owner.do("PATCH", fb, map[string]any{"enabled": true}, 200)
		row := owner.do("POST", ob+"/rows", map[string]any{"Customer": `Bob "B"`, "Email": "bob@example.com", "Amount": 150}, 201)
		runs := waitRuns(t, owner, fb, 1)
		run := owner.do("GET", base+"/runs/"+runs[0]["id"].(string), nil, 200)
		if run["status"] != "succeeded" || run["triggerKind"] != "row_created" {
			t.Fatalf("run = %v", run)
		}
		names := []string{}
		for _, st := range run["steps"].([]any) {
			m := st.(map[string]any)
			names = append(names, m["name"].(string)+":"+m["status"].(string))
		}
		if strings.Join(names, " ") != "Check:ok Optional:failed Big:ok SetBig:ok Pdf:ok Mail:ok Notify:ok Mark:ok" {
			t.Errorf("steps = %v", names)
		}
		if out := stepByName(run, "Big")["output"].(map[string]any); out["result"] != true {
			t.Errorf("condition = %v", out)
		}
		if in := stepByName(run, "Check")["inputs"].(map[string]any); in["query"] != "Echo" {
			t.Errorf("logged inputs = %v", in)
		}
		raw, _ := json.Marshal(run)
		if strings.Contains(string(raw), "s3cret-prod-key") {
			t.Error("the run log shows a secret")
		}
		got := owner.do("GET", ob+"/rows/"+jsonNum(row["_id"]), nil, 200)
		files, _ := got["Invoice"].([]any)
		if got["Status"] != "invoiced true" || len(files) != 1 || files[0].(map[string]any)["name"] != "Invoice "+jsonNum(row["_id"])+".pdf" {
			t.Fatalf("row after flow = %v", got)
		}
		res := owner.get(files[0].(map[string]any)["url"].(string))
		pdf, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !strings.HasPrefix(string(pdf), "%PDF-") {
			t.Errorf("invoice is not a PDF: %.20q", pdf)
		}
		if len(mailer.Sent) != 1 || mailer.Sent[0].To[0] != "bob@example.com" || mailer.Sent[0].Subject != `Your invoice, Bob "B"` ||
			mailer.Sent[0].Text != "Tier big. See Invoice "+jsonNum(row["_id"])+".pdf." || len(mailer.Sent[0].Attachments) != 1 {
			t.Errorf("mail = %+v", mailer.Sent)
		}
		mu.Lock()
		if len(echoed) != 1 || echoed[0]["key"] != "s3cret-prod-key" || echoed[0]["body"].(map[string]any)["customer"] != `Bob "B"` {
			t.Errorf("webhook got %v", echoed)
		}
		mu.Unlock()

		// A test run uses the newest row and development values.
		test := owner.do("POST", fb+"/test", nil, 200)
		if test["status"] != "succeeded" || test["test"] != true || test["trigger"].(map[string]any)["row"].(map[string]any)["Customer"] != `Bob "B"` {
			t.Errorf("test run = %v", test["status"])
		}

		// Flows that change rows they react to stop after three rounds.
		bump := owner.do("POST", base+"/flows", map[string]any{
			"name": "Bump", "enabled": true,
			"trigger": map[string]any{"type": "row_updated", "table": ordersID, "fields": []any{"Touched"}},
			"steps": []any{map[string]any{"name": "Again", "type": "update_row", "inputs": map[string]any{"table": ordersID, "rowId": "{{ trigger.row._id }}",
				"values": []any{map[string]any{"field": "Touched", "value": "{{ = trigger.row.Touched + 1 }}"}}}}},
		}, 201)
		bb := base + "/flows/" + bump["id"].(string)
		owner.do("PATCH", ob+"/rows/"+jsonNum(row["_id"]), map[string]any{"Status": "paid"}, 200)
		time.Sleep(100 * time.Millisecond)
		if n := len(owner.do("GET", bb+"/runs", nil, 200)["list"].([]any)); n != 0 {
			t.Errorf("a change to another field ran Bump %d times", n)
		}
		owner.do("PATCH", ob+"/rows/"+jsonNum(row["_id"]), map[string]any{"Touched": 1}, 200)
		waitRuns(t, owner, bb, 3)
		time.Sleep(150 * time.Millisecond)
		if n := len(waitRuns(t, owner, bb, 3)); n != 3 {
			t.Errorf("Bump ran %d times, want 3", n)
		}
		if got := owner.do("GET", ob+"/rows/"+jsonNum(row["_id"]), nil, 200); got["Touched"] != 4.0 {
			t.Errorf("Touched = %v", got["Touched"])
		}

		// A manual flow, as a screen button runs it: a loop, a sum and a
		// returned result.
		quote := owner.do("POST", base+"/flows", map[string]any{
			"name": "Quote",
			"trigger": map[string]any{"type": "manual", "role": "BASIC", "parameters": []any{
				map[string]any{"name": "qty", "type": "number", "default": "1"}, map[string]any{"name": "price", "type": "number"},
			}},
			"steps": []any{
				map[string]any{"name": "Each", "type": "loop", "inputs": map[string]any{"items": "{{ trigger.params.qty }}"}, "do": []any{
					map[string]any{"name": "Sum", "type": "set_variable", "inputs": map[string]any{"name": "total", "mode": "add", "value": "{{ trigger.params.price }}"}},
					map[string]any{"name": "Seen", "type": "set_variable", "inputs": map[string]any{"name": "seen", "mode": "append", "value": "{{ loop.number }}"}},
				}},
				map[string]any{"name": "Done", "type": "return", "inputs": map[string]any{"value": `{{ = CONCAT("total ", vars.total) }}`}},
				map[string]any{"name": "Never", "type": "set_variable", "inputs": map[string]any{"name": "x", "value": "1"}},
			},
		}, 201)
		qb := base + "/flows/" + quote["id"].(string)
		owner.do("POST", qb+"/run", map[string]any{"params": map[string]any{"qty": 3, "price": "2.5"}}, 409)
		out := owner.do("POST", qb+"/run", map[string]any{"params": map[string]any{"qty": 3, "price": "2.5"}, "test": true}, 200)
		if out["status"] != "succeeded" || out["output"] != "total 7.5" {
			t.Fatalf("quote = %v", out)
		}
		if steps := out["steps"].([]any); len(steps) != 8 || steps[len(steps)-1].(map[string]any)["name"] != "Done" {
			t.Errorf("quote steps = %d", len(steps))
		}
		owner.do("PATCH", qb, map[string]any{"enabled": true}, 200)
		anon := newClient(t, srv.URL)
		anon.do("POST", qb+"/run", map[string]any{}, 401)
		owner.do("POST", base+"/flows/"+invoice["id"].(string)+"/run", map[string]any{}, 400)
		rt := owner.do("GET", base+"/runtime", nil, 200)
		if fl := rt["flows"].([]any); len(fl) != 1 || fl[0].(map[string]any)["name"] != "Quote" {
			t.Errorf("runtime flows = %v", rt["flows"])
		}

		// A webhook that answers with the flow's result.
		wh := owner.do("POST", base+"/flows", map[string]any{
			"name": "Signup", "enabled": true,
			"trigger": map[string]any{"type": "webhook", "respond": true},
			"steps": []any{
				map[string]any{"name": "New", "type": "create_row", "inputs": map[string]any{"table": customers["id"], "values": []any{
					map[string]any{"field": "Name", "value": "{{ trigger.body.name | trim }}"}, map[string]any{"field": "Plan", "value": "{{ trigger.query.plan }}"},
				}}},
				map[string]any{"name": "Same", "type": "find_rows", "inputs": map[string]any{"table": customers["id"], "filter": "[Name] = {{ steps.New.row.Name }}"}},
				map[string]any{"name": "Answer", "type": "return", "inputs": map[string]any{"value": "{{ steps.Same.first }}"}},
			},
		}, 201)
		hookURL := "/api/hooks/" + wh["hookToken"].(string)
		ans := anon.do("POST", hookURL+"?plan=pro", map[string]any{"name": ` Zoë "Z" `}, 200)
		if ans["Name"] != `Zoë "Z"` || ans["Plan"] != "pro" {
			t.Errorf("webhook answer = %v", ans)
		}
		anon.do("POST", "/api/hooks/hook_nope", map[string]any{}, 404)
		owner.do("PATCH", base+"/flows/"+wh["id"].(string), map[string]any{"trigger": map[string]any{"type": "webhook"}}, 200)
		if r := anon.do("POST", hookURL, map[string]any{"name": "Async"}, 202); r["runId"] == "" {
			t.Errorf("async webhook = %v", r)
		}
		rotated := owner.do("POST", base+"/flows/"+wh["id"].(string)+"/hook", nil, 200)
		anon.do("POST", hookURL, map[string]any{}, 404)
		anon.do("POST", "/api/hooks/"+rotated["hookToken"].(string), map[string]any{"name": "New address"}, 202)

		// A schedule fires once in each minute it names.
		sched := owner.do("POST", base+"/flows", map[string]any{
			"name": "Nightly", "enabled": true,
			"trigger": map[string]any{"type": "schedule", "cron": "30 2 * * *", "timezone": "Europe/Paris"},
			"steps":   []any{map[string]any{"name": "Stamp", "type": "set_variable", "inputs": map[string]any{"name": "at", "value": "{{ trigger.time }}"}}},
		}, 201)
		paris, _ := time.LoadLocation("Europe/Paris")
		api.Engine.RunDue(time.Date(2026, 10, 20, 2, 29, 0, 0, paris))
		api.Engine.RunDue(time.Date(2026, 10, 20, 2, 30, 0, 0, paris))
		api.Engine.RunDue(time.Date(2026, 10, 20, 2, 30, 30, 0, paris))
		sr := waitRuns(t, owner, base+"/flows/"+sched["id"].(string), 1)
		if len(sr) != 1 {
			t.Fatalf("schedule runs = %d", len(sr))
		}
		full := owner.do("GET", base+"/runs/"+sr[0]["id"].(string), nil, 200)
		if full["trigger"].(map[string]any)["time"] != "2026-10-20T02:30:00+02:00" {
			t.Errorf("schedule trigger = %v", full["trigger"])
		}
		next := owner.do("GET", "/api/flows/schedule?cron=30+2+*+*+*&timezone=Europe/Paris", nil, 200)
		if len(next["next"].([]any)) != 5 {
			t.Errorf("next = %v", next)
		}

		// Deleting a flow deletes its runs.
		owner.do("DELETE", qb, nil, 200)
		owner.do("GET", qb+"/runs", nil, 200)
		owner.do("GET", base+"/runs/"+runs[0]["id"].(string), nil, 200)
	})
}

func jsonNum(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
