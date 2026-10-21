package api

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// linkedDBs set up three related tables: orders of customers, and order
// lines keyed by the order and a line number.
var linkedDBs = []struct {
	kind, env string
	setup     []string
}{
	{"postgres", "BUBIBASE_TEST_EXT_POSTGRES", []string{
		`DROP TABLE IF EXISTS gx_lines, gx_orders, gx_customers`,
		`CREATE TABLE gx_customers (id SERIAL PRIMARY KEY, name VARCHAR(80) NOT NULL)`,
		`CREATE TABLE gx_orders (id SERIAL PRIMARY KEY, title VARCHAR(80) NOT NULL, owner VARCHAR(80), customer_id INTEGER REFERENCES gx_customers(id), region_id INTEGER, amount NUMERIC(10,2))`,
		`CREATE TABLE gx_lines (order_id INTEGER NOT NULL REFERENCES gx_orders(id) ON DELETE CASCADE, line INTEGER NOT NULL, qty INTEGER, PRIMARY KEY (order_id, line))`,
	}},
	{"mysql", "BUBIBASE_TEST_EXT_MYSQL", []string{
		`DROP TABLE IF EXISTS gx_lines`, `DROP TABLE IF EXISTS gx_orders`, `DROP TABLE IF EXISTS gx_customers`,
		`CREATE TABLE gx_customers (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(80) NOT NULL)`,
		`CREATE TABLE gx_orders (id INT AUTO_INCREMENT PRIMARY KEY, title VARCHAR(80) NOT NULL, owner VARCHAR(80), customer_id INT, region_id INT, amount DECIMAL(10,2), FOREIGN KEY (customer_id) REFERENCES gx_customers(id))`,
		`CREATE TABLE gx_lines (order_id INT NOT NULL, line INT NOT NULL, qty INT, PRIMARY KEY (order_id, line), FOREIGN KEY (order_id) REFERENCES gx_orders(id) ON DELETE CASCADE)`,
	}},
	{"mssql", "BUBIBASE_TEST_EXT_MSSQL", []string{
		`IF OBJECT_ID('dbo.gx_lines') IS NOT NULL DROP TABLE dbo.gx_lines`,
		`IF OBJECT_ID('dbo.gx_orders') IS NOT NULL DROP TABLE dbo.gx_orders`,
		`IF OBJECT_ID('dbo.gx_customers') IS NOT NULL DROP TABLE dbo.gx_customers`,
		`CREATE TABLE dbo.gx_customers (id INT IDENTITY(1,1) PRIMARY KEY, name NVARCHAR(80) NOT NULL)`,
		`CREATE TABLE dbo.gx_orders (id INT IDENTITY(1,1) PRIMARY KEY, title NVARCHAR(80) NOT NULL, owner NVARCHAR(80), customer_id INT REFERENCES dbo.gx_customers(id), region_id INT, amount DECIMAL(10,2))`,
		`CREATE TABLE dbo.gx_lines (order_id INT NOT NULL REFERENCES dbo.gx_orders(id) ON DELETE CASCADE, line INT NOT NULL, qty INT, PRIMARY KEY (order_id, line))`,
	}},
}

func TestExternalLinksKeysFlowsAI(t *testing.T) {
	for _, db := range linkedDBs {
		uri := os.Getenv(db.env)
		t.Run(db.kind, func(t *testing.T) {
			if uri == "" {
				t.Skipf("%s is not set", db.env)
			}
			storetest.Each(t, func(t *testing.T, s *store.Store) { testLinked(t, s, db.kind, uri, db.setup) })
		})
	}
}

func field(view map[string]any, name string) map[string]any {
	for _, f := range view["fields"].([]any) {
		if m := f.(map[string]any); m["name"] == name {
			return m
		}
	}
	return nil
}

func idText(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprint(int64(f))
	}
	return fmt.Sprint(v)
}

func testLinked(t *testing.T, s *store.Store, kind, uri string, setup []string) {
	srv, hs := newTestServer(t, s)
	fake := &fakeClaude{}
	claude := httptest.NewServer(fake)
	defer claude.Close()
	srv.AI.BaseURL = claude.URL
	owner := newClient(t, hs.URL)
	owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
	owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
	base := "/api/apps/shop"
	owner.do("PUT", base+"/variables/DB_URI", map[string]any{"secret": true, "development": uri, "production": uri}, 200)
	ds := owner.do("POST", base+"/datasources", map[string]any{"name": "Shop DB", "kind": kind, "config": map[string]any{"db": map[string]any{"uri": "{{ env.DB_URI }}"}}}, 201)
	dsID := ds["id"].(string)
	for _, sql := range setup {
		owner.do("POST", base+"/queries/test", map[string]any{"query": map[string]any{"name": "Setup", "datasourceId": dsID, "sql": sql}, "env": "development"}, 200)
	}
	regions := owner.do("POST", base+"/tables", map[string]any{"name": "Regions", "fields": []any{map[string]any{"name": "Name", "type": "text"}}}, 201)
	regionsID := regions["id"].(string)
	north := owner.do("POST", base+"/tables/"+regionsID+"/rows", map[string]any{"Name": "North"}, 201)
	south := owner.do("POST", base+"/tables/"+regionsID+"/rows", map[string]any{"Name": "South"}, 201)

	schema := ""
	for _, x := range owner.do("GET", base+"/datasources/"+dsID+"/schema", nil, 200)["list"].([]any) {
		if m := x.(map[string]any); m["name"] == "gx_orders" {
			schema = m["schema"].(string)
		}
	}
	imp := func(source, name string) string {
		t.Helper()
		return owner.do("POST", base+"/datasources/"+dsID+"/tables", map[string]any{"schema": schema, "source": source, "name": name}, 201)["id"].(string)
	}
	// Foreign keys become links whichever table comes first.
	ordersID := imp("gx_orders", "Orders")
	customersID := imp("gx_customers", "Customers")
	linesID := imp("gx_lines", "Lines")
	ob, cb, lb := base+"/tables/"+ordersID, base+"/tables/"+customersID, base+"/tables/"+linesID
	orders := owner.do("GET", ob, nil, 200)
	if f := field(orders, "customer_id"); f == nil || f["type"] != "link" || f["linkTable"] != customersID {
		t.Fatalf("customer_id = %v", f)
	}
	if f := field(orders, "Lines"); f == nil || f["linkMany"] != true || f["linkTable"] != linesID || f["readOnly"] != true {
		t.Fatalf("Lines = %v", f)
	}
	if f := field(owner.do("GET", cb, nil, 200), "Orders"); f == nil || f["linkMany"] != true {
		t.Fatalf("Customers.Orders = %v", f)
	}
	lines := owner.do("GET", lb, nil, 200)
	if f := field(lines, "order_id"); f == nil || f["type"] != "link" || f["linkTable"] != ordersID {
		t.Fatalf("order_id = %v", f)
	}
	if lines["external"].(map[string]any)["readOnly"] != false {
		t.Fatalf("a composite key table is read only: %v", lines["external"])
	}
	// A builder links region_id to the built-in Regions, by name.
	links := orders["external"].(map[string]any)["links"].([]any)
	links = append(links, map[string]any{"column": "region_id", "table": "Regions"})
	orders = owner.do("PATCH", ob, map[string]any{"links": links}, 200)
	if f := field(orders, "region_id"); f == nil || f["linkTable"] != regionsID {
		t.Fatalf("region_id = %v", f)
	}
	owner.do("PATCH", ob, map[string]any{"links": []any{map[string]any{"column": "nope", "table": "Regions"}}}, 400)

	acme := owner.do("POST", cb+"/rows", map[string]any{"name": "Acme"}, 201)
	owner.do("POST", cb+"/rows", map[string]any{"name": "Globex"}, 201)
	// Links take a key, a label or a row.
	first := owner.do("POST", ob+"/rows", map[string]any{"title": "First", "owner": "bo@example.com", "customer_id": acme["_id"], "region_id": "North"}, 201)
	if c := first["customer_id"].(map[string]any); c["label"] != "Acme" || c["_id"] != acme["_id"] {
		t.Errorf("customer_id = %v", c)
	}
	if r := first["region_id"].(map[string]any); r["label"] != "North" || r["_id"] != north["_id"] {
		t.Errorf("region_id = %v", r)
	}
	second := owner.do("POST", ob+"/rows", map[string]any{"title": "Second", "owner": "cy@example.com", "customer_id": "Globex", "region_id": map[string]any{"_id": south["_id"]}}, 201)
	if second["customer_id"].(map[string]any)["label"] != "Globex" {
		t.Errorf("second = %v", second)
	}
	if e := owner.do("POST", ob+"/rows", map[string]any{"title": "Bad", "customer_id": 999}, 400); !strings.Contains(fmt.Sprint(e["error"]), "no row") {
		t.Errorf("bad link = %v", e)
	}
	firstID := idText(first["_id"])

	// Composite keys: the _id lists the key values.
	l1 := owner.do("POST", lb+"/rows", map[string]any{"order_id": first["_id"], "line": 1, "qty": 2}, 201)
	l2 := owner.do("POST", lb+"/rows", map[string]any{"order_id": firstID, "line": 2, "qty": 5}, 201)
	if l1["_id"] != "["+firstID+",1]" || l1["order_id"].(map[string]any)["label"] != "First" {
		t.Fatalf("line = %v", l1)
	}
	lp := lb + "/rows/" + url.PathEscape(l1["_id"].(string))
	if r := owner.do("PATCH", lp, map[string]any{"qty": 3}, 200); r["qty"] != 3.0 {
		t.Errorf("patched line = %v", r)
	}
	if r := owner.do("GET", lp, nil, 200); r["qty"] != 3.0 {
		t.Errorf("line = %v", r)
	}
	owner.do("GET", lb+"/rows/"+url.PathEscape("[1]"), nil, 404)
	owner.do("POST", lb+"/rows/delete", map[string]any{"ids": []any{l2["_id"]}}, 200)
	if page := owner.do("GET", lb+"/rows", nil, 200); page["total"] != 1.0 {
		t.Errorf("lines = %v", page)
	}
	// Reverse links list the rows pointing here.
	page := owner.do("GET", cb+"/rows?sort=name", nil, 200)
	acmeRow := page["rows"].([]any)[0].(map[string]any)
	if os := acmeRow["Orders"].([]any); len(os) != 1 || os[0].(map[string]any)["label"] != "First" {
		t.Errorf("Acme's orders = %v", acmeRow)
	}
	fo := owner.do("GET", ob+"/rows/"+firstID, nil, 200)
	if ls := fo["Lines"].([]any); len(ls) != 1 {
		t.Errorf("First's lines = %v", fo)
	}

	// CSV: links go out as labels and come back in as labels.
	csv := string(owner.raw("GET", ob+"/export", nil, 200))
	if !strings.HasPrefix(csv, "_id,id,title,owner,customer_id,region_id,amount,Lines") || !strings.Contains(csv, "First,bo@example.com,Acme,North,,#1-1") {
		t.Errorf("csv = %s", csv)
	}
	res := owner.do("POST", ob+"/import", "title,owner,customer_id,region_id,nope\nThird,bo@example.com,Acme,South,x\nBroken,bo@example.com,Nobody,North,x\n", 200)
	if res["imported"] != 1.0 || res["skipped"] != 1.0 || !strings.Contains(fmt.Sprint(res["errors"]), "line 3") || fmt.Sprint(res["ignoredFields"]) != "[nope]" {
		t.Errorf("import = %v", res)
	}

	// Row rules hold on links: Bo sees his orders, and other orders that
	// point at a customer stay unnamed.
	owner.do("PATCH", ob, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "BASIC", "delete": "BASIC",
		"rows": []any{map[string]any{"role": "BASIC", "filter": "[owner] = Current.User.Email"}}}}, 200)
	bo, boID := member(t, owner, hs.URL, "shop", "bo@example.com", "BASIC")
	if got := names(bo.do("GET", ob+"/rows?sort=title", nil, 200), "title"); got != "First,Third" {
		t.Errorf("Bo's orders = %s", got)
	}
	for _, r := range bo.do("GET", cb+"/rows?sort=name", nil, 200)["rows"].([]any) {
		m := r.(map[string]any)
		if m["name"] == "Globex" {
			if os := m["Orders"].([]any); len(os) != 1 || os[0].(map[string]any)["hidden"] != true || os[0].(map[string]any)["label"] != "" {
				t.Errorf("Globex's orders for Bo = %v", m["Orders"])
			}
		}
	}
	bo.do("POST", ob+"/rows", map[string]any{"title": "Not mine", "owner": "cy@example.com"}, 403)

	// Flows start on changes made through Bubibase and write external rows.
	flow := owner.do("POST", base+"/flows", map[string]any{
		"name":    "First line",
		"trigger": map[string]any{"type": "row_created", "table": ordersID},
		"steps": []any{
			map[string]any{"name": "Line", "type": "create_row", "inputs": map[string]any{"table": linesID, "values": []any{
				map[string]any{"field": "order_id", "value": "{{ trigger.row._id }}"},
				map[string]any{"field": "line", "value": "1"},
				map[string]any{"field": "qty", "value": "4"},
			}}},
			map[string]any{"name": "Total", "type": "update_row", "inputs": map[string]any{"table": ordersID, "rowId": "{{ trigger.row._id }}", "values": []any{
				map[string]any{"field": "amount", "value": "42.5"},
			}}},
			map[string]any{"name": "Count", "type": "find_rows", "inputs": map[string]any{"table": linesID, "filter": "[order_id] = {{ trigger.row._id }}"}},
			map[string]any{"name": "Drop", "type": "delete_row", "inputs": map[string]any{"table": linesID, "rowId": "{{ steps.Line.id }}"}},
		},
	}, 201)
	fb := base + "/flows/" + flow["id"].(string)
	owner.do("PATCH", fb, map[string]any{"enabled": true}, 200)
	flowed := bo.do("POST", ob+"/rows", map[string]any{"title": "Flowed", "owner": "bo@example.com", "customer_id": "Acme"}, 201)
	runs := waitRuns(t, owner, fb, 1)
	run := owner.do("GET", base+"/runs/"+runs[0]["id"].(string), nil, 200)
	if run["status"] != "succeeded" {
		t.Fatalf("run = %v", run)
	}
	if c := stepByName(run, "Count"); c == nil || c["output"].(map[string]any)["count"] != 1.0 {
		t.Errorf("count step = %v", c)
	}
	if d := stepByName(run, "Drop"); d == nil || d["output"].(map[string]any)["deleted"] != 1.0 {
		t.Errorf("drop step = %v", d)
	}
	if r := owner.do("GET", ob+"/rows/"+idText(flowed["_id"]), nil, 200); r["amount"] != 42.5 {
		t.Errorf("flowed = %v", r)
	}

	// The assistant reads external tables with the asker's access.
	owner.do("PATCH", "/api/ai", map[string]any{"apiKey": "sk-ant-api03-abcdefghijklmnop-WXYZ"}, 200)
	rowsSeen := ""
	fake.reset(func(n int, req map[string]any) map[string]any {
		if n == 0 {
			return toolUse("query_rows", map[string]any{"table": "Orders", "fields": []any{"title", "customer_id"}})
		}
		rowsSeen, _ = lastToolResult(req)
		return say("done")
	})
	owner.do("POST", base+"/ai/ask", map[string]any{"question": "Which orders?", "as": "user:" + boID}, 200)
	if !strings.Contains(rowsSeen, "First") || strings.Contains(rowsSeen, "Second") || !strings.Contains(rowsSeen, "Acme") {
		t.Errorf("rows seen as Bo = %s", rowsSeen)
	}
	if sys := systemText(fake.requests[0]); !strings.Contains(sys, "external") || !strings.Contains(sys, "gx_orders") {
		t.Errorf("system prompt lacks the external tables")
	}
	// Built-in tables cannot link to external ones in a draft.
	fake.reset(func(n int, req map[string]any) map[string]any {
		fields := []any{map[string]any{"name": "Text", "type": "text"}}
		if n == 0 {
			fields = []any{map[string]any{"name": "Order", "type": "link", "linkTable": "Orders"}}
		}
		return toolUse("propose_draft", map[string]any{"summary": "s", "changes": []any{}, "draft": map[string]any{"tables": []any{
			map[string]any{"name": "Notes", "fields": fields},
		}}})
	})
	owner.do("POST", base+"/ai/draft", map[string]any{"kind": "table", "prompt": "notes on orders"}, 200)
	if res, _ := lastToolResult(fake.requests[1]); !strings.Contains(res, "external database") {
		t.Errorf("draft check = %s", res)
	}

	// Links to a table that goes are dropped.
	owner.do("DELETE", base+"/tables/"+regionsID, nil, 200)
	owner.do("DELETE", cb, nil, 200)
	orders = owner.do("GET", ob, nil, 200)
	if f := field(orders, "region_id"); f["type"] != "number" {
		t.Errorf("region_id after Regions went = %v", f)
	}
	if f := field(orders, "customer_id"); f["type"] != "number" {
		t.Errorf("customer_id after Customers went = %v", f)
	}
}
