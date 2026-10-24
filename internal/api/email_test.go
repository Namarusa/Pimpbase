package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pimpbase/pimpbase/internal/email"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// waitMail waits for the log mailer to hold n mails.
func waitMail(t *testing.T, m *email.LogMailer, n int) *email.Mail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := m.Count()
		if got >= n {
			return m.Last()
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d mails, want %d", got, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var linkRe = regexp.MustCompile(`https?://[^\s"<>)]+token=[^\s"<>)&]+[^\s"<>)]*`)

func TestEmailTemplatesInFlows(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		api := New(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		mailer := &email.LogMailer{From: "shop@example.com"}
		api.Email.Server = mailer
		srv := httptest.NewServer(api.Handler())
		defer srv.Close()
		defer api.Engine.Stop(t.Context())

		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
		base := "/api/apps/shop"
		orders := owner.do("POST", base+"/tables", map[string]any{"name": "Orders", "fields": []any{
			map[string]any{"name": "Customer", "type": "text"},
			map[string]any{"name": "Email", "type": "text"},
			map[string]any{"name": "Total", "type": "number"},
		}}, 201)
		ordersID := orders["id"].(string)

		tpl := owner.do("POST", base+"/email-templates", map[string]any{
			"name": "Order confirmation", "subject": "Order {{ order._id }} confirmed",
			"params": []any{
				map[string]any{"name": "order", "type": "object", "sample": map[string]any{"_id": 1, "customer": "Sample"}},
				map[string]any{"name": "lines", "type": "list", "sample": []any{map[string]any{"name": "Tea"}}},
			},
			"blocks": []any{
				map[string]any{"type": "heading", "text": "Thanks {{ order.customer }}"},
				map[string]any{"type": "table", "items": "{{ lines }}", "columns": []any{map[string]any{"label": "Item", "value": "{{ loop.item.name }}"}}},
				map[string]any{"type": "button", "text": "See it", "url": "https://shop.example.com/o/{{ order._id }}"},
			},
		}, 201)
		tplID := tpl["id"].(string)
		if tpl["version"] != float64(1) || len(tpl["usedBy"].([]any)) != 0 {
			t.Fatalf("template = %v", tpl)
		}
		// A live preview of an unsaved draft, with sample values.
		pv := owner.do("POST", base+"/email-templates/preview", map[string]any{"template": tpl, "data": map[string]any{"order": map[string]any{"_id": 5, "customer": "<Zoe>"}}}, 200)
		if pv["subject"] != "Order 5 confirmed" || !strings.Contains(pv["html"].(string), "Thanks &lt;Zoe&gt;") || !strings.Contains(pv["html"].(string), ">Tea</td>") {
			t.Errorf("preview = %v", pv)
		}

		flow := owner.do("POST", base+"/flows", map[string]any{
			"name": "Confirm orders", "enabled": true,
			"trigger": map[string]any{"type": "row_created", "table": ordersID},
			"steps": []any{map[string]any{"name": "Confirm", "type": "send_email", "inputs": map[string]any{
				"mode": "template", "template": tplID, "to": "{{ trigger.row.Email }}", "bcc": "audit@example.com",
				"data": map[string]any{"order": "{{ trigger.row }}", "lines": `{{ = LIST("x") }}`},
			}}},
		}, 201)
		fb := base + "/flows/" + flow["id"].(string)
		if got := owner.do("GET", base+"/email-templates/"+tplID, nil, 200)["usedBy"].([]any); len(got) != 1 {
			t.Errorf("usedBy = %v", got)
		}
		// Deleting a template a flow sends is refused.
		owner.do("DELETE", base+"/email-templates/"+tplID, nil, 409)

		// The step's preview, over the sample the builder shows.
		prev := owner.do("POST", fb+"/email-preview", map[string]any{
			"inputs": map[string]any{"mode": "template", "template": tplID, "to": "{{ trigger.row.Email }}", "data": map[string]any{"order": "{{ trigger.row }}"}},
			"scope":  map[string]any{"trigger": map[string]any{"row": map[string]any{"_id": 9, "customer": "Sam", "Email": "sam@example.com"}}},
		}, 200)
		em := prev["email"].(map[string]any)
		if em["subject"] != "Order 9 confirmed" || em["to"].([]any)[0] != "sam@example.com" || em["template"] != "Order confirmation" {
			t.Errorf("step preview = %v", prev)
		}
		if w := em["warnings"].([]any); len(w) != 1 || w[0] != "lines is empty" {
			t.Errorf("warnings = %v", em["warnings"])
		}
		// A test send goes to the builder only.
		sent := owner.do("POST", fb+"/email-preview", map[string]any{
			"inputs": map[string]any{"mode": "template", "template": tplID, "to": "someone@else.com", "data": map[string]any{"order": `{{ = "x" }}`}},
			"scope":  map[string]any{}, "send": true,
		}, 200)
		if m := mailer.Last(); m == nil || m.To[0] != "ada@example.com" || m.Subject != "[Test] Order confirmed" || sent["sent"] == nil {
			t.Errorf("test send = %v %+v", sent, m)
		}

		// A real run: the template's newest version, logged with the email.
		mailer.Reset()
		owner.do("POST", base+"/tables/"+ordersID+"/rows", map[string]any{"Customer": "Bob", "Email": "bob@example.com", "Total": 12}, 201)
		runs := waitRuns(t, owner, fb, 1)
		run := owner.do("GET", base+"/runs/"+runs[0]["id"].(string), nil, 200)
		if run["status"] != "succeeded" {
			t.Fatalf("run = %v", run)
		}
		m := waitMail(t, mailer, 1)
		if m.To[0] != "bob@example.com" || m.Bcc[0] != "audit@example.com" || m.Subject != "Order 1 confirmed" || !strings.Contains(m.HTML, "Thanks Bob") || !strings.Contains(m.Text, "See it: https://shop.example.com/o/1") {
			t.Errorf("mail = %+v", m)
		}
		step := stepByName(run, "Confirm")
		out := step["output"].(map[string]any)
		if out["template"] != "Order confirmation" || out["templateVersion"] != float64(1) || out["via"] != "log" {
			t.Errorf("output = %v", out)
		}
		if em := step["email"].(map[string]any); !strings.Contains(em["html"].(string), "Thanks Bob") || em["bcc"].([]any)[0] != "audit@example.com" {
			t.Errorf("logged email = %v", em)
		}

		// Editing makes version 2, which the next run sends.
		tpl["subject"] = "Your order {{ order._id }}"
		tpl = owner.do("PUT", base+"/email-templates/"+tplID, tpl, 200)
		if tpl["version"] != float64(2) {
			t.Fatalf("version = %v", tpl["version"])
		}
		if vs := owner.do("GET", base+"/email-templates/"+tplID+"/versions", nil, 200)["list"].([]any); len(vs) != 2 {
			t.Errorf("versions = %v", vs)
		}
		// A dry test run renders without sending.
		mailer.Reset()
		test := owner.do("POST", fb+"/test", map[string]any{"dryRun": true}, 200)
		step = stepByName(test, "Confirm")
		if step["status"] != "ok" || step["output"].(map[string]any)["dryRun"] != true || step["email"].(map[string]any)["subject"] != "Your order 1" || mailer.Count() != 0 {
			t.Errorf("dry run step = %v, sent %d", step, mailer.Count())
		}
		old := owner.do("POST", base+"/email-templates/"+tplID+"/versions/1/restore", nil, 200)
		if old["version"] != float64(3) || old["subject"] != "Order {{ order._id }} confirmed" {
			t.Errorf("restored = %v", old)
		}

		// Exports carry the templates; a copy keeps the step working.
		exp := owner.raw("GET", base+"/export", nil, 200)
		owner.send("/api/apps/import?name=Shop+copy", exp, 201)
		copies := owner.do("GET", "/api/apps/shop-copy/email-templates", nil, 200)["list"].([]any)
		if len(copies) != 1 {
			t.Fatalf("copied templates = %v", copies)
		}
		cp := copies[0].(map[string]any)
		if cp["id"] == tplID || cp["subject"] != "Order {{ order._id }} confirmed" || len(cp["usedBy"].([]any)) != 1 {
			t.Errorf("copied template = %v", cp)
		}
		if vs := owner.do("GET", "/api/apps/shop-copy/email-templates/"+cp["id"].(string)+"/versions", nil, 200)["list"].([]any); len(vs) != 1 {
			t.Errorf("copied versions = %v", vs)
		}
		// Deleting an app takes its templates along.
		owner.do("DELETE", "/api/apps/shop-copy", nil, 200)
		if list, _ := api.Email.Templates(t.Context(), cp["appId"].(string)); len(list) != 0 {
			t.Errorf("templates outlived their app: %d", len(list))
		}
	})
}

func TestEmailLinks(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		api := New(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv := httptest.NewServer(api.Handler())
		defer srv.Close()
		defer api.Engine.Stop(t.Context())
		api.PublicURL = srv.URL

		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		// Without delivery, invitations and resets are refused or hidden.
		owner.do("POST", "/api/users", map[string]any{"email": "kim@example.com", "invite": true}, 409)
		if o := owner.do("GET", "/api/auth/options", nil, 200); o["passwordReset"] != false {
			t.Errorf("options = %v", o)
		}
		owner.do("PUT", "/api/email/settings", map[string]any{"method": "log", "fromEmail": "pimpbase@example.com"}, 200)
		mailer := &email.LogMailer{}
		api.Email.Server = mailer

		// An invitation: no password yet, a link to choose one.
		u := owner.do("POST", "/api/users", map[string]any{"email": "kim@example.com", "name": "Kim", "invite": true}, 201)
		if u["hasPassword"] != false || u["inviteError"] != nil {
			t.Fatalf("invited = %v", u)
		}
		m := waitMail(t, mailer, 1)
		if m.To[0] != "kim@example.com" || m.Subject != "You are invited to Pimpbase" || !strings.Contains(m.Text, "Ada invited you") {
			t.Fatalf("invite = %+v", m)
		}
		link := linkRe.FindString(m.Text)
		lu, _ := url.Parse(link)
		token := lu.Query().Get("token")
		if lu.Path != "/reset" || lu.Query().Get("invite") != "1" || token == "" {
			t.Fatalf("link = %s", link)
		}
		kim := newClient(t, srv.URL)
		if info := kim.do("GET", "/api/auth/reset?token="+url.QueryEscape(token), nil, 200); info["email"] != "kim@example.com" || info["kind"] != "invite" {
			t.Errorf("info = %v", info)
		}
		kim.do("POST", "/api/auth/reset", map[string]any{"token": token, "password": "short"}, 400)
		kim.do("POST", "/api/auth/reset", map[string]any{"token": token, "password": "kims long password"}, 200)
		if me := kim.do("GET", "/api/auth/me", nil, 200); me["email"] != "kim@example.com" {
			t.Errorf("me = %v", me)
		}
		// A link works once.
		kim.do("POST", "/api/auth/reset", map[string]any{"token": token, "password": "another password"}, 410)

		// Forgot password: same answer for unknown addresses.
		anon := newClient(t, srv.URL)
		anon.do("POST", "/api/auth/forgot", map[string]any{"email": "nobody@example.com"}, 200)
		anon.do("POST", "/api/auth/forgot", map[string]any{"email": "KIM@example.com", "next": "/app/shop"}, 200)
		m = waitMail(t, mailer, 2)
		if m.Subject != "Reset your Pimpbase password" {
			t.Fatalf("reset = %+v", m)
		}
		lu, _ = url.Parse(linkRe.FindString(m.Text))
		if lu.Query().Get("next") != "/app/shop" {
			t.Errorf("reset link = %s", lu)
		}
		// Asking again right away sends nothing more.
		anon.do("POST", "/api/auth/forgot", map[string]any{"email": "kim@example.com"}, 200)
		time.Sleep(100 * time.Millisecond)
		if n := mailer.Count(); n != 2 {
			t.Errorf("mails after a quick second ask = %d", n)
		}
		anon.do("POST", "/api/auth/reset", map[string]any{"token": lu.Query().Get("token"), "password": "kims newer password"}, 200)
		kim.do("GET", "/api/auth/me", nil, 401) // the new password signed out the old session
		anon.do("POST", "/api/auth/login", map[string]any{"email": "kim@example.com", "password": "kims newer password"}, 200)

		// Sign-in links are off until switched on.
		anon.do("POST", "/api/auth/email-link", map[string]any{"email": "kim@example.com"}, 403)
		owner.do("PUT", "/api/email/settings", map[string]any{"method": "log", "fromEmail": "pimpbase@example.com", "emailLinks": true}, 200)
		if o := anon.do("GET", "/api/auth/options", nil, 200); o["emailLinks"] != true {
			t.Errorf("options = %v", o)
		}
		other := newClient(t, srv.URL)
		other.do("POST", "/api/auth/email-link", map[string]any{"email": "kim@example.com", "next": "//evil.com"}, 200)
		m = waitMail(t, mailer, 3)
		link = linkRe.FindString(m.Text)
		if m.Subject != "Your Pimpbase sign-in link" || !strings.Contains(link, "/api/auth/link?") || strings.Contains(link, "evil") {
			t.Fatalf("sign-in mail = %+v", m)
		}
		other.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		req, _ := http.NewRequest("GET", link, nil)
		res, err := other.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 303 || res.Header.Get("Location") != "/" {
			t.Errorf("link: %d %s", res.StatusCode, res.Header.Get("Location"))
		}
		if me := other.do("GET", "/api/auth/me", nil, 200); me["email"] != "kim@example.com" {
			t.Errorf("signed in as %v", me)
		}
		res, _ = other.http.Do(req)
		res.Body.Close()
		if !strings.HasPrefix(res.Header.Get("Location"), "/?linkError=") {
			t.Errorf("a spent link went to %s", res.Header.Get("Location"))
		}

		// System emails can be changed and reset by admins.
		list := owner.do("GET", "/api/email/system", nil, 200)["list"].([]any)
		if len(list) != 3 {
			t.Fatalf("system emails = %v", list)
		}
		inv := list[0].(map[string]any)["template"].(map[string]any)
		inv["subject"] = "Join us, {{ name }}"
		owner.do("PUT", "/api/email/system/invite", inv, 200)
		kimID := u["id"].(string)
		owner.do("POST", "/api/users/"+kimID+"/invite", nil, 200) // kim has a password: a reset goes out
		if m = waitMail(t, mailer, 4); m.Subject != "Reset your Pimpbase password" {
			t.Errorf("resend for a user with a password = %s", m.Subject)
		}
		lena := owner.do("POST", "/api/users", map[string]any{"email": "lena@example.com", "name": "Lena", "invite": true}, 201)
		if m = waitMail(t, mailer, 5); m.Subject != "Join us, Lena" {
			t.Errorf("custom invite subject = %s", m.Subject)
		}
		_ = lena
		owner.do("DELETE", "/api/email/system/invite", nil, 200)
		kim.do("GET", "/api/email/system", nil, 401)
	})
}
