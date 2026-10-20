package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/ids"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
)

// Service manages tables and their rows.
type Service struct {
	store *store.Store
	now   func() time.Time

	// mu serialises schema changes, which touch two databases.
	mu sync.Mutex

	// OnRow, when set, hears every row created, changed or deleted through
	// CreateRow, UpdateRow and DeleteRows, after the change is committed.
	// Flows use it to start their row triggers. CSV imports do not call it.
	OnRow func(ctx context.Context, ev RowEvent)
}

// RowEvent is a committed row change.
type RowEvent struct {
	AppID   string
	TableID string
	// Kind is "created", "updated" or "deleted".
	Kind string
	// Row is the row after the change; for "deleted", the row as it was.
	Row map[string]any
	// Old is the row before an update.
	Old map[string]any
	// Actor made the change.
	Actor rbac.Actor
}

func NewService(s *store.Store) *Service {
	return &Service{store: s, now: time.Now}
}

// compiled is a table with its formulas checked.
type compiled struct {
	*Table
	formulas map[string]*formula.Checked // by field id
	// targets are the tables link fields point to, by link field id,
	// compiled without following their own links: formulas read linked
	// rows one link deep.
	targets map[string]*compiled
	// broken formulas no longer compile, because a linked table changed
	// under them; they give blanks until fixed.
	broken map[string]bool
}

// scope is where formulas of the table are checked; names says whether
// Current.User is available.
func (c *compiled) scope(names bool) tableScope {
	return tableScope{t: c.Table, names: names, target: c.targetTable}
}

func (c *compiled) targetTable(f *Field) *Table {
	if tc := c.targets[f.ID]; tc != nil {
		return tc.Table
	}
	return nil
}

// tableScope resolves names inside a table's formulas.
type tableScope struct {
	t        *Table
	names    bool // Current.User is available
	resolve  func(f *Field) (formula.Type, bool)
	selfName string
	// target is the table a link field points to; nil where formulas may
	// not read linked rows.
	target func(f *Field) *Table
}

func (s tableScope) Link(name string) (formula.Scope, bool, bool) {
	f := s.t.FieldByName(name)
	if f == nil || f.Type != TypeLink || s.target == nil {
		return nil, false, false
	}
	t := s.target(f)
	if t == nil {
		return nil, false, false
	}
	return tableScope{t: t, names: s.names}, f.LinkMany, true
}

// lookup loads the tables of an app by id, for the links of a table.
func (s *Service) lookup(ctx context.Context, appID string) func(id string) (*Table, error) {
	return func(id string) (*Table, error) { return s.load(ctx, appID, id) }
}

func (s tableScope) FieldType(name string) (formula.Type, bool) {
	f := s.t.FieldByName(name)
	if f == nil {
		return formula.Any, false
	}
	if f.Type == TypeFormula && s.resolve != nil {
		return s.resolve(f)
	}
	return formulaType(f), true
}

func (s tableScope) NameType(parts []string) (formula.Type, bool) {
	if !s.names || len(parts) != 3 || !strings.EqualFold(parts[0], "Current") || !strings.EqualFold(parts[1], "User") {
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

// checkPermissions checks a table's permissions: every role is set and
// every row rule is a valid condition, at most one per role.
func checkPermissions(c *compiled, p *Permissions) error {
	scope := c.scope(true)
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
		if _, err := compileRule(scope, rule.Filter); err != nil {
			return err
		}
	}
	return nil
}

// compileRule checks a row rule's filter; an empty filter gives nil.
func compileRule(scope tableScope, filter string) (*formula.Checked, error) {
	if strings.TrimSpace(filter) == "" {
		return nil, nil
	}
	ch, errs := formula.Compile(filter, scope)
	if errs != nil {
		return nil, &ValidationError{Field: "permissions", Message: "row rule: " + errs[0].Message, Formula: errs}
	}
	if ch.Type != formula.Bool && ch.Type != formula.Any && ch.Type != formula.Null {
		return nil, &ValidationError{Field: "permissions", Message: "a row rule must be true or false, this one gives " + ch.Type.String()}
	}
	return ch, nil
}

// RowRuleFor returns the row rule that applies to an actor, or "" when
// every row is open to them.
func (t *Table) RowRuleFor(actor rbac.Actor) string {
	if actor.Builder || actor.Role == rbac.Admin {
		return ""
	}
	for _, r := range actor.Chain() {
		for _, rule := range t.Permissions.Rows {
			if rule.Role == r {
				return rule.Filter
			}
		}
	}
	return ""
}

// compile checks every formula field of t, in dependency order, and sets
// their result types. It reports the first field whose formula is invalid.
// lookup loads the tables that link fields point to.
func compile(t *Table, lookup func(id string) (*Table, error)) (*compiled, error) {
	return build(t, lookup, false)
}

// compileLenient is compile for reading: formulas broken by a change to a
// linked table give blanks instead of making the table unusable.
func compileLenient(t *Table, lookup func(id string) (*Table, error)) *compiled {
	c, _ := build(t, lookup, true)
	return c
}

var blankFormula, _ = formula.Compile("NULL", tableScope{t: &Table{}})

func build(t *Table, lookup func(id string) (*Table, error), lenient bool) (*compiled, error) {
	c := &compiled{Table: t, formulas: map[string]*formula.Checked{}, targets: map[string]*compiled{}, broken: map[string]bool{}}
	if lookup != nil {
		for _, f := range t.Fields {
			if f.Type != TypeLink {
				continue
			}
			target := t
			if f.LinkTable != t.ID {
				var err error
				if target, err = lookup(f.LinkTable); err != nil {
					continue
				}
			}
			c.targets[f.ID], _ = build(target, nil, true)
		}
	}
	visiting := map[string]bool{}
	var errField *Field
	var errs []*formula.Error
	var resolve func(f *Field) (formula.Type, bool)
	resolve = func(f *Field) (formula.Type, bool) {
		if ch, ok := c.formulas[f.ID]; ok {
			return ch.Type, true
		}
		if visiting[f.ID] {
			if errField == nil {
				errField = f
				errs = []*formula.Error{{Start: 0, End: len(f.Formula), Message: "this formula refers back to itself through [" + f.Name + "]"}}
			}
			return formula.Any, false
		}
		visiting[f.ID] = true
		defer delete(visiting, f.ID)
		scope := c.scope(f.FormulaMode != Static)
		scope.resolve = resolve
		ch, e := formula.Compile(f.Formula, scope)
		if e == nil && f.FormulaMode == Static {
			if ch.Type == formula.List {
				e = []*formula.Error{{Start: 0, End: len(f.Formula), Message: "a stored formula cannot return a list"}}
			} else if ch.UsesLinks() {
				e = []*formula.Error{{Start: 0, End: len(f.Formula), Message: errStaticLinks}}
			}
		}
		if e != nil {
			if lenient {
				c.formulas[f.ID] = blankFormula
				c.broken[f.ID] = true
				return f.ResultType, true
			}
			if errField == nil {
				errField, errs = f, e
			}
			return formula.Any, false
		}
		c.formulas[f.ID] = ch
		f.ResultType = ch.Type
		return ch.Type, true
	}
	for _, f := range t.Fields {
		if f.Type == TypeFormula {
			resolve(f)
			if errField != nil {
				return nil, &ValidationError{Field: errField.Name, Message: errs[0].Message, Formula: errs}
			}
		}
	}
	return c, nil
}

// CheckFormula type checks a formula as if it were a field of the table, or
// a filter when asFilter is set. It is used by the formula editor.
func (s *Service) CheckFormula(ctx context.Context, appID, tableID, src string, static bool) (formula.Type, []*formula.Error, error) {
	t, err := s.load(ctx, appID, tableID)
	if err != nil {
		return formula.Any, nil, err
	}
	c, err := compile(t, s.lookup(ctx, appID))
	if err != nil {
		return formula.Any, nil, err
	}
	ch, errs := formula.Compile(src, c.scope(!static))
	if errs != nil {
		return formula.Any, errs, nil
	}
	if static && ch.UsesLinks() {
		return formula.Any, []*formula.Error{{Start: 0, End: len(src), Message: errStaticLinks}}, nil
	}
	return ch.Type, nil, nil
}

func (s *Service) load(ctx context.Context, appID, tableID string) (*Table, error) {
	var def string
	err := s.store.Meta.QueryRow(ctx, `SELECT definition FROM app_tables WHERE app_id = ? AND id = ?`, appID, tableID).Scan(&def)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("table %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	t := &Table{}
	if err := json.Unmarshal([]byte(def), t); err != nil {
		return nil, err
	}
	t.Permissions.normalize()
	return t, nil
}

const errStaticLinks = "a stored formula cannot read linked rows, which change without it; make it dynamic"

func (s *Service) loadCompiled(ctx context.Context, appID, tableID string) (*compiled, error) {
	t, err := s.load(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	return compileLenient(t, s.lookup(ctx, appID)), nil
}

// List returns the tables of an app, by name.
func (s *Service) List(ctx context.Context, appID string) ([]*Table, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT definition FROM app_tables WHERE app_id = ? ORDER BY name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Table{}
	for rows.Next() {
		var def string
		if err := rows.Scan(&def); err != nil {
			return nil, err
		}
		t := &Table{}
		if err := json.Unmarshal([]byte(def), t); err != nil {
			return nil, err
		}
		t.Permissions.normalize()
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get returns one table.
func (s *Service) Get(ctx context.Context, appID, tableID string) (*Table, error) {
	return s.load(ctx, appID, tableID)
}

func (s *Service) saveMeta(ctx context.Context, t *Table, create bool) error {
	def, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if create {
		_, err = s.store.Meta.Exec(ctx, `INSERT INTO app_tables (id, app_id, name, definition, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			t.ID, t.AppID, t.Name, string(def), t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339))
	} else {
		_, err = s.store.Meta.Exec(ctx, `UPDATE app_tables SET name = ?, definition = ?, updated_at = ? WHERE id = ?`,
			t.Name, string(def), t.UpdatedAt.Format(time.RFC3339), t.ID)
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return invalid("name", "a table named %q already exists", t.Name)
	}
	return err
}

// change runs a schema change: fn alters the data database inside a
// transaction and returns the new definition, which is saved before the
// transaction commits.
func (s *Service) change(ctx context.Context, appID string, create bool, fn func(tx *store.Tx) (*Table, error)) (*Table, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t, err := fn(tx)
	if err != nil {
		return nil, err
	}
	if !create {
		if err := s.checkDependents(ctx, t); err != nil {
			return nil, err
		}
	}
	t.UpdatedAt = s.now().UTC()
	if err := s.saveMeta(ctx, t, create); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return t, nil
}

// FieldInput describes a new field.
type FieldInput struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required"`
	Options     []string  `json:"options"`
	LinkTable   string    `json:"linkTable"`
	LinkMany    bool      `json:"linkMany"`
	Formula     string    `json:"formula"`
	FormulaMode string    `json:"formulaMode"`
}

// CreateTable creates a table with the given fields. The first text field
// becomes the row label.
func (s *Service) CreateTable(ctx context.Context, appID, name string, fields []FieldInput) (*Table, error) {
	name = strings.TrimSpace(name)
	if err := validName("table", name); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	t := &Table{
		ID: ids.New("tbl"), AppID: appID, Name: name, Fields: []*Field{},
		Permissions: Permissions{Read: rbac.Basic, Write: rbac.Basic, Delete: rbac.Basic, Rows: []RowRule{}},
		CreatedAt:   now, UpdatedAt: now,
	}
	return s.change(ctx, appID, true, func(tx *store.Tx) (*Table, error) {
		db := tx.DB()
		ddl := fmt.Sprintf(`CREATE TABLE %s (_id %s, _created_at TEXT NOT NULL, _updated_at TEXT NOT NULL)`, db.Table(t.sqlName()), db.TypeSerialPK())
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return nil, err
		}
		for _, in := range fields {
			if _, err := s.addField(ctx, tx, t, in); err != nil {
				return nil, err
			}
		}
		return t, nil
	})
}

// TablePatch changes table settings. Nil fields are left alone.
type TablePatch struct {
	Name         *string      `json:"name"`
	PrimaryField *string      `json:"primaryField"`
	Permissions  *Permissions `json:"permissions"`
}

func (s *Service) UpdateTable(ctx context.Context, appID, tableID string, p TablePatch) (*Table, error) {
	return s.change(ctx, appID, false, func(tx *store.Tx) (*Table, error) {
		t, err := s.load(ctx, appID, tableID)
		if err != nil {
			return nil, err
		}
		if p.Name != nil {
			if err := validName("table", *p.Name); err != nil {
				return nil, err
			}
			t.Name = strings.TrimSpace(*p.Name)
		}
		if p.PrimaryField != nil {
			f := t.FieldByID(*p.PrimaryField)
			if f == nil || f.Type == TypeLink || f.Type == TypeMultiSelect {
				return nil, invalid("primaryField", "the row label must be a text, number, date or formula field")
			}
			t.PrimaryField = f.ID
		}
		if p.Permissions != nil {
			perm := *p.Permissions
			perm.normalize()
			c, err := compile(t, s.lookup(ctx, appID))
			if err != nil {
				return nil, err
			}
			if err := checkPermissions(c, &perm); err != nil {
				return nil, err
			}
			t.Permissions = perm
		}
		return t, nil
	})
}

// DeleteTable drops a table and its data. Tables that other tables link to
// cannot be deleted until those links are removed.
func (s *Service) DeleteTable(ctx context.Context, appID, tableID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.load(ctx, appID, tableID)
	if err != nil {
		return err
	}
	all, err := s.List(ctx, appID)
	if err != nil {
		return err
	}
	for _, o := range all {
		if o.ID == t.ID {
			continue
		}
		for _, f := range o.Fields {
			if f.Type == TypeLink && f.LinkTable == t.ID {
				return invalid("", "the field [%s] in table %s links to this table; delete it first", f.Name, o.Name)
			}
		}
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range t.Fields {
		if f.Type == TypeLink && f.LinkMany {
			if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS `+db.Table(joinName(f))); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS `+db.Table(t.sqlName())); err != nil {
		return err
	}
	if _, err := s.store.Meta.Exec(ctx, `DELETE FROM app_tables WHERE id = ?`, t.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func columnType(db *store.DB, f *Field) string {
	switch f.Type {
	case TypeNumber:
		return db.TypeReal()
	case TypeBoolean, TypeAutoNumber, TypeLink:
		return db.TypeInt()
	case TypeFormula:
		switch f.ResultType {
		case formula.Number:
			return db.TypeReal()
		case formula.Bool:
			return db.TypeInt()
		}
	}
	return db.TypeText()
}

func newColumn() string { return "c_" + strings.TrimPrefix(ids.New("c"), "c_") }

// buildField validates input against table t and returns the new field.
func (s *Service) buildField(ctx context.Context, t *Table, in FieldInput) (*Field, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := validName("field", in.Name); err != nil {
		return nil, err
	}
	if t.FieldByName(in.Name) != nil {
		return nil, invalid("name", "a field named %q already exists", in.Name)
	}
	if !slices.Contains(allTypes, in.Type) {
		return nil, invalid("type", "unknown field type %q", in.Type)
	}
	f := &Field{ID: ids.New("fld"), Name: in.Name, Type: in.Type, Required: in.Required, Column: newColumn()}
	switch in.Type {
	case TypeSingleSelect, TypeMultiSelect:
		opts, err := cleanOptions(in.Options)
		if err != nil {
			return nil, err
		}
		f.Options = opts
	case TypeLink:
		if in.LinkTable == "" {
			return nil, invalid("linkTable", "choose the table to link to")
		}
		if in.LinkTable != t.ID {
			if _, err := s.load(ctx, t.AppID, in.LinkTable); err != nil {
				return nil, invalid("linkTable", "the linked table does not exist")
			}
		}
		f.LinkTable, f.LinkMany = in.LinkTable, in.LinkMany
	case TypeFormula:
		f.Formula = strings.TrimSpace(in.Formula)
		f.FormulaMode = in.FormulaMode
		if f.FormulaMode == "" {
			f.FormulaMode = Dynamic
		}
		if f.FormulaMode != Dynamic && f.FormulaMode != Static {
			return nil, invalid("formulaMode", "use dynamic or static")
		}
		f.Required = false
	case TypeAutoNumber:
		f.Required = false
	}
	return f, nil
}

func cleanOptions(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, o := range in {
		o = strings.TrimSpace(o)
		if o == "" || seen[o] {
			continue
		}
		seen[o] = true
		out = append(out, o)
	}
	return out, nil
}

// AddField adds a field to a table.
func (s *Service) AddField(ctx context.Context, appID, tableID string, in FieldInput) (*Table, *Field, error) {
	var added *Field
	t, err := s.change(ctx, appID, false, func(tx *store.Tx) (*Table, error) {
		t, err := s.load(ctx, appID, tableID)
		if err != nil {
			return nil, err
		}
		added, err = s.addField(ctx, tx, t, in)
		return t, err
	})
	return t, added, err
}

func (s *Service) addField(ctx context.Context, tx *store.Tx, t *Table, in FieldInput) (*Field, error) {
	f, err := s.buildField(ctx, t, in)
	if err != nil {
		return nil, err
	}
	t.Fields = append(t.Fields, f)
	c, err := compile(t, s.lookup(ctx, t.AppID))
	if err != nil {
		return nil, err
	}
	if err := createStorage(ctx, tx, t, f); err != nil {
		return nil, err
	}
	if t.PrimaryField == "" && (f.Type == TypeText || f.Type == TypeAutoNumber) {
		t.PrimaryField = f.ID
	}
	if f.Required {
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM `+tx.DB().Table(t.sqlName())).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, invalid("required", "a new field cannot be required while the table has rows; add it, fill it, then make it required")
		}
	}
	switch {
	case f.Type == TypeAutoNumber:
		tbl := tx.DB().Table(t.sqlName())
		col := store.Q(f.Column)
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s = (SELECT COUNT(*) FROM %s t2 WHERE t2._id <= %s._id)`, tbl, col, tbl, tbl)); err != nil {
			return nil, err
		}
	case f.Type == TypeFormula && f.FormulaMode == Static:
		if err := s.recompute(ctx, tx, c, nil); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func createStorage(ctx context.Context, tx *store.Tx, t *Table, f *Field) error {
	db := tx.DB()
	if f.Type == TypeLink && f.LinkMany {
		_, err := tx.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (src %s NOT NULL, dst %s NOT NULL, PRIMARY KEY (src, dst))`,
			db.Table(joinName(f)), db.TypeInt(), db.TypeInt()))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, fmt.Sprintf(`CREATE INDEX %s ON %s (dst)`, store.Q("i_"+joinName(f)), db.Table(joinName(f))))
		return err
	}
	if !f.stored() {
		return nil
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, db.Table(t.sqlName()), store.Q(f.Column), columnType(db, f)))
	return err
}

func dropStorage(ctx context.Context, tx *store.Tx, t *Table, f *Field) error {
	db := tx.DB()
	if f.Type == TypeLink && f.LinkMany {
		_, err := tx.Exec(ctx, `DROP TABLE IF EXISTS `+db.Table(joinName(f)))
		return err
	}
	if !f.stored() {
		return nil
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s DROP COLUMN %s`, db.Table(t.sqlName()), store.Q(f.Column)))
	return err
}

// FieldPatch changes a field. Nil members are left alone.
type FieldPatch struct {
	Name        *string    `json:"name"`
	Type        *FieldType `json:"type"`
	Required    *bool      `json:"required"`
	Options     []string   `json:"options"`
	Formula     *string    `json:"formula"`
	FormulaMode *string    `json:"formulaMode"`
	// Force converts values that do not fit a new type to blank instead of
	// failing.
	Force bool `json:"force"`
}

// UpdateField renames, retypes or reconfigures a field. Renaming rewrites
// formulas that refer to the field.
func (s *Service) UpdateField(ctx context.Context, appID, tableID, fieldID string, p FieldPatch) (*Table, error) {
	return s.change(ctx, appID, false, func(tx *store.Tx) (*Table, error) {
		t, err := s.load(ctx, appID, tableID)
		if err != nil {
			return nil, err
		}
		f := t.FieldByID(fieldID)
		if f == nil {
			return nil, fmt.Errorf("field %w", ErrNotFound)
		}
		before := *f
		prev := map[string]Field{}
		for _, o := range t.Fields {
			prev[o.ID] = *o
		}
		if p.Name != nil && *p.Name != f.Name {
			name := strings.TrimSpace(*p.Name)
			if err := validName("field", name); err != nil {
				return nil, err
			}
			if o := t.FieldByName(name); o != nil && o != f {
				return nil, invalid("name", "a field named %q already exists", name)
			}
			for _, o := range t.Fields {
				if o.Type == TypeFormula {
					o.Formula = formula.RenameField(o.Formula, f.Name, name)
				}
			}
			for i := range t.Permissions.Rows {
				t.Permissions.Rows[i].Filter = formula.RenameField(t.Permissions.Rows[i].Filter, f.Name, name)
			}
			if err := s.renameLinked(ctx, t, f.Name, name); err != nil {
				return nil, err
			}
			f.Name = name
		}
		if p.Options != nil {
			if f.Type != TypeSingleSelect && f.Type != TypeMultiSelect {
				return nil, invalid("options", "only select fields have options")
			}
			f.Options, _ = cleanOptions(p.Options)
		}
		if p.Formula != nil {
			if f.Type != TypeFormula {
				return nil, invalid("formula", "only formula fields have a formula")
			}
			f.Formula = strings.TrimSpace(*p.Formula)
		}
		if p.FormulaMode != nil {
			if f.Type != TypeFormula || (*p.FormulaMode != Dynamic && *p.FormulaMode != Static) {
				return nil, invalid("formulaMode", "use dynamic or static on a formula field")
			}
			f.FormulaMode = *p.FormulaMode
		}
		retype := p.Type != nil && *p.Type != f.Type
		if retype {
			if !f.Type.scalar() || !p.Type.scalar() {
				return nil, invalid("type", "a %s field cannot be changed to %s; add a new field instead", f.Type, *p.Type)
			}
			f.Type = *p.Type
		}
		c, err := compile(t, s.lookup(ctx, appID))
		if err != nil {
			return nil, err
		}
		if p.Required != nil && *p.Required && !f.Required {
			if !f.writable() {
				return nil, invalid("required", "computed fields cannot be required")
			}
			var n int
			q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s IS NULL`, tx.DB().Table(t.sqlName()), store.Q(f.Column))
			if f.Type == TypeLink && f.LinkMany {
				q = fmt.Sprintf(`SELECT COUNT(*) FROM %s r WHERE NOT EXISTS (SELECT 1 FROM %s j WHERE j.src = r._id)`, tx.DB().Table(t.sqlName()), tx.DB().Table(joinName(f)))
			}
			if err := tx.QueryRow(ctx, q).Scan(&n); err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, invalid("required", "%d row(s) have no value for [%s]; fill them first", n, f.Name)
			}
		}
		if p.Required != nil {
			f.Required = *p.Required && f.writable()
		}
		if retype {
			if err := s.convert(ctx, tx, t, &before, f, p.Force); err != nil {
				return nil, err
			}
		}
		// Static formulas: rebuild a column when its mode or result type
		// changed, here or in a formula it depends on.
		for _, o := range t.Fields {
			p, existed := prev[o.ID]
			wasStored := existed && p.Type == TypeFormula && p.FormulaMode == Static
			if o.Type != TypeFormula && !wasStored {
				continue
			}
			if wasStored && (!o.stored() || p.ResultType != o.ResultType) {
				if err := dropStorage(ctx, tx, t, &p); err != nil {
					return nil, err
				}
				wasStored = false
				o.Column = newColumn()
			}
			if o.stored() && !wasStored {
				if err := createStorage(ctx, tx, t, o); err != nil {
					return nil, err
				}
			}
		}
		// Any change can alter what stored formulas compute.
		if err := s.recompute(ctx, tx, c, nil); err != nil {
			return nil, err
		}
		return t, nil
	})
}

// convert moves the values of a field into a new column of its new type.
func (s *Service) convert(ctx context.Context, tx *store.Tx, t *Table, before, f *Field, force bool) error {
	db := tx.DB()
	tbl := db.Table(t.sqlName())
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT _id, %s FROM %s`, store.Q(before.Column), tbl))
	if err != nil {
		return err
	}
	type pair struct {
		id  int64
		val any
	}
	var vals []pair
	var bad int
	newOptions := append([]string{}, f.Options...)
	for rows.Next() {
		var id int64
		var raw any
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		v := decode(before, raw)
		if f.Type == TypeSingleSelect && v != nil {
			txt := formula.ToText(v)
			if b, ok := v.(bool); ok {
				txt = map[bool]string{true: "Yes", false: "No"}[b]
			}
			if !slices.Contains(newOptions, txt) {
				newOptions = append(newOptions, txt)
			}
			v = txt
		}
		enc, err := encode(f, v)
		if err != nil {
			bad++
			enc = nil
		}
		vals = append(vals, pair{id, enc})
	}
	rows.Close()
	if bad > 0 && !force {
		return invalid("type", "%d value(s) cannot be converted to %s; retry with force to blank them", bad, f.Type)
	}
	if f.Type == TypeSingleSelect {
		f.Options = newOptions
	}
	f.Column = newColumn()
	if err := createStorage(ctx, tx, t, f); err != nil {
		return err
	}
	for _, p := range vals {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s = ? WHERE _id = ?`, tbl, store.Q(f.Column)), p.val, p.id); err != nil {
			return err
		}
	}
	return dropStorage(ctx, tx, t, before)
}

// DeleteField removes a field and its data. Fields used by formulas cannot
// be deleted.
func (s *Service) DeleteField(ctx context.Context, appID, tableID, fieldID string) (*Table, error) {
	return s.change(ctx, appID, false, func(tx *store.Tx) (*Table, error) {
		t, err := s.load(ctx, appID, tableID)
		if err != nil {
			return nil, err
		}
		f := t.FieldByID(fieldID)
		if f == nil {
			return nil, fmt.Errorf("field %w", ErrNotFound)
		}
		for _, o := range t.Fields {
			if o.Type != TypeFormula || o == f {
				continue
			}
			if n, err := formula.Parse(o.Formula); err == nil {
				for _, ref := range formula.FieldRefs(n) {
					if strings.EqualFold(ref, f.Name) {
						return nil, invalid("", "the formula field [%s] uses [%s]; change it first", o.Name, f.Name)
					}
				}
			}
		}
		if err := dropStorage(ctx, tx, t, f); err != nil {
			return nil, err
		}
		t.Fields = slices.DeleteFunc(t.Fields, func(x *Field) bool { return x == f })
		if t.PrimaryField == f.ID {
			t.PrimaryField = ""
			for _, o := range t.Fields {
				if o.Type == TypeText {
					t.PrimaryField = o.ID
					break
				}
			}
		}
		return t, nil
	})
}

// CheckFields reports whether fields could be added to a table: to base,
// or to a new table named name when base is nil. Link fields name their
// table by id; ids listed in drafted stand for tables not made yet. It
// changes nothing.
func (s *Service) CheckFields(ctx context.Context, appID string, base *Table, name string, fields []FieldInput, drafted map[string]bool) error {
	t := &Table{ID: "tbl_draft", AppID: appID, Name: name, Fields: []*Field{}}
	if base != nil {
		cp := *base
		cp.Fields = slices.Clone(base.Fields)
		t = &cp
	} else if err := validName("table", name); err != nil {
		return err
	}
	for _, in := range fields {
		if in.Type == TypeLink && drafted[in.LinkTable] {
			in.LinkTable = t.ID // stands in for the table to come
		}
		f, err := s.buildField(ctx, t, in)
		if err != nil {
			var v *ValidationError
			if errors.As(err, &v) {
				v.Message = fmt.Sprintf("field %q: %s", in.Name, v.Message)
			}
			return err
		}
		t.Fields = append(t.Fields, f)
	}
	lookup := s.lookup(ctx, appID)
	if _, err := compile(t, func(id string) (*Table, error) {
		if id == t.ID {
			return t, nil
		}
		return lookup(id)
	}); err != nil {
		return err
	}
	return nil
}
