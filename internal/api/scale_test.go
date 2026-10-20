package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// TestScaleAPI covers what phase 7 adds over HTTP: aggregates under row
// rules, health and metrics, the rate limit and app quotas.
func TestScaleAPI(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		srv, hs := newTestServer(t, s)
		srv.Version = "test"
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Deals"}, 201)
		base := "/api/apps/deals"
		tbl := owner.do("POST", base+"/tables", map[string]any{"name": "Deals", "fields": []any{
			map[string]any{"name": "Title", "type": "text"},
			map[string]any{"name": "Owner", "type": "text", "indexed": true},
			map[string]any{"name": "Stage", "type": "single_select", "options": []any{"Lead", "Won"}},
			map[string]any{"name": "Amount", "type": "number"},
		}}, 201)
		tb := base + "/tables/" + tbl["id"].(string)
		for i, r := range []map[string]any{
			{"Title": "A", "Owner": "bo@example.com", "Stage": "Lead", "Amount": 100},
			{"Title": "B", "Owner": "cy@example.com", "Stage": "Won", "Amount": 200},
			{"Title": "C", "Owner": "bo@example.com", "Stage": "Won", "Amount": 300},
		} {
			_ = i
			owner.do("POST", tb+"/rows", r, 201)
		}
		owner.do("PATCH", tb, map[string]any{"permissions": map[string]any{"read": "BASIC", "write": "BASIC", "delete": "BASIC",
			"rows": []any{map[string]any{"role": "BASIC", "filter": "[Owner] = Current.User.Email"}}}}, 200)
		bo, _ := member(t, owner, hs.URL, "deals", "bo@example.com", "BASIC")

		agg := owner.do("GET", tb+"/aggregate?groupBy=Stage&values=sum:Amount&values=count", nil, 200)
		groups := agg["groups"].([]any)
		if len(groups) != 2 || agg["engine"] != "sql" || jsonNum(groups[1]) != `{"count":2,"key":"Won","values":[500,2]}` {
			t.Errorf("owner aggregate = %v", agg)
		}
		agg = bo.do("GET", tb+"/aggregate?groupBy=Stage&values=sum:Amount", nil, 200)
		if jsonNum(agg["groups"]) != `[{"count":1,"key":"Lead","values":[100]},{"count":1,"key":"Won","values":[300]}]` {
			t.Errorf("bo aggregate = %v", agg["groups"])
		}
		anon := newClient(t, hs.URL)
		anon.do("GET", tb+"/aggregate", nil, 401)
		owner.do("GET", tb+"/aggregate?values=median:Amount", nil, 400)

		// Health, readiness and metrics.
		if h := anon.do("GET", "/healthz", nil, 200); h["version"] != "test" {
			t.Errorf("healthz = %v", h)
		}
		anon.do("GET", "/readyz", nil, 200)
		metricsText := string(anon.raw("GET", "/metrics", nil, 200))
		for _, want := range []string{`bubibase_http_requests_total{route="GET /api/apps/{app}/tables/{table}/aggregate",code="2xx"}`, "bubibase_http_request_duration_seconds_bucket", `bubibase_build_info{version="test",storage=`} {
			if !strings.Contains(metricsText, want) {
				t.Errorf("metrics lack %s", want)
			}
		}
		srv.MetricsToken = "sesame"
		anon.do("GET", "/metrics", nil, 401)
		req, _ := http.NewRequest("GET", hs.URL+"/metrics", nil)
		req.Header.Set("Authorization", "Bearer sesame")
		if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 200 {
			t.Errorf("metrics with token: %v %v", res.StatusCode, err)
		} else {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		srv.Drain()
		anon.do("GET", "/readyz", nil, 503)
		srv.draining.Store(false)
		st := owner.do("GET", "/api/admin/status", nil, 200)
		if st["storage"] != "database" {
			t.Errorf("status = %v", st)
		}
		bo.do("GET", "/api/admin/status", nil, 403)

		// Quotas: only admins set them; rows and requests are capped.
		bo.do("PUT", base+"/quotas", map[string]any{"maxRows": 1}, 403)
		owner.do("PUT", base+"/quotas", map[string]any{"maxRows": -1}, 400)
		owner.do("PUT", base+"/quotas", map[string]any{"maxRows": 4, "requestsPerMinute": 3}, 200)
		owner.do("POST", tb+"/rows", map[string]any{"Title": "D", "Owner": "bo@example.com"}, 201)
		owner.do("POST", tb+"/rows", map[string]any{"Title": "E", "Owner": "bo@example.com"}, 403)
		owner.do("POST", tb+"/import", "Title\nF\nG\n", 403)
		use := owner.do("GET", base+"/usage", nil, 200)
		if use["rows"] != 4.0 || use["quotas"].(map[string]any)["maxRows"] != 4.0 {
			t.Errorf("usage = %v", use)
		}
		// bo's requests earlier this minute count too.
		refused := false
		for i := 0; i < 4 && !refused; i++ {
			refused = strings.Contains(string(bo.raw2("GET", tb+"/rows")), "quota_exceeded")
		}
		if !refused {
			t.Error("request quota never refused")
		}
		owner.do("GET", tb+"/rows", nil, 200) // builders are not counted
		owner.do("PUT", base+"/quotas", map[string]any{}, 200)

		// The per-client rate limit.
		srv.RateLimit, srv.RateBurst = 1, 2
		anon.do("GET", "/api/setup", nil, 200)
		anon.do("GET", "/api/setup", nil, 200)
		res := anon.raw("GET", "/api/setup", nil, 429)
		if !strings.Contains(string(res), "rate_limited") {
			t.Errorf("429 body = %s", res)
		}
		anon.do("GET", "/healthz", nil, 200) // never limited
	})
}

// raw2 sends a request and returns the body whatever the status.
func (c *client) raw2(method, path string) []byte {
	req, _ := http.NewRequest(method, c.base+path, nil)
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return b
}
