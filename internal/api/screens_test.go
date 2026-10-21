package api

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

func TestScreens(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>builder")}}
		srv := httptest.NewServer(New(s, ui, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
		defer srv.Close()

		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
		base := "/api/apps/shop"

		app := owner.do("PATCH", base, map[string]any{"design": map[string]any{"theme": "dark", "accent": "#0ea5e9", "navigation": "top"}}, 200)
		if d := app["design"].(map[string]any); d["theme"] != "dark" || d["navigation"] != "top" {
			t.Errorf("design = %v", d)
		}
		owner.do("PATCH", base, map[string]any{"design": map[string]any{"accent": "blue"}}, 400)
		app = owner.do("PATCH", base, map[string]any{"design": map[string]any{"chartPalette": "custom", "chartColors": []string{"#112233", "#445566"}}}, 200)
		if d := app["design"].(map[string]any); d["chartPalette"] != "custom" || len(d["chartColors"].([]any)) != 2 {
			t.Errorf("design = %v", d)
		}
		owner.do("PATCH", base, map[string]any{"design": map[string]any{"chartPalette": "neon"}}, 400)
		owner.do("PATCH", base, map[string]any{"design": map[string]any{"chartPalette": "custom"}}, 400)
		owner.do("PATCH", base, map[string]any{"design": map[string]any{"chartPalette": "custom", "chartColors": []string{"red"}}}, 400)
		if d := owner.do("PATCH", base, map[string]any{"design": map[string]any{}}, 200)["design"].(map[string]any); d["chartPalette"] != "theme" {
			t.Errorf("default palette = %v", d["chartPalette"])
		}

		home := owner.do("POST", base+"/screens", map[string]any{"name": "Home", "route": "home/"}, 201)
		if home["route"] != "/home" || home["role"] != "BASIC" {
			t.Errorf("screen = %v", home)
		}
		owner.do("POST", base+"/screens", map[string]any{"name": "Again", "route": "/home"}, 400)
		owner.do("POST", base+"/screens", map[string]any{"name": "Bad", "route": "/Orders/:id/:id"}, 400)
		detail := owner.do("POST", base+"/screens", map[string]any{"name": "Order", "route": "/orders/:id", "role": "PUBLIC"}, 201)
		if detail["position"].(float64) <= home["position"].(float64) {
			t.Errorf("positions %v, %v", home["position"], detail["position"])
		}

		id := home["id"].(string)
		def := map[string]any{"id": "root", "type": "container", "children": []any{
			map[string]any{"id": "h", "type": "heading", "props": map[string]any{"text": "Hi {{ user.name }}"}},
		}}
		got := owner.do("PATCH", base+"/screens/"+id, map[string]any{"definition": def}, 200)
		if kids := got["definition"].(map[string]any)["children"].([]any); len(kids) != 1 {
			t.Errorf("definition = %v", got["definition"])
		}
		dup := map[string]any{"id": "root", "type": "container", "children": []any{map[string]any{"id": "root", "type": "text"}}}
		owner.do("PATCH", base+"/screens/"+id, map[string]any{"definition": dup}, 400)
		owner.do("PATCH", base+"/screens/"+id, map[string]any{"definition": map[string]any{"id": "x", "type": "Bad Type"}}, 400)

		// Anonymous visitors see only PUBLIC screens.
		anon := newClient(t, srv.URL)
		rt := anon.do("GET", base+"/runtime", nil, 200)
		if sc := rt["screens"].([]any); len(sc) != 1 || sc[0].(map[string]any)["route"] != "/orders/:id" {
			t.Errorf("anonymous screens = %v", sc)
		}
		anon.do("GET", base+"/screens", nil, 401)
		owner.do("PATCH", base+"/screens/"+detail["id"].(string), map[string]any{"role": "BASIC"}, 200)
		anon.do("GET", base+"/runtime", nil, 401)
		if rt := owner.do("GET", base+"/runtime", nil, 200); len(rt["screens"].([]any)) != 2 {
			t.Errorf("owner runtime = %v", rt)
		}

		owner.do("DELETE", base+"/screens/"+id, nil, 200)
		owner.do("DELETE", base+"/screens/"+id, nil, 404)
		if list := owner.do("GET", base+"/screens", nil, 200)["list"].([]any); len(list) != 1 {
			t.Errorf("screens = %v", list)
		}
		owner.do("DELETE", base, nil, 200)
	})
}
