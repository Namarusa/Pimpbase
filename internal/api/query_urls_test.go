package api

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// TestQueryURLBindings puts bindings anywhere in an API address: scheme,
// host, IP, port, path and query, in the data source and in the query.
func TestQueryURLBindings(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		var logins atomic.Int32
		up := upstream(t, &logins)
		u, _ := url.Parse(up.URL) // http://127.0.0.1:port
		ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>builder")}}
		srv := httptest.NewServer(New(s, ui, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
		defer srv.Close()

		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
		base := "/api/apps/shop"
		owner.do("PUT", base+"/variables/SCHEME", map[string]any{"production": "http"}, 200)
		owner.do("PUT", base+"/variables/HOST", map[string]any{"production": u.Hostname()}, 200)
		owner.do("PUT", base+"/variables/PORT", map[string]any{"production": u.Port()}, 200)
		owner.do("PUT", base+"/variables/HOSTPORT", map[string]any{"production": u.Host}, 200)
		owner.do("PUT", base+"/variables/API_URL", map[string]any{"production": up.URL}, 200)
		owner.do("PUT", base+"/variables/PREFIX", map[string]any{"production": "/items"}, 200)

		run := func(name, dsURL, path string) map[string]any {
			t.Helper()
			d := owner.do("POST", base+"/datasources", map[string]any{"name": "DS " + name, "config": map[string]any{"baseUrl": dsURL}}, 201)
			ds, _ := d["id"].(string)
			q := owner.do("POST", base+"/queries", map[string]any{
				"name": name, "datasourceId": ds, "path": path,
				"parameters": []any{map[string]any{"name": "host", "default": u.Host}, map[string]any{"name": "cat", "default": "a b/c"}, map[string]any{"name": "url", "default": up.URL}},
			}, 201)
			id, _ := q["id"].(string)
			return owner.do("POST", base+"/queries/"+id+"/run", map[string]any{"env": "production"}, 200)
		}
		cases := []struct{ name, ds, path, want string }{
			{"PlainHTTP", "", up.URL + "/echo?x=1", "x=1"},
			{"HostVar", "", "http://{{ env.HOST }}:{{ env.PORT }}/echo?h=1", "h=1"},
			{"SchemeVar", "", "{{ env.SCHEME }}://{{ env.HOSTPORT }}/echo?s=1", "s=1"},
			{"ParamHost", "", "http://{{ params.host }}/echo?p={{ params.cat }}", "p=a+b%2Fc"},
			{"ParamURL", "", "{{ params.url }}/echo?u=1", "u=1"},
			{"DsHostVar", "http://{{ env.HOST }}:{{ env.PORT }}", "/echo?d=1", "d=1"},
			{"DsSchemeVar", "{{ env.SCHEME }}://{{ env.HOST }}:{{ env.PORT }}/", "echo?e=1", "e=1"},
			{"DsParamHost", "http://{{ params.host }}", "/echo?f=1", "f=1"},
			{"DsLiteralIP", up.URL, "/echo?g=1", "g=1"},
		}
		for _, c := range cases {
			res := run(c.name, c.ds, c.path)
			body, _ := res["body"].(map[string]any)
			if res["ok"] != true || body["query"] != c.want {
				t.Errorf("%s: %v", c.name, res)
			}
		}
		// A variable holding a path prefix keeps its slashes.
		res := run("PrefixVar", "", "{{ env.API_URL }}{{ env.PREFIX }}/books")
		if req, _ := res["request"].(map[string]any); req["url"] != up.URL+"/items/books" {
			t.Errorf("PrefixVar: %v", res)
		}
	})
}
