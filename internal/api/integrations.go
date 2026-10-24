package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pimpbase/pimpbase/internal/flows"
	"github.com/pimpbase/pimpbase/internal/integrations"
	"github.com/pimpbase/pimpbase/internal/rbac"
)

// Integrations: the catalog of services, an app's connections to them,
// trying their actions, syncs, and the webhook address each connection
// gets.

func (s *Server) integrationCatalog(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireBuilder(r); err != nil {
		return err
	}
	return writeJSON(w, 200, s.Integrations.Catalog.List())
}

// connectionView is a connection with its full webhook address.
type connectionView struct {
	*integrations.Connection
	HookURL string `json:"hookUrl,omitempty"`
}

func (s *Server) publicBase(r *http.Request) string {
	base := strings.TrimRight(s.PublicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || s.Secure || (s.BehindProxy && r.Header.Get("X-Forwarded-Proto") == "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base
}

func (s *Server) viewConn(r *http.Request, c *integrations.Connection) connectionView {
	v := connectionView{Connection: c}
	if m := s.Integrations.Catalog.Get(c.Kind); m != nil && m.Webhook != nil {
		v.HookURL = s.publicBase(r) + "/api/integrations/hooks/" + c.HookToken
	}
	return v
}

func (s *Server) listConnections(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Integrations.Connections(r.Context(), a.ID)
	if err != nil {
		return err
	}
	out := make([]connectionView, len(list))
	for i, c := range list {
		out[i] = s.viewConn(r, c)
	}
	return writeJSON(w, 200, out)
}

func (s *Server) getConnection(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.Connection(r.Context(), a.ID, r.PathValue("conn"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, s.viewConn(r, c))
}

func readConnInput(r *http.Request) (integrations.ConnInput, error) {
	var in integrations.ConnInput
	data, err := readBody(r)
	if err != nil {
		return in, err
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return in, errStatus(400, "invalid JSON: "+err.Error())
	}
	return in, nil
}

func (s *Server) createConnection(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	in, err := readConnInput(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.Create(r.Context(), a.ID, in)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, s.viewConn(r, c))
}

func (s *Server) updateConnection(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	in, err := readConnInput(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.Update(r.Context(), a.ID, r.PathValue("conn"), in)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, s.viewConn(r, c))
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.Connection(r.Context(), a.ID, r.PathValue("conn"))
	if err != nil {
		return err
	}
	// Flows that use it would fail at run time; say which first.
	list, err := s.Flows.Flows(r.Context(), a.ID)
	if err != nil {
		return err
	}
	var using []string
	for _, f := range list {
		if usesConnection(f, c.ID) {
			using = append(using, f.Name)
		}
	}
	if len(using) > 0 && r.URL.Query().Get("force") != "true" {
		return writeJSON(w, 409, map[string]any{"error": "flows still use " + c.Name + ": " + strings.Join(using, ", "), "flows": using})
	}
	if err := s.Integrations.Delete(r.Context(), a.ID, c.ID); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func usesConnection(f *flows.Flow, id string) bool {
	if f.Trigger.Type == flows.OnIntegration && f.Trigger.Connection == id {
		return true
	}
	found := false
	var walk func(list []*flows.Step)
	walk = func(list []*flows.Step) {
		for _, st := range list {
			if st.Type == "integration" {
				if c, _ := st.Inputs["connection"].(string); c == id {
					found = true
				}
			}
			walk(st.Then)
			walk(st.Else)
			walk(st.Do)
		}
	}
	walk(f.Steps)
	return found
}

func envOf(v string) string {
	if v == "test" || v == integrations.Test {
		return integrations.Test
	}
	return integrations.Live
}

func (s *Server) testConnectionSettings(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.Connection(r.Context(), a.ID, r.PathValue("conn"))
	if err != nil {
		return err
	}
	var in struct {
		Env string `json:"env"`
	}
	if data, _ := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<16)); len(data) > 0 {
		json.Unmarshal(data, &in)
	}
	out, err := s.Integrations.TestConnection(r.Context(), c, envOf(in.Env))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, out)
}

func (s *Server) newConnectionHook(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.NewHookToken(r.Context(), a.ID, r.PathValue("conn"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, s.viewConn(r, c))
}

func (s *Server) connectionEvents(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.Connection(r.Context(), a.ID, r.PathValue("conn"))
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.Integrations.Events(r.Context(), c.ID, limit)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

// tryAction runs one action from the Integrations tab, with test values
// unless live is asked for.
func (s *Server) tryAction(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Action string         `json:"action"`
		Inputs map[string]any `json:"inputs"`
		Env    string         `json:"env"`
	}
	data, err := readBody(r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return errStatus(400, "invalid JSON: "+err.Error())
	}
	if in.Env == "" {
		in.Env = "test"
	}
	res, err := s.Integrations.Run(r.Context(), integrations.Call{AppID: a.ID, Connection: r.PathValue("conn"), Action: in.Action, Inputs: in.Inputs, Env: envOf(in.Env)})
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"output": res.Output, "request": res.Request})
}

func (s *Server) runSync(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	sc, err := s.Integrations.RunSync(r.Context(), a.ID, r.PathValue("conn"), r.PathValue("sync"))
	if sc == nil {
		return err
	}
	status := 200
	if err != nil {
		status = 502
	}
	return writeJSON(w, status, sc)
}

// paymentFlow makes a ready flow that a "Take a payment" button runs: it
// creates a Stripe Checkout session and gives back its address. The
// price is in the flow, not the button, so payers cannot change it.
func (s *Server) paymentFlow(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	c, err := s.Integrations.Connection(r.Context(), a.ID, r.PathValue("conn"))
	if err != nil {
		return err
	}
	if c.Kind != "stripe" {
		return errStatus(400, "payment flows are made for Stripe connections")
	}
	var in struct {
		Product  string  `json:"product"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
		Name     string  `json:"name"`
	}
	if data, _ := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<16)); len(data) > 0 {
		json.Unmarshal(data, &in)
	}
	if strings.TrimSpace(in.Product) == "" {
		in.Product = "Order"
	}
	if in.Amount <= 0 {
		in.Amount = 25
	}
	if in.Currency == "" {
		in.Currency = "eur"
	}
	list, err := s.Flows.Flows(r.Context(), a.ID)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Take payment"
	}
	taken := map[string]bool{}
	for _, f := range list {
		taken[strings.ToLower(f.Name)] = true
	}
	base := name
	for i := 2; taken[strings.ToLower(name)]; i++ {
		name = base + " " + strconv.Itoa(i)
	}
	app := s.publicBase(r) + "/app/" + a.Slug
	def := map[string]any{
		"name": name, "enabled": true,
		"trigger": map[string]any{"type": "manual", "role": string(rbac.Basic), "parameters": []map[string]any{
			{"name": "quantity", "type": "number", "default": "1"},
			{"name": "email", "type": "text", "default": ""},
			{"name": "reference", "type": "text", "default": ""},
		}},
		"steps": []map[string]any{
			{"name": "Checkout", "type": "integration", "inputs": map[string]any{
				"connection": c.ID, "action": "create_checkout_session",
				"params": map[string]any{
					"mode": "payment",
					"lineItems": []any{map[string]any{
						"name": in.Product, "amount": strconv.FormatFloat(in.Amount, 'f', 2, 64), "currency": strings.ToLower(in.Currency),
						"quantity": "{{ trigger.params.quantity | default:1 }}",
					}},
					"successUrl":    app + "?paid={CHECKOUT_SESSION_ID}",
					"cancelUrl":     app + "?cancelled=1",
					"customerEmail": "{{ trigger.params.email }}",
					"reference":     "{{ trigger.params.reference }}",
				},
			}},
			{"name": "Result", "type": "return", "inputs": map[string]any{"value": "{{ steps.Checkout.url }}"}},
		},
	}
	data, _ := json.Marshal(def)
	f, err := s.Flows.CreateFlow(r.Context(), a.ID, data)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, f)
}

// integrationHook receives a service's webhook calls. The connection's
// address and the signature check guard it; repeats of an event are
// answered without starting flows again.
func (s *Server) integrationHook(w http.ResponseWriter, r *http.Request) error {
	token := r.PathValue("token")
	c, err := s.Integrations.ByHook(r.Context(), token)
	if err != nil {
		return errStatus(404, "no such webhook")
	}
	if a, err := s.Apps.Get(r.Context(), c.AppID); err != nil {
		return err
	} else if err := s.appRequest(a); err != nil {
		return err
	}
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		return errStatus(413, "the body is larger than 1 MB")
	}
	in := integrations.Incoming{Method: r.Method, Header: r.Header, Body: raw, URL: s.publicBase(r) + r.URL.RequestURI()}
	reply, err := s.Integrations.Receive(r.Context(), token, in, s.Engine.StartEvent)
	if err != nil {
		return errStatus(404, "no such webhook")
	}
	w.Header().Set("Content-Type", reply.ContentType)
	w.WriteHeader(reply.Status)
	_, err = w.Write(reply.Body)
	return err
}
