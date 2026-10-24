package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"

	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

func TestMailBytes(t *testing.T) {
	m := &Mail{From: "Pimpbase <flows@example.com>", To: []string{"bob@example.com"}, Bcc: []string{"audit@example.com"}, Subject: "Votre facture n°7", Text: "Bonjour", Attachments: []Attachment{{Name: "facture 7.pdf", Type: "application/pdf", Data: []byte("%PDF-1.4")}}}
	raw, err := m.Bytes("example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"From: \"Pimpbase\" <flows@example.com>", "To: <bob@example.com>", "Subject: =?utf-8?q?Votre_facture_n=C2=B07?=", "multipart/mixed", `filename="facture 7.pdf"`, "JVBERi0xLjQ="} {
		if !strings.Contains(s, want) {
			t.Errorf("mail lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "audit@example.com") {
		t.Error("Bcc must not be written into the mail")
	}
	if got := Addresses([]any{"a@x.com; b@y.com", map[string]any{"email": "c@z.com"}, map[string]any{"Email": "d@w.com"}}); strings.Join(got, ",") != "a@x.com,b@y.com,c@z.com,d@w.com" {
		t.Errorf("addresses = %v", got)
	}
}

// TestAlternative checks that a mail with HTML and text parses as
// multipart/alternative, with attachments around it.
func TestAlternative(t *testing.T) {
	for _, withFile := range []bool{false, true} {
		m := &Mail{From: "a@x.com", To: []string{"b@y.com"}, Subject: "Hi", Text: "plain words", HTML: "<p>html words</p>"}
		if withFile {
			m.Attachments = []Attachment{{Name: "a.txt", Type: "text/plain", Data: []byte("file")}}
		}
		raw, err := m.Bytes("x.com")
		if err != nil {
			t.Fatal(err)
		}
		msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		typ, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
		body := io.Reader(msg.Body)
		if withFile {
			if typ != "multipart/mixed" {
				t.Fatalf("type = %s", typ)
			}
			r := multipart.NewReader(msg.Body, params["boundary"])
			p, err := r.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			typ, params, _ = mime.ParseMediaType(p.Header.Get("Content-Type"))
			body = p
		}
		if typ != "multipart/alternative" {
			t.Fatalf("withFile=%v: body type = %s\n%s", withFile, typ, raw)
		}
		r := multipart.NewReader(body, params["boundary"])
		var got []string
		for {
			p, err := r.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(p)
			dec, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(data), "\r\n", ""))
			got = append(got, p.Header.Get("Content-Type")+"="+string(dec))
		}
		if len(got) != 2 || !strings.HasSuffix(got[0], "=plain words") || !strings.HasSuffix(got[1], "=<p>html words</p>") {
			t.Errorf("withFile=%v: parts = %q", withFile, got)
		}
	}
}

func orderTemplate() *Template {
	return &Template{
		Name: "Order confirmation", Subject: "Order {{ order.Number }} for {{ customer.Name }}", Preheader: "Thanks, {{ customer.Name }}",
		Style: Style{Brand: "#16a34a", Footer: "{{ app.name }} · 1 Main St"},
		Params: []Param{
			{Name: "customer", Type: "object", Sample: map[string]any{"Name": "Ada", "Email": "ada@example.com"}},
			{Name: "order", Type: "object", Sample: map[string]any{"Number": 7, "Paid": true, "Total": 42.5}},
			{Name: "lines", Type: "list", Sample: []any{map[string]any{"Product": "Tea", "Qty": 2}}},
		},
		Blocks: []Block{
			{Type: "heading", Text: "Thanks {{ customer.Name }}!"},
			{Type: "text", Text: "Your order **#{{ order.Number }}** is in.\n\n- fast\n- [track it](https://example.com/t/{{ order.Number }})"},
			{Type: "table", Items: "{{ lines }}", Columns: []Column{{Label: "Product", Value: "{{ loop.item.Product }}"}, {Label: "Qty", Value: "{{ loop.item.Qty }}", Align: "right"}}},
			{Type: "text", Text: "Paid, thank you.", ShowWhen: "{{ order.Paid }}"},
			{Type: "text", Text: "Payment is due.", ShowWhen: "{{ order.Due }}"},
			{Type: "button", Text: "See order", URL: "https://shop.example.com/orders/{{ order.Number }}"},
			{Type: "divider"},
			{Type: "image", URL: "https://example.com/logo.png", Alt: "Logo {{ customer.Name }}", Width: 100},
		},
	}
}

func TestRenderBlocks(t *testing.T) {
	tp := orderTemplate()
	if err := tp.Check(); err != nil {
		t.Fatal(err)
	}
	data := tp.SampleData()
	data["customer"] = map[string]any{"Name": `Bob <b>"&"</b>`}
	r, err := tp.Render(data, map[string]any{"name": "Shop"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != `Order 7 for Bob <b>"&"</b>` {
		t.Errorf("subject = %q", r.Subject)
	}
	for _, want := range []string{
		`Thanks Bob &lt;b&gt;&quot;&amp;&quot;&lt;/b&gt;!`,
		`<strong>#7</strong>`,
		`<li style="margin:0 0 4px"><a href="https://example.com/t/7"`,
		`>Tea</td>`, `Paid, thank you.`,
		`href="https://shop.example.com/orders/7"`, `background:#16a34a`,
		`alt="Logo Bob &lt;b&gt;&quot;&amp;&quot;&lt;/b&gt;"`,
		`Shop · 1 Main St`,
	} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("html lacks %q", want)
		}
	}
	if strings.Contains(r.HTML, "<b>") || strings.Contains(r.HTML, "Payment is due") {
		t.Error("html holds unescaped values or a hidden block")
	}
	for _, want := range []string{"Thanks Bob", "Your order #7 is in.", "track it (https://example.com/t/7)", "Product | Qty", "Tea | 2", "See order: https://shop.example.com/orders/7", "--\nShop · 1 Main St"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, r.Text)
		}
	}
	if len(r.Warnings) != 0 {
		t.Errorf("warnings = %v", r.Warnings)
	}
	delete(data, "lines")
	r, _ = tp.Render(data, nil)
	if len(r.Warnings) != 1 || r.Warnings[0] != "lines is empty" {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestRenderHTMLMode(t *testing.T) {
	tp := &Template{Name: "Raw", Mode: "html", Subject: "Hi", HTML: `<h1>Hi {{ who }}</h1><p>{{#each items}}<i>{{ loop.item }}</i>{{/each}}</p><a href="https://x.com/{{ who | urlencode }}">go</a>{{ html | raw }}`}
	if err := tp.Check(); err != nil {
		t.Fatal(err)
	}
	r, err := tp.Render(map[string]any{"who": "<Al>", "items": []any{"a", "b"}, "html": "<b>bold</b>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.HTML != `<h1>Hi &lt;Al&gt;</h1><p><i>a</i><i>b</i></p><a href="https://x.com/%3CAl%3E">go</a><b>bold</b>` {
		t.Errorf("html = %s", r.HTML)
	}
	if r.Text != "Hi <Al>\nab\ngo (https://x.com/%3CAl%3E)bold" {
		t.Errorf("text = %q", r.Text)
	}
}

func TestCheck(t *testing.T) {
	for _, bad := range []*Template{
		{Name: ""},
		{Name: "x", Blocks: []Block{{Type: "video"}}},
		{Name: "x", Blocks: []Block{{Type: "button", URL: "javascript:alert(1)"}}},
		{Name: "x", Subject: "{{#each a}}"},
		{Name: "x", Params: []Param{{Name: "app"}}},
		{Name: "x", Params: []Param{{Name: "a b"}}},
		{Name: "x", Style: Style{Brand: "red"}},
	} {
		if err := bad.Check(); err == nil {
			t.Errorf("Check(%+v) passed", bad)
		}
	}
	// Literal links that are not web addresses stay text.
	tp := &Template{Name: "x", Blocks: []Block{{Type: "text", Text: "[click](javascript:alert(1))"}}}
	tp.Check()
	r, _ := tp.Render(nil, nil)
	if strings.Contains(r.HTML, "href=\"javascript") {
		t.Error("a javascript: link was made")
	}
}

func TestSystemDefaultsRender(t *testing.T) {
	for _, k := range SystemKeys {
		tp := DefaultSystem(k)
		if err := tp.Check(); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		r, err := tp.Render(map[string]any{"name": "", "email": "kim@example.com", "link": "https://x.com/reset?token=abc", "expires": "1 hour", "site": "https://x.com", "inviter": "Nassim"}, nil)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if !strings.Contains(r.HTML, `href="https://x.com/reset?token=abc"`) || !strings.Contains(r.Text, "there") {
			t.Errorf("%s: %s\n%s", k, r.HTML, r.Text)
		}
	}
}

func TestProviders(t *testing.T) {
	var got []map[string]any
	var lastPath, lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath, lastAuth = r.URL.Path, r.Header.Get("Authorization")+r.Header.Get("X-Postmark-Server-Token")
		body := map[string]any{}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			r.ParseMultipartForm(1 << 20)
			for k, v := range r.MultipartForm.Value {
				body[k] = strings.Join(v, ",")
			}
			body["files"] = len(r.MultipartForm.File["attachment"])
		} else {
			json.NewDecoder(r.Body).Decode(&body)
		}
		got = append(got, body)
		switch {
		case strings.Contains(r.URL.Path, "mail/send"):
			w.Header().Set("X-Message-Id", "sg-1")
			w.WriteHeader(202)
		case r.URL.Path == "/email":
			if body["From"] == "bad@x.com" {
				w.WriteHeader(422)
				w.Write([]byte(`{"ErrorCode":300,"Message":"Invalid From"}`))
				return
			}
			w.Write([]byte(`{"MessageID":"pm-1","ErrorCode":0}`))
		default:
			w.Write([]byte(`{"id":"id-1"}`))
		}
	}))
	defer srv.Close()
	m := func() *Mail {
		return &Mail{From: "Shop <shop@x.com>", To: []string{"a@y.com"}, Cc: []string{"c@y.com"}, Bcc: []string{"b@y.com"}, ReplyTo: "help@x.com", Subject: "Hi", Text: "t", HTML: "<p>h</p>",
			Attachments: []Attachment{{Name: "f.pdf", Type: "application/pdf", Data: []byte("pdf")}}}
	}
	ctx := context.Background()
	for _, c := range []struct{ provider, path, auth, id string }{
		{"sendgrid", "/v3/mail/send", "Bearer key", "sg-1"},
		{"mailgun", "/v3/mg.x.com/messages", "Basic ", "id-1"},
		{"postmark", "/email", "key", "pm-1"},
		{"resend", "/emails", "Bearer key", "id-1"},
	} {
		a := &APIMailer{Provider: c.provider, APIKey: "key", Domain: "mg.x.com", BaseURL: srv.URL}
		rc, err := a.Send(ctx, m())
		if err != nil {
			t.Fatalf("%s: %v", c.provider, err)
		}
		if lastPath != c.path || !strings.HasPrefix(lastAuth, c.auth) || rc.ID != c.id {
			t.Errorf("%s: path %s auth %q id %q", c.provider, lastPath, lastAuth, rc.ID)
		}
	}
	sg, mg, pm, rs := got[0], got[1], got[2], got[3]
	if p := sg["personalizations"].([]any)[0].(map[string]any); len(p["bcc"].([]any)) != 1 || sg["from"].(map[string]any)["name"] != "Shop" || len(sg["attachments"].([]any)) != 1 {
		t.Errorf("sendgrid body = %v", sg)
	}
	if mg["bcc"] != "b@y.com" || mg["h:Reply-To"] != "help@x.com" || mg["files"] != 1 || mg["html"] != "<p>h</p>" {
		t.Errorf("mailgun body = %v", mg)
	}
	if pm["Bcc"] != "b@y.com" || pm["TextBody"] != "t" || pm["MessageStream"] != "outbound" {
		t.Errorf("postmark body = %v", pm)
	}
	if rs["reply_to"] != "help@x.com" || len(rs["attachments"].([]any)) != 1 {
		t.Errorf("resend body = %v", rs)
	}
	bad := m()
	bad.From = "bad@x.com"
	if _, err := (&APIMailer{Provider: "postmark", APIKey: "key", BaseURL: srv.URL}).Send(ctx, bad); err == nil || !strings.Contains(err.Error(), "Invalid From") {
		t.Errorf("postmark error = %v", err)
	}
}

func TestSettingsAndTemplates(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		s := NewService(st)
		if s.Configured(ctx) {
			t.Fatal("configured without settings")
		}
		if _, err := s.Send(ctx, &Mail{To: []string{"a@x.com"}}); err != ErrNotSetUp {
			t.Fatalf("send = %v", err)
		}
		if err := s.Update(ctx, Settings{Method: "sendgrid", FromEmail: "shop@x.com", FromName: "Shop", APIKey: "sg-key"}); err != nil {
			t.Fatal(err)
		}
		// A blank key keeps the saved one; the status hides it.
		if err := s.Update(ctx, Settings{Method: "sendgrid", FromEmail: "shop@x.com", FromName: "Shop", ReplyTo: "help@x.com"}); err != nil {
			t.Fatal(err)
		}
		status, _ := s.Status(ctx)
		if !status.Configured || !status.APIKeySet || status.APIKey != "" || status.ReplyTo != "help@x.com" {
			t.Errorf("status = %+v", status)
		}
		if err := s.Update(ctx, Settings{Method: "smtp", FromEmail: "Shop <shop@x.com>"}); err == nil {
			t.Error("a named sender was accepted as the address")
		}
		// Switching provider drops the key.
		s.Update(ctx, Settings{Method: "postmark", FromEmail: "shop@x.com"})
		if status, _ = s.Status(ctx); status.APIKeySet || status.Configured {
			t.Errorf("status after switching = %+v", status)
		}
		log := &LogMailer{}
		s.Server = log
		if _, err := s.Send(ctx, &Mail{To: []string{"a@x.com"}, Subject: "x"}); err != nil || len(log.Sent) != 1 {
			t.Errorf("server mailer: %v %d", err, len(log.Sent))
		}

		tp, err := s.CreateTemplate(ctx, "app_1", orderTemplate(), "kim@x.com")
		if err != nil {
			t.Fatal(err)
		}
		if tp.Version != 1 || len(tp.Blocks) != 8 || tp.Blocks[0].ID == "" {
			t.Fatalf("created = %+v", tp)
		}
		if _, err := s.CreateTemplate(ctx, "app_1", &Template{Name: "order CONFIRMATION"}, ""); err == nil {
			t.Error("a second template with the same name was made")
		}
		if _, err := s.CreateTemplate(ctx, "app_2", &Template{Name: "Order confirmation"}, ""); err != nil {
			t.Errorf("another app may reuse the name: %v", err)
		}
		// Renaming alone keeps the version; changing the content adds one.
		tp.Name = "Order receipt"
		if tp, err = s.UpdateTemplate(ctx, "app_1", tp.ID, tp, "kim@x.com"); err != nil || tp.Version != 1 {
			t.Fatalf("rename: %v %d", err, tp.Version)
		}
		tp.Subject = "Receipt {{ order.Number }}"
		if tp, err = s.UpdateTemplate(ctx, "app_1", "order receipt", tp, "bo@x.com"); err != nil || tp.Version != 2 || tp.UpdatedBy != "bo@x.com" {
			t.Fatalf("change: %v %+v", err, tp)
		}
		vs, err := s.Versions(ctx, "app_1", tp.ID)
		if err != nil || len(vs) != 2 || vs[0].Version != 2 || vs[1].Subject != "Order {{ order.Number }} for {{ customer.Name }}" {
			t.Fatalf("versions = %+v %v", vs, err)
		}
		if tp, err = s.Restore(ctx, "app_1", tp.ID, 1, "kim@x.com"); err != nil || tp.Version != 3 || tp.Subject != "Order {{ order.Number }} for {{ customer.Name }}" || tp.Name != "Order receipt" {
			t.Fatalf("restore: %v %+v", err, tp)
		}
		if _, err := s.Template(ctx, "app_2", tp.ID); err != ErrNotFound {
			t.Errorf("another app's template was found: %v", err)
		}

		// System emails: built in until saved, back to built in on reset.
		sys, custom, _ := s.SystemTemplate(ctx, PasswordReset)
		if custom || sys.Subject != "Reset your Pimpbase password" {
			t.Fatalf("system = %v %+v", custom, sys)
		}
		sys.Subject = "New password for {{ email }}"
		if _, err := s.SaveSystem(ctx, PasswordReset, sys, "admin"); err != nil {
			t.Fatal(err)
		}
		log.Sent = nil
		if _, err := s.SendSystem(ctx, PasswordReset, "kim@x.com", map[string]any{"email": "kim@x.com", "link": "https://x"}); err != nil || log.Last().Subject != "New password for kim@x.com" {
			t.Fatalf("send system: %v %+v", err, log.Last())
		}
		if list, _ := s.Templates(ctx, "app_1"); len(list) != 1 {
			t.Errorf("system emails show among app templates: %d", len(list))
		}
		s.ResetSystem(ctx, PasswordReset)
		if _, custom, _ := s.SystemTemplate(ctx, PasswordReset); custom {
			t.Error("reset kept the saved system email")
		}
		if err := s.DeleteApp(ctx, "app_1"); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.Templates(ctx, "app_1"); len(list) != 0 {
			t.Error("templates outlived their app")
		}
	})
}
