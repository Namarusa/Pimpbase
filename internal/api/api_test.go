package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
	key  string
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any, wantStatus int) map[string]any {
	c.t.Helper()
	raw := c.raw(method, path, body, wantStatus)
	var out map[string]any
	if len(raw) > 0 && raw[0] == '{' {
		json.Unmarshal(raw, &out)
	} else {
		out = map[string]any{"list": nil}
		var list []any
		json.Unmarshal(raw, &list)
		out["list"] = list
	}
	return out
}

func (c *client) raw(method, path string, body any, wantStatus int) []byte {
	c.t.Helper()
	var r io.Reader
	ct := "application/json"
	switch b := body.(type) {
	case nil:
	case string:
		r, ct = strings.NewReader(b), "text/csv"
	default:
		js, _ := json.Marshal(b)
		r = bytes.NewReader(js)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Origin", c.base)
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, res.StatusCode, wantStatus, data)
	}
	return data
}

func TestAPI(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>builder")}}
		srv := httptest.NewServer(New(s, ui, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
		defer srv.Close()

		owner := newClient(t, srv.URL)
		if got := owner.do("GET", "/api/setup", nil, 200); got["needsSetup"] != true {
			t.Fatalf("setup = %v", got)
		}
		owner.do("GET", "/api/apps", nil, 401)
		owner.do("POST", "/api/setup", map[string]any{"email": "Ada@Example.com", "name": "Ada", "password": "short"}, 400)
		me := owner.do("POST", "/api/setup", map[string]any{"email": "Ada@Example.com", "name": "Ada", "password": "correct horse"}, 200)
		if me["role"] != "owner" || me["email"] != "ada@example.com" {
			t.Fatalf("me = %v", me)
		}
		owner.do("POST", "/api/setup", map[string]any{"email": "x@example.com", "password": "correct horse"}, 409)

		app := owner.do("POST", "/api/apps", map[string]any{"name": "Field Ops"}, 201)
		if app["slug"] != "field-ops" {
			t.Errorf("slug = %v", app["slug"])
		}
		base := "/api/apps/field-ops"
		tbl := owner.do("POST", base+"/tables", map[string]any{
			"name": "Jobs",
			"fields": []any{
				map[string]any{"name": "Title", "type": "text", "required": true},
				map[string]any{"name": "Hours", "type": "number"},
				map[string]any{"name": "Rate", "type": "number"},
				map[string]any{"name": "Cost", "type": "formula", "formula": "[Hours] * [Rate]"},
				map[string]any{"name": "Status", "type": "single_select", "options": []any{"Open", "Done"}},
			},
		}, 201)
		tb := base + "/tables/" + tbl["id"].(string)

		bad := owner.do("POST", tb+"/fields", map[string]any{"name": "Oops", "type": "formula", "formula": `[Hours] + "x"`}, 400)
		if fe, _ := bad["formulaErrors"].([]any); len(fe) != 1 || fe[0].(map[string]any)["start"] != 10.0 {
			t.Errorf("formula error = %v", bad)
		}
		check := owner.do("POST", tb+"/formula/check", map[string]any{"formula": "[Cost] > 100"}, 200)
		if check["type"] != "boolean" {
			t.Errorf("check = %v", check)
		}

		row := owner.do("POST", tb+"/rows", map[string]any{"Title": "Fix pump", "Hours": 3, "Rate": 50, "Status": "Open"}, 201)
		if row["Cost"] != 150.0 {
			t.Errorf("row = %v", row)
		}
		owner.do("POST", tb+"/rows", map[string]any{"Hours": 1}, 400)
		id := int(row["_id"].(float64))
		owner.do("PATCH", tb+"/rows/"+itoa(id), map[string]any{"Status": "Done"}, 200)
		owner.do("GET", tb+"/rows/999", nil, 404)

		res := owner.do("POST", tb+"/import", "Title,Hours,Rate,Status,Extra\nPaint,2,30,Open,x\n,1,1,Open,y\nWire,4,40,Waiting,z\n", 200)
		if res["imported"] != 2.0 || res["skipped"] != 1.0 {
			t.Errorf("import = %v", res)
		}
		page := owner.do("GET", tb+"/rows?filter="+urlq(`[Cost] > 100 AND [Status] <> "Open"`)+"&sort=Cost&desc=true", nil, 200)
		if page["total"] != 2.0 || page["engine"] != "sql" {
			t.Errorf("page = %v", page)
		}
		csv := string(owner.raw("GET", tb+"/export?filter="+urlq(`[Status] = "Waiting"`), nil, 200))
		if !strings.Contains(csv, "Wire,4,40,160,Waiting") {
			t.Errorf("csv = %q", csv)
		}

		// A member with no role sees nothing; with BASIC they read but
		// cannot write once writes need POWER.
		owner.do("POST", "/api/users", map[string]any{"email": "bo@example.com", "password": "another secret", "role": "member"}, 201)
		users := owner.do("GET", "/api/users", nil, 200)["list"].([]any)
		var bo string
		for _, u := range users {
			if u.(map[string]any)["email"] == "bo@example.com" {
				bo = u.(map[string]any)["id"].(string)
			}
		}
		member := newClient(t, srv.URL)
		member.do("POST", "/api/auth/login", map[string]any{"email": "bo@example.com", "password": "nope"}, 401)
		member.do("POST", "/api/auth/login", map[string]any{"email": "bo@example.com", "password": "another secret"}, 200)
		if apps := member.do("GET", "/api/apps", nil, 200)["list"].([]any); len(apps) != 0 {
			t.Errorf("member sees %v", apps)
		}
		member.do("GET", tb+"/rows", nil, 403)
		owner.do("PUT", base+"/members/"+bo, map[string]any{"role": "BASIC"}, 200)
		member.do("GET", tb+"/rows", nil, 200)
		member.do("POST", tb+"/rows", map[string]any{"Title": "By Bo"}, 201)
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "POWER"}}, 200)
		member.do("POST", tb+"/rows", map[string]any{"Title": "By Bo"}, 403)
		member.do("POST", base+"/tables", map[string]any{"name": "Nope"}, 403)

		// Public tables need no sign-in.
		anon := newClient(t, srv.URL)
		anon.do("GET", tb+"/rows", nil, 401)
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "PUBLIC", "write": "POWER"}}, 200)
		anon.do("GET", tb+"/rows", nil, 200)

		// API keys act as their user, without cookies.
		key := owner.do("POST", "/api/auth/keys", map[string]any{"name": "ci"}, 201)["key"].(string)
		bot := newClient(t, srv.URL)
		bot.key = key
		bot.do("GET", tb+"/rows", nil, 200)
		bot.key = "bbk_wrong"
		bot.do("GET", tb+"/rows", nil, 401)

		// Writes from another site with the session cookie are refused.
		req, _ := http.NewRequest("POST", srv.URL+tb+"/rows", strings.NewReader(`{"Title":"evil"}`))
		req.Header.Set("Origin", "https://evil.example")
		resp, err := owner.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("cross-site write: %d", resp.StatusCode)
		}

		// The builder is served for any non-API path.
		if body := string(owner.raw("GET", "/apps/field-ops/data", nil, 200)); !strings.Contains(body, "builder") {
			t.Errorf("spa = %q", body)
		}
		owner.do("POST", "/api/auth/logout", nil, 200)
		owner.do("GET", "/api/auth/me", nil, 401)
	})
}

func itoa(i int) string { return strings.TrimSpace(strings.Replace(jsonString(i), "\n", "", -1)) }

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func urlq(s string) string {
	r := strings.NewReplacer(" ", "%20", "[", "%5B", "]", "%5D", `"`, "%22", "<", "%3C", ">", "%3E", "&", "%26", "=", "%3D")
	return r.Replace(s)
}
