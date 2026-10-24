package integrations

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pimpbase/pimpbase/internal/apps"
	"github.com/pimpbase/pimpbase/internal/files"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
	"github.com/pimpbase/pimpbase/internal/tables"
)

// These tests run every built-in integration against stand-in servers
// that answer the way each service's documentation describes. None of
// them reach the real services.

type env struct {
	t     *testing.T
	svc   *Service
	app   string
	ctx   context.Context
	files *files.Service
}

func newEnv(t *testing.T, s *store.Store) *env {
	ctx := context.Background()
	a, err := apps.NewService(s).Create(ctx, "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	fs := files.NewService(s)
	svc := NewService(s, cat, tables.NewService(s), fs, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.sleep = func(context.Context, time.Duration) error { return nil }
	return &env{t: t, svc: svc, app: a.ID, ctx: ctx, files: fs}
}

func (e *env) connect(kind string, settings, secrets map[string]string) *Connection {
	e.t.Helper()
	c, err := e.svc.Create(e.ctx, e.app, ConnInput{Kind: kind, Settings: settings, Secrets: secrets})
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *env) run(c *Connection, action string, in map[string]any) (any, error) {
	res, err := e.svc.Run(e.ctx, Call{AppID: e.app, Connection: c.ID, Action: action, Inputs: in, Key: "run|step|0"})
	if err != nil {
		return nil, err
	}
	return res.Output, nil
}

type seen struct {
	mu   sync.Mutex
	reqs []*http.Request
	body []string
}

func (s *seen) add(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r)
	s.body = append(s.body, string(b))
	return string(b)
}

func TestSlack(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		e := newEnv(t, s)
		var got seen
		slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := got.add(r)
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/chat.postMessage":
				if r.Header.Get("Authorization") != "Bearer xoxb-1" {
					w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
					return
				}
				if strings.Contains(body, `"channel":"#nope"`) {
					w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
					return
				}
				w.Write([]byte(`{"ok":true,"channel":"C1","ts":"1761300000.000100"}`))
			case "/hook":
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte("ok"))
			}
		}))
		defer slack.Close()
		e.svc.Client = slack.Client()
		c := e.connect("slack", map[string]string{"baseUrl": slack.URL + "/api", "channel": "#sales"}, map[string]string{"botToken": "xoxb-1", "signingSecret": "slacksecret", "webhookUrl": slack.URL + "/hook"})

		out, err := e.run(c, "post_message", map[string]any{"text": "New lead: *Acme*"})
		if err != nil || out.(map[string]any)["ts"] != "1761300000.000100" {
			t.Fatalf("post: %v %v", out, err)
		}
		if !strings.Contains(got.body[0], `"channel":"#sales"`) {
			t.Fatalf("the default channel was not used: %s", got.body[0])
		}
		// Slack answers 200 with ok false; that is an error.
		if _, err := e.run(c, "post_message", map[string]any{"text": "x", "channel": "#nope"}); err == nil || !strings.Contains(err.Error(), "channel_not_found") {
			t.Fatalf("ok:false should fail: %v", err)
		}
		// The incoming webhook answers plain "ok".
		if _, err := e.run(c, "webhook_message", map[string]any{"text": "hi"}); err != nil {
			t.Fatal(err)
		}

		// Events: the address check echoes the challenge, signed.
		challenge := []byte(`{"type":"url_verification","challenge":"abc123"}`)
		h := Sign(Verify{Scheme: "slack"}, "slacksecret", challenge, time.Now())
		rep, err := e.svc.Receive(e.ctx, c.HookToken, Incoming{Header: h, Body: challenge}, nil)
		if err != nil || rep.Status != 200 || string(rep.Body) != "abc123" {
			t.Fatalf("challenge: %+v %v", rep, err)
		}
		ev := []byte(`{"type":"event_callback","event_id":"Ev1","event":{"type":"app_mention","text":"hello","channel":"C1"}}`)
		var started *Event
		start := func(ctx context.Context, c *Connection, e *Event) (int, error) { started = e; return 1, nil }
		rep, _ = e.svc.Receive(e.ctx, c.HookToken, Incoming{Header: Sign(Verify{Scheme: "slack"}, "slacksecret", ev, time.Now()), Body: ev}, start)
		if rep.Status != 200 || started == nil || started.Type != "app_mention" || started.ID != "Ev1" || Dig(started.Object, "text") != "hello" {
			t.Fatalf("event: %+v %+v", rep, started)
		}
		rep, _ = e.svc.Receive(e.ctx, c.HookToken, Incoming{Header: http.Header{"X-Slack-Signature": {"v0=00"}, "X-Slack-Request-Timestamp": {fmt.Sprint(time.Now().Unix())}}, Body: ev}, start)
		if rep.Status != 401 {
			t.Fatalf("bad signature answered %d", rep.Status)
		}
	})
}

func TestGitHub(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		e := newEnv(t, s)
		var got seen
		issues := []map[string]any{}
		for i := 1; i <= 3; i++ {
			issues = append(issues, map[string]any{"id": float64(100 + i), "number": float64(i), "title": fmt.Sprintf("Issue %d", i), "state": "open", "user": map[string]any{"login": "octocat"}, "labels": []any{map[string]any{"name": "bug"}, map[string]any{"name": "ui"}}, "created_at": "2026-10-01T10:00:00Z"})
		}
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got.add(r)
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" || r.Header.Get("Authorization") != "Bearer ghp_x" {
				w.WriteHeader(401)
				w.Write([]byte(`{"message":"Bad credentials"}`))
				return
			}
			switch {
			case r.Method == "POST" && r.URL.Path == "/repos/acme/web/issues":
				w.WriteHeader(201)
				w.Write([]byte(`{"id":1,"number":42,"html_url":"https://github.com/acme/web/issues/42","state":"open"}`))
			case r.Method == "GET" && r.URL.Path == "/repos/acme/web/issues":
				page := r.URL.Query().Get("page")
				switch page {
				case "1":
					json.NewEncoder(w).Encode(issues[:2])
				case "2":
					json.NewEncoder(w).Encode(issues[2:])
				default:
					w.Write([]byte(`[]`))
				}
			case r.URL.Path == "/repos/acme/web/actions/workflows/deploy.yml/dispatches":
				w.WriteHeader(204)
			default:
				w.WriteHeader(404)
				w.Write([]byte(`{"message":"Not Found"}`))
			}
		}))
		defer gh.Close()
		e.svc.Client = gh.Client()
		c := e.connect("github", map[string]string{"baseUrl": gh.URL, "owner": "acme", "repo": "web"}, map[string]string{"token": "ghp_x", "webhookSecret": "hooksecret"})

		out, err := e.run(c, "create_issue", map[string]any{"title": "Broken", "labels": "bug, from-app"})
		if err != nil || out.(map[string]any)["number"] != 42.0 {
			t.Fatalf("issue: %v %v", out, err)
		}
		if !strings.Contains(got.body[0], `"labels":["bug","from-app"]`) {
			t.Fatalf("labels: %s", got.body[0])
		}
		if out, err := e.run(c, "run_workflow", map[string]any{"workflow": "deploy.yml", "ref": "main"}); err != nil || out.(map[string]any)["ok"] != true {
			t.Fatalf("workflow: %v %v", out, err)
		}
		if _, err := e.run(c, "comment", map[string]any{"number": "7", "body": "x", "repo": "missing"}); err == nil || !strings.Contains(err.Error(), "GitHub answered 404: Not Found") {
			t.Fatalf("404: %v", err)
		}

		// Issues sync page by page into a table.
		sc, err := e.svc.RunSync(e.ctx, e.app, c.ID, "issues")
		if err != nil || sc.Created != 3 {
			t.Fatalf("sync: %+v %v", sc, err)
		}
		page, _ := e.svc.Tables.ListRows(e.ctx, rbac.Actor{Role: rbac.Admin, Builder: true}, e.app, sc.Table, tables.Query{Sort: "Number"})
		if len(page.Rows) != 3 || page.Rows[0]["Labels"] != "bug, ui" || page.Rows[0]["Author"] != "octocat" {
			t.Fatalf("rows %v", page.Rows)
		}

		// Events: the type joins the header and the action; the item of
		// issue events is under issue.
		e.svc.Update(e.ctx, e.app, c.ID, ConnInput{Syncs: []*SyncConfig{{Key: "issues", Enabled: true, Live: true}}})
		body := []byte(`{"action":"closed","issue":{"id":101,"number":1,"title":"Issue 1","state":"closed","user":{"login":"octocat"},"labels":[]}}`)
		h := Sign(Verify{Scheme: "github"}, "hooksecret", body, time.Now())
		h.Set("X-GitHub-Event", "issues")
		h.Set("X-GitHub-Delivery", "d-1")
		var typ string
		rep, _ := e.svc.Receive(e.ctx, c.HookToken, Incoming{Header: h, Body: body}, func(ctx context.Context, c *Connection, ev *Event) (int, error) { typ = ev.Type; return 0, nil })
		if rep.Status != 200 || typ != "issues.closed" {
			t.Fatalf("event %d %q", rep.Status, typ)
		}
		page, _ = e.svc.Tables.ListRows(e.ctx, rbac.Actor{Role: rbac.Admin, Builder: true}, e.app, sc.Table, tables.Query{Filter: `[Number] = 1`})
		if page.Rows[0]["State"] != "closed" {
			t.Fatalf("live update %v", page.Rows)
		}
	})
}

func TestTwilio(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		e := newEnv(t, s)
		var got seen
		const sid = "AC" + "0123456789abcdef0123456789abcdef"
		tw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got.add(r)
			u, p, _ := r.BasicAuth()
			w.Header().Set("Content-Type", "application/json")
			if u != sid || p != "tok" {
				w.WriteHeader(401)
				w.Write([]byte(`{"code":20003,"message":"Authenticate"}`))
				return
			}
			if r.URL.Path == "/2010-04-01/Accounts/"+sid+"/Messages.json" {
				w.WriteHeader(201)
				w.Write([]byte(`{"sid":"SM1","status":"queued","to":"+33612345678"}`))
				return
			}
			w.Write([]byte(`{"friendly_name":"Test","status":"active"}`))
		}))
		defer tw.Close()
		e.svc.Client = tw.Client()
		c := e.connect("twilio", map[string]string{"accountSid": sid, "from": "+15550001111", "baseUrl": tw.URL + "/2010-04-01/Accounts/{accountSid}"}, map[string]string{"authToken": "tok"})
		if out, err := e.svc.TestConnection(e.ctx, c, Live); err != nil || out["status"] != "active" {
			t.Fatalf("test: %v %v", out, err)
		}
		if _, err := e.run(c, "send_sms", map[string]any{"to": "+33612345678", "body": "Your order shipped"}); err != nil {
			t.Fatal(err)
		}
		form, _ := url.ParseQuery(got.body[1])
		if form.Get("From") != "+15550001111" || form.Get("To") != "+33612345678" || form.Get("Body") != "Your order shipped" {
			t.Fatalf("form %v", form)
		}
		// An incoming SMS is a signed form post; the answer is empty TwiML.
		in := url.Values{"MessageSid": {"SM9"}, "From": {"+33612345678"}, "Body": {"STOP"}}
		addr := "https://apps.example.com/api/integrations/hooks/" + c.HookToken
		h := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "X-Twilio-Signature": {TwilioSignature("tok", addr, in)}}
		var ev *Event
		rep, _ := e.svc.Receive(e.ctx, c.HookToken, Incoming{Header: h, Body: []byte(in.Encode()), URL: addr}, func(ctx context.Context, c *Connection, x *Event) (int, error) { ev = x; return 1, nil })
		if rep.Status != 200 || !strings.Contains(string(rep.Body), "<Response>") || ev.Type != "message.received" || Dig(ev.Object, "Body") != "STOP" {
			t.Fatalf("sms in: %+v %+v", rep, ev)
		}
		rep, _ = e.svc.Receive(e.ctx, c.HookToken, Incoming{Header: h, Body: []byte(in.Encode()), URL: "https://other.example.com/x"}, nil)
		if rep.Status != 401 {
			t.Fatal("a signature for another address was accepted")
		}
	})
}

func TestAirtableImport(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		e := newEnv(t, s)
		at := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path != "/v0/appX/Leads" {
				w.WriteHeader(404)
				w.Write([]byte(`{"error":{"type":"NOT_FOUND","message":"Could not find table Leads"}}`))
				return
			}
			if r.URL.Query().Get("offset") == "" {
				w.Write([]byte(`{"records":[{"id":"rec1","createdTime":"2026-10-01T09:00:00.000Z","fields":{"Name":"Acme","Deal size":1200,"Won":true}},{"id":"rec2","createdTime":"2026-10-02T09:00:00.000Z","fields":{"Name":"Globex"}}],"offset":"itr2"}`))
				return
			}
			w.Write([]byte(`{"records":[{"id":"rec3","createdTime":"2026-10-03T09:00:00.000Z","fields":{"Name":"Initech","Deal size":300}}]}`))
		}))
		defer at.Close()
		e.svc.Client = at.Client()
		c := e.connect("airtable", map[string]string{"baseUrl": at.URL + "/v0", "baseId": "appX"}, map[string]string{"token": "pat1"})
		e.svc.Update(e.ctx, e.app, c.ID, ConnInput{Syncs: []*SyncConfig{{Key: "records", Enabled: true, Params: map[string]string{"table": "Leads"}}}})
		sc, err := e.svc.RunSync(e.ctx, e.app, c.ID, "records")
		if err != nil || sc.Created != 3 {
			t.Fatalf("import %+v %v", sc, err)
		}
		tbl, _ := e.svc.Tables.Get(e.ctx, e.app, sc.Table)
		for _, name := range []string{"Airtable ID", "Name", "Deal size", "Won"} {
			if tbl.FieldByName(name) == nil {
				t.Errorf("no field %s in %v", name, tbl.Fields)
			}
		}
		if f := tbl.FieldByName("Deal size"); f != nil && f.Type != tables.TypeNumber {
			t.Errorf("Deal size is %s", f.Type)
		}
		page, _ := e.svc.Tables.ListRows(e.ctx, rbac.Actor{Role: rbac.Admin, Builder: true}, e.app, sc.Table, tables.Query{Filter: `[Name] = "Acme"`})
		if len(page.Rows) != 1 || page.Rows[0]["Deal size"] != 1200.0 || page.Rows[0]["Won"] != true {
			t.Fatalf("rows %v", page.Rows)
		}
		e.svc.Update(e.ctx, e.app, c.ID, ConnInput{Syncs: []*SyncConfig{{Key: "records", Enabled: true, Params: map[string]string{"table": "Nope"}}}})
		if _, err := e.svc.RunSync(e.ctx, e.app, c.ID, "records"); err == nil || !strings.Contains(err.Error(), "Could not find table") {
			t.Fatalf("missing table: %v", err)
		}
	})
}

func TestGoogleSheets(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		e := newEnv(t, s)
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		der, _ := x509.MarshalPKCS8PrivateKey(key)
		pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
		var tokens int
		var appended string
		g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/token" {
				r.ParseForm()
				if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(r.Form.Get("assertion"), ".") != 2 {
					w.WriteHeader(400)
					w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
				tokens++
				w.Write([]byte(`{"access_token":"ya29.test","token_type":"Bearer","expires_in":3600}`))
				return
			}
			if r.Header.Get("Authorization") != "Bearer ya29.test" {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":{"code":401,"message":"Request had invalid authentication credentials."}}`))
				return
			}
			switch {
			case strings.HasSuffix(r.URL.Path, ":append"):
				b, _ := io.ReadAll(r.Body)
				appended = r.URL.EscapedPath() + "?" + r.URL.RawQuery + " " + string(b)
				w.Write([]byte(`{"updates":{"updatedRange":"Orders!A5:C5","updatedRows":1}}`))
			case strings.HasPrefix(r.URL.Path, "/v4/spreadsheets/sheet1/values/"):
				w.Write([]byte(`{"range":"Orders!A1:C3","values":[["Name","Email","Total"],["Jane","jane@example.com","12"],["Jo","jo@example.com"]]}`))
			default:
				w.Write([]byte(`{"properties":{"title":"Orders 2026"}}`))
			}
		}))
		defer g.Close()
		e.svc.Client = g.Client()
		sa, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "bot@proj.iam.gserviceaccount.com", "private_key": pemKey, "private_key_id": "k1", "token_uri": g.URL + "/token"})
		c := e.connect("google_sheets", map[string]string{"baseUrl": g.URL + "/v4/spreadsheets", "spreadsheetId": "sheet1"}, map[string]string{"serviceAccount": string(sa)})
		if out, err := e.svc.TestConnection(e.ctx, c, Live); err != nil || out["properties.title"] != "Orders 2026" {
			t.Fatalf("test %v %v", out, err)
		}
		if _, err := e.run(c, "append_row", map[string]any{"range": "Orders!A:C", "values": []any{"Jane", "jane@example.com", 12.0}}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(appended, "/v4/spreadsheets/sheet1/values/Orders%21A:C:append?") || !strings.Contains(appended, "valueInputOption=USER_ENTERED") || !strings.HasSuffix(appended, `{"values":[["Jane","jane@example.com",12]]}`) {
			t.Fatalf("append %s", appended)
		}
		out, err := e.run(c, "read_range", map[string]any{"range": "Orders!A1:C3"})
		if err != nil {
			t.Fatal(err)
		}
		rows := out.(map[string]any)["rows"].([]any)
		if len(rows) != 2 || rows[0].(map[string]any)["Email"] != "jane@example.com" || rows[1].(map[string]any)["Total"] != "" {
			t.Fatalf("rows %v", rows)
		}
		if tokens != 1 {
			t.Errorf("%d tokens fetched; one should be kept", tokens)
		}
	})
}

func TestS3(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		e := newEnv(t, s)
		var mu sync.Mutex
		objects := map[string][]byte{}
		types := map[string]string{}
		s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIDTEST/") {
				w.WriteHeader(403)
				w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`))
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch r.Method {
			case "PUT":
				b, _ := io.ReadAll(r.Body)
				objects[r.URL.Path], types[r.URL.Path] = b, r.Header.Get("Content-Type")
				w.Header().Set("ETag", `"abc"`)
			case "GET":
				if r.URL.Query().Get("list-type") == "2" {
					fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated>`)
					for k, v := range objects {
						fmt.Fprintf(w, `<Contents><Key>%s</Key><Size>%d</Size></Contents>`, strings.TrimPrefix(k, "/bucket/"), len(v))
					}
					fmt.Fprint(w, `</ListBucketResult>`)
					return
				}
				b, ok := objects[r.URL.Path]
				if !ok {
					w.WriteHeader(404)
					w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`))
					return
				}
				w.Header().Set("Content-Type", types[r.URL.Path])
				w.Write(b)
			}
		}))
		defer s3.Close()
		e.svc.Client = s3.Client()
		c := e.connect("s3", map[string]string{"accessKeyId": "AKIDTEST", "bucket": "bucket", "endpoint": s3.URL, "pathStyle": "true", "region": "eu-west-3"}, map[string]string{"secretAccessKey": "secret"})
		if _, err := e.svc.TestConnection(e.ctx, c, Live); err != nil {
			t.Fatal(err)
		}
		// A Pimpbase file goes up under its own name when the key ends in /.
		tbl, err := e.svc.Tables.CreateTable(e.ctx, e.app, "Docs", []tables.FieldInput{{Name: "Name", Type: tables.TypeText}})
		if err != nil {
			t.Fatal(err)
		}
		f, err := e.files.Save(e.ctx, e.app, tbl.ID, "", "report.pdf", "application/pdf", strings.NewReader("%PDF-1.4 test"))
		if err != nil {
			t.Fatal(err)
		}
		out, err := e.run(c, "put_object", map[string]any{"key": "reports/", "file": map[string]any{"id": f.ID, "name": f.Name}})
		if err != nil || out.(map[string]any)["key"] != "reports/report.pdf" {
			t.Fatalf("put %v %v", out, err)
		}
		if string(objects["/bucket/reports/report.pdf"]) != "%PDF-1.4 test" || types["/bucket/reports/report.pdf"] != "application/pdf" {
			t.Fatalf("objects %v", objects)
		}
		e.run(c, "put_object", map[string]any{"key": "notes/a.json", "content": `{"a":1}`, "contentType": "application/json"})
		out, err = e.run(c, "get_object", map[string]any{"key": "notes/a.json"})
		if err != nil || Dig(out, "data.a") != 1.0 {
			t.Fatalf("get %v %v", out, err)
		}
		if _, err := e.run(c, "get_object", map[string]any{"key": "nope"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("missing key: %v", err)
		}
		out, _ = e.run(c, "list_objects", map[string]any{})
		if Dig(out, "count") != 2 {
			t.Fatalf("list %v", out)
		}
		out, _ = e.run(c, "presign", map[string]any{"key": "reports/report.pdf", "minutes": 15})
		link := Dig(out, "url").(string)
		if !strings.HasPrefix(link, s3.URL+"/bucket/reports/report.pdf?") || !strings.Contains(link, "X-Amz-Expires=900") || !strings.Contains(link, "X-Amz-Signature=") {
			t.Fatalf("link %s", link)
		}
	})
}

func TestEncodingAndInputs(t *testing.T) {
	got := formValues(map[string]any{"line_items": []any{map[string]any{"price": "price_1", "quantity": 2.0}}, "metadata": map[string]any{"b": "2", "a": true}, "amount": 1250.0})
	if got.Encode() != "amount=1250&line_items%5B0%5D%5Bprice%5D=price_1&line_items%5B0%5D%5Bquantity%5D=2&metadata%5Ba%5D=true&metadata%5Bb%5D=2" {
		t.Errorf("form %s", got.Encode())
	}
	in := &Input{Key: "amount", Label: "Amount", Kind: "number", Transform: "cents"}
	if v, _ := inputValue(in, map[string]any{"amount": "19.99"}); v != 1999.0 {
		t.Errorf("cents %v", v)
	}
	if _, err := inputValue(in, map[string]any{"amount": "lots"}); err == nil {
		t.Error("a word passed as a number")
	}
	if !MatchEvent([]string{"invoice.*"}, "invoice.paid") || MatchEvent([]string{"invoice.*"}, "charge.refunded") || !MatchEvent(nil, "x") {
		t.Error("event matching")
	}
	if h := hint("sk_test_51Habcdefghijk4242"); h != "sk_test_…4242" {
		t.Errorf("hint %q", h)
	}
}

func TestVerifySchemes(t *testing.T) {
	body := []byte(`{"x":1}`)
	now := time.Now()
	for _, v := range []Verify{{Scheme: "stripe"}, {Scheme: "github"}, {Scheme: "slack"}, {Scheme: "standard"}, {Scheme: "hmac", Header: "X-Sig", Prefix: "sha1=", Algorithm: "sha1"}} {
		secret := "whsec_c2VjcmV0LXZhbHVlLWZvci10ZXN0cw=="
		h := Sign(v, secret, body, now)
		if err := Check(v, secret, Signed{Header: h, Body: body, Now: now}); err != nil {
			t.Errorf("%s: %v", v.Scheme, err)
		}
		if err := Check(v, secret, Signed{Header: h, Body: []byte(`{"x":2}`), Now: now}); err == nil {
			t.Errorf("%s: a changed body passed", v.Scheme)
		}
		if err := Check(v, "", Signed{Header: h, Body: body, Now: now}); err == nil {
			t.Errorf("%s: no secret passed", v.Scheme)
		}
	}
}
