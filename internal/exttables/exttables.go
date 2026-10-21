// Package exttables makes tables of an external database usable like
// built-in ones: a builder imports a table (or MongoDB collection) from a
// database data source, and screens, the data studio and the REST API
// read and write its rows, with the same table permissions and row rules.
// Bubibase keeps only the description of the table; the rows stay in the
// database.
package exttables

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/extdb"
	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/ids"
	"github.com/bubibase/bubibase/internal/queries"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/tables"
)

// Prefix starts the id of every external table.
const Prefix = "ext_"

// Is reports whether a table id names an external table.
func Is(id string) bool { return strings.HasPrefix(id, Prefix) }

// Table is an imported table.
type Table struct {
	ID           string `json:"id"`
	AppID        string `json:"appId"`
	DatasourceID string `json:"datasourceId"`
	Name         string `json:"name"`
	// Schema and Source name the table in the database.
	Schema string `json:"schema"`
	Source string `json:"source"`
	// Kind is "table", "view" or "collection".
	Kind       string   `json:"kind"`
	Columns    []Column `json:"columns"`
	PrimaryKey []string `json:"primaryKey"`
	// PrimaryField is the column shown as a row's label.
	PrimaryField string             `json:"primaryField,omitempty"`
	Permissions  tables.Permissions `json:"permissions"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

// Column is a database column and whether apps see it.
type Column struct {
	extdb.Column
	Hidden bool `json:"hidden,omitempty"`
}

// Key is the single primary key column rows are found by, or "".
func (t *Table) Key() string {
	if len(t.PrimaryKey) == 1 {
		return t.PrimaryKey[0]
	}
	return ""
}

// Writable reports whether rows can be added, changed and deleted.
func (t *Table) Writable() bool { return t.Key() != "" }

func (t *Table) column(name string) *Column {
	for i := range t.Columns {
		if strings.EqualFold(t.Columns[i].Name, name) {
			return &t.Columns[i]
		}
	}
	return nil
}

// visible lists the columns apps see.
func (t *Table) visible() []*Column {
	var out []*Column
	for i := range t.Columns {
		if c := &t.Columns[i]; !c.Hidden && !c.Binary {
			out = append(out, c)
		}
	}
	return out
}

// target is what the driver reads: the visible columns and the key.
func (t *Table) target() *extdb.Target {
	tg := &extdb.Target{Schema: t.Schema, Name: t.Source, Key: t.Key()}
	for _, c := range t.Columns {
		if (!c.Hidden && !c.Binary) || c.Name == tg.Key {
			tg.Columns = append(tg.Columns, c.Column)
		}
	}
	return tg
}

// Field is a column as the builder and screens see it, in the shape of a
// built-in table's field.
type Field struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Type     tables.FieldType `json:"type"`
	Required bool             `json:"required,omitempty"`
	Options  []string         `json:"options,omitempty"`
	ReadOnly bool             `json:"readOnly,omitempty"`
}

// View is an external table in the shape of a built-in table, with what
// is particular to it under "external".
type View struct {
	ID           string             `json:"id"`
	AppID        string             `json:"appId"`
	Name         string             `json:"name"`
	Fields       []Field            `json:"fields"`
	PrimaryField string             `json:"primaryField,omitempty"`
	Permissions  tables.Permissions `json:"permissions"`
	External     External           `json:"external"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

// External describes where an external table's rows live.
type External struct {
	DatasourceID string   `json:"datasourceId"`
	Kind         string   `json:"kind"`
	Schema       string   `json:"schema"`
	Source       string   `json:"source"`
	SourceKind   string   `json:"sourceKind"`
	PrimaryKey   []string `json:"primaryKey"`
	ReadOnly     bool     `json:"readOnly"`
	Columns      []Column `json:"columns"`
}

// View gives the table as the API shows it.
func (t *Table) View(dsKind string, readOnly bool) *View {
	v := &View{ID: t.ID, AppID: t.AppID, Name: t.Name, Fields: []Field{}, PrimaryField: t.PrimaryField, Permissions: t.Permissions,
		External: External{DatasourceID: t.DatasourceID, Kind: dsKind, Schema: t.Schema, Source: t.Source, SourceKind: t.Kind,
			PrimaryKey: t.PrimaryKey, ReadOnly: readOnly || !t.Writable(), Columns: t.Columns},
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	for _, c := range t.visible() {
		v.Fields = append(v.Fields, Field{ID: c.Name, Name: c.Name, Type: tables.FieldType(c.Type), Options: c.Options,
			Required: required(&c.Column), ReadOnly: c.Generated || readOnly || !t.Writable()})
	}
	return v
}

// required columns must be given a value when a row is added.
func required(c *extdb.Column) bool { return !c.Nullable && !c.Auto && !c.Generated }

// Errors.
var ErrNotFound = errors.New("not found")

// Service stores imported tables and works with their rows.
type Service struct {
	store   *store.Store
	queries *queries.Service
	runner  *queries.Runner
	now     func() time.Time
}

func NewService(s *store.Store, qs *queries.Service, r *queries.Runner) *Service {
	return &Service{store: s, queries: qs, runner: r, now: time.Now}
}

func invalid(field, format string, args ...any) error {
	return &tables.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

const cols = `id, app_id, datasource_id, name, definition, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (*Table, error) {
	var t Table
	var def, created, updated string
	if err := row.Scan(&t.ID, &t.AppID, &t.DatasourceID, &t.Name, &def, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("table %w", tables.ErrNotFound)
		}
		return nil, err
	}
	var d definition
	if err := json.Unmarshal([]byte(def), &d); err != nil {
		return nil, err
	}
	t.Schema, t.Source, t.Kind, t.Columns, t.PrimaryKey, t.PrimaryField, t.Permissions = d.Schema, d.Source, d.Kind, d.Columns, d.PrimaryKey, d.PrimaryField, d.Permissions
	if t.Columns == nil {
		t.Columns = []Column{}
	}
	if t.PrimaryKey == nil {
		t.PrimaryKey = []string{}
	}
	if t.Permissions.Delete == "" {
		t.Permissions.Delete = t.Permissions.Write
	}
	if t.Permissions.Rows == nil {
		t.Permissions.Rows = []tables.RowRule{}
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &t, nil
}

// definition is what the definition column holds.
type definition struct {
	Schema       string             `json:"schema"`
	Source       string             `json:"source"`
	Kind         string             `json:"kind"`
	Columns      []Column           `json:"columns"`
	PrimaryKey   []string           `json:"primaryKey"`
	PrimaryField string             `json:"primaryField,omitempty"`
	Permissions  tables.Permissions `json:"permissions"`
}

func (t *Table) definition() string {
	js, _ := json.Marshal(definition{t.Schema, t.Source, t.Kind, t.Columns, t.PrimaryKey, t.PrimaryField, t.Permissions})
	return string(js)
}

// List gives an app's external tables.
func (s *Service) List(ctx context.Context, appID string) ([]*Table, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT `+cols+` FROM app_ext_tables WHERE app_id = ? ORDER BY name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Table{}
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get finds a table by id, or by name ignoring case.
func (s *Service) Get(ctx context.Context, appID, idOrName string) (*Table, error) {
	return scan(s.store.Meta.QueryRow(ctx, `SELECT `+cols+` FROM app_ext_tables WHERE app_id = ? AND (id = ? OR LOWER(name) = LOWER(?)) ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`, appID, idOrName, idOrName, idOrName))
}

// Views gives the tables as the API shows them.
func (s *Service) Views(ctx context.Context, appID string, ts []*Table) ([]*View, error) {
	dss, err := s.queries.Datasources(ctx, appID)
	if err != nil {
		return nil, err
	}
	byID := map[string]*queries.Datasource{}
	for _, d := range dss {
		byID[d.ID] = d
	}
	out := make([]*View, 0, len(ts))
	for _, t := range ts {
		kind, ro := "", true
		if d := byID[t.DatasourceID]; d != nil {
			kind, ro = d.Kind, d.Config.DB != nil && d.Config.DB.ReadOnly
		}
		out = append(out, t.View(kind, ro))
	}
	return out, nil
}

// View gives one table as the API shows it.
func (s *Service) View(ctx context.Context, t *Table) (*View, error) {
	vs, err := s.Views(ctx, t.AppID, []*Table{t})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

// source opens the database behind a table.
func (s *Service) source(ctx context.Context, t *Table, env string) (*queries.Datasource, extdb.Source, error) {
	ds, err := s.queries.Datasource(ctx, t.AppID, t.DatasourceID)
	if err != nil {
		return nil, nil, err
	}
	src, err := s.runner.Source(ctx, ds, env)
	if err != nil {
		return nil, nil, err
	}
	return ds, src, nil
}

// nameTaken reports whether a built-in or external table is named name.
func (s *Service) nameTaken(ctx context.Context, appID, name, except string) (bool, error) {
	var n int
	if err := s.store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM app_tables WHERE app_id = ? AND LOWER(name) = LOWER(?)`, appID, name).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := s.store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM app_ext_tables WHERE app_id = ? AND LOWER(name) = LOWER(?) AND id <> ?`, appID, name, except).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func checkName(name string) (string, error) {
	n := strings.TrimSpace(name)
	switch {
	case n == "":
		return "", invalid("name", "table name is required")
	case len(n) > 100:
		return "", invalid("name", "table name is too long")
	case strings.ContainsAny(n, "[]"):
		return "", invalid("name", "table name cannot contain [ or ]")
	case strings.HasPrefix(n, "_"):
		return "", invalid("name", "table name cannot start with _")
	}
	return n, nil
}

// Import describes a database table and saves it as an external table.
func (s *Service) Import(ctx context.Context, appID, dsID, schema, source, name string) (*Table, error) {
	ds, err := s.queries.Datasource(ctx, appID, dsID)
	if err != nil {
		return nil, err
	}
	if !ds.Database() {
		return nil, invalid("datasourceId", "only database data sources have tables")
	}
	if strings.TrimSpace(name) == "" {
		name = source
	}
	n, err := checkName(name)
	if err != nil {
		return nil, err
	}
	if taken, err := s.nameTaken(ctx, appID, n, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, invalid("name", "another table is already named %s", n)
	}
	src, err := s.runner.Source(ctx, ds, queries.Development)
	if err != nil {
		return nil, err
	}
	info, err := src.Describe(ctx, schema, source)
	if err != nil {
		return nil, invalid("source", "%s", err)
	}
	t := &Table{ID: ids.New("ext"), AppID: appID, DatasourceID: dsID, Name: n, Schema: info.Schema, Source: info.Name, Kind: info.Kind,
		PrimaryKey:  info.PrimaryKey,
		Permissions: tables.Permissions{Read: rbac.Basic, Write: rbac.Basic, Delete: rbac.Basic, Rows: []tables.RowRule{}}}
	if t.PrimaryKey == nil {
		t.PrimaryKey = []string{}
	}
	for _, c := range info.Columns {
		t.Columns = append(t.Columns, Column{Column: c})
	}
	t.PrimaryField = defaultLabel(t)
	now := s.now().UTC().Format(time.RFC3339)
	if _, err := s.store.Meta.Exec(ctx, `INSERT INTO app_ext_tables (`+cols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, appID, dsID, t.Name, t.definition(), now, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, invalid("name", "another table is already named %s", n)
		}
		return nil, err
	}
	return s.Get(ctx, appID, t.ID)
}

// defaultLabel picks a name-like text column to label rows.
func defaultLabel(t *Table) string {
	for _, want := range []string{"name", "title", "label", "email", "code"} {
		for _, c := range t.visible() {
			if strings.EqualFold(c.Name, want) {
				return c.Name
			}
		}
	}
	for _, c := range t.visible() {
		if c.Type == "text" && c.Name != t.Key() {
			return c.Name
		}
	}
	return ""
}

// Patch changes an external table; nil fields stay.
type Patch struct {
	Name         *string             `json:"name"`
	PrimaryField *string             `json:"primaryField"`
	Permissions  *tables.Permissions `json:"permissions"`
	// Hidden lists the columns apps do not see.
	Hidden *[]string `json:"hidden"`
	// Options turn text columns into choice fields, by column name; an
	// empty list turns one back into text.
	Options map[string][]string `json:"options"`
}

// Update changes a table's name, label, permissions or hidden columns.
func (s *Service) Update(ctx context.Context, appID, id string, p Patch) (*Table, error) {
	t, err := s.Get(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		n, err := checkName(*p.Name)
		if err != nil {
			return nil, err
		}
		if taken, err := s.nameTaken(ctx, appID, n, t.ID); err != nil {
			return nil, err
		} else if taken {
			return nil, invalid("name", "another table is already named %s", n)
		}
		t.Name = n
	}
	if p.Hidden != nil {
		hide := map[string]bool{}
		for _, h := range *p.Hidden {
			hide[strings.ToLower(h)] = true
		}
		for i := range t.Columns {
			c := &t.Columns[i]
			c.Hidden = hide[strings.ToLower(c.Name)] && c.Name != t.Key()
		}
	}
	for name, opts := range p.Options {
		c := t.column(name)
		if c == nil {
			return nil, invalid("options", "there is no column named %s", name)
		}
		if c.Type != "text" && c.Type != "single_select" {
			return nil, invalid("options", "only text columns can offer choices")
		}
		if len(opts) == 0 {
			c.Type, c.Options = "text", nil
		} else {
			c.Type, c.Options = "single_select", opts
		}
	}
	if p.PrimaryField != nil {
		if *p.PrimaryField != "" {
			c := t.column(*p.PrimaryField)
			if c == nil || c.Hidden {
				return nil, invalid("primaryField", "there is no field named %s", *p.PrimaryField)
			}
			t.PrimaryField = c.Name
		} else {
			t.PrimaryField = ""
		}
	}
	if c := t.column(t.PrimaryField); c != nil && c.Hidden {
		t.PrimaryField = defaultLabel(t)
	}
	if p.Permissions != nil {
		perm := *p.Permissions
		if perm.Delete == "" {
			perm.Delete = perm.Write
		}
		if perm.Rows == nil {
			perm.Rows = []tables.RowRule{}
		}
		if err := checkPermissions(t, &perm); err != nil {
			return nil, err
		}
		t.Permissions = perm
	}
	// Row rules may name hidden columns no longer.
	if err := checkPermissions(t, &t.Permissions); err != nil {
		return nil, err
	}
	return s.save(ctx, t)
}

func (s *Service) save(ctx context.Context, t *Table) (*Table, error) {
	_, err := s.store.Meta.Exec(ctx, `UPDATE app_ext_tables SET name = ?, definition = ?, updated_at = ? WHERE app_id = ? AND id = ?`,
		t.Name, t.definition(), s.now().UTC().Format(time.RFC3339), t.AppID, t.ID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, invalid("name", "another table is already named %s", t.Name)
		}
		return nil, err
	}
	return s.Get(ctx, t.AppID, t.ID)
}

// Refresh reads a table's columns again, keeping which are hidden and the
// choices given to text columns.
func (s *Service) Refresh(ctx context.Context, appID, id string) (*Table, error) {
	t, err := s.Get(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	_, src, err := s.source(ctx, t, queries.Development)
	if err != nil {
		return nil, err
	}
	info, err := src.Describe(ctx, t.Schema, t.Source)
	if err != nil {
		return nil, invalid("source", "%s", err)
	}
	old := map[string]Column{}
	for _, c := range t.Columns {
		old[c.Name] = c
	}
	t.Columns = nil
	for _, c := range info.Columns {
		nc := Column{Column: c}
		if o, ok := old[c.Name]; ok {
			nc.Hidden = o.Hidden
			if o.Type == "single_select" && c.Type == "text" {
				nc.Type, nc.Options = o.Type, o.Options
			}
		}
		t.Columns = append(t.Columns, nc)
	}
	t.Kind = info.Kind
	t.PrimaryKey = info.PrimaryKey
	if t.PrimaryKey == nil {
		t.PrimaryKey = []string{}
	}
	if c := t.column(t.PrimaryField); c == nil || c.Hidden {
		t.PrimaryField = defaultLabel(t)
	}
	// Rules naming dropped columns are reported, not silently kept.
	if err := checkPermissions(t, &t.Permissions); err != nil {
		return nil, err
	}
	return s.save(ctx, t)
}

// Delete forgets an imported table; the database table stays.
func (s *Service) Delete(ctx context.Context, appID, id string) error {
	res, err := s.store.Meta.Exec(ctx, `DELETE FROM app_ext_tables WHERE app_id = ? AND id = ?`, appID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("table %w", tables.ErrNotFound)
	}
	return nil
}

// DeleteFor forgets every table imported from a data source.
func (s *Service) DeleteFor(ctx context.Context, appID, dsID string) error {
	_, err := s.store.Meta.Exec(ctx, `DELETE FROM app_ext_tables WHERE app_id = ? AND datasource_id = ?`, appID, dsID)
	return err
}

// Formulas.

// scope resolves names in filters and row rules.
type scope struct{ t *Table }

func (s scope) FieldType(name string) (formula.Type, bool) {
	c := s.t.column(name)
	if c == nil || c.Hidden || c.Binary {
		return formula.Any, false
	}
	return extdb.FormulaType(c.Type), true
}

func (s scope) NameType(parts []string) (formula.Type, bool) {
	if len(parts) != 3 || !strings.EqualFold(parts[0], "Current") || !strings.EqualFold(parts[1], "User") {
		return formula.Any, false
	}
	switch strings.ToLower(parts[2]) {
	case "id", "email", "name", "role":
		return formula.Text, true
	case "groups", "roles":
		return formula.List, true
	}
	return formula.Any, false
}

func compileCond(t *Table, field, src string) (*formula.Checked, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	ch, errs := formula.Compile(src, scope{t})
	if errs != nil {
		return nil, &tables.ValidationError{Field: field, Message: errs[0].Message, Formula: errs}
	}
	if ch.Type != formula.Bool && ch.Type != formula.Any && ch.Type != formula.Null {
		return nil, &tables.ValidationError{Field: field, Message: "a condition must be true or false, this one gives " + ch.Type.String()}
	}
	return ch, nil
}

func checkPermissions(t *Table, p *tables.Permissions) error {
	for _, r := range []rbac.Role{p.Read, p.Write, p.Delete} {
		if r == "" || r == rbac.None {
			return invalid("permissions", "pick a role for reading, writing and deleting rows")
		}
	}
	seen := map[rbac.Role]bool{}
	for _, rule := range p.Rows {
		if rule.Role == "" || rule.Role == rbac.None {
			return invalid("permissions", "a row rule needs a role")
		}
		if rule.Role == rbac.Admin {
			return invalid("permissions", "admins always see every row")
		}
		if seen[rule.Role] {
			return invalid("permissions", "%s has two row rules", rule.Role)
		}
		seen[rule.Role] = true
		if _, err := compileCond(t, "permissions", rule.Filter); err != nil {
			var ve *tables.ValidationError
			if errors.As(err, &ve) {
				ve.Message = "row rule: " + ve.Message
			}
			return err
		}
	}
	return nil
}

// rule is the row rule that applies to an actor, nil when every row is
// open to them.
func (t *Table) rule(actor rbac.Actor) (*formula.Checked, error) {
	if actor.Builder || actor.Role == rbac.Admin {
		return nil, nil
	}
	for _, r := range actor.Chain() {
		for _, rule := range t.Permissions.Rows {
			if rule.Role == r {
				return compileCond(t, "permissions", rule.Filter)
			}
		}
	}
	return nil, nil
}

// rowEnv gives formulas a row's values and the actor.
type rowEnv struct {
	t     *Table
	row   map[string]any
	actor rbac.Actor
	now   time.Time
}

func (e *rowEnv) Now() time.Time { return e.now }

func (e *rowEnv) Field(name string) any {
	c := e.t.column(name)
	if c == nil {
		return nil
	}
	return e.row[c.Name]
}

func (e *rowEnv) Name(parts []string) any {
	switch strings.ToLower(parts[len(parts)-1]) {
	case "id":
		return e.actor.UserID
	case "email":
		return e.actor.Email
	case "name":
		return e.actor.Name
	case "role":
		return e.actor.Role.String()
	case "groups":
		out := make([]any, len(e.actor.Groups))
		for i, g := range e.actor.Groups {
			out[i] = g
		}
		return out
	case "roles":
		chain := e.actor.Chain()
		out := make([]any, len(chain))
		for i, r := range chain {
			out[i] = string(r)
		}
		return out
	}
	return nil
}

// CheckFormula type checks a filter or row rule on the table's fields, for
// the formula editor.
func (s *Service) CheckFormula(ctx context.Context, appID, id, src string) (formula.Type, []*formula.Error, error) {
	t, err := s.Get(ctx, appID, id)
	if err != nil {
		return formula.Any, nil, err
	}
	ch, errs := formula.Compile(src, scope{t})
	if errs != nil {
		return formula.Any, errs, nil
	}
	return ch.Type, nil, nil
}
