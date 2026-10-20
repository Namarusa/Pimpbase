package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bubibase/bubibase/internal/sso/ssotest"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

func newTestServer(t *testing.T, s *store.Store) (*Server, *httptest.Server) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>builder")}}
	srv := New(s, ui, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return srv, hs
}

// member adds a workspace member with an app role and signs them in.
func member(t *testing.T, owner *client, base, app, email, role string) (*client, string) {
	t.Helper()
	u := owner.do("POST", "/api/users", map[string]any{"email": email, "password": "a long secret", "role": "member"}, 201)
	id := u["id"].(string)
	if role != "" {
		owner.do("PUT", "/api/apps/"+app+"/members/"+id, map[string]any{"role": role}, 200)
	}
	c := newClient(t, base)
	c.do("POST", "/api/auth/login", map[string]any{"email": email, "password": "a long secret"}, 200)
	return c, id
}

func titles(page map[string]any) string {
	var out []string
	for _, r := range page["rows"].([]any) {
		out = append(out, r.(map[string]any)["Title"].(string))
	}
	return strings.Join(out, ",")
}

func TestRolesAndRowRules(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		_, hs := newTestServer(t, s)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Deals"}, 201)
		base := "/api/apps/deals"

		// Built-in roles come first; custom roles inherit one other role.
		roles := owner.do("GET", base+"/roles", nil, 200)["list"].([]any)
		if len(roles) != 4 || roles[0].(map[string]any)["id"] != "ADMIN" {
			t.Fatalf("roles = %v", roles)
		}
		sales := owner.do("POST", base+"/roles", map[string]any{"name": "Sales team", "inherits": "BASIC", "color": "#16a34a"}, 201)
		if sales["id"] != "SALES_TEAM" {
			t.Fatalf("role = %v", sales)
		}
		owner.do("POST", base+"/roles", map[string]any{"name": "Loop", "inherits": "NOPE"}, 400)
		owner.do("POST", base+"/roles", map[string]any{"id": "LEADS", "name": "Leads", "inherits": "SALES_TEAM"}, 201)
		owner.do("PATCH", base+"/roles/SALES_TEAM", map[string]any{"name": "Sales team", "inherits": "LEADS"}, 400) // a loop
		owner.do("PATCH", base+"/roles/BASIC", map[string]any{"name": "x", "inherits": "PUBLIC"}, 400)

		tbl := owner.do("POST", base+"/tables", map[string]any{"name": "Deals", "fields": []any{
			map[string]any{"name": "Title", "type": "text"},
			map[string]any{"name": "Owner", "type": "text"},
			map[string]any{"name": "Team", "type": "text"},
			map[string]any{"name": "Amount", "type": "number"},
		}}, 201)
		tb := base + "/tables/" + tbl["id"].(string)
		for _, r := range []map[string]any{
			{"Title": "Acme", "Owner": "bo@example.com", "Team": "north", "Amount": 100},
			{"Title": "Globex", "Owner": "cy@example.com", "Team": "south", "Amount": 200},
			{"Title": "Initech", "Owner": "bo@example.com", "Team": "south", "Amount": 300},
		} {
			owner.do("POST", tb+"/rows", r, 201)
		}

		// Permissions name roles the app has, and rules must be conditions.
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "GHOST", "delete": "POWER"}}, 400)
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "SALES_TEAM", "delete": "POWER",
			"rows": []any{map[string]any{"role": "BASIC", "filter": "[Amount] + 1"}}}}, 400)
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "SALES_TEAM", "delete": "POWER",
			"rows": []any{map[string]any{"role": "ADMIN", "filter": "TRUE"}}}}, 400)
		got := owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "SALES_TEAM", "delete": "POWER",
			"rows": []any{
				map[string]any{"role": "BASIC", "filter": "[Owner] = Current.User.Email"},
				map[string]any{"role": "POWER", "filter": ""},
				map[string]any{"role": "LEADS", "filter": "CONTAINS(Current.User.Groups, [Team]) OR [Owner] = Current.User.Email"},
			}}}, 200)
		if got["permissions"].(map[string]any)["delete"] != "POWER" {
			t.Errorf("table = %v", got)
		}

		bo, _ := member(t, owner, hs.URL, "deals", "bo@example.com", "BASIC")
		cy, _ := member(t, owner, hs.URL, "deals", "cy@example.com", "SALES_TEAM")
		di, _ := member(t, owner, hs.URL, "deals", "di@example.com", "POWER")

		// Basic users see their own rows only, filtered in SQL.
		page := bo.do("GET", tb+"/rows?sort=Title", nil, 200)
		if titles(page) != "Acme,Initech" || page["engine"] != "sql" {
			t.Errorf("bo sees %v", page)
		}
		if page := bo.do("GET", tb+"/rows?filter="+urlq("[Amount] > 150"), nil, 200); titles(page) != "Initech" {
			t.Errorf("bo filtered sees %v", titles(page))
		}
		bo.do("GET", tb+"/rows/2", nil, 404)
		bo.do("POST", tb+"/rows", map[string]any{"Title": "Mine", "Owner": "bo@example.com"}, 403)

		// Sales inherits Basic's rule and may write, but only inside it.
		if page := cy.do("GET", tb+"/rows", nil, 200); titles(page) != "Globex" {
			t.Errorf("cy sees %v", titles(page))
		}
		cy.do("POST", tb+"/rows", map[string]any{"Title": "Hooli", "Owner": "cy@example.com"}, 201)
		cy.do("POST", tb+"/rows", map[string]any{"Title": "Sneaky", "Owner": "bo@example.com"}, 403)
		cy.do("PATCH", tb+"/rows/1", map[string]any{"Amount": 1}, 404)
		cy.do("PATCH", tb+"/rows/2", map[string]any{"Owner": "bo@example.com"}, 403)
		cy.do("PATCH", tb+"/rows/2", map[string]any{"Amount": 250}, 200)
		cy.do("POST", tb+"/rows/delete", map[string]any{"ids": []int{2}}, 403)
		imp := cy.do("POST", tb+"/import", "Title,Owner\nOk,cy@example.com\nNo,bo@example.com\n", 200)
		if imp["imported"] != 1.0 || imp["skipped"] != 1.0 {
			t.Errorf("import = %v", imp)
		}

		// Power users opened every row again, and may delete.
		page = di.do("GET", tb+"/rows", nil, 200)
		if page["total"] != 5.0 {
			t.Errorf("di sees %v", page["total"])
		}
		// Ids differ by database: refused inserts use up Postgres serials.
		var okID any
		for _, r := range page["rows"].([]any) {
			if r.(map[string]any)["Title"] == "Ok" {
				okID = r.(map[string]any)["_id"]
			}
		}
		if del := di.do("POST", tb+"/rows/delete", map[string]any{"ids": []any{okID}}, 200); del["deleted"] != 1.0 {
			t.Errorf("delete = %v", del)
		}

		// A row rule can use the groups single sign-on reported.
		owner.do("POST", "/api/users", map[string]any{"email": "ed@example.com", "password": "a long secret", "role": "member"}, 201)
		ed := newClient(t, hs.URL)
		ed.do("POST", "/api/auth/login", map[string]any{"email": "ed@example.com", "password": "a long secret"}, 200)
		edID := ""
		for _, u := range owner.do("GET", "/api/users", nil, 200)["list"].([]any) {
			if u.(map[string]any)["email"] == "ed@example.com" {
				edID = u.(map[string]any)["id"].(string)
			}
		}
		owner.do("PUT", base+"/members/"+edID, map[string]any{"role": "LEADS"}, 200)
		if _, err := s.Meta.Exec(t.Context(), `UPDATE users SET sso_groups = ? WHERE id = ?`, `["south"]`, edID); err != nil {
			t.Fatal(err)
		}
		if page := ed.do("GET", tb+"/rows?sort=Title", nil, 200); titles(page) != "Globex,Initech" {
			t.Errorf("ed sees %v", titles(page))
		}

		// Removing a role moves its members to the role it inherited.
		owner.do("DELETE", base+"/roles/LEADS", nil, 200)
		if page := ed.do("GET", tb+"/rows", nil, 200); titles(page) != "" {
			t.Errorf("ed now sees %v", titles(page))
		}
		for _, m := range owner.do("GET", base+"/members", nil, 200)["list"].([]any) {
			if m.(map[string]any)["userId"] == edID && m.(map[string]any)["role"] != "SALES_TEAM" {
				t.Errorf("ed's role = %v", m)
			}
		}
		owner.do("DELETE", base+"/roles/BASIC", nil, 400)
		owner.do("PUT", base+"/members/"+edID, map[string]any{"role": "LEADS"}, 400)
	})
}

func TestScreenVisibility(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		_, hs := newTestServer(t, s)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
		base := "/api/apps/shop"
		owner.do("POST", base+"/roles", map[string]any{"id": "STAFF", "name": "Staff", "inherits": "BASIC"}, 201)

		def := map[string]any{"id": "root", "type": "container", "props": map[string]any{}, "children": []any{
			map[string]any{"id": "hello", "type": "heading", "props": map[string]any{"text": "Hello"}, "children": []any{}},
			map[string]any{"id": "staff", "type": "button", "props": map[string]any{"text": "Refund", "roles": []any{"STAFF"}}, "children": []any{}},
			map[string]any{"id": "admin", "type": "container", "props": map[string]any{"roles": []any{"ADMIN"}}, "children": []any{
				map[string]any{"id": "secret", "type": "heading", "props": map[string]any{"text": "Margins"}, "children": []any{}},
			}},
		}}
		owner.do("POST", base+"/screens", map[string]any{"name": "Home", "route": "/", "role": "BASIC", "definition": def}, 201)
		owner.do("POST", base+"/screens", map[string]any{"name": "Till", "route": "/till", "role": "STAFF"}, 201)
		owner.do("POST", base+"/screens", map[string]any{"name": "Bad", "route": "/bad", "role": "GHOST"}, 400)
		bad := map[string]any{"id": "root", "type": "container", "props": map[string]any{}, "children": []any{
			map[string]any{"id": "x", "type": "heading", "props": map[string]any{"roles": []any{"GHOST"}}, "children": []any{}},
		}}
		owner.do("POST", base+"/screens", map[string]any{"name": "Bad", "route": "/bad", "definition": bad}, 400)

		ids := func(rt map[string]any) string {
			var out []string
			for _, sc := range rt["screens"].([]any) {
				out = append(out, sc.(map[string]any)["name"].(string)+":"+jsonString(sc.(map[string]any)["definition"]))
			}
			return strings.Join(out, " | ")
		}
		basic, _ := member(t, owner, hs.URL, "shop", "bo@example.com", "BASIC")
		got := ids(basic.do("GET", base+"/runtime", nil, 200))
		if strings.Contains(got, "Till") || strings.Contains(got, "Refund") || strings.Contains(got, "Margins") || !strings.Contains(got, "Hello") {
			t.Errorf("basic runtime = %s", got)
		}
		staff, _ := member(t, owner, hs.URL, "shop", "cy@example.com", "STAFF")
		got = ids(staff.do("GET", base+"/runtime", nil, 200))
		if !strings.Contains(got, "Till") || !strings.Contains(got, "Refund") || strings.Contains(got, "Margins") {
			t.Errorf("staff runtime = %s", got)
		}
		rt := owner.do("GET", base+"/runtime", nil, 200)
		if got := ids(rt); !strings.Contains(got, "Margins") || rt["roles"] == nil {
			t.Errorf("builder runtime = %s", got)
		}

		// Builders can preview the app as a role.
		req, _ := http.NewRequest("GET", hs.URL+base+"/runtime", nil)
		req.Header.Set(previewHeader, "BASIC")
		res, err := owner.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if strings.Contains(string(body), "Margins") || !strings.Contains(string(body), `"preview":true`) {
			t.Errorf("preview = %s", body)
		}
		// Others cannot use the preview header to gain anything.
		req, _ = http.NewRequest("GET", hs.URL+base+"/runtime", nil)
		req.Header.Set(previewHeader, "ADMIN")
		res, _ = basic.http.Do(req)
		body, _ = io.ReadAll(res.Body)
		res.Body.Close()
		if strings.Contains(string(body), "Margins") {
			t.Errorf("member preview leaked: %s", body)
		}
	})
}

func TestAPIKeysAndDisabledUsers(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		_, hs := newTestServer(t, s)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		app := owner.do("POST", "/api/apps", map[string]any{"name": "Ops"}, 201)
		other := owner.do("POST", "/api/apps", map[string]any{"name": "Other"}, 201)
		base := "/api/apps/ops"
		tbl := owner.do("POST", base+"/tables", map[string]any{"name": "Tasks", "fields": []any{
			map[string]any{"name": "Title", "type": "text"}, map[string]any{"name": "Owner", "type": "text"},
		}}, 201)
		tb := base + "/tables/" + tbl["id"].(string)
		owner.do("POST", tb+"/rows", map[string]any{"Title": "A", "Owner": "bo@example.com"}, 201)
		owner.do("POST", tb+"/rows", map[string]any{"Title": "B", "Owner": "cy@example.com"}, 201)
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "BASIC", "delete": "POWER",
			"rows": []any{map[string]any{"role": "BASIC", "filter": "[Owner] = Current.User.Email"}}}}, 200)

		bo, boID := member(t, owner, hs.URL, "ops", "bo@example.com", "POWER")
		// A key may only carry a role its maker holds.
		bo.do("POST", "/api/auth/keys", map[string]any{"name": "x", "appId": app["id"], "role": "ADMIN"}, 403)
		k := bo.do("POST", "/api/auth/keys", map[string]any{"name": "sync", "appId": app["id"], "role": "BASIC", "expiresInDays": 30}, 201)
		if k["key"] == nil || k["expiresAt"] == nil || k["role"] != "BASIC" {
			t.Fatalf("key = %v", k)
		}
		bot := newClient(t, hs.URL)
		bot.key = k["key"].(string)
		if page := bot.do("GET", tb+"/rows", nil, 200); titles(page) != "A" {
			t.Errorf("BASIC key sees %v", titles(page))
		}
		bot.do("POST", tb+"/rows/delete", map[string]any{"ids": []int{1}}, 403)
		bot.do("GET", "/api/apps/"+other["slug"].(string)+"/tables", nil, 403)
		bot.do("POST", "/api/auth/keys", map[string]any{"name": "more"}, 403)
		if apps := bot.do("GET", "/api/apps", nil, 200)["list"].([]any); len(apps) != 1 {
			t.Errorf("scoped key lists %v", apps)
		}
		keys := bo.do("GET", "/api/auth/keys", nil, 200)["list"].([]any)
		if len(keys) != 1 || keys[0].(map[string]any)["lastUsedAt"] == nil || keys[0].(map[string]any)["key"] != nil {
			t.Errorf("keys = %v", keys)
		}

		// A builder's app key loses builder powers.
		ok := owner.do("POST", "/api/auth/keys", map[string]any{"name": "app only", "appId": app["id"]}, 201)
		ob := newClient(t, hs.URL)
		ob.key = ok["key"].(string)
		ob.do("GET", tb+"/rows", nil, 200)
		ob.do("POST", base+"/tables", map[string]any{"name": "Nope"}, 403)
		ob.do("GET", "/api/users", nil, 200)
		ob.do("POST", "/api/users", map[string]any{"email": "x@example.com", "password": "a long secret"}, 403)

		// Losing the role stops the key.
		owner.do("PUT", base+"/members/"+boID, map[string]any{"role": "NONE"}, 200)
		bot.do("GET", tb+"/rows", nil, 403)
		owner.do("PUT", base+"/members/"+boID, map[string]any{"role": "BASIC"}, 200)
		bot.do("GET", tb+"/rows", nil, 200)

		// Switching a user off ends their sessions and keys.
		owner.do("PATCH", "/api/users/"+boID, map[string]any{"disabled": true}, 200)
		bo.do("GET", "/api/auth/me", nil, 401)
		bot.do("GET", tb+"/rows", nil, 401)
		bo.do("POST", "/api/auth/login", map[string]any{"email": "bo@example.com", "password": "a long secret"}, 403)
		bo.do("POST", "/api/auth/login", map[string]any{"email": "bo@example.com", "password": "wrong password"}, 401)
		me := owner.do("GET", "/api/auth/me", nil, 200)
		owner.do("PATCH", "/api/users/"+me["id"].(string), map[string]any{"disabled": true}, 400)
		owner.do("PATCH", "/api/users/"+boID, map[string]any{"disabled": false}, 200)
		bo.do("POST", "/api/auth/login", map[string]any{"email": "bo@example.com", "password": "a long secret"}, 200)
		bot.do("GET", tb+"/rows", nil, 200)

		// Revoked keys stop working.
		bo.do("DELETE", "/api/auth/keys/"+k["id"].(string), nil, 200)
		bot.do("GET", tb+"/rows", nil, 401)
	})
}

func TestFlowsRunAsUser(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		_ = srv
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Desk"}, 201)
		base := "/api/apps/desk"
		tbl := owner.do("POST", base+"/tables", map[string]any{"name": "Log", "fields": []any{map[string]any{"name": "Note", "type": "text"}}}, 201)
		owner.do("PATCH", base+"/tables/"+tbl["id"].(string), map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "POWER", "delete": "POWER"}}, 200)
		flow := func(name, runAs string) string {
			f := owner.do("POST", base+"/flows", map[string]any{"name": name, "enabled": true, "runAs": runAs,
				"trigger": map[string]any{"type": "manual", "role": "BASIC"},
				"steps": []any{map[string]any{"name": "Add", "type": "create_row", "inputs": map[string]any{"table": tbl["id"], "values": map[string]any{"Note": "hi"}}}},
			}, 201)
			if f["runAs"] != runAs {
				t.Fatalf("flow = %v", f)
			}
			return f["id"].(string)
		}
		asFlow, asUser := flow("As flow", "flow"), flow("As user", "user")
		owner.do("POST", base+"/flows", map[string]any{"name": "Bad", "runAs": "root"}, 400)
		owner.do("POST", base+"/flows", map[string]any{"name": "Bad", "trigger": map[string]any{"type": "manual", "role": "GHOST"}}, 400)

		bo, _ := member(t, owner, hs.URL, "desk", "bo@example.com", "BASIC")
		if run := bo.do("POST", base+"/flows/"+asFlow+"/run", map[string]any{}, 200); run["status"] != "succeeded" {
			t.Errorf("as flow = %v", run)
		}
		run := bo.do("POST", base+"/flows/"+asUser+"/run", map[string]any{}, 200)
		if run["status"] != "failed" || !strings.Contains(run["error"].(string), "POWER") {
			t.Errorf("as user = %v", run)
		}
		if run := owner.do("POST", base+"/flows/"+asUser+"/run", map[string]any{}, 200); run["status"] != "succeeded" {
			t.Errorf("builder as user = %v", run)
		}
	})
}

func TestSingleSignOn(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		_, hs := newTestServer(t, s)
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		app := owner.do("POST", "/api/apps", map[string]any{"name": "Crm"}, 201)
		owner.do("POST", "/api/apps/crm/roles", map[string]any{"id": "SALES", "name": "Sales", "inherits": "BASIC"}, 201)
		owner.do("POST", "/api/users", map[string]any{"email": "old@corp.example", "password": "a long secret", "role": "member"}, 201)

		idp := ssotest.New("", "bubibase", "s3cret",
			ssotest.User{Subject: "u1", Email: "Kim@corp.example", EmailVerified: true, Name: "Kim Lee", Groups: []string{"sales", "staff"}},
			ssotest.User{Subject: "u2", Email: "old@corp.example", EmailVerified: false, Name: "Old Timer"},
			ssotest.User{Subject: "u3", Email: "eve@elsewhere.example", EmailVerified: true, Name: "Eve"},
			ssotest.User{Subject: "u4", Email: "lee@corp.example", EmailVerified: true, Name: "Lee", Groups: []string{"it"}},
		)
		ids := httptest.NewServer(idp.Handler())
		defer ids.Close()
		idp.Issuer = ids.URL

		providers := owner.do("GET", "/api/sso/providers", nil, 200)
		if providers["redirectUrl"] != hs.URL+"/api/sso/callback" {
			t.Errorf("redirect = %v", providers["redirectUrl"])
		}
		owner.do("POST", "/api/sso/providers", map[string]any{"kind": "oidc", "issuer": "not a url", "clientId": "x", "clientSecret": "y"}, 400)
		owner.do("POST", "/api/sso/providers", map[string]any{"kind": "oidc", "issuer": ids.URL, "clientId": "bubibase"}, 400)
		owner.do("POST", "/api/sso/providers", map[string]any{"kind": "oidc", "issuer": ids.URL, "clientId": "bubibase", "clientSecret": "s3cret",
			"mappings": []any{map[string]any{"claim": "group", "value": "sales", "apps": []any{map[string]any{"appId": app["id"], "role": "GHOST"}}}}}, 400)
		p := owner.do("POST", "/api/sso/providers", map[string]any{
			"kind": "oidc", "name": "Corp login", "issuer": ids.URL, "clientId": "bubibase", "clientSecret": "s3cret",
			"scopes": []any{"groups"}, "allowedDomains": []any{"corp.example"}, "autoCreate": true,
			"mappings": []any{
				map[string]any{"claim": "group", "value": "sales", "apps": []any{map[string]any{"appId": app["id"], "role": "SALES"}}},
				map[string]any{"claim": "group", "value": "it", "workspaceRole": "builder"},
				map[string]any{"claim": "email_domain", "value": "corp.example", "apps": []any{map[string]any{"appId": app["id"], "role": "BASIC"}}},
			},
		}, 201)
		if p["hasSecret"] != true || p["clientSecret"] != nil || jsonString(p["scopes"]) != `["openid","email","profile","groups"]` {
			t.Fatalf("provider = %v", p)
		}
		pid := p["id"].(string)
		test := owner.do("POST", "/api/sso/providers/"+pid+"/test", map[string]any{"email": "x@corp.example", "groups": []any{"sales"}}, 200)
		if jsonString(test["matched"]) != "[1,3]" || test["apps"].(map[string]any)[app["id"].(string)] != "SALES" {
			t.Errorf("test = %v", test)
		}
		pub := newClient(t, hs.URL).do("GET", "/api/auth/providers", nil, 200)["list"].([]any)
		if len(pub) != 1 || pub[0].(map[string]any)["name"] != "Corp login" || pub[0].(map[string]any)["clientId"] != nil {
			t.Errorf("public providers = %v", pub)
		}

		// signIn runs the whole browser round trip as one user.
		signIn := func(sub, ret string) (*client, *url.URL) {
			idp.Pick = sub
			c := newClient(t, hs.URL)
			res, err := c.http.Get(hs.URL + "/api/sso/" + pid + "/start?return=" + url.QueryEscape(ret))
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			return c, res.Request.URL
		}

		kim, landed := signIn("u1", "/app/crm")
		if landed.Path != "/app/crm" || landed.Query().Get("sso_error") != "" {
			t.Fatalf("landed on %v", landed)
		}
		me := kim.do("GET", "/api/auth/me", nil, 200)
		if me["email"] != "kim@corp.example" || me["role"] != "member" || jsonString(me["groups"]) != `["sales","staff"]` || me["hasPassword"] != false {
			t.Errorf("kim = %v", me)
		}
		if rt := kim.do("GET", "/api/apps/crm/runtime", nil, 200); rt["role"] != "SALES" {
			t.Errorf("kim's role = %v", rt["role"])
		}
		// SSO users have no password to guess.
		newClient(t, hs.URL).do("POST", "/api/auth/login", map[string]any{"email": "kim@corp.example", "password": ""}, 401)

		// A mapping can make someone a builder.
		lee, _ := signIn("u4", "/")
		if me := lee.do("GET", "/api/auth/me", nil, 200); me["role"] != "builder" {
			t.Errorf("lee = %v", me)
		}

		// Other domains are refused; so are unverified emails of existing
		// accounts.
		_, landed = signIn("u3", "/app/crm")
		if !strings.Contains(landed.Query().Get("sso_error"), "elsewhere.example cannot sign in") || landed.Path != "/app/crm" {
			t.Errorf("eve landed on %v", landed)
		}
		_, landed = signIn("u2", "/")
		if !strings.Contains(landed.Query().Get("sso_error"), "already exists") {
			t.Errorf("old landed on %v", landed)
		}
		owner.do("PATCH", "/api/sso/providers/"+pid, map[string]any{"name": "Corp login", "issuer": ids.URL, "clientId": "bubibase", "trustEmail": true,
			"allowedDomains": []any{"corp.example"}, "autoCreate": true, "mappings": []any{
				map[string]any{"claim": "email_domain", "value": "corp.example", "apps": []any{map[string]any{"appId": app["id"], "role": "BASIC"}}},
			}}, 200)
		old, landed := signIn("u2", "/")
		if landed.Query().Get("sso_error") != "" {
			t.Fatalf("old landed on %v", landed)
		}
		if me := old.do("GET", "/api/auth/me", nil, 200); me["email"] != "old@corp.example" {
			t.Errorf("old = %v", me)
		}

		// The next sign-in follows the new mapping: Kim drops to BASIC.
		kim, _ = signIn("u1", "/")
		if rt := kim.do("GET", "/api/apps/crm/runtime", nil, 200); rt["role"] != "BASIC" {
			t.Errorf("kim's role now = %v", rt["role"])
		}
		// Roles given in the builder win over mappings.
		var kimID string
		for _, u := range owner.do("GET", "/api/users", nil, 200)["list"].([]any) {
			if u := u.(map[string]any); u["email"] == "kim@corp.example" {
				kimID = u["id"].(string)
				if jsonString(u["signIn"]) != `["Corp login"]` {
					t.Errorf("kim signs in with %v", u["signIn"])
				}
			}
		}
		owner.do("PUT", "/api/apps/crm/members/"+kimID, map[string]any{"role": "SALES"}, 200)
		kim, _ = signIn("u1", "/")
		if rt := kim.do("GET", "/api/apps/crm/runtime", nil, 200); rt["role"] != "SALES" {
			t.Errorf("kim's manual role = %v", rt["role"])
		}

		// Switched-off users and providers cannot sign in.
		owner.do("PATCH", "/api/users/"+kimID, map[string]any{"disabled": true}, 200)
		if _, landed := signIn("u1", "/"); !strings.Contains(landed.Query().Get("sso_error"), "switched off") {
			t.Errorf("disabled kim landed on %v", landed)
		}
		owner.do("PATCH", "/api/sso/providers/"+pid, map[string]any{"enabled": false, "issuer": ids.URL, "clientId": "bubibase"}, 200)
		if _, landed := signIn("u4", "/"); !strings.Contains(landed.Query().Get("sso_error"), "switched off") {
			t.Errorf("landed on %v", landed)
		}

		// A forged or replayed state is refused.
		c := newClient(t, hs.URL)
		res, err := c.http.Get(hs.URL + "/api/sso/callback?state=made-up&code=x")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if !strings.Contains(res.Request.URL.Query().Get("sso_error"), "start again") {
			t.Errorf("forged state landed on %v", res.Request.URL)
		}
		// Unsafe return addresses fall back to the start page.
		owner.do("PATCH", "/api/sso/providers/"+pid, map[string]any{"enabled": true, "issuer": ids.URL, "clientId": "bubibase"}, 200)
		if _, landed := signIn("u4", "//evil.example/x"); landed.Host != strings.TrimPrefix(hs.URL, "http://") || landed.Path != "/" {
			t.Errorf("open redirect to %v", landed)
		}
		owner.do("DELETE", "/api/sso/providers/"+pid, nil, 200)
		if pub := newClient(t, hs.URL).do("GET", "/api/auth/providers", nil, 200)["list"].([]any); len(pub) != 0 {
			t.Errorf("providers after delete = %v", pub)
		}
	})
}
