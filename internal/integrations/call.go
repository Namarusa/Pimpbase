package integrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/formula"
	"github.com/pimpbase/pimpbase/internal/interp"
)

const (
	callTimeout = 30 * time.Second
	maxBody     = 10 << 20
	maxTries    = 3
)

// Call is an action to run for a flow step.
type Call struct {
	AppID      string
	Connection string
	Action     string
	// Inputs are the step's settings with their bindings filled.
	Inputs map[string]any
	// Env is Test for test runs, which use test keys.
	Env string
	// Key makes repeated writes of one step safe: the same run, step and
	// loop item always send the same idempotency key.
	Key string
}

// Result is what an action gave.
type Result struct {
	Output any
	// Request says what was sent, with secrets left out, for the log.
	Request string
}

// CallError is an action the service refused or that could not run; its
// message is safe to show.
type CallError struct {
	Status  int
	Message string
}

func (e *CallError) Error() string { return e.Message }

// Run runs an action of a connection.
func (s *Service) Run(ctx context.Context, c Call) (*Result, error) {
	conn, err := s.Connection(ctx, c.AppID, c.Connection)
	if err != nil {
		return nil, invalid("choose the connection this step uses; %s", err)
	}
	m := s.Catalog.Get(conn.Kind)
	if m == nil {
		return nil, invalid("the integration %q is no longer installed", conn.Kind)
	}
	a := m.Action(c.Action)
	if a == nil {
		return nil, invalid("%s has no action %q", m.Name, c.Action)
	}
	if c.Env == "" {
		c.Env = Live
	}
	if missing := conn.Missing(m, c.Env); len(missing) > 0 {
		return nil, invalid("the connection %s has no %s yet; set it in the Integrations tab", conn.Name, strings.Join(missing, ", "))
	}
	cx := &callCtx{s: s, conn: conn, m: m, vals: conn.Values(c.Env), env: c.Env, key: c.Key, skipOK: a.SkipOK}
	if a.Handler != "" {
		h := handlers[a.Handler]
		if h == nil {
			return nil, fmt.Errorf("the action %s needs code this server does not have", a.Label)
		}
		out, err := h(ctx, cx, a, c.Inputs)
		if err != nil {
			return nil, err
		}
		return &Result{Output: out, Request: cx.sent}, nil
	}
	req, err := cx.build(a.Request, a.Inputs, c.Inputs)
	if err != nil {
		return nil, err
	}
	body, err := cx.do(ctx, req)
	if err != nil {
		return nil, err
	}
	return &Result{Output: shape(body, a.Items), Request: cx.sent}, nil
}

// TestConnection checks a connection's settings with its manifest's test
// request, and returns what it shows.
func (s *Service) TestConnection(ctx context.Context, conn *Connection, env string) (map[string]any, error) {
	m := s.Catalog.Get(conn.Kind)
	if m == nil {
		return nil, invalid("the integration %q is no longer installed", conn.Kind)
	}
	if missing := conn.Missing(m, env); len(missing) > 0 {
		return nil, invalid("fill in %s first", strings.Join(missing, ", "))
	}
	out := map[string]any{"ok": true}
	h := handlers[m.ID+".test"]
	if m.Test == nil && h == nil {
		return out, nil
	}
	cx := &callCtx{s: s, conn: conn, m: m, vals: conn.Values(env), env: env}
	var body any
	var err error
	if h != nil {
		body, err = h(ctx, cx, nil, nil)
	} else {
		var req *built
		if req, err = cx.build(*m.Test, nil, nil); err == nil {
			body, err = cx.do(ctx, req)
		}
	}
	if err != nil {
		return nil, err
	}
	for _, p := range m.TestShows {
		out[p] = Dig(body, p)
	}
	return out, nil
}

// shape makes an answer something later steps read well: objects as
// they are, with the list at items also as rows; lists as rows.
func shape(body any, items string) any {
	switch b := body.(type) {
	case map[string]any:
		if items != "" {
			out := make(map[string]any, len(b)+1)
			for k, v := range b {
				out[k] = v
			}
			rows := Dig(b, items)
			if rows == nil {
				rows = []any{}
			}
			out["rows"] = rows
			return out
		}
		return b
	case []any:
		return map[string]any{"rows": b, "count": len(b)}
	case nil:
		return map[string]any{"ok": true}
	}
	return map[string]any{"body": body}
}

// Dig reads a dotted path (a.b.0.c, or a["odd key"]) from a value.
func Dig(v any, path string) any {
	if path == "" {
		return v
	}
	parts, err := interp.SplitPath(path)
	if err != nil {
		return nil
	}
	for _, p := range parts {
		switch x := v.(type) {
		case map[string]any:
			v = x[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil {
				if p == "last" && len(x) > 0 {
					v = x[len(x)-1]
					continue
				}
				return nil
			}
			if i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// callCtx is one action being run.
type callCtx struct {
	s    *Service
	conn *Connection
	m    *Manifest
	vals map[string]string
	env  string
	key  string
	// sent describes the last request, for the log.
	sent string
	// skipOK leaves out the manifest's OK check.
	skipOK bool
}

type built struct {
	method  string
	url     string
	header  http.Header
	body    []byte
	ctype   string
	retries bool
}

var holeRe = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_]*)\}`)

// fill puts values into the {name} parts of a path; escape is used for
// each value.
func fill(src string, pick func(name string) (string, bool), escape func(string) string) (string, error) {
	var missing []string
	out := holeRe.ReplaceAllStringFunc(src, func(h string) string {
		name := h[1 : len(h)-1]
		v, ok := pick(name)
		if !ok || v == "" {
			missing = append(missing, name)
			return ""
		}
		return escape(v)
	})
	if len(missing) > 0 {
		return "", invalid("%s is needed", strings.Join(missing, ", "))
	}
	return out, nil
}

func (cx *callCtx) setting(name string) (string, bool) {
	v, ok := cx.vals[name]
	if !ok || v == "" {
		if cr := cx.m.Credential(name); cr != nil && cr.Default != "" {
			return cr.Default, true
		}
	}
	return v, ok
}

// base is where the connection's requests go.
func (cx *callCtx) base() (string, error) {
	b := cx.m.BaseURL
	if v := strings.TrimSpace(cx.vals["baseUrl"]); v != "" && cx.m.Credential("baseUrl") != nil {
		b = v
	}
	return fill(strings.TrimRight(b, "/"), cx.setting, url.PathEscape)
}

// build turns an action's inputs into a request.
func (cx *callCtx) build(r Request, specs []*Input, given map[string]any) (*built, error) {
	pathVals := map[string]string{}
	rawPath := map[string]bool{}
	query := map[string]any{}
	body := map[string]any{}
	header := http.Header{}
	where := func(in *Input) string {
		if in.In != "" {
			return in.In
		}
		if r.Method == "GET" || r.Method == "DELETE" {
			return "query"
		}
		return "body"
	}
	for _, in := range specs {
		v, err := inputValue(in, given)
		if err != nil {
			return nil, err
		}
		if empty(v) && in.Setting != "" {
			if sv, ok := cx.setting(in.Setting); ok && sv != "" {
				v = sv
			}
		}
		if in.Kind == "select" && where(in) == "path" && !empty(v) {
			// Choices may hold slashes (checkout/sessions); they go in as
			// they are, so only listed ones are let through.
			ok := false
			for _, o := range in.Options {
				ok = ok || o.Value == interp.ToText(v)
			}
			if !ok {
				return nil, invalid("%s cannot be %q", in.Label, interp.ToText(v))
			}
			rawPath[in.Key] = true
		}
		if empty(v) {
			if in.Required {
				return nil, invalid("%s is required", in.Label)
			}
			continue
		}
		to := in.To
		if to == "" {
			to = in.Key
		}
		switch where(in) {
		case "path":
			pathVals[in.Key] = interp.ToText(v)
		case "query":
			setPath(query, to, v)
		case "header":
			header.Set(to, strings.NewReplacer("\r", "", "\n", "").Replace(interp.ToText(v)))
		default:
			setPath(body, to, v)
		}
	}
	for k, v := range r.Fixed {
		if r.Method == "GET" || r.Method == "DELETE" || r.Body == "none" {
			setPath(query, k, v)
		} else {
			setPath(body, k, v)
		}
	}
	for k, v := range r.Query {
		if _, ok := query[k]; !ok {
			query[k] = v
		}
	}
	pick := func(name string) (string, bool) {
		if v, ok := pathVals[name]; ok {
			return v, true
		}
		return cx.setting(name)
	}
	var addr string
	if r.URL != "" {
		u, err := fill(r.URL, pick, func(s string) string { return s })
		if err != nil {
			return nil, err
		}
		addr = u
	} else {
		base, err := cx.base()
		if err != nil {
			return nil, invalid("the connection's address: %s", err)
		}
		p, err := fillPath(r.Path, pick, rawPath)
		if err != nil {
			return nil, err
		}
		addr = base + p
	}
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, invalid("%q is not a web address", addr)
	}
	if len(query) > 0 {
		q := u.Query()
		for k, vs := range formValues(query) {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
	}
	b := &built{method: r.Method, url: u.String(), header: header}
	if r.Method != "GET" && r.Body != "none" {
		switch r.Body {
		case "form":
			b.body, b.ctype = []byte(formValues(body).Encode()), "application/x-www-form-urlencoded"
		default:
			b.body, _ = json.Marshal(body)
			b.ctype = "application/json"
		}
	}
	return b, nil
}

// fillPath fills a path, escaping each value except the raw ones.
func fillPath(src string, pick func(string) (string, bool), raw map[string]bool) (string, error) {
	var missing []string
	out := holeRe.ReplaceAllStringFunc(src, func(h string) string {
		name := h[1 : len(h)-1]
		v, ok := pick(name)
		if !ok || v == "" {
			missing = append(missing, name)
			return ""
		}
		if raw[name] {
			return v
		}
		return url.PathEscape(v)
	})
	if len(missing) > 0 {
		return "", invalid("%s is needed", strings.Join(missing, ", "))
	}
	return out, nil
}

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// inputValue reads one input from the step's filled settings, converted
// to its kind and transformed for the wire.
func inputValue(in *Input, given map[string]any) (any, error) {
	v, ok := given[in.Key]
	if !ok || empty(v) {
		v = in.Default
	}
	if empty(v) {
		return nil, nil
	}
	var err error
	switch in.Kind {
	case "number":
		n, ok := toNumber(v)
		if !ok {
			return nil, invalid("%s should be a number, not %q", in.Label, interp.ToText(v))
		}
		v = n
	case "boolean":
		switch x := v.(type) {
		case bool:
		case string:
			v = x == "true" || x == "yes" || x == "1" || strings.EqualFold(x, "true")
		default:
			v = !empty(x)
		}
	case "json":
		if s, ok := v.(string); ok {
			var out any
			if err := json.Unmarshal([]byte(s), &out); err != nil {
				return nil, invalid("%s is not valid JSON: %s", in.Label, err)
			}
			v = out
		}
	case "pairs":
		v, err = pairsValue(in, v)
		if err != nil {
			return nil, err
		}
	case "list":
		switch x := v.(type) {
		case string:
			var items []any
			for _, p := range strings.Split(x, ",") {
				if p = strings.TrimSpace(p); p != "" {
					items = append(items, p)
				}
			}
			v = items
		case []any:
		default:
			v = []any{x}
		}
	case "items":
		list, ok := v.([]any)
		if !ok {
			if m, isMap := v.(map[string]any); isMap {
				list = []any{m}
			} else {
				return nil, invalid("%s should be a list", in.Label)
			}
		}
		var out []any
		for i, item := range list {
			m, _ := item.(map[string]any)
			obj := map[string]any{}
			for _, f := range in.Fields {
				fv, err := inputValue(f, m)
				if err != nil {
					return nil, invalid("%s %d: %s", in.Label, i+1, err)
				}
				if empty(fv) {
					if f.Required {
						return nil, invalid("%s %d needs %s", in.Label, i+1, f.Label)
					}
					continue
				}
				to := f.To
				if to == "" {
					to = f.Key
				}
				setPath(obj, to, fv)
			}
			if len(obj) > 0 {
				out = append(out, obj)
			}
		}
		v = out
	}
	switch in.Transform {
	case "cents":
		n, ok := toNumber(v)
		if !ok {
			return nil, invalid("%s should be an amount", in.Label)
		}
		v = math.Round(n * 100)
	case "row":
		if l, ok := v.([]any); ok {
			v = []any{l}
		} else {
			v = []any{[]any{v}}
		}
	case "string":
		v = interp.ToText(v)
	case "unix":
		switch x := v.(type) {
		case float64:
		case time.Time:
			v = float64(x.Unix())
		default:
			t, err := formula.ParseDate(interp.ToText(x))
			if err != nil {
				return nil, invalid("%s is not a date", in.Label)
			}
			v = float64(t.Unix())
		}
	}
	return v, nil
}

func pairsValue(in *Input, v any) (any, error) {
	out := map[string]any{}
	switch x := v.(type) {
	case map[string]any:
		for k, y := range x {
			out[k] = y
		}
	case []any:
		for _, item := range x {
			m, _ := item.(map[string]any)
			k := strings.TrimSpace(interp.ToText(m["key"]))
			if off, _ := m["off"].(bool); off {
				continue
			}
			if k != "" {
				out[k] = m["value"]
			}
		}
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(x), &m); err != nil {
			return nil, invalid("%s should be key and value pairs", in.Label)
		}
		out = m
	}
	return out, nil
}

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(x, ",", "")), 64)
		return f, err == nil
	}
	return 0, false
}

// setPath puts v at a dotted path, making objects on the way.
func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}

// formValues encodes nested values the way Stripe reads them:
// a[b][0][c]=v.
func formValues(m map[string]any) url.Values {
	out := url.Values{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case nil:
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(prefix+"["+k+"]", x[k])
			}
		case []any:
			for i, y := range x {
				walk(prefix+"["+strconv.Itoa(i)+"]", y)
			}
		default:
			out.Add(prefix, scalar(x))
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		walk(k, m[k])
	}
	return out
}

func scalar(v any) string {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return interp.ToText(v)
}

// do sends a request with the connection's credentials, retrying where
// that is safe, and returns the parsed answer.
func (cx *callCtx) do(ctx context.Context, b *built) (any, error) {
	if b.method != "GET" && cx.m.Idempotency != "" && cx.key != "" {
		sum := sha256.Sum256([]byte(cx.conn.ID + "|" + cx.key))
		b.header.Set(cx.m.Idempotency, "pimpbase-"+hex.EncodeToString(sum[:16]))
	}
	safe := b.method == "GET" || b.header.Get(cx.m.Idempotency) != ""
	cx.sent = b.method + " " + redactURL(b.url)
	var lastErr error
	for attempt := 1; ; attempt++ {
		req, err := cx.request(ctx, b)
		if err != nil {
			return nil, err
		}
		status, header, data, err := cx.send(ctx, req)
		retry, wait := false, time.Duration(1<<(attempt-1))*time.Second
		if err != nil {
			lastErr = err
			retry = safe && ctx.Err() == nil
		} else if status == 429 || (status >= 500 && safe) {
			lastErr = cx.answerError(status, data)
			retry = true
			if ra, err := strconv.Atoi(header.Get("Retry-After")); err == nil && ra >= 0 && ra <= 20 {
				wait = time.Duration(ra) * time.Second
			}
		} else {
			return cx.parse(status, data)
		}
		if !retry || attempt >= maxTries {
			if attempt > 1 {
				return nil, fmt.Errorf("%w (after %d tries)", lastErr, attempt)
			}
			return nil, lastErr
		}
		if err := cx.s.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.RawQuery = ""
	return u.String()
}

func (cx *callCtx) request(ctx context.Context, b *built) (*http.Request, error) {
	var body io.Reader
	if b.body != nil {
		body = bytes.NewReader(b.body)
	}
	req, err := http.NewRequestWithContext(ctx, b.method, b.url, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range b.header {
		req.Header[k] = vs
	}
	for k, v := range cx.m.Headers {
		if req.Header.Get(k) == "" {
			fv, _ := fill(v, cx.setting, func(s string) string { return s })
			if fv != "" {
				req.Header.Set(k, fv)
			}
		}
	}
	if b.ctype != "" {
		req.Header.Set("Content-Type", b.ctype)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", "Pimpbase")
	if err := cx.authorize(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

func (cx *callCtx) authorize(ctx context.Context, req *http.Request) error {
	a := cx.m.Auth
	tok := cx.vals[a.Credential]
	switch a.Type {
	case "bearer":
		p := a.Prefix
		if p == "" {
			p = "Bearer "
		}
		req.Header.Set("Authorization", p+tok)
	case "header":
		req.Header.Set(a.Name, a.Prefix+tok)
	case "query":
		q := req.URL.Query()
		q.Set(a.Name, tok)
		req.URL.RawQuery = q.Encode()
	case "basic":
		req.SetBasicAuth(cx.vals[a.Username], cx.vals[a.Password])
	case "google_service_account":
		t, err := cx.googleToken(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+t)
	case "aws_sigv4":
		// S3 actions sign their own requests (see s3.go).
	}
	return nil
}

func (cx *callCtx) send(ctx context.Context, req *http.Request) (int, http.Header, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := cx.s.Client.Do(req.WithContext(ctx))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, nil, nil, &CallError{Message: fmt.Sprintf("%s gave no answer within %s", cx.m.Name, callTimeout)}
		}
		if ue, ok := err.(*url.Error); ok {
			return 0, nil, nil, &CallError{Message: fmt.Sprintf("%s could not be reached: %s", cx.m.Name, ue.Err)}
		}
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, data, nil
}

func decode(data []byte) any {
	t := bytes.TrimSpace(data)
	if len(t) == 0 {
		return nil
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(t))
	if d.Decode(&v) == nil {
		return v
	}
	return string(data)
}

func (cx *callCtx) answerError(status int, data []byte) error {
	body := decode(data)
	msg := ""
	if cx.m.Errors.Message != "" {
		msg = interp.ToText(Dig(body, cx.m.Errors.Message))
	}
	if msg == "" {
		if s, ok := body.(string); ok && len(s) < 300 {
			msg = strings.TrimSpace(s)
		}
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &CallError{Status: status, Message: fmt.Sprintf("%s answered %d: %s", cx.m.Name, status, msg)}
}

func (cx *callCtx) parse(status int, data []byte) (any, error) {
	if status < 200 || status >= 300 {
		return nil, cx.answerError(status, data)
	}
	body := decode(data)
	if p := cx.m.Errors.OK; p != "" && !cx.skipOK {
		if ok, _ := Dig(body, p).(bool); !ok {
			msg := interp.ToText(Dig(body, cx.m.Errors.Message))
			if msg == "" {
				msg = "the request was refused"
			}
			return nil, &CallError{Status: status, Message: fmt.Sprintf("%s refused it: %s", cx.m.Name, msg)}
		}
	}
	return body, nil
}
