// Package queries stores an app's API connectors: data sources (a REST
// API's address, headers and sign-in), the queries made against them, and
// the app's variables, and runs queries with their bindings filled.
package queries

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/ids"
	"github.com/bubibase/bubibase/internal/interp"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
)

// Datasource is an API the app talks to.
type Datasource struct {
	ID    string `json:"id"`
	AppID string `json:"appId"`
	Name  string `json:"name"`
	// Kind is "rest"; other connectors come later.
	Kind      string    `json:"kind"`
	Config    Config    `json:"config"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Config is a REST data source's settings. Text values may hold bindings,
// usually {{ env.NAME }}.
type Config struct {
	BaseURL string `json:"baseUrl"`
	// Headers are sent with every query; a query's own headers win.
	Headers        []Pair `json:"headers"`
	Auth           Auth   `json:"auth"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// Auth signs requests in.
type Auth struct {
	// Type is "none", "basic", "bearer", "header" (Name: Value) or "query"
	// (?Name=Value).
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	Name     string `json:"name,omitempty"`
	Value    string `json:"value,omitempty"`
}

// Pair is a header, query string or form entry. Off entries are kept but
// not sent.
type Pair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Off   bool   `json:"off,omitempty"`
}

// Query is one request against a data source.
type Query struct {
	ID           string `json:"id"`
	AppID        string `json:"appId"`
	DatasourceID string `json:"datasourceId"`
	Name         string `json:"name"`
	Definition
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Definition is what a query sends and how its answer is read.
type Definition struct {
	Method string `json:"method"`
	// Path is added to the data source's address. Without a data source
	// address it may be a whole URL.
	Path    string `json:"path"`
	Query   []Pair `json:"query"`
	Headers []Pair `json:"headers"`
	// BodyType is "none", "json", "form", "xml" or "text".
	BodyType string `json:"bodyType"`
	Body     string `json:"body"`
	Form     []Pair `json:"form"`
	// Parameters are the values callers pass: {{ params.name }}.
	Parameters []Param `json:"parameters"`
	// ResponseType is "auto", "json", "xml" or "text".
	ResponseType string `json:"responseType"`
	// ResultPath picks the rows out of the body, such as data.items.
	ResultPath string `json:"resultPath"`
	// Role is who may run the query from a published app.
	Role         rbac.Role `json:"role"`
	CacheSeconds int       `json:"cacheSeconds"`
	// Fields is the shape of the rows, kept from the last test run so the
	// designer can offer them.
	Fields []FieldInfo `json:"fields"`
}

// Param is a value a query takes.
type Param struct {
	Name string `json:"name"`
	// Type is "text", "number", "boolean" or "json".
	Type    string `json:"type"`
	Default string `json:"default"`
	// Test is used when the query runs in the API studio.
	Test string `json:"test"`
}

// FieldInfo describes one column of a query's rows.
type FieldInfo struct {
	Name string `json:"name"`
	// Type is "text", "number", "boolean", "date" or "object".
	Type string `json:"type"`
}

// Variable is an app value queries read as {{ env.NAME }}, with one value
// for the builder (development) and one for the published app
// (production). Secret values are never sent back to the browser.
type Variable struct {
	Name        string `json:"name"`
	Secret      bool   `json:"secret"`
	Development string `json:"development"`
	Production  string `json:"production"`
	// HasDevelopment and HasProduction tell whether a secret is set.
	HasDevelopment bool `json:"hasDevelopment"`
	HasProduction  bool `json:"hasProduction"`
}

const (
	Development = "development"
	Production  = "production"
)

var (
	ErrNotFound = errors.New("not found")
	nameRe      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _-]{0,62}$`)
	varRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	methods     = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
)

// InvalidError messages are safe to show.
type InvalidError struct {
	Message string
	// Field names the setting at fault, such as "path" or "headers".
	Field string
}

func (e *InvalidError) Error() string { return e.Message }

func invalid(field, format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...), Field: field}
}

type Service struct {
	store *store.Store
	now   func() time.Time
}

func NewService(s *store.Store) *Service { return &Service{store: s, now: time.Now} }

func checkName(what, name string) (string, error) {
	n := strings.TrimSpace(name)
	if !nameRe.MatchString(n) {
		return "", invalid("name", "a %s name starts with a letter and uses letters, digits, spaces, dashes and underscores (up to 63)", what)
	}
	return n, nil
}

func checkTemplate(field, src string) error {
	if _, err := interp.Parse(src); err != nil {
		return invalid(field, "%s: %s", field, err)
	}
	return nil
}

func checkPairs(field string, ps []Pair) error {
	for _, p := range ps {
		if err := checkTemplate(field, p.Key); err != nil {
			return err
		}
		if err := checkTemplate(field, p.Value); err != nil {
			return err
		}
	}
	return nil
}

// Check validates a data source and fills in defaults.
func (d *Datasource) Check() error {
	n, err := checkName("data source", d.Name)
	if err != nil {
		return err
	}
	d.Name = n
	if d.Kind == "" {
		d.Kind = "rest"
	}
	if d.Kind != "rest" {
		return invalid("kind", "only REST data sources are supported")
	}
	c := &d.Config
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	if err := checkTemplate("baseUrl", c.BaseURL); err != nil {
		return err
	}
	if c.BaseURL != "" && !strings.HasPrefix(c.BaseURL, "{{") {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("baseUrl", "the address starts with http:// or https://")
		}
	}
	if c.Headers == nil {
		c.Headers = []Pair{}
	}
	if err := checkPairs("headers", c.Headers); err != nil {
		return err
	}
	switch c.Auth.Type {
	case "":
		c.Auth.Type = "none"
	case "none", "basic", "bearer", "header", "query":
	default:
		return invalid("auth", "sign-in is none, basic, bearer, header or query")
	}
	for _, s := range []string{c.Auth.Username, c.Auth.Password, c.Auth.Token, c.Auth.Name, c.Auth.Value} {
		if err := checkTemplate("auth", s); err != nil {
			return err
		}
	}
	if (c.Auth.Type == "header" || c.Auth.Type == "query") && strings.TrimSpace(c.Auth.Name) == "" {
		return invalid("auth", "name the header or query parameter that carries the key")
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 30
	}
	if c.TimeoutSeconds > 120 {
		return invalid("timeoutSeconds", "the timeout is at most 120 seconds")
	}
	return nil
}

// Check validates a query and fills in defaults.
func (q *Query) Check() error {
	n, err := checkName("query", q.Name)
	if err != nil {
		return err
	}
	q.Name = n
	d := &q.Definition
	d.Method = strings.ToUpper(strings.TrimSpace(d.Method))
	if d.Method == "" {
		d.Method = "GET"
	}
	ok := false
	for _, m := range methods {
		ok = ok || m == d.Method
	}
	if !ok {
		return invalid("method", "the method is one of %s", strings.Join(methods, ", "))
	}
	if err := checkTemplate("path", d.Path); err != nil {
		return err
	}
	for _, list := range []*[]Pair{&d.Query, &d.Headers, &d.Form} {
		if *list == nil {
			*list = []Pair{}
		}
	}
	if err := checkPairs("query", d.Query); err != nil {
		return err
	}
	if err := checkPairs("headers", d.Headers); err != nil {
		return err
	}
	if err := checkPairs("form", d.Form); err != nil {
		return err
	}
	switch d.BodyType {
	case "":
		d.BodyType = "none"
	case "none", "json", "form", "xml", "text":
	default:
		return invalid("bodyType", "the body is none, json, form, xml or text")
	}
	if err := checkTemplate("body", d.Body); err != nil {
		return err
	}
	if d.Parameters == nil {
		d.Parameters = []Param{}
	}
	seen := map[string]bool{}
	for i := range d.Parameters {
		p := &d.Parameters[i]
		p.Name = strings.TrimSpace(p.Name)
		if !varRe.MatchString(p.Name) {
			return invalid("parameters", "parameter names use letters, digits and underscores, and start with a letter")
		}
		if seen[strings.ToLower(p.Name)] {
			return invalid("parameters", "the parameter %s is listed twice", p.Name)
		}
		seen[strings.ToLower(p.Name)] = true
		switch p.Type {
		case "":
			p.Type = "text"
		case "text", "number", "boolean", "json":
		default:
			return invalid("parameters", "a parameter is text, number, boolean or json")
		}
	}
	switch d.ResponseType {
	case "":
		d.ResponseType = "auto"
	case "auto", "json", "xml", "text":
	default:
		return invalid("responseType", "the response is read as auto, json, xml or text")
	}
	d.ResultPath = strings.Trim(strings.TrimSpace(d.ResultPath), ".")
	if d.Role < rbac.Public || d.Role > rbac.Admin {
		return invalid("role", "the role is PUBLIC, BASIC, POWER or ADMIN")
	}
	if d.CacheSeconds < 0 || d.CacheSeconds > 86400 {
		return invalid("cacheSeconds", "results are cached for 0 to 86400 seconds")
	}
	if d.Fields == nil {
		d.Fields = []FieldInfo{}
	}
	return nil
}

// NewQuery returns a query with defaults, for JSON to be decoded onto.
func NewQuery() *Query {
	return &Query{Definition: Definition{Method: "GET", BodyType: "none", ResponseType: "auto", Role: rbac.Basic}}
}

// decodeOnto reads JSON onto v, keeping the fields the JSON leaves out.
func decodeOnto(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return invalid("", "invalid JSON: %s", err)
	}
	return nil
}

func uniqueErr(err error, what, name string) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return invalid("name", "another %s is already named %s", what, name)
	}
	return err
}

func stamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// Data sources.

const dsCols = `id, app_id, name, kind, config, created_at, updated_at`

func scanDatasource(row interface{ Scan(...any) error }) (*Datasource, error) {
	var d Datasource
	var config, created, updated string
	if err := row.Scan(&d.ID, &d.AppID, &d.Name, &d.Kind, &config, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("data source %w", ErrNotFound)
		}
		return nil, err
	}
	json.Unmarshal([]byte(config), &d.Config)
	if d.Config.Headers == nil {
		d.Config.Headers = []Pair{}
	}
	d.CreatedAt, d.UpdatedAt = stamp(created), stamp(updated)
	return &d, nil
}

func (s *Service) Datasources(ctx context.Context, appID string) ([]*Datasource, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT `+dsCols+` FROM app_datasources WHERE app_id = ? ORDER BY name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Datasource{}
	for rows.Next() {
		d, err := scanDatasource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Service) Datasource(ctx context.Context, appID, id string) (*Datasource, error) {
	return scanDatasource(s.store.Meta.QueryRow(ctx, `SELECT `+dsCols+` FROM app_datasources WHERE app_id = ? AND id = ?`, appID, id))
}

// CreateDatasource adds a data source from its JSON.
func (s *Service) CreateDatasource(ctx context.Context, appID string, data []byte) (*Datasource, error) {
	d := &Datasource{Kind: "rest"}
	if err := decodeOnto(data, d); err != nil {
		return nil, err
	}
	d.ID, d.AppID = ids.New("ds"), appID
	if err := d.Check(); err != nil {
		return nil, err
	}
	config, _ := json.Marshal(d.Config)
	now := s.now().UTC().Format(time.RFC3339)
	_, err := s.store.Meta.Exec(ctx, `INSERT INTO app_datasources (`+dsCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.ID, appID, d.Name, d.Kind, string(config), now, now)
	if err := uniqueErr(err, "data source", d.Name); err != nil {
		return nil, err
	}
	return s.Datasource(ctx, appID, d.ID)
}

// UpdateDatasource changes the settings the JSON names.
func (s *Service) UpdateDatasource(ctx context.Context, appID, id string, data []byte) (*Datasource, error) {
	d, err := s.Datasource(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, invalid("", "invalid JSON: %s", err)
	}
	if _, ok := keys["config"]; ok {
		d.Config = Config{}
	}
	if err := decodeOnto(data, d); err != nil {
		return nil, err
	}
	d.ID, d.AppID = id, appID
	if err := d.Check(); err != nil {
		return nil, err
	}
	config, _ := json.Marshal(d.Config)
	_, err = s.store.Meta.Exec(ctx, `UPDATE app_datasources SET name = ?, kind = ?, config = ?, updated_at = ? WHERE app_id = ? AND id = ?`,
		d.Name, d.Kind, string(config), s.now().UTC().Format(time.RFC3339), appID, id)
	if err := uniqueErr(err, "data source", d.Name); err != nil {
		return nil, err
	}
	return s.Datasource(ctx, appID, id)
}

// DeleteDatasource removes a data source and its queries.
func (s *Service) DeleteDatasource(ctx context.Context, appID, id string) error {
	tx, err := s.store.Meta.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `DELETE FROM app_queries WHERE app_id = ? AND datasource_id = ?`, appID, id); err != nil {
		return err
	}
	res, err := tx.Exec(ctx, `DELETE FROM app_datasources WHERE app_id = ? AND id = ?`, appID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("data source %w", ErrNotFound)
	}
	return tx.Commit()
}

// Queries.

const qCols = `id, app_id, datasource_id, name, definition, created_at, updated_at`

func scanQuery(row interface{ Scan(...any) error }) (*Query, error) {
	q := NewQuery()
	var def, created, updated string
	if err := row.Scan(&q.ID, &q.AppID, &q.DatasourceID, &q.Name, &def, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("query %w", ErrNotFound)
		}
		return nil, err
	}
	json.Unmarshal([]byte(def), &q.Definition)
	q.Check()
	q.CreatedAt, q.UpdatedAt = stamp(created), stamp(updated)
	return q, nil
}

func (s *Service) Queries(ctx context.Context, appID string) ([]*Query, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT `+qCols+` FROM app_queries WHERE app_id = ? ORDER BY name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Query{}
	for rows.Next() {
		q, err := scanQuery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Query finds a query by id, or by name ignoring case.
func (s *Service) Query(ctx context.Context, appID, idOrName string) (*Query, error) {
	return scanQuery(s.store.Meta.QueryRow(ctx, `SELECT `+qCols+` FROM app_queries WHERE app_id = ? AND (id = ? OR LOWER(name) = LOWER(?)) ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`, appID, idOrName, idOrName, idOrName))
}

// prepare decodes a query's JSON onto q and checks it and its data source.
func (s *Service) prepare(ctx context.Context, appID string, q *Query, data []byte) error {
	id, created := q.ID, q.CreatedAt
	// Lists the JSON names replace the old ones rather than merge into them.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return invalid("", "invalid JSON: %s", err)
	}
	for k, list := range map[string]any{"query": &q.Query, "headers": &q.Headers, "form": &q.Form} {
		if _, ok := keys[k]; ok {
			*list.(*[]Pair) = nil
		}
	}
	if _, ok := keys["parameters"]; ok {
		q.Parameters = nil
	}
	if _, ok := keys["fields"]; ok {
		q.Fields = nil
	}
	if err := decodeOnto(data, q); err != nil {
		return err
	}
	q.ID, q.AppID, q.CreatedAt = id, appID, created
	if err := q.Check(); err != nil {
		return err
	}
	if _, err := s.Datasource(ctx, appID, q.DatasourceID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return invalid("datasourceId", "choose the data source the query uses")
		}
		return err
	}
	// Bindings name queries ignoring case, so names must differ in more
	// than case.
	var n int
	if err := s.store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM app_queries WHERE app_id = ? AND LOWER(name) = LOWER(?) AND id <> ?`, appID, q.Name, q.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return invalid("name", "another query is already named %s", q.Name)
	}
	return nil
}

func (s *Service) CreateQuery(ctx context.Context, appID string, data []byte) (*Query, error) {
	q := NewQuery()
	if err := s.prepare(ctx, appID, q, data); err != nil {
		return nil, err
	}
	q.ID = ids.New("qry")
	def, _ := json.Marshal(q.Definition)
	now := s.now().UTC().Format(time.RFC3339)
	_, err := s.store.Meta.Exec(ctx, `INSERT INTO app_queries (`+qCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		q.ID, appID, q.DatasourceID, q.Name, string(def), now, now)
	if err := uniqueErr(err, "query", q.Name); err != nil {
		return nil, err
	}
	return s.Query(ctx, appID, q.ID)
}

func (s *Service) UpdateQuery(ctx context.Context, appID, id string, data []byte) (*Query, error) {
	q, err := s.Query(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	// Lists are replaced, not merged, when the JSON names them.
	if err := s.prepare(ctx, appID, q, data); err != nil {
		return nil, err
	}
	def, _ := json.Marshal(q.Definition)
	_, err = s.store.Meta.Exec(ctx, `UPDATE app_queries SET datasource_id = ?, name = ?, definition = ?, updated_at = ? WHERE app_id = ? AND id = ?`,
		q.DatasourceID, q.Name, string(def), s.now().UTC().Format(time.RFC3339), appID, q.ID)
	if err := uniqueErr(err, "query", q.Name); err != nil {
		return nil, err
	}
	return s.Query(ctx, appID, q.ID)
}

// CheckQuery validates a query that may not be saved, for test runs.
func (s *Service) CheckQuery(ctx context.Context, appID string, data []byte) (*Query, error) {
	q := NewQuery()
	// A saved query being edited keeps its id, so its own name is no
	// conflict.
	var head struct {
		ID string `json:"id"`
	}
	json.Unmarshal(data, &head)
	q.ID = head.ID
	if err := s.prepare(ctx, appID, q, data); err != nil {
		return nil, err
	}
	return q, nil
}

func (s *Service) DeleteQuery(ctx context.Context, appID, id string) error {
	res, err := s.store.Meta.Exec(ctx, `DELETE FROM app_queries WHERE app_id = ? AND id = ?`, appID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("query %w", ErrNotFound)
	}
	return nil
}

// Variables.

// Variables lists an app's variables. With reveal false, secret values are
// left out.
func (s *Service) Variables(ctx context.Context, appID string, reveal bool) ([]*Variable, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT name, secret, dev_value, prod_value FROM app_variables WHERE app_id = ? ORDER BY name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Variable{}
	for rows.Next() {
		var v Variable
		var secret int
		if err := rows.Scan(&v.Name, &secret, &v.Development, &v.Production); err != nil {
			return nil, err
		}
		v.Secret = secret != 0
		v.HasDevelopment, v.HasProduction = v.Development != "", v.Production != ""
		if v.Secret && !reveal {
			v.Development, v.Production = "", ""
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

// VariableInput changes a variable; nil fields keep their value.
type VariableInput struct {
	Secret      *bool   `json:"secret"`
	Development *string `json:"development"`
	Production  *string `json:"production"`
}

// SetVariable creates or changes a variable.
func (s *Service) SetVariable(ctx context.Context, appID, name string, in VariableInput) (*Variable, error) {
	if !varRe.MatchString(name) {
		return nil, invalid("name", "variable names use letters, digits and underscores, and do not start with a digit")
	}
	for _, v := range []*string{in.Development, in.Production} {
		if v != nil && len(*v) > 8192 {
			return nil, invalid("value", "a value has at most 8192 characters")
		}
	}
	all, err := s.Variables(ctx, appID, true)
	if err != nil {
		return nil, err
	}
	var cur *Variable
	for _, v := range all {
		if strings.EqualFold(v.Name, name) {
			cur = v
		}
	}
	if cur == nil {
		cur = &Variable{Name: name}
		if _, err := s.store.Meta.Exec(ctx, `INSERT INTO app_variables (app_id, name, secret, dev_value, prod_value) VALUES (?, ?, 0, '', '')`, appID, name); err != nil {
			return nil, err
		}
	}
	if in.Secret != nil {
		cur.Secret = *in.Secret
	}
	if in.Development != nil {
		cur.Development = *in.Development
	}
	if in.Production != nil {
		cur.Production = *in.Production
	}
	secret := 0
	if cur.Secret {
		secret = 1
	}
	if _, err := s.store.Meta.Exec(ctx, `UPDATE app_variables SET secret = ?, dev_value = ?, prod_value = ? WHERE app_id = ? AND name = ?`,
		secret, cur.Development, cur.Production, appID, cur.Name); err != nil {
		return nil, err
	}
	cur.HasDevelopment, cur.HasProduction = cur.Development != "", cur.Production != ""
	if cur.Secret {
		cur.Development, cur.Production = "", ""
	}
	return cur, nil
}

func (s *Service) DeleteVariable(ctx context.Context, appID, name string) error {
	res, err := s.store.Meta.Exec(ctx, `DELETE FROM app_variables WHERE app_id = ? AND name = ?`, appID, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("variable %w", ErrNotFound)
	}
	return nil
}

// values returns the variables of one environment, and the secret values
// to hide from echoed requests.
func (s *Service) values(ctx context.Context, appID, env string) (map[string]any, []string, error) {
	all, err := s.Variables(ctx, appID, true)
	if err != nil {
		return nil, nil, err
	}
	vals := map[string]any{}
	var secrets []string
	for _, v := range all {
		val := v.Production
		if env == Development {
			val = v.Development
			// A variable set only for production is used in development too,
			// unless it is secret.
			if val == "" && !v.Secret {
				val = v.Production
			}
		}
		vals[v.Name] = val
		if v.Secret && len(val) >= 3 {
			secrets = append(secrets, val)
		}
	}
	// Longer secrets first, so one that contains another is hidden whole.
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return vals, secrets, nil
}

// Values returns the variables of one environment, as {{ env.NAME }}
// reads them, and the secret values to hide from logs.
func (s *Service) Values(ctx context.Context, appID, env string) (map[string]any, []string, error) {
	return s.values(ctx, appID, env)
}
