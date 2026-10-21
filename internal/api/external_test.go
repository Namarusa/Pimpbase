package api

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// externalDBs are the databases the external connector tests use, named
// like the internal/extdb tests' variables. Each one is skipped when its
// variable is unset.
var externalDBs = []struct {
	kind, env string
	setup     []string
}{
	{"postgres", "PIMPBASE_TEST_EXT_POSTGRES", []string{
		`DROP TABLE IF EXISTS bb_deals`,
		`CREATE TABLE bb_deals (id SERIAL PRIMARY KEY, title VARCHAR(80) NOT NULL, owner VARCHAR(80), amount NUMERIC(12,2), won BOOLEAN, closed DATE, secret_note BYTEA)`,
	}},
	{"mysql", "PIMPBASE_TEST_EXT_MYSQL", []string{
		`DROP TABLE IF EXISTS bb_deals`,
		`CREATE TABLE bb_deals (id INT AUTO_INCREMENT PRIMARY KEY, title VARCHAR(80) NOT NULL, owner VARCHAR(80), amount DECIMAL(12,2), won TINYINT(1), closed DATE, secret_note BLOB)`,
	}},
	{"mssql", "PIMPBASE_TEST_EXT_MSSQL", []string{
		`IF OBJECT_ID('dbo.bb_deals') IS NOT NULL DROP TABLE dbo.bb_deals`,
		`CREATE TABLE dbo.bb_deals (id INT IDENTITY(1,1) PRIMARY KEY, title NVARCHAR(80) NOT NULL, owner NVARCHAR(80), amount DECIMAL(12,2), won BIT, closed DATE, secret_note VARBINARY(100))`,
	}},
	{"mongodb", "PIMPBASE_TEST_EXT_MONGO", nil},
}

func TestExternalTables(t *testing.T) {
	for _, db := range externalDBs {
		uri := os.Getenv(db.env)
		t.Run(db.kind, func(t *testing.T) {
			if uri == "" {
				t.Skipf("%s is not set", db.env)
			}
			storetest.Each(t, func(t *testing.T, s *store.Store) { testExternal(t, s, db.kind, uri, db.setup) })
		})
	}
}

func names(page map[string]any, field string) string {
	var out []string
	for _, r := range page["rows"].([]any) {
		out = append(out, fmt.Sprint(r.(map[string]any)[field]))
	}
	return strings.Join(out, ",")
}

func testExternal(t *testing.T, s *store.Store, kind, uri string, setup []string) {
	_, hs := newTestServer(t, s)
	owner := newClient(t, hs.URL)
	owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
	owner.do("POST", "/api/apps", map[string]any{"name": "Deals"}, 201)
	base := "/api/apps/deals"

	kinds := owner.do("GET", "/api/connectors", nil, 200)["list"].([]any)
	if len(kinds) != 4 {
		t.Errorf("connectors = %v", kinds)
	}

	// The connection string lives in a secret variable.
	owner.do("PUT", base+"/variables/DB_URI", map[string]any{"secret": true, "development": uri, "production": uri}, 200)
	cfg := map[string]any{"db": map[string]any{"uri": "{{ env.DB_URI }}"}}
	res := owner.do("POST", base+"/datasources/test", map[string]any{"kind": kind, "config": cfg, "env": "development"}, 200)
	if res["ok"] != true {
		t.Fatalf("test connection = %v", res)
	}
	// A wrong password fails the test and the password is not shown.
	if kind != "mongodb" {
		bad := strings.Replace(uri, "@", "x-wrong@", 1)
		if u, err := url.Parse(uri); err == nil && u.User != nil {
			if p, ok := u.User.Password(); ok {
				bad = strings.Replace(uri, ":"+p+"@", ":Wrong-pass-123@", 1)
			}
		} else if i := strings.Index(uri, ":"); i > 0 && strings.Contains(uri, "@tcp(") {
			bad = uri[:i] + ":Wrong-pass-123" + uri[strings.Index(uri, "@"):]
		}
		owner.do("PUT", base+"/variables/BAD_URI", map[string]any{"secret": true, "development": bad}, 200)
		res = owner.do("POST", base+"/datasources/test", map[string]any{"kind": kind, "config": map[string]any{"db": map[string]any{"uri": "{{ env.BAD_URI }}"}}, "env": "development"}, 200)
		if res["ok"] != false || strings.Contains(fmt.Sprint(res["message"]), "Wrong-pass-123") {
			t.Errorf("bad password test = %v", res)
		}
	}
	ds := owner.do("POST", base+"/datasources", map[string]any{"name": "Sales DB", "kind": kind, "config": cfg}, 201)
	dsID := ds["id"].(string)

	// Raw queries set the table up; bindings are parameters.
	run := func(body map[string]any, status int) map[string]any {
		t.Helper()
		q := map[string]any{"name": "Setup", "datasourceId": dsID}
		for k, v := range body {
			q[k] = v
		}
		return owner.do("POST", base+"/queries/test", map[string]any{"query": q, "env": "development"}, status)
	}
	for _, sql := range setup {
		run(map[string]any{"sql": sql}, 200)
	}
	if kind == "mongodb" {
		run(map[string]any{"collection": "bb_deals", "operation": "deleteMany", "filter": `{"_id": {"$exists": true}}`}, 200)
		run(map[string]any{"collection": "bb_deals", "operation": "insertOne", "document": `{"title": "Seed", "owner": "x", "amount": 1.5, "won": false, "closed": {"$date": "2026-01-01T00:00:00Z"}}`}, 200)
	}

	schema := owner.do("GET", base+"/datasources/"+dsID+"/schema", nil, 200)["list"].([]any)
	var found map[string]any
	for _, x := range schema {
		if x.(map[string]any)["name"] == "bb_deals" {
			found = x.(map[string]any)
		}
	}
	if found == nil {
		t.Fatalf("schema = %v", schema)
	}
	tbl := owner.do("POST", base+"/datasources/"+dsID+"/tables", map[string]any{"schema": found["schema"], "source": "bb_deals", "name": "Deals"}, 201)
	owner.do("POST", base+"/datasources/"+dsID+"/tables", map[string]any{"schema": found["schema"], "source": "bb_deals", "name": "Deals"}, 400)
	id := tbl["id"].(string)
	if !strings.HasPrefix(id, "ext_") || tbl["primaryField"] != "title" {
		t.Fatalf("table = %v", tbl)
	}
	fields := map[string]string{}
	for _, f := range tbl["fields"].([]any) {
		f := f.(map[string]any)
		fields[f["name"].(string)] = f["type"].(string)
	}
	want := map[string]string{"title": "text", "owner": "text", "amount": "number", "won": "boolean", "closed": "date"}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("field %s = %s, want %s (%v)", k, fields[k], v, fields)
		}
	}
	if _, ok := fields["secret_note"]; ok {
		t.Errorf("a binary column is shown: %v", fields)
	}
	tb := base + "/tables/" + id
	if kind == "mongodb" {
		run(map[string]any{"collection": "bb_deals", "operation": "deleteMany", "filter": `{"title": "Seed"}`}, 200)
	}

	// The table lists with the app's tables and in the runtime.
	list := owner.do("GET", base+"/tables", nil, 200)["list"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["external"].(map[string]any)["kind"] != kind {
		t.Fatalf("tables = %v", list)
	}
	owner.do("POST", tb+"/fields", map[string]any{"name": "x", "type": "text"}, 400)

	for _, r := range []map[string]any{
		{"title": "Acme", "owner": "bo@example.com", "amount": 100, "won": true, "closed": "2026-03-01"},
		{"title": "Globex", "owner": "cy@example.com", "amount": 250.5, "won": false},
		{"title": "Initech", "owner": "bo@example.com", "amount": 300},
	} {
		row := owner.do("POST", tb+"/rows", r, 201)
		if row["title"] != r["title"] || row["_id"] == nil {
			t.Fatalf("created %v", row)
		}
	}
	if kind != "mongodb" {
		owner.do("POST", tb+"/rows", map[string]any{"owner": "x"}, 400) // title is NOT NULL
	}
	owner.do("POST", tb+"/rows", map[string]any{"nope": 1}, 400)

	page := owner.do("GET", tb+"/rows?sort=amount&desc=true", nil, 200)
	if names(page, "title") != "Initech,Globex,Acme" || page["total"].(float64) != 3 {
		t.Fatalf("sorted = %v", page)
	}
	acme := page["rows"].([]any)[2].(map[string]any)
	if acme["amount"] != 100.0 || acme["won"] != true || acme["closed"] != "2026-03-01T00:00:00Z" {
		t.Errorf("acme = %v", acme)
	}
	page = owner.do("GET", tb+"/rows?filter="+url.QueryEscape(`AND([amount] > 120, NOT [won])`)+"&sort=title", nil, 200)
	if names(page, "title") != "Globex,Initech" || page["engine"] != "sql" {
		t.Errorf("filtered = %v", page)
	}
	// The screen search filter runs in the database too.
	page = owner.do("GET", tb+"/rows?filter="+url.QueryEscape(`OR(CONTAINS(LOWER(TEXT([title])), "glo"), CONTAINS(LOWER(TEXT([amount])), "300"))`)+"&sort=title", nil, 200)
	if names(page, "title") != "Globex,Initech" || page["engine"] != "sql" {
		t.Errorf("search = %v", page)
	}
	// A filter with no database form is worked out in Go.
	page = owner.do("GET", tb+"/rows?filter="+url.QueryEscape(`LEN([title]) = 4`), nil, 200)
	if names(page, "title") != "Acme" || page["engine"] != "go" {
		t.Errorf("go filter = %v", page)
	}
	page = owner.do("GET", tb+"/rows?limit=1&offset=1&sort=title", nil, 200)
	if names(page, "title") != "Globex" || page["total"].(float64) != 3 {
		t.Errorf("paged = %v", page)
	}
	acmeID := fmt.Sprint(acme["_id"])
	if v, ok := acme["_id"].(float64); ok {
		acmeID = fmt.Sprint(int64(v))
	}
	row := owner.do("PATCH", tb+"/rows/"+acmeID, map[string]any{"amount": 120, "won": false}, 200)
	if row["amount"] != 120.0 || row["won"] != false {
		t.Errorf("updated = %v", row)
	}
	owner.do("GET", tb+"/rows/"+acmeID, nil, 200)

	// Roles and row rules: a member sees and changes only their own deals.
	owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "BASIC", "delete": "ADMIN",
		"rows": []any{map[string]any{"role": "BASIC", "filter": "[owner] = Current.User.Email"}}}}, 200)
	owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "BASIC", "delete": "ADMIN",
		"rows": []any{map[string]any{"role": "BASIC", "filter": "[nope] = 1"}}}}, 400)
	bo, _ := member(t, owner, hs.URL, "deals", "bo@example.com", "BASIC")
	page = bo.do("GET", tb+"/rows?sort=title", nil, 200)
	if names(page, "title") != "Acme,Initech" || page["total"].(float64) != 2 {
		t.Errorf("bo sees %v", page)
	}
	all := owner.do("GET", tb+"/rows?sort=title", nil, 200)["rows"].([]any)
	globexID := fmt.Sprint(all[1].(map[string]any)["_id"])
	if v, ok := all[1].(map[string]any)["_id"].(float64); ok {
		globexID = fmt.Sprint(int64(v))
	}
	bo.do("GET", tb+"/rows/"+globexID, nil, 404)
	bo.do("PATCH", tb+"/rows/"+globexID, map[string]any{"amount": 1}, 404)
	bo.do("PATCH", tb+"/rows/"+acmeID, map[string]any{"owner": "cy@example.com"}, 403)
	bo.do("POST", tb+"/rows", map[string]any{"title": "Hooli", "owner": "cy@example.com"}, 403)
	bo.do("POST", tb+"/rows", map[string]any{"title": "Hooli", "owner": "bo@example.com"}, 201)
	bo.do("DELETE", tb+"/rows/"+acmeID, nil, 403)
	rt := bo.do("GET", base+"/runtime", nil, 200)
	if len(rt["tables"].([]any)) != 1 {
		t.Errorf("runtime tables = %v", rt["tables"])
	}
	cy, _ := member(t, owner, hs.URL, "deals", "cy@example.com", "")
	cy.do("GET", tb+"/rows", nil, 403)

	// Hidden columns leave the API, but not one a row rule reads.
	owner.do("PATCH", tb, map[string]any{"hidden": []any{"owner"}}, 400)
	tbl = owner.do("PATCH", tb, map[string]any{"hidden": []any{"closed"}, "primaryField": "title"}, 200)
	for _, f := range tbl["fields"].([]any) {
		if f.(map[string]any)["name"] == "closed" {
			t.Errorf("hidden column is listed")
		}
	}
	if _, ok := bo.do("GET", tb+"/rows", nil, 200)["rows"].([]any)[0].(map[string]any)["closed"]; ok {
		t.Errorf("hidden column is in rows")
	}
	owner.do("PATCH", tb, map[string]any{"hidden": []any{}}, 200)

	// Deleting rows and a query with parameters bound safely.
	n := owner.do("POST", tb+"/rows/delete", map[string]any{"ids": []any{globexID}}, 200)
	if n["deleted"].(float64) != 1 {
		t.Errorf("deleted %v", n)
	}
	if kind != "mongodb" {
		q := owner.do("POST", base+"/queries", map[string]any{"name": "By owner", "datasourceId": dsID, "role": "BASIC",
			"sql":        "SELECT title, amount FROM bb_deals WHERE owner = {{ params.owner }} ORDER BY title",
			"parameters": []any{map[string]any{"name": "owner", "type": "text", "default": "bo@example.com"}}}, 201)
		res := bo.do("POST", base+"/queries/"+q["id"].(string)+"/run", map[string]any{}, 200)
		var titles []string
		for _, r := range res["rows"].([]any) {
			titles = append(titles, r.(map[string]any)["title"].(string))
		}
		sort.Strings(titles)
		if strings.Join(titles, ",") != "Acme,Hooli,Initech" || res["request"] != nil {
			t.Errorf("query rows = %v", res)
		}
		res = bo.do("POST", base+"/queries/"+q["id"].(string)+"/run", map[string]any{"params": map[string]any{"owner": "x' OR '1'='1"}}, 200)
		if len(res["rows"].([]any)) != 0 {
			t.Errorf("an injected parameter matched rows: %v", res["rows"])
		}
		owner.do("POST", base+"/queries", map[string]any{"name": "Quoted", "datasourceId": dsID, "sql": "SELECT * FROM bb_deals WHERE owner = '{{ params.owner }}'"}, 400)
	} else {
		q := owner.do("POST", base+"/queries", map[string]any{"name": "By owner", "datasourceId": dsID, "role": "BASIC", "collection": "bb_deals", "operation": "find",
			"filter": `{"owner": "{{ params.owner }}"}`, "options": `{"sort": {"title": 1}}`,
			"parameters": []any{map[string]any{"name": "owner", "type": "text", "default": "bo@example.com"}}}, 201)
		res := bo.do("POST", base+"/queries/"+q["id"].(string)+"/run", map[string]any{"params": map[string]any{"owner": `x", "$where": "1`}}, 200)
		if len(res["rows"].([]any)) != 0 {
			t.Errorf("an injected parameter matched rows: %v", res["rows"])
		}
		res = bo.do("POST", base+"/queries/"+q["id"].(string)+"/run", map[string]any{}, 200)
		if len(res["rows"].([]any)) != 3 {
			t.Errorf("query rows = %v", res["rows"])
		}
	}

	// CSV out and back in, and a flow started by a row added through
	// Pimpbase that writes to the table.
	csv := string(owner.raw("GET", tb+"/export?filter="+url.QueryEscape(`[title] = "Initech"`), nil, 200))
	if !strings.Contains(csv, "title") || !strings.Contains(csv, "Initech") || strings.Contains(csv, "Acme") {
		t.Errorf("csv = %s", csv)
	}
	res = owner.do("POST", tb+"/import", "title,owner,amount\nCsv one,bo@example.com,7\nCsv two,bo@example.com,not a number\n", 200)
	if res["imported"] != 1.0 || res["skipped"] != 1.0 {
		t.Errorf("import = %v", res)
	}
	flow := owner.do("POST", base+"/flows", map[string]any{
		"name":    "Stamp",
		"trigger": map[string]any{"type": "row_created", "table": id},
		"steps": []any{
			map[string]any{"name": "Stamp", "type": "update_row", "inputs": map[string]any{"table": id, "rowId": "{{ trigger.row._id }}", "values": []any{
				map[string]any{"field": "won", "value": "true"},
			}}},
		},
	}, 201)
	owner.do("PATCH", base+"/flows/"+flow["id"].(string), map[string]any{"enabled": true}, 200)
	stamped := bo.do("POST", tb+"/rows", map[string]any{"title": "Flowed", "owner": "bo@example.com", "won": false}, 201)
	runs := waitRuns(t, owner, base+"/flows/"+flow["id"].(string), 1)
	if runs[0]["status"] != "succeeded" {
		t.Errorf("run = %v", owner.do("GET", base+"/runs/"+runs[0]["id"].(string), nil, 200))
	}
	if r := owner.do("GET", tb+"/rows/"+url.PathEscape(idText(stamped["_id"])), nil, 200); r["won"] != true {
		t.Errorf("stamped = %v", r)
	}
	owner.do("DELETE", base+"/flows/"+flow["id"].(string), nil, 200)

	// A read-only data source refuses writes.
	owner.do("PATCH", base+"/datasources/"+dsID, map[string]any{"config": map[string]any{"db": map[string]any{"uri": "{{ env.DB_URI }}", "readOnly": true}}}, 200)
	owner.do("POST", tb+"/rows", map[string]any{"title": "Nope"}, 400)
	if kind != "mongodb" {
		run(map[string]any{"sql": "DELETE FROM bb_deals"}, 502)
	}
	// Deleting the data source forgets its tables.
	owner.do("DELETE", base+"/datasources/"+dsID, nil, 200)
	owner.do("GET", tb, nil, 404)
}
