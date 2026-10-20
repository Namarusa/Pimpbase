package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// send posts raw bytes and answers the decoded JSON.
func (c *client) send(path string, body []byte, wantStatus int) map[string]any {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.base+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/zip")
	req.Header.Set("Origin", c.base)
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		c.t.Fatalf("POST %s: status %d, want %d: %s", path, res.StatusCode, wantStatus, data)
	}
	var out map[string]any
	json.Unmarshal(data, &out)
	return out
}

func zipEntry(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			return string(b)
		}
	}
	return ""
}

func changes(plan map[string]any) map[string]string {
	out := map[string]string{}
	for _, c := range plan["changes"].([]any) {
		m := c.(map[string]any)
		out[m["kind"].(string)+" "+m["name"].(string)] = m["action"].(string)
	}
	return out
}

// TestBundle moves an app as one file: a copy next to itself with its
// data, then a new version handed from a development server to a
// production one that keeps its rows and secrets.
func TestBundle(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		_, hs := newTestServer(t, s)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		app := owner.do("POST", "/api/apps", map[string]any{"name": "CRM"}, 201)
		base := "/api/apps/crm"
		companies := owner.do("POST", base+"/tables", map[string]any{"name": "Companies", "fields": []any{
			map[string]any{"name": "Name", "type": "text"},
			map[string]any{"name": "Size", "type": "number", "indexed": true},
		}}, 201)
		deals := owner.do("POST", base+"/tables", map[string]any{"name": "Deals", "fields": []any{
			map[string]any{"name": "Title", "type": "text"},
			map[string]any{"name": "Amount", "type": "number"},
			map[string]any{"name": "Company", "type": "link", "linkTable": companies["id"]},
			map[string]any{"name": "Partners", "type": "link", "linkTable": companies["id"], "linkMany": true},
			map[string]any{"name": "Contract", "type": "attachment"},
			map[string]any{"name": "Big", "type": "formula", "formula": "[Amount] > 100"},
		}}, 201)
		ct := base + "/tables/" + companies["id"].(string)
		dt := base + "/tables/" + deals["id"].(string)
		acme := owner.do("POST", ct+"/rows", map[string]any{"Name": "Acme", "Size": 50}, 201)
		globex := owner.do("POST", ct+"/rows", map[string]any{"Name": "Globex", "Size": 900}, 201)
		pdf := owner.upload(dt+"/files", "contract.pdf", "application/pdf", []byte(tinyPDF), 201)
		owner.do("POST", dt+"/rows", map[string]any{"Title": "Big one", "Amount": 500, "Company": acme["_id"], "Partners": []any{acme["_id"], globex["_id"]}, "Contract": []any{pdf}}, 201)
		owner.do("POST", dt+"/rows", map[string]any{"Title": "Small", "Amount": 20, "Company": globex["_id"]}, 201)
		owner.do("POST", base+"/roles", map[string]any{"id": "SALES", "name": "Sales", "inherits": "BASIC"}, 201)
		owner.do("POST", base+"/screens", map[string]any{"name": "Home", "route": "/", "role": "BASIC"}, 201)
		owner.do("PUT", base+"/variables/API_TOKEN", map[string]any{"secret": true, "development": "dev-s3cret", "production": "prod-s3cret"}, 200)
		owner.do("PUT", base+"/variables/REGION", map[string]any{"production": "eu"}, 200)
		ds := owner.do("POST", base+"/datasources", map[string]any{"name": "Shop", "config": map[string]any{
			"baseUrl": "https://shop.example.com", "auth": map[string]any{"type": "bearer", "token": "literal-token"}}}, 201)
		owner.do("POST", base+"/queries", map[string]any{"name": "Items", "datasourceId": ds["id"], "path": "/items"}, 201)
		owner.do("POST", base+"/flows", map[string]any{
			"name": "Nightly", "enabled": true,
			"trigger": map[string]any{"type": "schedule", "cron": "0 3 * * *", "timezone": "UTC"},
			"steps":   []any{map[string]any{"name": "Stamp", "type": "set_variable", "inputs": map[string]any{"name": "at", "value": "{{ trigger.time }}"}}},
		}, 201)

		// Without data or secrets: no rows, and no secret in the file.
		plain := owner.raw("GET", base+"/export", nil, 200)
		appJSON := zipEntry(t, plain, "app.json")
		if appJSON == "" || strings.Contains(appJSON, "s3cret") || strings.Contains(appJSON, "literal-token") || !strings.Contains(appJSON, "https://shop.example.com") {
			t.Fatalf("app.json leaks or lacks settings: %.300s", appJSON)
		}
		if m := zipEntry(t, plain, "manifest.json"); !strings.Contains(m, `"data": false`) || !strings.Contains(m, `"tables": 2`) {
			t.Errorf("manifest = %s", m)
		}

		// A member who is not a builder cannot export.
		basic, _ := member(t, owner, hs.URL, "crm", "bo@example.com", "BASIC")
		basic.raw("GET", base+"/export", nil, 403)
		basic.send("/api/apps/import", plain, 403)
		owner.send("/api/apps/import", []byte("not a zip"), 400)

		// A full copy next to the original: every id is taken, so all are new.
		full := owner.raw("GET", base+"/export?data=1&secrets=1", nil, 200)
		dry := owner.send("/api/apps/import?name=CRM+copy&dryRun=1", full, 200)
		if dry["applied"] != false || dry["mode"] != "new" {
			t.Fatalf("dry run = %v", dry)
		}
		owner.do("GET", "/api/apps/crm-copy", nil, 404)
		plan := owner.send("/api/apps/import?name=CRM+copy", full, 201)
		if plan["rows"] != float64(4) || plan["files"] != float64(1) || plan["applied"] != true {
			t.Fatalf("copy plan = %v", plan)
		}
		cp := plan["app"].(map[string]any)
		if cp["slug"] != "crm-copy" || cp["id"] == app["id"] {
			t.Fatalf("copy = %v", cp)
		}
		cbase := "/api/apps/crm-copy"
		ctables := owner.do("GET", cbase+"/tables", nil, 200)["list"].([]any)
		var cdeals map[string]any
		for _, x := range ctables {
			if x.(map[string]any)["name"] == "Deals" {
				cdeals = x.(map[string]any)
			}
			if x.(map[string]any)["id"] == deals["id"] || x.(map[string]any)["id"] == companies["id"] {
				t.Errorf("copy kept table id %v", x.(map[string]any)["id"])
			}
		}
		rows := owner.do("GET", cbase+"/tables/"+cdeals["id"].(string)+"/rows?sort=Amount&desc=1", nil, 200)["rows"].([]any)
		big := rows[0].(map[string]any)
		if len(rows) != 2 || big["Title"] != "Big one" || big["Big"] != true || jsonNum(big["Company"]) != jsonNum(map[string]any{"_id": acme["_id"], "label": "Acme"}) || len(big["Partners"].([]any)) != 2 {
			t.Fatalf("copied rows = %v", rows)
		}
		file := big["Contract"].([]any)[0].(map[string]any)
		if !strings.HasPrefix(file["url"].(string), "/api/apps/"+cp["id"].(string)+"/") {
			t.Errorf("copied file url = %v", file["url"])
		}
		if got := owner.raw("GET", file["url"].(string), nil, 200); string(got) != tinyPDF {
			t.Errorf("copied file = %q", got)
		}
		// New rows keep counting after the copied ids.
		next := owner.do("POST", cbase+"/tables/"+cdeals["id"].(string)+"/rows", map[string]any{"Title": "New"}, 201)
		if next["_id"].(float64) <= big["_id"].(float64) {
			t.Errorf("new row id %v after %v", next["_id"], big["_id"])
		}
		flows := owner.do("GET", cbase+"/flows", nil, 200)["list"].([]any)
		if len(flows) != 1 || flows[0].(map[string]any)["enabled"] != false {
			t.Errorf("copied flows = %v", flows)
		}
		vars := jsonNum(owner.do("GET", cbase+"/variables", nil, 200))
		if !strings.Contains(vars, `"API_TOKEN"`) || !strings.Contains(vars, `"REGION"`) {
			t.Errorf("copied variables = %s", vars)
		}
		// The slug is taken now; a second copy gets a free one.
		again := owner.send("/api/apps/import?name=CRM+copy", plain, 201)
		if again["app"].(map[string]any)["slug"] == "crm-copy" {
			t.Errorf("second copy reused the slug: %v", again["app"])
		}

		// Development to production: production starts from the plain file
		// on its own server, so ids are kept.
		prodStore, err := store.Open(context.Background(), t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { prodStore.Close() })
		_, phs := newTestServer(t, prodStore)
		prod := newClient(t, phs.URL)
		prod.do("POST", "/api/setup", map[string]any{"email": "ops@example.com", "name": "Ops", "password": "correct horse"}, 200)
		first := prod.send("/api/apps/import", plain, 201)
		if first["app"].(map[string]any)["id"] != app["id"] {
			t.Fatalf("production app id = %v, want %v", first["app"], app["id"])
		}
		pt := "/api/apps/crm/tables/" + companies["id"].(string)
		prod.do("POST", pt+"/rows", map[string]any{"Name": "Initech", "Size": 10}, 201)
		prod.do("PUT", "/api/apps/crm/variables/API_TOKEN", map[string]any{"secret": true, "production": "prod-only"}, 200)
		prod.do("PUT", "/api/apps/crm/variables/REGION", map[string]any{"production": "us"}, 200)
		pds := prod.do("GET", "/api/apps/crm/datasources", nil, 200)["list"].([]any)[0].(map[string]any)
		prod.do("PATCH", "/api/apps/crm/datasources/"+pds["id"].(string), map[string]any{"config": map[string]any{
			"baseUrl": "https://shop.example.com", "auth": map[string]any{"type": "bearer", "token": "prod-token"}}}, 200)

		// Development moves on.
		owner.do("POST", ct+"/fields", map[string]any{"name": "Region", "type": "text"}, 201)
		var titleID, amountID string
		for _, f := range deals["fields"].([]any) {
			switch f.(map[string]any)["name"] {
			case "Title":
				titleID = f.(map[string]any)["id"].(string)
			case "Amount":
				amountID = f.(map[string]any)["id"].(string)
			}
		}
		owner.do("PATCH", dt+"/fields/"+titleID, map[string]any{"name": "Deal name"}, 200)
		owner.do("PATCH", dt+"/fields/"+amountID, map[string]any{"indexed": true}, 200)
		owner.do("POST", base+"/screens", map[string]any{"name": "Report", "route": "/report", "role": "SALES"}, 201)
		qs := owner.do("GET", base+"/queries", nil, 200)["list"].([]any)
		owner.do("DELETE", base+"/queries/"+qs[0].(map[string]any)["id"].(string), nil, 200)
		owner.do("PUT", base+"/variables/NEW_FLAG", map[string]any{"production": "on"}, 200)
		v2 := owner.raw("GET", base+"/export", nil, 200)

		// The plan, without changing anything.
		dry = prod.send("/api/apps/crm/import?dryRun=1", v2, 200)
		got := changes(dry)
		for k, want := range map[string]string{
			"field Companies.Region": "add", "field Deals.Deal name": "change", "field Deals.Amount": "change",
			"screen Report": "add", "query Items": "keep", "variable NEW_FLAG": "add",
		} {
			if got[k] != want {
				t.Errorf("plan %q = %q, want %q (plan %v)", k, got[k], want, got)
			}
		}
		if dry["applied"] != false {
			t.Errorf("dry run applied")
		}
		ptable := prod.do("GET", "/api/apps/crm/tables/"+deals["id"].(string), nil, 200)
		if strings.Contains(jsonNum(ptable), "Deal name") {
			t.Fatalf("dry run changed the table")
		}

		// Applied, with prune: the query goes too.
		done := prod.send("/api/apps/crm/import?prune=1", v2, 200)
		if done["applied"] != true || changes(done)["query Items"] != "remove" {
			t.Fatalf("update = %v", done)
		}
		ptable = prod.do("GET", "/api/apps/crm/tables/"+deals["id"].(string), nil, 200)
		pc := prod.do("GET", pt, nil, 200)
		if !strings.Contains(jsonNum(ptable), `"name":"Deal name"`) || !strings.Contains(jsonNum(pc), `"name":"Region"`) || !strings.Contains(jsonNum(ptable), `"indexed":true`) {
			t.Errorf("updated tables = %s / %s", jsonNum(ptable), jsonNum(pc))
		}
		prows := prod.do("GET", pt+"/rows", nil, 200)["rows"].([]any)
		if len(prows) != 1 || prows[0].(map[string]any)["Name"] != "Initech" {
			t.Errorf("production rows = %v", prows)
		}
		if n := len(prod.do("GET", "/api/apps/crm/screens", nil, 200)["list"].([]any)); n != 2 {
			t.Errorf("production screens = %d", n)
		}
		if n := len(prod.do("GET", "/api/apps/crm/queries", nil, 200)["list"].([]any)); n != 0 {
			t.Errorf("production queries = %d after prune", n)
		}
		pvars := jsonNum(prod.do("GET", "/api/apps/crm/variables", nil, 200))
		if !strings.Contains(pvars, `"production":"us"`) || !strings.Contains(pvars, `"NEW_FLAG"`) {
			t.Errorf("production variables = %s", pvars)
		}
		pds = prod.do("GET", "/api/apps/crm/datasources", nil, 200)["list"].([]any)[0].(map[string]any)
		if tok := pds["config"].(map[string]any)["auth"].(map[string]any)["token"]; tok != "prod-token" {
			t.Errorf("production token = %v", tok)
		}
		pflows := prod.do("GET", "/api/apps/crm/flows", nil, 200)["list"].([]any)
		if len(pflows) != 1 || pflows[0].(map[string]any)["enabled"] != false {
			t.Errorf("production flows = %v", pflows)
		}
	})
}
