package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/interp"
)

// Incoming is a webhook call as it arrived.
type Incoming struct {
	Method string
	Header http.Header
	Body   []byte
	// URL is the full public address that was called.
	URL string
}

// Event is a verified webhook event, ready to start flows.
type Event struct {
	ID   string `json:"id"`
	Type string `json:"event"`
	// Object is what the event is about (Stripe's data.object).
	Object any `json:"object"`
	// Body is the whole payload; Headers the call's headers without
	// credentials.
	Body    any            `json:"body"`
	Headers map[string]any `json:"headers"`
	// Test is true when the call was signed with the test secret.
	Test bool `json:"test"`
}

// Data is what flows read as {{ trigger.… }}.
func (ev *Event) Data(c *Connection) map[string]any {
	return map[string]any{
		"event": ev.Type, "id": ev.ID, "object": ev.Object, "body": ev.Body, "headers": ev.Headers, "test": ev.Test,
		"connection": map[string]any{"id": c.ID, "name": c.Name, "kind": c.Kind},
	}
}

// Reply is the answer to give the caller.
type Reply struct {
	Status      int
	ContentType string
	Body        []byte
}

func jsonReply(status int, v any) *Reply {
	b, _ := json.Marshal(v)
	return &Reply{Status: status, ContentType: "application/json", Body: b}
}

// EventLog is one received call, for the connection page.
type EventLog struct {
	ID         string    `json:"id"`
	Type       string    `json:"event"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail"`
	ReceivedAt time.Time `json:"receivedAt"`
}

// Event statuses: handled ones started their flows; failed ones could
// not and may be sent again; rejected ones had a bad signature.
const (
	EvHandled   = "handled"
	EvFailed    = "failed"
	EvRejected  = "rejected"
	EvDuplicate = "duplicate"
)

const (
	keepEvents   = 5000
	keepPayloads = 50
	eventMaxAge  = 7 * 24 * time.Hour
	maxPayload   = 32 << 10
)

// Starter starts the flows an event triggers and says how many.
type Starter func(ctx context.Context, c *Connection, ev *Event) (int, error)

// Receive checks and handles a webhook call to a connection's address.
// Errors are only for calls that name no connection; everything else is
// answered, so the service knows whether to send it again.
func (s *Service) Receive(ctx context.Context, token string, in Incoming, start Starter) (*Reply, error) {
	conn, err := s.ByHook(ctx, token)
	if err != nil {
		return nil, err
	}
	m := s.Catalog.Get(conn.Kind)
	if m == nil || m.Webhook == nil {
		return nil, fmt.Errorf("connection %w", ErrNotFound)
	}
	w := m.Webhook
	ct, _, _ := mime.ParseMediaType(in.Header.Get("Content-Type"))
	var form url.Values
	if ct == "application/x-www-form-urlencoded" {
		form, _ = url.ParseQuery(string(in.Body))
	}
	signed := Signed{Header: in.Header, Body: in.Body, URL: in.URL, Form: form, Now: s.now()}
	test := false
	live := conn.Values(Live)[w.Verify.Secret]
	err = Check(w.Verify, live, signed)
	if err != nil && w.Verify.Scheme != "none" {
		if t := conn.testSecrets[w.Verify.Secret]; t != "" && t != live {
			if Check(w.Verify, t, signed) == nil {
				err, test = nil, true
			}
		}
	}
	if err != nil {
		s.logEvent(ctx, conn.ID, ids.New("x"), "", EvRejected, err.Error(), nil)
		return jsonReply(401, map[string]any{"error": err.Error()}), nil
	}
	var body any
	switch {
	case form != nil:
		flat := map[string]any{}
		for k, v := range form {
			if len(v) == 1 {
				flat[k] = v[0]
			} else {
				items := make([]any, len(v))
				for i, x := range v {
					items[i] = x
				}
				flat[k] = items
			}
		}
		body = flat
	default:
		body = decode(in.Body)
		if body == nil {
			body = map[string]any{}
		}
	}
	if w.ChallengeWhen != "" && interp.ToText(Dig(body, w.ChallengeWhen)) == w.ChallengeValue {
		return &Reply{Status: 200, ContentType: "text/plain; charset=utf-8", Body: []byte(interp.ToText(Dig(body, w.Challenge)))}, nil
	}
	ev := &Event{Body: body, Test: test, Headers: map[string]any{}}
	for k, v := range in.Header {
		switch strings.ToLower(k) {
		case "cookie", "authorization":
			continue
		}
		ev.Headers[k] = strings.Join(v, ", ")
	}
	switch {
	case w.FixedEvent != "":
		ev.Type = w.FixedEvent
	case w.EventHeader != "":
		ev.Type = in.Header.Get(w.EventHeader)
		if a := interp.ToText(Dig(body, "action")); a != "" && w.Event == "action" {
			ev.Type += "." + a
		}
	default:
		ev.Type = interp.ToText(Dig(body, w.Event))
	}
	if w.IDHeader != "" {
		ev.ID = in.Header.Get(w.IDHeader)
	} else if w.ID != "" {
		ev.ID = interp.ToText(Dig(body, w.ID))
	}
	if ev.ID == "" {
		sum := sha256.Sum256(in.Body)
		ev.ID = "body_" + hex.EncodeToString(sum[:12])
	}
	ev.Object = body
	if w.Object != "" {
		ev.Object = Dig(body, w.Object)
	}
	reply := func() *Reply {
		if w.Respond != "" {
			ctype := w.RespondType
			if ctype == "" {
				ctype = "text/plain; charset=utf-8"
			}
			return &Reply{Status: 200, ContentType: ctype, Body: []byte(w.Respond)}
		}
		return jsonReply(200, map[string]any{"received": true})
	}
	// The same event sent again (a retry, or a replay) starts nothing.
	fresh, err := s.claimEvent(ctx, conn.ID, ev)
	if err != nil {
		return jsonReply(503, map[string]any{"error": "the event could not be recorded; send it again"}), nil
	}
	if !fresh {
		return reply(), nil
	}
	detail := []string{}
	if n, err := s.applyEvent(ctx, conn, m, ev); err != nil {
		detail = append(detail, "sync: "+err.Error())
	} else if n > 0 {
		detail = append(detail, fmt.Sprintf("updated %d synced row%s", n, plural(n)))
	}
	n := 0
	if start != nil {
		n, err = start(ctx, conn, ev)
		if err != nil {
			s.setEvent(ctx, conn.ID, ev.ID, EvFailed, "flows could not start: "+err.Error())
			return jsonReply(503, map[string]any{"error": "the flows could not start; send it again"}), nil
		}
	}
	started := "no flow listens for it"
	if n > 0 {
		started = fmt.Sprintf("started %d flow%s", n, plural(n))
	}
	detail = append([]string{started}, detail...)
	s.setEvent(ctx, conn.ID, ev.ID, EvHandled, strings.Join(detail, "; "))
	return reply(), nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// claimEvent records an event, and says whether it is new. An event that
// failed before may be handled again.
func (s *Service) claimEvent(ctx context.Context, connID string, ev *Event) (bool, error) {
	var status, detail string
	err := s.store.Meta.QueryRow(ctx, `SELECT status, detail FROM integration_events WHERE connection_id = ? AND event_id = ?`, connID, ev.ID).Scan(&status, &detail)
	payload, _ := json.Marshal(ev.Body)
	if len(payload) > maxPayload {
		payload = []byte(`{"_trimmed":true}`)
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if err == nil {
		if status != EvFailed {
			// Keep what handling it said, with the latest resend.
			detail, _, _ = strings.Cut(detail, " · sent again")
			again := "sent again and ignored at " + s.now().UTC().Format("15:04:05")
			if detail != "" {
				again = detail + " · " + again
			}
			s.store.Meta.Exec(ctx, `UPDATE integration_events SET detail = ? WHERE connection_id = ? AND event_id = ?`, again, connID, ev.ID)
			return false, nil
		}
		_, err := s.store.Meta.Exec(ctx, `UPDATE integration_events SET status = ?, detail = '', payload = ?, received_at = ? WHERE connection_id = ? AND event_id = ? AND status = ?`, "received", string(payload), now, connID, ev.ID, EvFailed)
		return err == nil, err
	}
	if _, err := s.store.Meta.Exec(ctx, `INSERT INTO integration_events (connection_id, event_id, event_type, status, detail, payload, received_at) VALUES (?, ?, ?, ?, '', ?, ?)`,
		connID, ev.ID, ev.Type, "received", string(payload), now); err != nil {
		// Another server took it at the same moment.
		var n int
		if s.store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM integration_events WHERE connection_id = ? AND event_id = ?`, connID, ev.ID).Scan(&n) == nil && n > 0 {
			return false, nil
		}
		return false, err
	}
	s.prune(ctx, connID)
	return true, nil
}

func (s *Service) setEvent(ctx context.Context, connID, id, status, detail string) {
	ctx = context.WithoutCancel(ctx)
	if _, err := s.store.Meta.Exec(ctx, `UPDATE integration_events SET status = ?, detail = ? WHERE connection_id = ? AND event_id = ?`, status, detail, connID, id); err != nil && s.Log != nil {
		s.Log.Error("recording a webhook event failed", "err", err)
	}
}

func (s *Service) logEvent(ctx context.Context, connID, id, typ, status, detail string, body any) {
	payload := ""
	if body != nil {
		b, _ := json.Marshal(body)
		payload = string(b)
	}
	s.store.Meta.Exec(context.WithoutCancel(ctx), `INSERT INTO integration_events (connection_id, event_id, event_type, status, detail, payload, received_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		connID, id, typ, status, detail, payload, s.now().UTC().Format(time.RFC3339Nano))
	s.prune(ctx, connID)
}

func (s *Service) prune(ctx context.Context, connID string) {
	ctx = context.WithoutCancel(ctx)
	old := s.now().Add(-eventMaxAge).UTC().Format(time.RFC3339Nano)
	s.store.Meta.Exec(ctx, `DELETE FROM integration_events WHERE connection_id = ? AND (received_at < ? OR event_id NOT IN (SELECT event_id FROM integration_events WHERE connection_id = ? ORDER BY received_at DESC LIMIT ?))`, connID, old, connID, keepEvents)
	s.store.Meta.Exec(ctx, `UPDATE integration_events SET payload = '' WHERE connection_id = ? AND payload <> '' AND event_id NOT IN (SELECT event_id FROM integration_events WHERE connection_id = ? ORDER BY received_at DESC LIMIT ?)`, connID, connID, keepPayloads)
}

// Events lists a connection's latest calls, newest first.
func (s *Service) Events(ctx context.Context, connID string, limit int) ([]EventLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.store.Meta.Query(ctx, `SELECT event_id, event_type, status, detail, received_at FROM integration_events WHERE connection_id = ? ORDER BY received_at DESC LIMIT ?`, connID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventLog{}
	for rows.Next() {
		var e EventLog
		var at string
		if err := rows.Scan(&e.ID, &e.Type, &e.Status, &e.Detail, &at); err != nil {
			return nil, err
		}
		e.ReceivedAt = stamp(at)
		if strings.HasPrefix(e.ID, "x_") {
			e.ID = ""
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Sample is the newest received event of one of the types (any type when
// none are given), or the manifest's sample, for test runs.
func (s *Service) Sample(ctx context.Context, c *Connection, types []string) (*Event, error) {
	m := s.Catalog.Get(c.Kind)
	if m == nil || m.Webhook == nil {
		return nil, errors.New("this connection takes no webhooks")
	}
	q := `SELECT event_id, event_type, payload FROM integration_events WHERE connection_id = ? AND payload <> '' AND status IN ('handled', 'failed', 'received')`
	args := []any{c.ID}
	var exact []string
	for _, t := range types {
		if !strings.Contains(t, "*") {
			exact = append(exact, t)
		}
	}
	if len(exact) > 0 && len(exact) == len(types) {
		q += ` AND event_type IN (?` + strings.Repeat(", ?", len(exact)-1) + `)`
		for _, t := range exact {
			args = append(args, t)
		}
	}
	q += ` ORDER BY received_at DESC LIMIT 20`
	rows, err := s.store.Meta.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, typ, payload string
		if err := rows.Scan(&id, &typ, &payload); err != nil {
			return nil, err
		}
		if len(types) > 0 && !MatchEvent(types, typ) {
			continue
		}
		ev := &Event{ID: id, Type: typ, Body: decode([]byte(payload)), Headers: map[string]any{}}
		ev.Object = ev.Body
		if m.Webhook.Object != "" {
			ev.Object = Dig(ev.Body, m.Webhook.Object)
		}
		return ev, nil
	}
	ev := &Event{ID: "evt_sample", Type: "", Headers: map[string]any{}, Test: true}
	if len(types) > 0 && !strings.Contains(types[0], "*") {
		ev.Type = types[0]
	} else if len(m.Webhook.Events) > 0 {
		ev.Type = m.Webhook.Events[0].Value
	}
	body := map[string]any{}
	for k, v := range m.Webhook.Sample {
		body[k] = v
	}
	if m.Webhook.Event != "" && m.Webhook.EventHeader == "" {
		setPath(body, m.Webhook.Event, ev.Type)
	}
	ev.Body, ev.Object = body, body
	if m.Webhook.Object != "" {
		ev.Object = Dig(body, m.Webhook.Object)
	}
	return ev, nil
}

// MatchEvent reports whether an event type is one of the wanted ones;
// "invoice.*" matches every invoice event, and no wanted ones match all.
func MatchEvent(wanted []string, typ string) bool {
	if len(wanted) == 0 {
		return true
	}
	for _, w := range wanted {
		w = strings.TrimSpace(w)
		if w == "*" || w == typ {
			return true
		}
		if p, ok := strings.CutSuffix(w, "*"); ok && strings.HasPrefix(typ, p) {
			return true
		}
	}
	return false
}
