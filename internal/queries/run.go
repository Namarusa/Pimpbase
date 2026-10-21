package queries

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pimpbase/pimpbase/internal/extdb"
	"github.com/pimpbase/pimpbase/internal/interp"
	"github.com/pimpbase/pimpbase/internal/xmltree"
)

const (
	maxResponse = 10 << 20
	maxRaw      = 64 << 10
	maxDepth    = 8
)

// RunInput is who runs a query, where, and with what.
type RunInput struct {
	Params map[string]any
	// Env is Development (the builder) or Production (published apps).
	Env  string
	User map[string]any // id, email, name, role; nil when signed out
	App  map[string]any // id, name, slug
	// Test uses the parameters' test values where Params has none.
	Test bool
}

// Result is a query's answer.
type Result struct {
	OK         bool              `json:"ok"`
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	// Format is how the body was read: "json", "xml" or "text".
	Format string `json:"format"`
	Body   any    `json:"body"`
	// Rows are the records at the result path, each an object.
	Rows       []any       `json:"rows"`
	Fields     []FieldInfo `json:"fields"`
	ParseError string      `json:"parseError,omitempty"`
	Raw        string      `json:"raw,omitempty"`
	Size       int         `json:"size"`
	DurationMs int64       `json:"durationMs"`
	Cached     bool        `json:"cached"`
	// Request is what was sent, with secrets hidden; only builders see it.
	Request *Echo `json:"request,omitempty"`
	// Database queries: rows changed by a statement, whether rows were cut
	// at the limit, and the column names in order.
	RowsAffected int64    `json:"rowsAffected,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
	Columns      []string `json:"columns,omitempty"`
}

// Echo is a request as sent.
type Echo struct {
	Method  string `json:"method"`
	URL     string `json:"url"`
	Headers []Pair `json:"headers"`
	Body    string `json:"body"`
	// Params are a database query's parameters, in placeholder order.
	Params []any `json:"params,omitempty"`
}

// RunError is a query that could not be sent or read; its message is safe
// to show.
type RunError struct {
	Message string
	// Binding is true when filling a binding failed, rather than the call.
	Binding bool
}

func (e *RunError) Error() string { return e.Message }

// Runner sends queries. It is safe for concurrent use.
type Runner struct {
	svc    *Service
	client *http.Client
	pool   *extdb.Pool

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	res     *Result
	expires time.Time
}

func NewRunner(svc *Service) *Runner {
	return &Runner{
		svc: svc,
		client: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		}},
		cache: map[string]cached{},
		pool:  extdb.NewPool(),
	}
}

// Close closes the database connections the runner keeps.
func (r *Runner) Close() { r.pool.Close() }

// run is one Run call: queries read by other queries are sent once.
type run struct {
	r       *Runner
	appID   string
	in      RunInput
	env     map[string]any
	secrets []string
	done    map[string]*Result
	stack   []string
}

// Run sends a query, first sending any query its bindings read.
func (r *Runner) Run(ctx context.Context, appID string, q *Query, in RunInput) (*Result, error) {
	if in.Env != Development {
		in.Env = Production
	}
	env, secrets, err := r.svc.values(ctx, appID, in.Env)
	if err != nil {
		return nil, err
	}
	st := &run{r: r, appID: appID, in: in, env: env, secrets: secrets, done: map[string]*Result{}}
	return st.query(ctx, q, in.Params)
}

func (st *run) query(ctx context.Context, q *Query, given map[string]any) (*Result, error) {
	for _, name := range st.stack {
		if strings.EqualFold(name, q.Name) {
			return nil, &RunError{Message: fmt.Sprintf("the query %s reads itself: %s → %s", q.Name, strings.Join(st.stack, " → "), q.Name), Binding: true}
		}
	}
	if len(st.stack) >= maxDepth {
		return nil, &RunError{Message: "queries read each other more than 8 deep", Binding: true}
	}
	if res, ok := st.done[q.ID]; ok && q.ID != "" {
		return res, nil
	}
	st.stack = append(st.stack, q.Name)
	defer func() { st.stack = st.stack[:len(st.stack)-1] }()

	params, err := q.params(given, st.in.Test)
	if err != nil {
		return nil, err
	}
	scope := interp.Map{
		"params":  params,
		"env":     st.env,
		"user":    st.in.User,
		"app":     st.in.App,
		"queries": interp.Lazy(func(rest []string) (any, error) { return st.dependency(ctx, rest, params) }),
	}
	if st.in.User == nil {
		scope["user"] = map[string]any{}
	}
	ds, err := st.r.svc.Datasource(ctx, st.appID, q.DatasourceID)
	if err != nil {
		return nil, err
	}
	if ds.Database() {
		return st.runDB(ctx, ds, q, scope)
	}
	req, echo, err := build(ctx, ds, q, scope)
	if err != nil {
		return nil, err
	}
	key := ""
	if q.CacheSeconds > 0 {
		h := sha256.New()
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00", st.appID, q.ID, req.Method, req.URL, echo.Body)
		for _, p := range echo.Headers {
			fmt.Fprintf(h, "%s:%s\x00", p.Key, p.Value)
		}
		key = hex.EncodeToString(h.Sum(nil))
	}
	// The cache key above uses the real values; what is shown hides them.
	st.redact(echo)
	// A test run sends the query being tested even when an answer is
	// cached; the queries it reads may still come from the cache.
	if key != "" && !(st.in.Test && len(st.stack) == 1) {
		if res := st.r.cached(key); res != nil {
			res.Request = echo
			st.done[q.ID] = res
			return res, nil
		}
	}

	timeout := time.Duration(ds.Config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	resp, err := st.r.client.Do(req.WithContext(cctx))
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			msg = fmt.Sprintf("no answer within %s", timeout)
		} else if ue, ok := err.(*url.Error); ok {
			msg = ue.Err.Error()
		}
		return nil, &RunError{Message: fmt.Sprintf("%s could not be reached: %s", q.Name, st.hide(msg))}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, &RunError{Message: fmt.Sprintf("reading the answer of %s failed: %s", q.Name, err)}
	}
	if len(data) > maxResponse {
		return nil, &RunError{Message: fmt.Sprintf("the answer of %s is larger than 10 MB", q.Name)}
	}
	res := &Result{
		OK:         resp.StatusCode >= 200 && resp.StatusCode < 300,
		Status:     resp.StatusCode,
		StatusText: strings.TrimSpace(strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode))),
		Headers:    map[string]string{},
		Size:       len(data),
		DurationMs: time.Since(start).Milliseconds(),
		Request:    echo,
	}
	for k, v := range resp.Header {
		res.Headers[k] = strings.Join(v, ", ")
	}
	res.Format, res.Body, res.ParseError = decode(q.ResponseType, resp.Header.Get("Content-Type"), data)
	res.Rows = rows(res.Body, q.ResultPath)
	res.Fields = fields(res.Rows)
	if len(data) > maxRaw {
		res.Raw = string(data[:maxRaw])
	} else {
		res.Raw = string(data)
	}
	if key != "" && res.OK {
		st.r.store(key, res, time.Duration(q.CacheSeconds)*time.Second)
	}
	st.done[q.ID] = res
	return res, nil
}

// dependency runs the query a {{ queries.Name.… }} binding names and
// reads the rest of the path from its result.
func (st *run) dependency(ctx context.Context, rest []string, params map[string]any) (any, error) {
	if len(rest) == 0 {
		return nil, nil
	}
	q, err := st.r.svc.Query(ctx, st.appID, rest[0])
	if errors.Is(err, ErrNotFound) {
		return nil, &RunError{Message: fmt.Sprintf("there is no query named %s", rest[0]), Binding: true}
	}
	if err != nil {
		return nil, err
	}
	res, err := st.query(ctx, q, params)
	if err != nil {
		return nil, err
	}
	if !res.OK {
		return nil, &RunError{Message: fmt.Sprintf("the query %s answered %d %s", q.Name, res.Status, res.StatusText)}
	}
	return interp.Walk(res.view(), rest[1:])
}

// view is what {{ queries.Name }} shows of a result.
func (res *Result) view() map[string]any {
	h := map[string]any{}
	for k, v := range res.Headers {
		h[k] = v
	}
	return map[string]any{"status": float64(res.Status), "headers": h, "body": res.Body, "rows": res.Rows}
}

// params gives every declared parameter a value: the given one, else the
// test value in a test run, else the default; converted to its type.
func (q *Query) params(given map[string]any, test bool) (map[string]any, error) {
	out := map[string]any{}
	for _, p := range q.Parameters {
		v, ok := given[p.Name]
		if !ok {
			for k, x := range given {
				if strings.EqualFold(k, p.Name) {
					v, ok = x, true
				}
			}
		}
		if !ok || v == nil || v == "" {
			if test && p.Test != "" {
				v = p.Test
			} else if !ok || v == nil {
				v = p.Default
			}
		}
		cv, err := convert(p, v)
		if err != nil {
			return nil, &RunError{Message: fmt.Sprintf("the parameter %s of %s: %s", p.Name, q.Name, err), Binding: true}
		}
		out[p.Name] = cv
	}
	return out, nil
}

func convert(p Param, v any) (any, error) {
	s, isText := v.(string)
	switch p.Type {
	case "number":
		if !isText {
			if f, ok := v.(float64); ok {
				return f, nil
			}
			if n, ok := v.(json.Number); ok {
				return n.Float64()
			}
			return nil, errors.New("is not a number")
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		return f, nil
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "yes", "1", "on":
			return true, nil
		case "false", "no", "0", "off", "":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not true or false", s)
	case "json":
		if !isText {
			return v, nil
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		var out any
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if err := dec.Decode(&out); err != nil {
			return nil, fmt.Errorf("is not valid JSON: %s", err)
		}
		return out, nil
	}
	if isText {
		return s, nil
	}
	return interp.ToText(v), nil
}

// build makes the HTTP request a query describes.
func build(ctx context.Context, ds *Datasource, q *Query, scope interp.Scope) (*http.Request, *Echo, error) {
	fill := func(field, src string, mode interp.Mode) (string, error) {
		t, err := interp.Parse(src)
		if err != nil {
			return "", &RunError{Message: fmt.Sprintf("%s: %s", field, err), Binding: true}
		}
		out, err := t.Render(mode, scope)
		if err != nil {
			var re *RunError
			if errors.As(err, &re) {
				return "", err
			}
			return "", &RunError{Message: fmt.Sprintf("%s: %s", field, err), Binding: true}
		}
		return out, nil
	}
	value := func(field, src string) (any, error) {
		t, err := interp.Parse(src)
		if err != nil {
			return nil, &RunError{Message: fmt.Sprintf("%s: %s", field, err), Binding: true}
		}
		v, err := t.Value(scope)
		if err != nil {
			return nil, err
		}
		return v, nil
	}

	base, err := fill("the data source address", ds.Config.BaseURL, interp.Text)
	if err != nil {
		return nil, nil, err
	}
	path, err := fill("the path", q.Path, interp.URL)
	if err != nil {
		return nil, nil, err
	}
	addr := path
	if base != "" {
		addr = strings.TrimRight(base, "/")
		if path != "" {
			if strings.HasPrefix(path, "?") {
				addr += path
			} else {
				addr += "/" + strings.TrimLeft(path, "/")
			}
		}
	}
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, nil, &RunError{Message: fmt.Sprintf("%q is not a web address; set the data source address or start the path with https://", addr), Binding: true}
	}
	qs := u.Query()
	addPairs := func(field string, ps []Pair, add func(k string, v string)) error {
		for _, p := range ps {
			if p.Off || strings.TrimSpace(p.Key) == "" {
				continue
			}
			k, err := fill(field, p.Key, interp.Text)
			if err != nil {
				return err
			}
			v, err := value(field, p.Value)
			if err != nil {
				return err
			}
			// A list adds the key once per item: ?id=1&id=2.
			if list, ok := v.([]any); ok {
				for _, x := range list {
					add(k, interp.ToText(x))
				}
				continue
			}
			add(k, interp.ToText(v))
		}
		return nil
	}
	if err := addPairs("the query string", q.Query, qs.Add); err != nil {
		return nil, nil, err
	}

	auth := ds.Config.Auth
	var authHeader []Pair
	switch auth.Type {
	case "basic":
		user, err := fill("the user name", auth.Username, interp.Text)
		if err != nil {
			return nil, nil, err
		}
		pass, err := fill("the password", auth.Password, interp.Text)
		if err != nil {
			return nil, nil, err
		}
		r := &http.Request{Header: http.Header{}}
		r.SetBasicAuth(user, pass)
		authHeader = []Pair{{Key: "Authorization", Value: r.Header.Get("Authorization")}}
	case "bearer":
		tok, err := fill("the token", auth.Token, interp.Header)
		if err != nil {
			return nil, nil, err
		}
		authHeader = []Pair{{Key: "Authorization", Value: "Bearer " + tok}}
	case "header", "query":
		name, err := fill("the key name", auth.Name, interp.Header)
		if err != nil {
			return nil, nil, err
		}
		val, err := fill("the key", auth.Value, interp.Header)
		if err != nil {
			return nil, nil, err
		}
		if auth.Type == "query" {
			qs.Set(name, val)
		} else {
			authHeader = []Pair{{Key: name, Value: val}}
		}
	}
	u.RawQuery = qs.Encode()

	// Headers: the data source's, then sign-in, then the query's own,
	// each replacing earlier ones of the same name.
	header := http.Header{}
	var order []string
	set := func(k, v string) {
		k = http.CanonicalHeaderKey(strings.TrimSpace(k))
		if k == "" {
			return
		}
		if _, ok := header[k]; !ok {
			order = append(order, k)
		}
		header.Set(k, v)
	}
	for _, list := range [][]Pair{ds.Config.Headers, authHeader, q.Headers} {
		for _, p := range list {
			if p.Off || strings.TrimSpace(p.Key) == "" {
				continue
			}
			k, err := fill("a header name", p.Key, interp.Header)
			if err != nil {
				return nil, nil, err
			}
			v, err := fill("the header "+k, p.Value, interp.Header)
			if err != nil {
				return nil, nil, err
			}
			set(k, v)
		}
	}

	var body string
	contentType := ""
	switch q.BodyType {
	case "json":
		body, err = fill("the JSON body", q.Body, interp.JSON)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(body) != "" && !json.Valid([]byte(body)) {
			var probe any
			msg := json.Unmarshal([]byte(body), &probe)
			return nil, nil, &RunError{Message: fmt.Sprintf("the JSON body is not valid once its bindings are filled: %s", msg), Binding: true}
		}
		contentType = "application/json"
	case "xml":
		body, err = fill("the XML body", q.Body, interp.XML)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(body) != "" {
			if err := xmltree.Check(body); err != nil {
				return nil, nil, &RunError{Message: fmt.Sprintf("the XML body is not well formed once its bindings are filled: %s", err), Binding: true}
			}
		}
		contentType = "application/xml; charset=utf-8"
	case "form":
		form := url.Values{}
		if err := addPairs("the form", q.Form, form.Add); err != nil {
			return nil, nil, err
		}
		body = form.Encode()
		contentType = "application/x-www-form-urlencoded"
	case "text":
		body, err = fill("the body", q.Body, interp.Text)
		if err != nil {
			return nil, nil, err
		}
		contentType = "text/plain; charset=utf-8"
	}
	if contentType != "" && header.Get("Content-Type") == "" {
		set("Content-Type", contentType)
	}
	if header.Get("Accept") == "" {
		set("Accept", "application/json, application/xml;q=0.9, text/*;q=0.8, */*;q=0.5")
	}
	if header.Get("User-Agent") == "" {
		set("User-Agent", "Pimpbase")
	}

	var rd io.Reader
	if q.BodyType != "none" && body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, q.Method, u.String(), rd)
	if err != nil {
		return nil, nil, &RunError{Message: err.Error(), Binding: true}
	}
	req.Header = header
	echo := &Echo{Method: q.Method, URL: u.String(), Body: body}
	for _, k := range order {
		echo.Headers = append(echo.Headers, Pair{Key: k, Value: header.Get(k)})
	}
	return req, echo, nil
}

// redact hides secret values and credentials in an echoed request.
func (st *run) redact(e *Echo) {
	e.URL = st.hide(e.URL)
	e.Body = st.hide(e.Body)
	for i := range e.Headers {
		p := &e.Headers[i]
		if strings.EqualFold(p.Key, "Authorization") {
			if scheme, _, ok := strings.Cut(p.Value, " "); ok {
				p.Value = scheme + " ••••••"
			} else {
				p.Value = "••••••"
			}
			continue
		}
		p.Value = st.hide(p.Value)
	}
}

func (st *run) hide(s string) string {
	for _, sec := range st.secrets {
		s = strings.ReplaceAll(s, sec, "••••••")
		if esc := url.QueryEscape(sec); esc != sec {
			s = strings.ReplaceAll(s, esc, "••••••")
		}
	}
	return s
}

func (r *Runner) cached(key string) *Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.cache[key]
	if !ok || time.Now().After(c.expires) {
		delete(r.cache, key)
		return nil
	}
	copy := *c.res
	copy.Cached = true
	return &copy
}

func (r *Runner) store(key string, res *Result, ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cache) >= 500 {
		now := time.Now()
		for k, c := range r.cache {
			if now.After(c.expires) {
				delete(r.cache, k)
			}
		}
		if len(r.cache) >= 500 {
			r.cache = map[string]cached{}
		}
	}
	r.cache[key] = cached{res: res, expires: time.Now().Add(ttl)}
}

// decode reads a body as the query says, or by its content type and
// first characters.
func decode(want, contentType string, data []byte) (format string, body any, parseErr string) {
	format = want
	if format == "auto" || format == "" {
		ct := strings.ToLower(contentType)
		trimmed := bytes.TrimSpace(data)
		switch {
		case strings.Contains(ct, "json"):
			format = "json"
		case strings.Contains(ct, "xml") && !strings.Contains(ct, "html"):
			format = "xml"
		case len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '['):
			format = "json"
		case len(trimmed) > 0 && trimmed[0] == '<' && !strings.Contains(ct, "html") && !bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<!doctype html")) && !bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<html")):
			format = "xml"
		default:
			format = "text"
		}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return format, nil, ""
	}
	switch format {
	case "json":
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return "text", string(data), "the answer is not valid JSON: " + err.Error()
		}
		return format, numbers(v), ""
	case "xml":
		v, err := xmltree.Decode(bytes.NewReader(data))
		if err != nil {
			return "text", string(data), "the answer is not valid XML: " + err.Error()
		}
		return format, v, ""
	}
	return "text", string(data), ""
}

// numbers turns json.Number into float64 where that loses nothing, so
// bindings and formulas see numbers; long integers stay text.
func numbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		if err != nil || (!strings.ContainsAny(x.String(), ".eE") && len(strings.TrimLeft(x.String(), "-")) > 15) {
			return x.String()
		}
		return f
	case map[string]any:
		for k, y := range x {
			x[k] = numbers(y)
		}
	case []any:
		for i, y := range x {
			x[i] = numbers(y)
		}
	}
	return v
}

// rows finds the records in a body: at the result path, or else the body
// itself when it is a list, or else the first list inside it.
func rows(body any, path string) []any {
	var v any
	if path != "" {
		v, _ = interp.Walk(body, strings.Split(path, "."))
	} else {
		v = firstList(body, 4)
		if v == nil {
			v = body
		}
	}
	var list []any
	switch x := v.(type) {
	case nil:
		return []any{}
	case []any:
		list = x
	default:
		list = []any{x}
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		} else {
			out = append(out, map[string]any{"value": item})
		}
	}
	return out
}

func firstList(v any, depth int) any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		if depth == 0 {
			return nil
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if l, ok := x[k].([]any); ok {
				return l
			}
		}
		for _, k := range keys {
			if l := firstList(x[k], depth-1); l != nil {
				return l
			}
		}
	}
	return nil
}

// fields describes the columns of rows from the first 100 of them.
func fields(rows []any) []FieldInfo {
	var names []string
	kinds := map[string]map[string]bool{}
	for i, r := range rows {
		if i >= 100 {
			break
		}
		m, _ := r.(map[string]any)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if kinds[k] == nil {
				kinds[k] = map[string]bool{}
				names = append(names, k)
			}
			if t := kindOf(m[k]); t != "" {
				kinds[k][t] = true
			}
		}
	}
	out := make([]FieldInfo, 0, len(names))
	for _, n := range names {
		t := "text"
		if len(kinds[n]) == 1 {
			for k := range kinds[n] {
				t = k
			}
		} else if len(kinds[n]) == 2 && kinds[n]["number"] && kinds[n]["numtext"] {
			t = "number"
		}
		if t == "numtext" {
			t = "number"
		}
		out = append(out, FieldInfo{Name: n, Type: t})
	}
	return out
}

func kindOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return "number"
	case bool:
		return "boolean"
	case string:
		if x == "" {
			return ""
		}
		// Leading zeros (phone numbers, codes) stay text.
		if _, err := strconv.ParseFloat(x, 64); err == nil && len(x) < 16 && !(len(x) > 1 && x[0] == '0' && x[1] != '.') {
			return "numtext"
		}
		if len(x) >= 10 && x[4] == '-' && x[7] == '-' {
			if _, err := time.Parse("2006-01-02", x[:10]); err == nil {
				return "date"
			}
		}
		return "text"
	case map[string]any, []any:
		return "object"
	}
	return "text"
}
