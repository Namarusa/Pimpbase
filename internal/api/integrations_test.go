package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pimpbase/pimpbase/internal/integrations"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// fakeStripe stands in for api.stripe.com. It is not Stripe: it only
// answers the calls these tests make, the way Stripe's documentation
// describes them.
type fakeStripe struct {
	mu        sync.Mutex
	calls     []stripeCall
	customers []map[string]any
	failNext  map[string]int // path → 500s to give first
}

type stripeCall struct {
	Method, Path, Key, Idem string
	Form                    url.Values
}

func (f *fakeStripe) handler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.calls = append(f.calls, stripeCall{r.Method, r.URL.Path, key, r.Header.Get("Idempotency-Key"), r.Form})
	w.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(key, "sk_test_") && !strings.HasPrefix(key, "sk_live_") {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"Invalid API Key provided"}}`))
		return
	}
	if n := f.failNext[r.URL.Path]; n > 0 {
		f.failNext[r.URL.Path] = n - 1
		w.WriteHeader(500)
		w.Write([]byte(`{"error":{"message":"try again"}}`))
		return
	}
	live := strings.HasPrefix(key, "sk_live_")
	out := func(v any) { json.NewEncoder(w).Encode(v) }
	switch {
	case r.URL.Path == "/v1/balance":
		out(map[string]any{"object": "balance", "livemode": live})
	case r.URL.Path == "/v1/customers" && r.Method == "POST":
		out(map[string]any{"id": "cus_new", "object": "customer", "email": r.Form.Get("email"), "livemode": live})
	case r.URL.Path == "/v1/customers" && r.Method == "GET":
		after := r.Form.Get("starting_after")
		start := 0
		for i, c := range f.customers {
			if c["id"] == after {
				start = i + 1
			}
		}
		end := min(start+2, len(f.customers))
		out(map[string]any{"object": "list", "data": f.customers[start:end], "has_more": end < len(f.customers)})
	case r.URL.Path == "/v1/checkout/sessions":
		out(map[string]any{"id": "cs_test_1", "object": "checkout.session", "url": "https://checkout.stripe.test/c/pay/cs_test_1", "status": "open", "amount_total": 9900})
	case r.URL.Path == "/v1/invoices":
		out(map[string]any{"id": "in_1", "object": "invoice", "status": "draft", "customer": r.Form.Get("customer")})
	case r.URL.Path == "/v1/invoiceitems":
		out(map[string]any{"id": fmt.Sprintf("ii_%d", len(f.calls)), "object": "invoiceitem"})
	case r.URL.Path == "/v1/invoices/in_1/finalize":
		out(map[string]any{"id": "in_1", "status": "open", "hosted_invoice_url": "https://invoice.stripe.test/i/in_1"})
	case r.URL.Path == "/v1/invoices/in_1/send":
		out(map[string]any{"id": "in_1", "status": "open", "hosted_invoice_url": "https://invoice.stripe.test/i/in_1"})
	case r.URL.Path == "/v1/refunds":
		if r.Form.Get("payment_intent") == "pi_refunded" {
			w.WriteHeader(400)
			out(map[string]any{"error": map[string]any{"message": "Charge ch_1 has already been refunded."}})
			return
		}
		out(map[string]any{"id": "re_1", "status": "succeeded", "amount": 500})
	case r.URL.Path == "/v1/checkout/sessions/cs_test_1":
		out(map[string]any{"id": "cs_test_1", "status": "complete"})
	default:
		w.WriteHeader(404)
		out(map[string]any{"error": map[string]any{"message": "Unrecognized request URL " + r.URL.Path}})
	}
}

func (f *fakeStripe) find(method, path string) []stripeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []stripeCall
	for _, c := range f.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func signedPost(t *testing.T, srvURL, path string, body []byte, h http.Header) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", srvURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h {
		req.Header[k] = v
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestStripeIntegration(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		fake := &fakeStripe{failNext: map[string]int{"/v1/checkout/sessions": 1}}
		for i := 1; i <= 5; i++ {
			fake.customers = append(fake.customers, map[string]any{"id": fmt.Sprintf("cus_%d", i), "object": "customer", "email": fmt.Sprintf("c%d@example.com", i), "name": fmt.Sprintf("Customer %d", i), "balance": float64(-1250 * i), "created": float64(1761300000 + i), "livemode": true, "delinquent": i == 3})
		}
		stripe := httptest.NewServer(http.HandlerFunc(fake.handler))
		defer stripe.Close()

		api := New(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		api.Integrations.Client = stripe.Client()
		srv := httptest.NewServer(api.Handler())
		defer srv.Close()
		defer api.Engine.Stop(t.Context())

		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Shop"}, 201)
		base := "/api/apps/shop"
		orders := owner.do("POST", base+"/tables", map[string]any{"name": "Orders", "fields": []any{
			map[string]any{"name": "Ref", "type": "text"}, map[string]any{"name": "Status", "type": "text"}, map[string]any{"name": "Paid", "type": "number"},
		}}, 201)

		// The catalog lists Stripe first among the official ones.
		cat := owner.do("GET", "/api/integrations", nil, 200)["list"].([]any)
		if len(cat) < 7 || cat[0].(map[string]any)["official"] != true {
			t.Fatalf("catalog %v", cat)
		}

		// A key that is not a Stripe key is refused.
		owner.do("POST", base+"/integrations", map[string]any{"kind": "stripe", "secrets": map[string]any{"secretKey": "pk_live_123"}}, 400)
		conn := owner.do("POST", base+"/integrations", map[string]any{
			"kind": "stripe", "settings": map[string]any{"baseUrl": stripe.URL},
			"secrets":     map[string]any{"secretKey": "sk_live_livekey123456", "webhookSecret": "whsec_livesecret"},
			"testSecrets": map[string]any{"secretKey": "sk_test_testkey123456"},
		}, 201)
		connID := conn["id"].(string)
		raw, _ := json.Marshal(conn)
		if strings.Contains(string(raw), "livekey123456") || strings.Contains(string(raw), "whsec_livesecret") {
			t.Fatalf("a secret came back: %s", raw)
		}
		if st := conn["secrets"].(map[string]any)["secretKey"].(map[string]any); st["live"] != true || st["test"] != true {
			t.Fatalf("secret states %v", st)
		}
		if h := conn["hints"].(map[string]any)["secretKey"].(map[string]any); h["live"] != "sk_live_…3456" {
			t.Fatalf("hint %v", h)
		}
		hook, _ := conn["hookUrl"].(string)
		if !strings.Contains(hook, "/api/integrations/hooks/ihook_") {
			t.Fatalf("hook address %q", hook)
		}
		hookPath := hook[strings.Index(hook, "/api/"):]

		// Testing the connection uses the key of the environment asked for.
		if res := owner.do("POST", base+"/integrations/"+connID+"/test", map[string]any{"env": "test"}, 200); res["livemode"] != false {
			t.Fatalf("test %v", res)
		}
		if res := owner.do("POST", base+"/integrations/"+connID+"/test", map[string]any{"env": "live"}, 200); res["livemode"] != true {
			t.Fatalf("live test %v", res)
		}

		// A button flow: create a customer, then a checkout session.
		flow := owner.do("POST", base+"/flows", map[string]any{
			"name": "Checkout", "enabled": true,
			"trigger": map[string]any{"type": "manual", "parameters": []any{map[string]any{"name": "email", "type": "text"}, map[string]any{"name": "qty", "type": "number", "default": "2"}}},
			"steps": []any{
				map[string]any{"name": "Customer", "type": "integration", "inputs": map[string]any{"connection": connID, "action": "create_customer", "params": map[string]any{
					"email": "{{ trigger.params.email }}", "metadata": []any{map[string]any{"key": "source", "value": "pimpbase"}},
				}}},
				map[string]any{"name": "Session", "type": "integration", "inputs": map[string]any{"connection": connID, "action": "create_checkout_session", "params": map[string]any{
					"mode": "payment", "customer": "{{ steps.Customer.id }}", "successUrl": "https://shop.example/thanks?s={CHECKOUT_SESSION_ID}",
					"lineItems": []any{map[string]any{"name": "Tea box", "amount": "49.50", "currency": "eur", "quantity": "{{ trigger.params.qty }}"}},
				}}},
				map[string]any{"name": "Result", "type": "return", "inputs": map[string]any{"value": "{{ steps.Session.url }}"}},
			},
		}, 201)
		flowID := flow["id"].(string)
		run := owner.do("POST", base+"/flows/"+flowID+"/test", map[string]any{"trigger": map[string]any{"params": map[string]any{"email": "jane@example.com"}}}, 200)
		if run["status"] != "succeeded" || run["output"] != "https://checkout.stripe.test/c/pay/cs_test_1" {
			t.Fatalf("run %v", run)
		}
		runJSON, _ := json.Marshal(run)
		if strings.Contains(string(runJSON), "testkey123456") {
			t.Fatal("the run log shows the key")
		}
		cust := fake.find("POST", "/v1/customers")[0]
		if cust.Key != "sk_test_testkey123456" || cust.Form.Get("email") != "jane@example.com" || cust.Form.Get("metadata[source]") != "pimpbase" {
			t.Fatalf("customer call %+v", cust)
		}
		sessions := fake.find("POST", "/v1/checkout/sessions")
		if len(sessions) != 2 || sessions[0].Idem == "" || sessions[0].Idem != sessions[1].Idem {
			t.Fatalf("checkout should be retried once with the same idempotency key: %+v", sessions)
		}
		f := sessions[1].Form
		for k, want := range map[string]string{
			"mode": "payment", "customer": "cus_new", "line_items[0][price_data][unit_amount]": "4950",
			"line_items[0][price_data][currency]": "eur", "line_items[0][price_data][product_data][name]": "Tea box",
			"line_items[0][quantity]": "2", "success_url": "https://shop.example/thanks?s={CHECKOUT_SESSION_ID}",
		} {
			if f.Get(k) != want {
				t.Errorf("checkout %s = %q, want %q (form %v)", k, f.Get(k), want, f)
			}
		}
		// A second run sends new keys, so it is a new session.
		owner.do("POST", base+"/flows/"+flowID+"/test", map[string]any{"trigger": map[string]any{"params": map[string]any{"email": "jo@example.com"}}}, 200)
		if again := fake.find("POST", "/v1/checkout/sessions"); again[2].Idem == sessions[0].Idem {
			t.Fatal("a new run reused the idempotency key")
		}

		// An invoice takes several calls, each with its own key.
		inv := owner.do("POST", base+"/integrations/"+connID+"/run", map[string]any{"action": "create_invoice", "inputs": map[string]any{
			"customer": "cus_new", "send": true,
			"lines": []any{map[string]any{"description": "Consulting", "amount": "120", "currency": "eur"}, map[string]any{"description": "Travel", "amount": "35.10", "currency": "eur"}},
		}}, 200)
		if inv["output"].(map[string]any)["hosted_invoice_url"] != "https://invoice.stripe.test/i/in_1" {
			t.Fatalf("invoice %v", inv)
		}
		items := fake.find("POST", "/v1/invoiceitems")
		if len(items) != 2 || items[1].Form.Get("amount") != "3510" || items[0].Form.Get("invoice") != "in_1" || items[0].Idem == items[1].Idem {
			t.Fatalf("invoice items %+v", items)
		}
		if len(fake.find("POST", "/v1/invoices/in_1/send")) != 1 {
			t.Fatal("the invoice was not sent")
		}
		if c := fake.find("POST", "/v1/invoices")[0]; c.Form.Get("collection_method") != "send_invoice" || c.Form.Get("days_until_due") != "30" {
			t.Fatalf("invoice call %+v", c.Form)
		}

		// Stripe's refusals come through as the step's error.
		res := owner.raw("POST", base+"/integrations/"+connID+"/run", map[string]any{"action": "create_refund", "inputs": map[string]any{"paymentIntent": "pi_refunded"}}, 502)
		if !strings.Contains(string(res), "Stripe answered 400: Charge ch_1 has already been refunded.") {
			t.Fatalf("refund error %s", res)
		}
		// Getting an object puts a slashed object type in the path as is.
		owner.do("POST", base+"/integrations/"+connID+"/run", map[string]any{"action": "retrieve", "inputs": map[string]any{"object": "checkout/sessions", "id": "cs_test_1"}}, 200)
		owner.do("POST", base+"/integrations/"+connID+"/run", map[string]any{"action": "retrieve", "inputs": map[string]any{"object": "../../v2", "id": "x"}}, 400)

		// Events: a flow on paid checkouts adds an order.
		evFlow := owner.do("POST", base+"/flows", map[string]any{
			"name": "Paid", "enabled": true,
			"trigger": map[string]any{"type": "integration", "connection": connID, "events": []any{"checkout.session.completed"}},
			"steps": []any{map[string]any{"name": "Order", "type": "create_row", "inputs": map[string]any{"table": orders["id"], "values": []any{
				map[string]any{"field": "Ref", "value": "{{ trigger.object.client_reference_id }}"},
				map[string]any{"field": "Status", "value": "Paid"},
				map[string]any{"field": "Paid", "value": "{{ = trigger.object.amount_total / 100 }}"},
			}}}},
		}, 201)
		event := func(id, typ string) []byte {
			b, _ := json.Marshal(map[string]any{"id": id, "object": "event", "type": typ, "data": map[string]any{"object": map[string]any{"id": "cs_1", "object": "checkout.session", "client_reference_id": "order-7", "amount_total": 9900}}})
			return b
		}
		verify := integrations.Verify{Scheme: "stripe"}
		body := event("evt_1", "checkout.session.completed")
		if code, out := signedPost(t, srv.URL, hookPath, body, integrations.Sign(verify, "whsec_livesecret", body, time.Now())); code != 200 {
			t.Fatalf("event: %d %s", code, out)
		}
		waitRuns(t, owner, base+"/flows/"+evFlow["id"].(string), 1)
		// Stripe sends it again: nothing more starts.
		if code, _ := signedPost(t, srv.URL, hookPath, body, integrations.Sign(verify, "whsec_livesecret", body, time.Now())); code != 200 {
			t.Fatal("a repeat should be answered 200")
		}
		// Wrong secret, no signature, an old signature: refused.
		if code, out := signedPost(t, srv.URL, hookPath, body, integrations.Sign(verify, "whsec_other", body, time.Now())); code != 401 || !strings.Contains(out, "does not match") {
			t.Fatalf("bad signature: %d %s", code, out)
		}
		if code, _ := signedPost(t, srv.URL, hookPath, body, nil); code != 401 {
			t.Fatal("unsigned event accepted")
		}
		other := event("evt_2", "checkout.session.completed")
		if code, out := signedPost(t, srv.URL, hookPath, other, integrations.Sign(verify, "whsec_livesecret", other, time.Now().Add(-10*time.Minute))); code != 401 || !strings.Contains(out, "old") {
			t.Fatalf("replayed event: %d %s", code, out)
		}
		// Another type is recorded but starts nothing.
		ignored := event("evt_3", "invoice.paid")
		signedPost(t, srv.URL, hookPath, ignored, integrations.Sign(verify, "whsec_livesecret", ignored, time.Now()))
		time.Sleep(100 * time.Millisecond)
		runs := waitRuns(t, owner, base+"/flows/"+evFlow["id"].(string), 1)
		if len(runs) != 1 {
			t.Fatalf("%d runs, want 1", len(runs))
		}
		rows := owner.do("GET", base+"/tables/"+orders["id"].(string)+"/rows", nil, 200)["rows"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["Ref"] != "order-7" || rows[0].(map[string]any)["Paid"] != 99.0 {
			t.Fatalf("orders %v", rows)
		}
		evs := owner.do("GET", base+"/integrations/"+connID+"/events", nil, 200)["list"].([]any)
		statuses := map[string]int{}
		for _, e := range evs {
			statuses[e.(map[string]any)["status"].(string)]++
		}
		if statuses["handled"] != 2 || statuses["rejected"] != 3 {
			t.Fatalf("events %v", evs)
		}
		// A test run of the event flow uses the newest received event.
		test := owner.do("POST", base+"/flows/"+evFlow["id"].(string)+"/test", nil, 200)
		if test["trigger"].(map[string]any)["id"] != "evt_1" {
			t.Fatalf("test trigger %v", test["trigger"])
		}

		// Sync: customers become a table, page by page.
		sc := owner.do("POST", base+"/integrations/"+connID+"/syncs/customers/run", nil, 200)
		if sc["created"] != 5.0 || sc["lastOk"] != true {
			t.Fatalf("sync %v", sc)
		}
		tableID := sc["table"].(string)
		if pages := fake.find("GET", "/v1/customers"); len(pages) != 3 || pages[2].Form.Get("starting_after") != "cus_4" || pages[0].Key != "sk_live_livekey123456" {
			t.Fatalf("pages %+v", pages)
		}
		crow := owner.do("GET", base+"/tables/"+tableID+"/rows?sort=Stripe%20ID", nil, 200)["rows"].([]any)
		first := crow[0].(map[string]any)
		if len(crow) != 5 || first["Email"] != "c1@example.com" || first["Balance"] != -12.5 || crow[2].(map[string]any)["Delinquent"] != true {
			t.Fatalf("synced rows %v", crow)
		}
		// Later: one customer is gone, one changed.
		fake.mu.Lock()
		fake.customers = append(fake.customers[:1], fake.customers[2:]...)
		fake.customers[0]["name"] = "Renamed"
		fake.mu.Unlock()
		sc = owner.do("POST", base+"/integrations/"+connID+"/syncs/customers/run", nil, 200)
		if sc["created"] != 0.0 || sc["updated"] != 1.0 || sc["deleted"] != 1.0 {
			t.Fatalf("second sync %v", sc)
		}
		// With live updates on, customer events change rows at once.
		owner.do("PATCH", base+"/integrations/"+connID, map[string]any{"syncs": []any{map[string]any{"key": "customers", "enabled": true, "live": true}}}, 200)
		upd, _ := json.Marshal(map[string]any{"id": "evt_9", "type": "customer.updated", "data": map[string]any{"object": map[string]any{"id": "cus_4", "object": "customer", "email": "new@example.com", "name": "Customer 4"}}})
		signedPost(t, srv.URL, hookPath, upd, integrations.Sign(verify, "whsec_livesecret", upd, time.Now()))
		found := owner.do("GET", base+"/tables/"+tableID+"/rows?filter="+url.QueryEscape(`[Stripe ID] = "cus_4"`), nil, 200)["rows"].([]any)
		if len(found) != 1 || found[0].(map[string]any)["Email"] != "new@example.com" {
			t.Fatalf("live update %v", found)
		}
		del, _ := json.Marshal(map[string]any{"id": "evt_10", "type": "customer.deleted", "data": map[string]any{"object": map[string]any{"id": "cus_4", "object": "customer"}}})
		signedPost(t, srv.URL, hookPath, del, integrations.Sign(verify, "whsec_livesecret", del, time.Now()))
		if n := len(owner.do("GET", base+"/tables/"+tableID+"/rows", nil, 200)["rows"].([]any)); n != 3 {
			t.Fatalf("%d rows after the delete event, want 3", n)
		}

		// The ready-made payment flow runs from a button and gives back
		// the checkout address.
		pf := owner.do("POST", base+"/integrations/"+connID+"/payment-flow", map[string]any{"product": "Ticket", "amount": 12.5, "currency": "EUR"}, 201)
		out := owner.do("POST", base+"/flows/"+pf["id"].(string)+"/run", map[string]any{"params": map[string]any{"quantity": 3}, "test": true}, 200)
		if out["status"] != "succeeded" || out["output"] != "https://checkout.stripe.test/c/pay/cs_test_1" {
			t.Fatalf("payment flow %v", out)
		}
		last := fake.find("POST", "/v1/checkout/sessions")
		if lf := last[len(last)-1].Form; lf.Get("line_items[0][price_data][unit_amount]") != "1250" || lf.Get("line_items[0][quantity]") != "3" || lf.Get("line_items[0][price_data][currency]") != "eur" {
			t.Fatalf("payment flow form %v", lf)
		}

		// A connection in use is not deleted without force.
		owner.do("DELETE", base+"/integrations/"+connID, nil, 409)
		owner.do("DELETE", base+"/integrations/"+connID+"?force=true", nil, 200)
		if code, _ := signedPost(t, srv.URL, hookPath, body, nil); code != 404 {
			t.Fatal("the deleted connection's address still answers")
		}
	})
}

func TestWebhookSignatures(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		api := New(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv := httptest.NewServer(api.Handler())
		defer srv.Close()
		defer api.Engine.Stop(t.Context())
		owner := newClient(t, srv.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "ada@example.com", "name": "Ada", "password": "correct horse"}, 200)
		owner.do("POST", "/api/apps", map[string]any{"name": "Hooks"}, 201)
		base := "/api/apps/hooks"
		owner.do("PUT", base+"/variables/HOOK_SECRET", map[string]any{"secret": true, "production": "s3cr3t-value", "development": "dev"}, 200)

		// A signature check needs a variable to read the secret from.
		owner.do("POST", base+"/flows", map[string]any{"name": "Bad", "trigger": map[string]any{"type": "webhook", "signature": map[string]any{"scheme": "hmac"}}}, 400)
		cases := []struct {
			name string
			sig  map[string]any
			v    integrations.Verify
		}{
			{"Generic", map[string]any{"scheme": "hmac", "header": "X-Signature-256", "prefix": "sha256=", "secret": "HOOK_SECRET"}, integrations.Verify{Scheme: "hmac", Header: "X-Signature-256", Prefix: "sha256="}},
			{"Base64", map[string]any{"scheme": "hmac", "header": "X-Sig", "algorithm": "sha512", "encoding": "base64", "secret": "HOOK_SECRET"}, integrations.Verify{Scheme: "hmac", Header: "X-Sig", Algorithm: "sha512", Encoding: "base64"}},
			{"Shopify", map[string]any{"scheme": "shopify", "secret": "HOOK_SECRET"}, integrations.Verify{Scheme: "hmac", Header: "X-Shopify-Hmac-Sha256", Encoding: "base64"}},
			{"Github", map[string]any{"scheme": "github", "secret": "HOOK_SECRET"}, integrations.Verify{Scheme: "github"}},
			{"Slack", map[string]any{"scheme": "slack", "secret": "HOOK_SECRET"}, integrations.Verify{Scheme: "slack"}},
			{"Standard", map[string]any{"scheme": "standard", "secret": "HOOK_SECRET"}, integrations.Verify{Scheme: "standard"}},
		}
		for _, c := range cases {
			f := owner.do("POST", base+"/flows", map[string]any{"name": c.name, "enabled": true, "trigger": map[string]any{"type": "webhook", "respond": true, "signature": c.sig},
				"steps": []any{map[string]any{"name": "Result", "type": "return", "inputs": map[string]any{"value": "{{ trigger.verified }}"}}}}, 201)
			path := "/api/hooks/" + f["hookToken"].(string)
			body := []byte(`{"order":42}`)
			if code, out := signedPost(t, srv.URL, path, body, nil); code != 401 {
				t.Errorf("%s: unsigned call answered %d %s", c.name, code, out)
			}
			if code, _ := signedPost(t, srv.URL, path, body, integrations.Sign(c.v, "wrong", body, time.Now())); code != 401 {
				t.Errorf("%s: wrong secret accepted", c.name)
			}
			code, out := signedPost(t, srv.URL, path, body, integrations.Sign(c.v, "s3cr3t-value", body, time.Now()))
			if code != 200 || strings.TrimSpace(out) != "true" {
				t.Errorf("%s: signed call answered %d %s", c.name, code, out)
			}
		}
		// Flows without a check still take any call.
		f := owner.do("POST", base+"/flows", map[string]any{"name": "Open", "enabled": true, "trigger": map[string]any{"type": "webhook"}}, 201)
		if code, _ := signedPost(t, srv.URL, "/api/hooks/"+f["hookToken"].(string), []byte(`{}`), nil); code != 202 {
			t.Errorf("open hook answered %d", code)
		}
	})
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
