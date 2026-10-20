package api

import (
	"io"
	"strings"
	"testing"

	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// TestRowRulesReachFilesAndLinks checks that a table's row rules also hold
// for its files and for the labels other tables show of its rows.
func TestRowRulesReachFilesAndLinks(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		_, hs := newTestServer(t, s)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Deals"}, 201)
		base := "/api/apps/deals"

		customers := owner.do("POST", base+"/tables", map[string]any{"name": "Customers", "fields": []any{
			map[string]any{"name": "Name", "type": "text"},
			map[string]any{"name": "Owner", "type": "text"},
		}}, 201)
		ct := base + "/tables/" + customers["id"].(string)
		deals := owner.do("POST", base+"/tables", map[string]any{"name": "Deals", "fields": []any{
			map[string]any{"name": "Title", "type": "text"},
			map[string]any{"name": "Owner", "type": "text"},
			map[string]any{"name": "Contract", "type": "attachment"},
			map[string]any{"name": "Customer", "type": "link", "linkTable": customers["id"]},
			map[string]any{"name": "Also", "type": "link", "linkTable": customers["id"], "linkMany": true},
		}}, 201)
		dt := base + "/tables/" + deals["id"].(string)
		mine := owner.do("POST", ct+"/rows", map[string]any{"Name": "Acme", "Owner": "bo@example.com"}, 201)
		theirs := owner.do("POST", ct+"/rows", map[string]any{"Name": "Globex", "Owner": "cy@example.com"}, 201)

		boPDF := owner.upload(dt+"/files", "bo.pdf", "application/pdf", []byte(tinyPDF), 201)
		cyPDF := owner.upload(dt+"/files", "cy.pdf", "application/pdf", []byte(tinyPDF), 201)
		owner.do("POST", dt+"/rows", map[string]any{"Title": "Bo's deal", "Owner": "bo@example.com", "Contract": []any{boPDF},
			"Customer": theirs["_id"], "Also": []any{mine["_id"], theirs["_id"]}}, 201)
		owner.do("POST", dt+"/rows", map[string]any{"Title": "Cy's deal", "Owner": "cy@example.com", "Contract": []any{cyPDF}}, 201)

		rule := map[string]any{"read": "BASIC", "write": "BASIC", "delete": "POWER",
			"rows": []any{map[string]any{"role": "BASIC", "filter": "[Owner] = Current.User.Email"}}}
		owner.do("PATCH", dt, map[string]any{"permissions": rule}, 200)
		owner.do("PATCH", ct, map[string]any{"permissions": rule}, 200)

		bo, _ := member(t, owner, hs.URL, "deals", "bo@example.com", "BASIC")
		status := func(c *client, url string) int {
			res := c.get(url)
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			return res.StatusCode
		}

		// Files follow the row rule of the rows that hold them.
		if got := status(bo, boPDF["url"].(string)); got != 200 {
			t.Errorf("bo's own contract: %d", got)
		}
		if got := status(bo, cyPDF["url"].(string)); got != 404 {
			t.Errorf("cy's contract for bo: %d", got)
		}
		if got := status(owner, cyPDF["url"].(string)); got != 200 {
			t.Errorf("cy's contract for the owner: %d", got)
		}
		// A file not saved to a row yet is open to its uploader only.
		draft := bo.upload(dt+"/files", "draft.pdf", "application/pdf", []byte(tinyPDF), 201)
		if got := status(bo, draft["url"].(string)); got != 200 {
			t.Errorf("bo's unsaved upload: %d", got)
		}
		cy, _ := member(t, owner, hs.URL, "deals", "cy@example.com", "BASIC")
		if got := status(cy, draft["url"].(string)); got != 404 {
			t.Errorf("bo's unsaved upload for cy: %d", got)
		}
		if got := status(newClient(t, hs.URL), boPDF["url"].(string)); got != 401 {
			t.Errorf("anonymous: %d", got)
		}

		// Links show labels only of rows the reader may see in the linked table.
		row := bo.do("GET", dt+"/rows", nil, 200)["rows"].([]any)[0].(map[string]any)
		cust := row["Customer"].(map[string]any)
		if cust["label"] != "" || cust["hidden"] != true {
			t.Errorf("hidden customer = %v", cust)
		}
		also := row["Also"].([]any)
		if also[0].(map[string]any)["label"] != "Acme" || also[1].(map[string]any)["hidden"] != true || also[1].(map[string]any)["label"] != "" {
			t.Errorf("also = %v", also)
		}
		one := bo.do("GET", dt+"/rows/"+jsonNum(row["_id"]), nil, 200)
		if one["Customer"].(map[string]any)["hidden"] != true {
			t.Errorf("get row = %v", one["Customer"])
		}
		upd := bo.do("PATCH", dt+"/rows/"+jsonNum(row["_id"]), map[string]any{"Title": "Bo's big deal"}, 200)
		if upd["Customer"].(map[string]any)["hidden"] != true {
			t.Errorf("updated row = %v", upd["Customer"])
		}
		csv := string(bo.raw("GET", dt+"/export", nil, 200))
		if strings.Contains(csv, "Globex") {
			t.Errorf("export leaks a label: %s", csv)
		}
		// Without read access to the linked table, every label is hidden.
		owner.do("PATCH", ct, map[string]any{"permissions": map[string]any{"read": "POWER", "write": "POWER", "delete": "POWER"}}, 200)
		row = bo.do("GET", dt+"/rows", nil, 200)["rows"].([]any)[0].(map[string]any)
		if row["Also"].([]any)[0].(map[string]any)["hidden"] != true {
			t.Errorf("no table access = %v", row["Also"])
		}
		// Builders see everything.
		row = owner.do("GET", dt+"/rows?sort=_id", nil, 200)["rows"].([]any)[0].(map[string]any)
		if row["Customer"].(map[string]any)["label"] != "Globex" {
			t.Errorf("owner = %v", row["Customer"])
		}
	})
}

// TestUnattendedFlowsRunAsRole checks that scheduled and webhook flows that
// keep to a user's permissions act as the role they name, never as Public.
func TestUnattendedFlowsRunAsRole(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		_, hs := newTestServer(t, s)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Desk"}, 201)
		base := "/api/apps/desk"
		tbl := owner.do("POST", base+"/tables", map[string]any{"name": "Log", "fields": []any{map[string]any{"name": "Note", "type": "text"}}}, 201)
		owner.do("PATCH", base+"/tables/"+tbl["id"].(string), map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "POWER", "delete": "POWER"}}, 200)
		def := func(trigger map[string]any, runRole string) map[string]any {
			return map[string]any{"name": "Hook", "enabled": true, "runAs": "user", "runRole": runRole, "trigger": trigger,
				"steps": []any{map[string]any{"name": "Add", "type": "create_row", "inputs": map[string]any{"table": tbl["id"], "values": []any{map[string]any{"field": "Note", "value": "{{ user.role }}"}}}}}}
		}
		hook := map[string]any{"type": "webhook", "respond": true}
		sched := map[string]any{"type": "schedule", "cron": "0 8 * * *"}

		// A flow that runs without a user must name the role it acts as.
		if e := owner.do("POST", base+"/flows", def(hook, ""), 400); !strings.Contains(e["error"].(string), "role") {
			t.Errorf("no role = %v", e)
		}
		owner.do("POST", base+"/flows", def(sched, ""), 400)
		owner.do("POST", base+"/flows", def(hook, "GHOST"), 400)

		f := owner.do("POST", base+"/flows", def(hook, "BASIC"), 201)
		if f["runRole"] != "BASIC" {
			t.Fatalf("flow = %v", f)
		}
		anon := newClient(t, hs.URL)
		hookURL := "/api/hooks/" + f["hookToken"].(string)
		// Basic may not write the log, so the run fails on that.
		if e := anon.do("POST", hookURL, map[string]any{}, 500); !strings.Contains(e["error"].(string), "POWER") {
			t.Errorf("as basic = %v", e)
		}
		owner.do("PATCH", base+"/flows/"+f["id"].(string), map[string]any{"runRole": "POWER"}, 200)
		anon.do("POST", hookURL, map[string]any{}, 200)
		rows := owner.do("GET", base+"/tables/"+tbl["id"].(string)+"/rows", nil, 200)["rows"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["Note"] != "POWER" {
			t.Errorf("rows = %v", rows)
		}
		// A test run of the flow acts the same way, not as the builder.
		owner.do("PATCH", base+"/flows/"+f["id"].(string), map[string]any{"runRole": "BASIC"}, 200)
		if run := owner.do("POST", base+"/flows/"+f["id"].(string)+"/test", map[string]any{}, 200); run["status"] != "failed" {
			t.Errorf("test run = %v", run)
		}
		// Running with full access drops the role.
		f = owner.do("PATCH", base+"/flows/"+f["id"].(string), map[string]any{"runAs": "flow"}, 200)
		if _, ok := f["runRole"]; ok {
			t.Errorf("full access flow = %v", f)
		}
	})
}
