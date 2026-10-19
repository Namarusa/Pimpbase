package api

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// upstream is a fake third-party API: a JSON sign-in, a JSON list that
// needs its token, and a SOAP-style XML search.
func upstream(t *testing.T, logins *atomic.Int32) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		logins.Add(1)
		var in struct{ User, Key string }
		json.NewDecoder(r.Body).Decode(&in)
		token := map[string]string{"dev-key": "tok-dev", "prod-key": "tok-prod"}[in.Key]
		if token == "" || in.User != "ada" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token":"` + token + `","expires":3600}`))
	})
	mux.HandleFunc("GET /items/{category}", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"category": r.PathValue("category"),
			"token":    r.Header.Get("Authorization"),
			"limit":    r.URL.Query().Get("limit"),
			"tags":     r.URL.Query()["tag"],
			"items":    []any{map[string]any{"id": 1, "name": "Dune", "price": 9.5}, map[string]any{"id": 2, "name": "Emma", "price": 4}},
		}})
	})
	mux.HandleFunc("POST /soap", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Name string   `xml:"Name"`
			IDs  []string `xml:"Id"`
		}
		if err := xml.Unmarshal(body, &in); err != nil || r.Header.Get("SOAPAction") != "Search" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		var items strings.Builder
		for _, id := range in.IDs {
			items.WriteString(`<m:Item id="` + id + `"><m:Name>` + xmlText(in.Name) + `</m:Name><m:Stock>3</m:Stock></m:Item>`)
		}
		w.Write([]byte(`<?xml version="1.0"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:m="urn:shop"><soap:Body><m:Result>` + items.String() + `</m:Result></soap:Body></soap:Envelope>`))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "query": r.URL.RawQuery, "contentType": r.Header.Get("Content-Type"), "body": string(body), "key": r.Header.Get("X-Api-Key")})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func TestQueries(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		var logins atomic.Int32
		up := upstream(t, &logins)
		ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>builder")}}
		srv := httptest.NewServer(New(s, ui, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
		defer srv.Close()

		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
		base := "/api/apps/shop"

		// Variables: a secret key with a value per environment.
		owner.do("PUT", base+"/variables/API_URL", map[string]any{"production": up.URL}, 200)
		owner.do("PUT", base+"/variables/API_USER", map[string]any{"development": "ada", "production": "ada"}, 200)
		v := owner.do("PUT", base+"/variables/API_KEY", map[string]any{"secret": true, "development": "dev-key", "production": "prod-key"}, 200)
		if v["development"] != "" || v["hasDevelopment"] != true {
			t.Errorf("secret variable = %v", v)
		}
		owner.do("PUT", base+"/variables/bad-name", map[string]any{"production": "x"}, 400)
		vars := owner.do("GET", base+"/variables", nil, 200)["list"].([]any)
		if len(vars) != 3 || vars[0].(map[string]any)["production"] != "" || vars[1].(map[string]any)["production"] != up.URL {
			t.Errorf("variables = %v", vars)
		}

		owner.do("POST", base+"/datasources", map[string]any{"name": "Shop API", "config": map[string]any{"baseUrl": "ftp://x"}}, 400)
		ds := owner.do("POST", base+"/datasources", map[string]any{"name": "Shop API", "config": map[string]any{
			"baseUrl": "{{ env.API_URL }}", "headers": []any{map[string]any{"key": "X-Client", "value": "bubibase"}},
		}}, 201)
		dsID := ds["id"].(string)
		if ds["config"].(map[string]any)["timeoutSeconds"].(float64) != 30 {
			t.Errorf("datasource = %v", ds)
		}

		owner.do("POST", base+"/queries", map[string]any{
			"name": "Login", "datasourceId": dsID, "method": "POST", "path": "/login", "bodyType": "json",
			"body": `{"user": "{{ env.API_USER }}", "key": "{{ env.API_KEY }}"}`, "cacheSeconds": 60,
		}, 201)
		items := owner.do("POST", base+"/queries", map[string]any{
			"name": "Items", "datasourceId": dsID, "path": "/items/{{ params.category }}",
			"query":      []any{map[string]any{"key": "limit", "value": "{{ params.limit }}"}, map[string]any{"key": "tag", "value": "{{ params.tags }}"}, map[string]any{"key": "off", "value": "x", "off": true}},
			"headers":    []any{map[string]any{"key": "Authorization", "value": "Bearer {{ queries.Login.body.token }}"}},
			"parameters": []any{map[string]any{"name": "category", "default": "books"}, map[string]any{"name": "limit", "type": "number", "default": "2"}, map[string]any{"name": "tags", "type": "json", "default": `["a","b"]`}},
			"resultPath": "data.items",
		}, 201)
		if items["role"] != "BASIC" || items["method"] != "GET" {
			t.Errorf("query defaults = %v", items)
		}
		owner.do("POST", base+"/queries", map[string]any{"name": "items", "datasourceId": dsID}, 400)
		owner.do("POST", base+"/queries", map[string]any{"name": "Bad", "datasourceId": dsID, "path": "/x/{{ params.id"}, 400)
		owner.do("POST", base+"/queries", map[string]any{"name": "Bad", "datasourceId": "ds_nope"}, 400)
		itemsID := items["id"].(string)

		// The builder runs with development values; the token comes from
		// the Login query, which runs first.
		res := owner.do("POST", base+"/queries/"+itemsID+"/run", map[string]any{"env": "development", "params": map[string]any{"category": "old books/rare"}}, 200)
		if res["ok"] != true || len(res["rows"].([]any)) != 2 || res["format"] != "json" {
			t.Fatalf("run = %v", res)
		}
		body := res["body"].(map[string]any)["data"].(map[string]any)
		if body["category"] != "old books/rare" || body["token"] != "Bearer tok-dev" || body["limit"] != "2" || len(body["tags"].([]any)) != 2 {
			t.Errorf("upstream saw %v", body)
		}
		fields := res["fields"].([]any)
		if len(fields) != 3 || fields[1].(map[string]any)["name"] != "name" || fields[2].(map[string]any)["type"] != "number" {
			t.Errorf("fields = %v", fields)
		}
		req := res["request"].(map[string]any)
		if !strings.Contains(req["url"].(string), "/items/old%20books%2Frare?limit=2&tag=a&tag=b") || strings.Contains(req["url"].(string), "off") {
			t.Errorf("request url = %v", req["url"])
		}

		// Published apps use production values; Login is cached per request.
		res = owner.do("POST", base+"/queries/Items/run", nil, 200)
		if res["body"].(map[string]any)["data"].(map[string]any)["token"] != "Bearer tok-prod" {
			t.Errorf("production run = %v", res["body"])
		}
		before := logins.Load()
		owner.do("POST", base+"/queries/Items/run", nil, 200)
		if logins.Load() != before {
			t.Errorf("the cached login ran again")
		}

		// The echoed request hides secrets.
		login := owner.do("POST", base+"/queries/Login/run", map[string]any{"env": "development"}, 200)
		if b := login["request"].(map[string]any)["body"].(string); strings.Contains(b, "dev-key") || !strings.Contains(b, "••••••") {
			t.Errorf("echoed body = %s", b)
		}

		// XML: a request body built from bindings, a response read as data.
		soap := owner.do("POST", base+"/queries", map[string]any{
			"name": "Search", "datasourceId": dsID, "method": "POST", "path": "/soap", "bodyType": "xml", "role": "PUBLIC",
			"headers":    []any{map[string]any{"key": "SOAPAction", "value": "Search"}},
			"body":       `<?xml version="1.0"?><Search><Name>{{ params.name }}</Name>{{ params.ids | xml:"Id" }}</Search>`,
			"parameters": []any{map[string]any{"name": "name"}, map[string]any{"name": "ids", "type": "json", "default": "[]"}},
			"resultPath": "Envelope.Body.Result.Item",
		}, 201)
		res = owner.do("POST", base+"/queries/"+soap["id"].(string)+"/run", map[string]any{"params": map[string]any{"name": "Fish & <Chips>", "ids": []any{"7", "8"}}}, 200)
		rows := res["rows"].([]any)
		if res["format"] != "xml" || len(rows) != 2 || rows[1].(map[string]any)["@id"] != "8" || rows[0].(map[string]any)["Name"] != "Fish & <Chips>" {
			t.Fatalf("xml run = %v", res)
		}
		if f := res["fields"].([]any); f[2].(map[string]any)["name"] != "Stock" || f[2].(map[string]any)["type"] != "number" {
			t.Errorf("xml fields = %v", f)
		}
		// One item is still a list of rows.
		res = owner.do("POST", base+"/queries/Search/run", map[string]any{"params": map[string]any{"name": "x", "ids": []any{"1"}}}, 200)
		if len(res["rows"].([]any)) != 1 {
			t.Errorf("one xml row = %v", res["rows"])
		}

		// Test runs use unsaved definitions and test values.
		res = owner.do("POST", base+"/queries/test", map[string]any{"query": map[string]any{
			"name": "Draft", "datasourceId": dsID, "method": "PUT", "path": "/echo", "bodyType": "form",
			"form":       []any{map[string]any{"key": "q", "value": "{{ params.q }} & co"}, map[string]any{"key": "email", "value": "{{ user.email }}"}},
			"parameters": []any{map[string]any{"name": "q", "default": "dflt", "test": "tested"}},
		}}, 200)
		echo := res["body"].(map[string]any)
		if echo["method"] != "PUT" || echo["body"] != "email=ada%40example.com&q=tested+%26+co" || echo["contentType"] != "application/x-www-form-urlencoded" {
			t.Errorf("form echo = %v", echo)
		}
		res = owner.do("POST", base+"/queries/test", map[string]any{"query": map[string]any{
			"name": "Draft", "datasourceId": dsID, "method": "POST", "path": "/echo", "bodyType": "json", "body": `{"n": {{ params.n }}}`,
			"parameters": []any{map[string]any{"name": "n", "type": "number", "test": "4.5"}},
		}}, 200)
		if res["body"].(map[string]any)["body"] != `{"n": 4.5}` {
			t.Errorf("json echo = %v", res["body"])
		}
		owner.do("POST", base+"/queries/test", map[string]any{"query": map[string]any{
			"name": "Draft", "datasourceId": dsID, "method": "POST", "path": "/echo", "bodyType": "json", "body": `{"n": {{ params.n }}`,
		}}, 400)
		owner.do("POST", base+"/queries/test", map[string]any{"query": map[string]any{
			"name": "Draft", "datasourceId": dsID, "method": "POST", "path": "/echo", "bodyType": "xml", "body": `<a>{{ params.n | raw }}</a>`,
			"parameters": []any{map[string]any{"name": "n", "test": "<b>"}},
		}}, 400)

		// Queries that read each other in a circle fail clearly.
		owner.do("POST", base+"/queries", map[string]any{"name": "A", "datasourceId": dsID, "path": "/echo?x={{ queries.B.status }}"}, 201)
		owner.do("POST", base+"/queries", map[string]any{"name": "B", "datasourceId": dsID, "path": "/echo?x={{ queries.A.status }}"}, 201)
		if e := owner.do("POST", base+"/queries/A/run", nil, 400); !strings.Contains(e["error"].(string), "A → B → A") {
			t.Errorf("cycle error = %v", e)
		}

		// API key sign-in from the data source, read from a variable.
		owner.do("PATCH", base+"/datasources/"+dsID, map[string]any{"config": map[string]any{
			"baseUrl": "{{ env.API_URL }}", "auth": map[string]any{"type": "header", "name": "X-Api-Key", "value": "{{ env.API_KEY }}"},
		}}, 200)
		res = owner.do("POST", base+"/queries/test", map[string]any{"query": map[string]any{"name": "Draft", "datasourceId": dsID, "path": "/echo"}}, 200)
		if res["body"].(map[string]any)["key"] != "dev-key" {
			t.Errorf("api key = %v", res["body"])
		}
		if h := res["request"].(map[string]any)["headers"].([]any); !strings.Contains(toJSON(h), `"X-Api-Key","value":"••••••"`) {
			t.Errorf("echoed headers = %s", toJSON(h))
		}

		// App users run queries their role allows, without the echo.
		owner.do("POST", "/api/users", map[string]any{"email": "bo@example.com", "password": "another secret", "role": "member"}, 201)
		var bo string
		for _, u := range owner.do("GET", "/api/users", nil, 200)["list"].([]any) {
			if u.(map[string]any)["email"] == "bo@example.com" {
				bo = u.(map[string]any)["id"].(string)
			}
		}
		owner.do("PUT", base+"/members/"+bo, map[string]any{"role": "BASIC"}, 200)
		member := newClient(t, srv.URL)
		member.do("POST", "/api/auth/login", map[string]any{"email": "bo@example.com", "password": "another secret"}, 200)
		res = member.do("POST", base+"/queries/Search/run", map[string]any{"env": "development", "params": map[string]any{"name": "x", "ids": []any{"1"}}}, 200)
		if _, ok := res["request"]; ok {
			t.Errorf("members see the request: %v", res)
		}
		member.do("GET", base+"/queries", nil, 403)
		member.do("GET", base+"/variables", nil, 403)
		anon := newClient(t, srv.URL)
		anon.do("POST", base+"/queries/Items/run", nil, 401)
		anon.do("POST", base+"/queries/Search/run", map[string]any{"params": map[string]any{"name": "x"}}, 200)
		anon.do("POST", base+"/queries/test", map[string]any{"query": map[string]any{"name": "Draft", "datasourceId": dsID}}, 401)

		owner.do("POST", base+"/screens", map[string]any{"name": "Home", "route": "/", "role": "PUBLIC"}, 201)
		rt := anon.do("GET", base+"/runtime", nil, 200)
		if qs := rt["queries"].([]any); len(qs) != 1 || qs[0].(map[string]any)["name"] != "Search" {
			t.Errorf("anonymous runtime queries = %v", qs)
		}

		// Deleting a data source deletes its queries.
		owner.do("DELETE", base+"/datasources/"+dsID, nil, 200)
		if list := owner.do("GET", base+"/queries", nil, 200)["list"].([]any); len(list) != 0 {
			t.Errorf("queries left = %v", list)
		}
		owner.do("DELETE", base+"/variables/API_KEY", nil, 200)
		owner.do("DELETE", base, nil, 200)
	})
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
