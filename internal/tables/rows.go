package tables

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
)

// LinkRef is a linked row as rows show it.
type LinkRef struct {
	ID    int64  `json:"_id"`
	Label string `json:"label"`
	// Hidden marks a linked row the reader may not see under the linked
	// table's permissions; its label is left blank.
	Hidden bool `json:"hidden,omitempty"`
}

// Row is a row with every stored and linked value loaded. Values are keyed
// by field id: nil, float64, string, bool, time.Time, []any (multi
// select), LinkRef or []LinkRef.
type Row struct {
	ID      int64
	Created time.Time
	Updated time.Time
	Values  map[string]any
}

// decode turns a scanned SQL value into the field's Go value.
func decode(f *Field, raw any) any {
	if b, ok := raw.([]byte); ok {
		raw = string(b)
	}
	if raw == nil {
		return nil
	}
	typ := f.Type
	if typ == TypeFormula {
		switch f.ResultType {
		case formula.Number:
			typ = TypeNumber
		case formula.Bool:
			typ = TypeBoolean
		case formula.Date:
			typ = TypeDate
		default:
			typ = TypeText
		}
	}
	switch typ {
	case TypeNumber, TypeAutoNumber:
		switch v := raw.(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil
			}
			return f
		}
	case TypeBoolean:
		switch v := raw.(type) {
		case int64:
			return v != 0
		case bool:
			return v
		case float64:
			return v != 0
		}
	case TypeDate:
		if s, ok := raw.(string); ok {
			if t, err := formula.ParseDate(s); err == nil {
				return t
			}
		}
		return nil
	case TypeMultiSelect:
		var items []string
		if s, ok := raw.(string); ok && json.Unmarshal([]byte(s), &items) == nil {
			out := make([]any, len(items))
			for i, x := range items {
				out[i] = x
			}
			return out
		}
		return nil
	case TypeAttachment:
		if s, ok := raw.(string); ok {
			return decodeFiles(s)
		}
		return nil
	case TypeLink:
		switch v := raw.(type) {
		case int64:
			return LinkRef{ID: v}
		case float64:
			return LinkRef{ID: int64(v)}
		}
		return nil
	default:
		if s, ok := raw.(string); ok {
			return s
		}
		return fmt.Sprint(raw)
	}
	return nil
}

// encode converts an input value to what is stored in the field's column.
// It does not check select options or that linked rows exist.
func encode(f *Field, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if n, ok := v.(json.Number); ok {
		v = string(n)
	}
	switch f.Type {
	case TypeText, TypeLongText, TypeSingleSelect:
		switch x := v.(type) {
		case string:
			if f.Type != TypeLongText {
				x = strings.TrimSpace(x)
			}
			if x == "" {
				return nil, nil
			}
			return x, nil
		case float64, bool, time.Time:
			return formula.ToText(x), nil
		}
		return nil, fmt.Errorf("expected text")
	case TypeNumber:
		switch x := v.(type) {
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, fmt.Errorf("expected a finite number")
			}
			return x, nil
		case string:
			x = strings.TrimSpace(x)
			if x == "" {
				return nil, nil
			}
			n, err := strconv.ParseFloat(strings.ReplaceAll(x, ",", ""), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, fmt.Errorf("%q is not a number", x)
			}
			return n, nil
		}
		return nil, fmt.Errorf("expected a number")
	case TypeBoolean:
		switch x := v.(type) {
		case bool:
			if x {
				return int64(1), nil
			}
			return int64(0), nil
		case float64:
			if x == 0 || x == 1 {
				return int64(x), nil
			}
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "":
				return nil, nil
			case "true", "yes", "1", "y":
				return int64(1), nil
			case "false", "no", "0", "n":
				return int64(0), nil
			}
		}
		return nil, fmt.Errorf("expected true or false")
	case TypeDate:
		switch x := v.(type) {
		case time.Time:
			return x.UTC().Truncate(time.Second).Format(formula.DateLayout), nil
		case string:
			if strings.TrimSpace(x) == "" {
				return nil, nil
			}
			t, err := formula.ParseDate(x)
			if err != nil {
				return nil, err
			}
			return t.Format(formula.DateLayout), nil
		}
		return nil, fmt.Errorf("expected a date such as 2026-10-18")
	case TypeMultiSelect:
		var items []string
		switch x := v.(type) {
		case []any:
			for _, i := range x {
				s, ok := i.(string)
				if !ok {
					return nil, fmt.Errorf("expected a list of text")
				}
				items = append(items, s)
			}
		case []string:
			items = x
		case string:
			for _, s := range strings.Split(x, ",") {
				items = append(items, s)
			}
		default:
			return nil, fmt.Errorf("expected a list of options")
		}
		clean, _ := cleanOptions(items)
		if len(clean) == 0 {
			return nil, nil
		}
		b, _ := json.Marshal(clean)
		return string(b), nil
	case TypeLink:
		return linkID(v)
	case TypeAttachment:
		return encodeFiles(v)
	}
	return nil, fmt.Errorf("field cannot be set")
}

func linkID(v any) (any, error) {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && x > 0 {
			return int64(x), nil
		}
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case string:
		if strings.TrimSpace(x) == "" {
			return nil, nil
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil && n > 0 {
			return n, nil
		}
	case map[string]any:
		return linkID(x["_id"])
	case LinkRef:
		return x.ID, nil
	}
	return nil, fmt.Errorf("expected the _id of a linked row")
}

// linkIDs reads the value of a many-link field.
func linkIDs(v any) ([]int64, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		list = []any{v}
	}
	var out []int64
	for _, x := range list {
		id, err := linkID(x)
		if err != nil {
			return nil, err
		}
		if id != nil && !slices.Contains(out, id.(int64)) {
			out = append(out, id.(int64))
		}
	}
	return out, nil
}

// selectRows loads rows of c matching where, without link labels.
func selectRows(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, where string, args []any, tail string) ([]*Row, error) {
	cols := []string{"_id", "_created_at", "_updated_at"}
	var stored []*Field
	for _, f := range c.Fields {
		if f.stored() {
			cols = append(cols, store.Q(f.Column))
			stored = append(stored, f)
		}
	}
	q := "SELECT " + strings.Join(cols, ", ") + " FROM " + db.Table(c.sqlName())
	if where != "" {
		q += " WHERE " + where
	}
	q += " " + tail
	rs, err := ex.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []*Row
	for rs.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, err
		}
		r := &Row{Values: map[string]any{}}
		switch id := raw[0].(type) {
		case int64:
			r.ID = id
		case float64:
			r.ID = int64(id)
		}
		r.Created = parseTime(raw[1])
		r.Updated = parseTime(raw[2])
		for i, f := range stored {
			r.Values[f.ID] = decode(f, raw[i+3])
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func parseTime(v any) time.Time {
	if b, ok := v.([]byte); ok {
		v = string(b)
	}
	s, _ := v.(string)
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func inList(n int) string {
	return "(" + strings.TrimSuffix(strings.Repeat("?, ", n), ", ") + ")"
}

func chunks(ids []int64, size int) [][]int64 {
	var out [][]int64
	for len(ids) > size {
		out = append(out, ids[:size])
		ids = ids[size:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

func toArgs(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// hydrate fills link fields with their linked rows and labels. depth stops
// labels of linked rows from loading their own links forever.
func (s *Service) hydrate(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, rows []*Row, depth int) error {
	if len(rows) == 0 {
		return nil
	}
	byID := map[int64]*Row{}
	var ids []int64
	for _, r := range rows {
		byID[r.ID] = r
		ids = append(ids, r.ID)
	}
	for _, f := range c.Fields {
		if f.Type != TypeLink {
			continue
		}
		// Collect the linked ids.
		need := map[int64]bool{}
		if f.LinkMany {
			for _, r := range rows {
				r.Values[f.ID] = []LinkRef{}
			}
			for _, chunk := range chunks(ids, 500) {
				rs, err := ex.Query(ctx, "SELECT src, dst FROM "+db.Table(joinName(f))+" WHERE src IN "+inList(len(chunk))+" ORDER BY src, dst", toArgs(chunk)...)
				if err != nil {
					return err
				}
				for rs.Next() {
					var src, dst int64
					if err := rs.Scan(&src, &dst); err != nil {
						rs.Close()
						return err
					}
					r := byID[src]
					r.Values[f.ID] = append(r.Values[f.ID].([]LinkRef), LinkRef{ID: dst})
					need[dst] = true
				}
				rs.Close()
			}
		} else {
			for _, r := range rows {
				if l, ok := r.Values[f.ID].(LinkRef); ok {
					need[l.ID] = true
				}
			}
		}
		if depth > 0 || len(need) == 0 {
			continue
		}
		labels, err := s.labels(ctx, ex, db, c.AppID, f.LinkTable, need, c)
		if err != nil {
			return err
		}
		for _, r := range rows {
			switch v := r.Values[f.ID].(type) {
			case LinkRef:
				if _, ok := labels[v.ID]; !ok {
					r.Values[f.ID] = nil // the linked row is gone
					continue
				}
				v.Label = labels[v.ID]
				r.Values[f.ID] = v
			case []LinkRef:
				out := v[:0]
				for _, l := range v {
					if lbl, ok := labels[l.ID]; ok {
						l.Label = lbl
						out = append(out, l)
					}
				}
				r.Values[f.ID] = out
			}
		}
	}
	return nil
}

// labels returns the label of each existing row of a table.
func (s *Service) labels(ctx context.Context, ex store.Execer, db *store.DB, appID, tableID string, need map[int64]bool, self *compiled) (map[int64]string, error) {
	target := self
	if tableID != self.ID {
		var err error
		if target, err = s.loadCompiled(ctx, appID, tableID); err != nil {
			return nil, err
		}
	}
	var ids []int64
	for id := range need {
		ids = append(ids, id)
	}
	out := map[int64]string{}
	for _, chunk := range chunks(ids, 500) {
		rows, err := selectRows(ctx, ex, db, target, "_id IN "+inList(len(chunk)), toArgs(chunk), "")
		if err != nil {
			return nil, err
		}
		if err := s.hydrate(ctx, ex, db, target, rows, 1); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ID] = s.label(target, r)
		}
	}
	return out, nil
}

func (s *Service) label(c *compiled, r *Row) string {
	p := c.primary()
	if p == nil {
		return "#" + strconv.FormatInt(r.ID, 10)
	}
	v := newRowEnv(c, r, rbac.Actor{}, s.now()).value(p)
	if txt := formula.ToText(v); txt != "" {
		return txt
	}
	return "#" + strconv.FormatInt(r.ID, 10)
}

// maskLinks blanks the labels of linked rows that the actor may not read:
// all of them when the actor lacks read access to the linked table,
// otherwise those outside the actor's row rule there. Rows are hydrated
// without an actor so that formulas and row rules see every label; this
// runs last, just before rows are shown.
func (s *Service) maskLinks(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, rows []*Row, actor rbac.Actor) error {
	if len(rows) == 0 || actor.Builder || actor.Role == rbac.Admin {
		return nil
	}
	for _, f := range c.Fields {
		if f.Type != TypeLink {
			continue
		}
		need := map[int64]bool{}
		for _, r := range rows {
			switch v := r.Values[f.ID].(type) {
			case LinkRef:
				need[v.ID] = true
			case []LinkRef:
				for _, l := range v {
					need[l.ID] = true
				}
			}
		}
		if len(need) == 0 {
			continue
		}
		target := c
		if f.LinkTable != c.ID {
			var err error
			if target, err = s.loadCompiled(ctx, c.AppID, f.LinkTable); err != nil {
				return err
			}
		}
		var hidden func(id int64) bool
		if !actor.Has(target.Permissions.Read) {
			hidden = func(int64) bool { return true }
		} else {
			rule, err := target.rule(actor)
			if err != nil {
				return err
			}
			if rule == nil {
				continue
			}
			open := map[int64]bool{}
			var ids []int64
			for id := range need {
				ids = append(ids, id)
			}
			for _, chunk := range chunks(ids, 500) {
				linked, err := selectRows(ctx, ex, db, target, "_id IN "+inList(len(chunk)), toArgs(chunk), "")
				if err != nil {
					return err
				}
				if err := s.hydrate(ctx, ex, db, target, linked, 0); err != nil {
					return err
				}
				for _, lr := range linked {
					if s.allowed(target, rule, actor, lr) {
						open[lr.ID] = true
					}
				}
			}
			hidden = func(id int64) bool { return !open[id] }
		}
		for _, r := range rows {
			switch v := r.Values[f.ID].(type) {
			case LinkRef:
				if hidden(v.ID) {
					r.Values[f.ID] = LinkRef{ID: v.ID, Hidden: true}
				}
			case []LinkRef:
				out := make([]LinkRef, len(v))
				for i, l := range v {
					if hidden(l.ID) {
						l = LinkRef{ID: l.ID, Hidden: true}
					}
					out[i] = l
				}
				r.Values[f.ID] = out
			}
		}
	}
	return nil
}

// rowEnv evaluates formulas over one row.
type rowEnv struct {
	c     *compiled
	r     *Row
	actor rbac.Actor
	now   time.Time
	memo  map[string]any
}

func newRowEnv(c *compiled, r *Row, actor rbac.Actor, now time.Time) *rowEnv {
	return &rowEnv{c: c, r: r, actor: actor, now: now, memo: map[string]any{}}
}

func (e *rowEnv) Now() time.Time { return e.now }

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

// rule compiles the row rule that applies to the actor, or gives nil when
// every row is open to them.
func (c *compiled) rule(actor rbac.Actor) (*formula.Checked, error) {
	return compileRule(c.Table, c.RowRuleFor(actor))
}

// allowed reports whether the actor's row rule lets them at a loaded row.
func (s *Service) allowed(c *compiled, rule *formula.Checked, actor rbac.Actor, r *Row) bool {
	if rule == nil {
		return true
	}
	ok, err := rule.EvalBool(newRowEnv(c, r, actor, s.now()))
	return err == nil && ok
}

// loadRow reads one row with its links.
func (s *Service) loadRow(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, id int64) (*Row, error) {
	rows, err := selectRows(ctx, ex, db, c, "_id = ?", []any{id}, "")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("row %w", ErrNotFound)
	}
	if err := s.hydrate(ctx, ex, db, c, rows, 0); err != nil {
		return nil, err
	}
	return rows[0], nil
}

// ruleRow loads a row the actor's row rule must allow; other rows do not
// exist for them.
func (s *Service) ruleRow(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, rule *formula.Checked, actor rbac.Actor, id int64) (*Row, error) {
	r, err := s.loadRow(ctx, ex, db, c, id)
	if err != nil {
		return nil, err
	}
	if !s.allowed(c, rule, actor, r) {
		return nil, fmt.Errorf("row %w", ErrNotFound)
	}
	return r, nil
}

// errOutsideRule refuses writes that leave a row the actor may not see.
func errOutsideRule(c *compiled) error {
	return fmt.Errorf("%w: the row must match your row rule for %s", rbac.ErrForbidden, c.Name)
}

func (e *rowEnv) Field(name string) any {
	f := e.c.FieldByName(name)
	if f == nil {
		return nil
	}
	return e.value(f)
}

// value is the formula value of a field: links become their labels.
func (e *rowEnv) value(f *Field) any {
	if f.Type == TypeFormula && f.FormulaMode == Dynamic {
		if v, ok := e.memo[f.ID]; ok {
			return v
		}
		e.memo[f.ID] = nil // guards against cycles, which compile rejects
		v, err := e.c.formulas[f.ID].Eval(e)
		if err != nil {
			v = nil
		}
		e.memo[f.ID] = v
		return v
	}
	switch v := e.r.Values[f.ID].(type) {
	case LinkRef:
		return v.Label
	case []LinkRef:
		out := make([]any, len(v))
		for i, l := range v {
			out[i] = l.Label
		}
		return out
	case []FileRef:
		out := make([]any, len(v))
		for i, f := range v {
			out[i] = f.Name
		}
		return out
	default:
		return v
	}
}

// RowJSON is a row as the API shows it, keyed by field name.
func (s *Service) rowJSON(c *compiled, r *Row, actor rbac.Actor) map[string]any {
	out := map[string]any{
		"_id":        r.ID,
		"_createdAt": r.Created.UTC().Format(formula.DateLayout),
		"_updatedAt": r.Updated.UTC().Format(formula.DateLayout),
	}
	env := newRowEnv(c, r, actor, s.now())
	for _, f := range c.Fields {
		var v any
		if f.Type == TypeFormula && f.FormulaMode == Dynamic {
			v = env.value(f)
		} else {
			v = r.Values[f.ID]
		}
		if t, ok := v.(time.Time); ok {
			v = t.UTC().Format(formula.DateLayout)
		}
		if fl, ok := v.(float64); ok && (math.IsNaN(fl) || math.IsInf(fl, 0)) {
			v = nil
		}
		if files, ok := v.([]FileRef); ok {
			v = filesJSON(c.AppID, files)
		}
		out[f.Name] = v
	}
	return out
}

// recompute stores the static formulas of the given rows (all rows when
// ids is nil).
func (s *Service) recompute(ctx context.Context, tx *store.Tx, c *compiled, ids []int64) error {
	var statics []*Field
	for _, f := range c.Fields {
		if f.Type == TypeFormula && f.FormulaMode == Static {
			statics = append(statics, f)
		}
	}
	if len(statics) == 0 {
		return nil
	}
	db := tx.DB()
	sets := make([]string, len(statics))
	for i, f := range statics {
		sets[i] = store.Q(f.Column) + " = ?"
	}
	update := "UPDATE " + db.Table(c.sqlName()) + " SET " + strings.Join(sets, ", ") + " WHERE _id = ?"
	process := func(rows []*Row) error {
		if err := s.hydrate(ctx, tx, db, c, rows, 0); err != nil {
			return err
		}
		for _, r := range rows {
			env := newRowEnv(c, r, rbac.Actor{}, s.now())
			args := make([]any, 0, len(statics)+1)
			for _, f := range statics {
				v, err := c.formulas[f.ID].Eval(env)
				if err != nil {
					v = nil
				}
				// Earlier static formulas may feed later ones.
				r.Values[f.ID] = v
				enc, err := encodeResult(f, v)
				if err != nil {
					enc = nil
				}
				args = append(args, enc)
			}
			args = append(args, r.ID)
			if _, err := tx.Exec(ctx, update, args...); err != nil {
				return err
			}
		}
		return nil
	}
	if ids != nil {
		for _, chunk := range chunks(ids, 500) {
			rows, err := selectRows(ctx, tx, db, c, "_id IN "+inList(len(chunk)), toArgs(chunk), "")
			if err != nil {
				return err
			}
			if err := process(rows); err != nil {
				return err
			}
		}
		return nil
	}
	var last int64
	for {
		rows, err := selectRows(ctx, tx, db, c, "_id > ?", []any{last}, "ORDER BY _id LIMIT 500")
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if err := process(rows); err != nil {
			return err
		}
		last = rows[len(rows)-1].ID
	}
}

func encodeResult(f *Field, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch f.ResultType {
	case formula.Number:
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, nil
		}
		return n, nil
	case formula.Bool:
		if b, ok := v.(bool); ok && b {
			return int64(1), nil
		}
		return int64(0), nil
	case formula.Date:
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(formula.DateLayout), nil
		}
		return nil, nil
	}
	return formula.ToText(v), nil
}

// Query selects rows.
type Query struct {
	Filter string `json:"filter"` // a formula that is true for wanted rows
	Sort   string `json:"sort"`   // field name, _id, _createdAt or _updatedAt
	Desc   bool   `json:"desc"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// Page is one page of rows.
type Page struct {
	Rows  []map[string]any `json:"rows"`
	Total int              `json:"total"`
	// Engine says whether the filter and sort ran in SQL or in Go.
	Engine string `json:"engine"`
}

// MaxScan is the largest table that filters without an SQL form may scan.
const MaxScan = 100_000

type resolver struct {
	c       *compiled
	dialect string
	env     formula.Env
	depth   int
}

func (r resolver) FieldSQL(name string) (string, []any, bool) {
	f := r.c.FieldByName(name)
	if f == nil {
		return "", nil, false
	}
	switch {
	case f.Type == TypeFormula && f.FormulaMode == Dynamic:
		if r.depth > 8 || f.ResultType == formula.Bool {
			return "", nil, false
		}
		inner := r
		inner.depth++
		expr, args, ok := r.c.formulas[f.ID].CompileValueSQL(r.dialect, inner, r.env)
		return "(" + expr + ")", args, ok
	case f.Type == TypeLink || f.Type == TypeMultiSelect || f.Type == TypeAttachment:
		return "", nil, false
	}
	return store.Q(f.Column), nil, true
}

// ListRows returns a page of rows that match the query.
func (s *Service) ListRows(ctx context.Context, actor rbac.Actor, appID, tableID string, q Query) (*Page, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(c.Permissions.Read, "reading "+c.Name); err != nil {
		return nil, err
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 100
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	now := s.now()
	env := newRowEnv(c, &Row{Values: map[string]any{}}, actor, now)

	filters, err := s.filters(c, actor, q.Filter)
	if err != nil {
		return nil, err
	}

	var sortField *Field
	sortCol := "_id"
	sortSQL := true
	switch q.Sort {
	case "", "_id":
	case "_createdAt":
		sortCol = "_created_at"
	case "_updatedAt":
		sortCol = "_updated_at"
	default:
		sortField = c.FieldByName(q.Sort)
		if sortField == nil {
			return nil, invalid("sort", "there is no field named %q", q.Sort)
		}
		if sortField.stored() && sortField.Type != TypeMultiSelect && sortField.Type != TypeLink && sortField.Type != TypeAttachment {
			sortCol = store.Q(sortField.Column)
		} else {
			sortSQL = false
		}
	}
	var sortArgs []any
	if sortField != nil && sortField.Type == TypeFormula && sortField.FormulaMode == Dynamic {
		// Sort by the formula itself when it has an SQL form.
		if expr, args, ok := (resolver{c: c, dialect: string(db.Dialect), env: env}).FieldSQL(sortField.Name); ok {
			sortCol, sortArgs, sortSQL = expr, args, true
		}
	}

	where, args, pushed := pushdown(db, c, env, filters)

	if pushed && sortSQL {
		dir := " ASC"
		if q.Desc {
			dir = " DESC"
		}
		order := sortCol
		if db.Dialect == store.Postgres && sortField != nil && formulaType(sortField) == formula.Text {
			order += ` COLLATE "C"`
		}
		total, err := s.count(ctx, db, c, where, args)
		if err != nil {
			return nil, err
		}
		var rows []*Row
		switch {
		case sortField == nil:
			// _id and the timestamps are never blank.
			tail := fmt.Sprintf("ORDER BY %s%s, _id%s LIMIT %d OFFSET %d", sortCol, dir, dir, q.Limit, q.Offset)
			rows, err = selectRows(ctx, db, db, c, where, args, tail)
		case sortField.Indexed && sortField.stored():
			// Rows with a value first, in index order, then blank ones: two
			// queries an index can serve, where one "blanks last" ordering
			// would sort the whole table.
			rows, err = s.indexedPage(ctx, db, c, where, args, sortCol, order+dir, dir, q)
		default:
			tail := fmt.Sprintf("ORDER BY (%s IS NULL), %s%s, _id%s LIMIT %d OFFSET %d", sortCol, order, dir, dir, q.Limit, q.Offset)
			// The sort expression appears twice in the tail.
			all := append(append(append([]any{}, args...), sortArgs...), sortArgs...)
			rows, err = selectRows(ctx, db, db, c, where, all, tail)
		}
		if err != nil {
			return nil, err
		}
		if err := s.hydrate(ctx, db, db, c, rows, 0); err != nil {
			return nil, err
		}
		if err := s.maskLinks(ctx, db, db, c, rows, actor); err != nil {
			return nil, err
		}
		page := &Page{Rows: []map[string]any{}, Total: total, Engine: "sql"}
		for _, r := range rows {
			page.Rows = append(page.Rows, s.rowJSON(c, r, actor))
		}
		return page, nil
	}

	// Go path: load every row, then filter and sort.
	rows, err := s.scan(ctx, db, c, actor, filters, now)
	if err != nil {
		return nil, err
	}
	type item struct {
		r   *Row
		key any
	}
	var kept []item
	for _, r := range rows {
		env := newRowEnv(c, r, actor, now)
		var key any
		switch {
		case sortField != nil:
			key = env.value(sortField)
		case sortCol == "_created_at":
			key = r.Created
		case sortCol == "_updated_at":
			key = r.Updated
		default:
			key = float64(r.ID)
		}
		kept = append(kept, item{r, key})
	}
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := kept[i].key, kept[j].key
		if (a == nil) != (b == nil) {
			return b == nil // blanks last in both directions
		}
		c := compareKeys(a, b)
		if c == 0 {
			c = cmpInt(kept[i].r.ID, kept[j].r.ID)
		}
		if q.Desc {
			return c > 0
		}
		return c < 0
	})
	page := &Page{Rows: []map[string]any{}, Total: len(kept), Engine: "go"}
	var shown []*Row
	for i := q.Offset; i < len(kept) && i < q.Offset+q.Limit; i++ {
		shown = append(shown, kept[i].r)
	}
	if err := s.maskLinks(ctx, db, db, c, shown, actor); err != nil {
		return nil, err
	}
	for i := q.Offset; i < len(kept) && i < q.Offset+q.Limit; i++ {
		page.Rows = append(page.Rows, s.rowJSON(c, kept[i].r, actor))
	}
	return page, nil
}

// indexedPage reads a page sorted by an indexed column: rows with a
// value, then blank rows by _id.
func (s *Service) indexedPage(ctx context.Context, db *store.DB, c *compiled, where string, args []any, col, order, dir string, q Query) ([]*Row, error) {
	and := func(cond string) string {
		if where == "" {
			return cond
		}
		return where + " AND " + cond
	}
	filled, err := s.count(ctx, db, c, and(col+" IS NOT NULL"), args)
	if err != nil {
		return nil, err
	}
	var rows []*Row
	if q.Offset < filled {
		tail := fmt.Sprintf("ORDER BY %s, _id%s LIMIT %d OFFSET %d", order, dir, q.Limit, q.Offset)
		if rows, err = selectRows(ctx, db, db, c, and(col+" IS NOT NULL"), args, tail); err != nil {
			return nil, err
		}
	}
	if len(rows) < q.Limit {
		tail := fmt.Sprintf("ORDER BY _id%s LIMIT %d OFFSET %d", dir, q.Limit-len(rows), max(0, q.Offset-filled))
		blank, err := selectRows(ctx, db, db, c, and(col+" IS NULL"), args, tail)
		if err != nil {
			return nil, err
		}
		rows = append(rows, blank...)
	}
	return rows, nil
}

// filters compiles a query filter and adds the actor's row rule.
func (s *Service) filters(c *compiled, actor rbac.Actor, filter string) ([]*formula.Checked, error) {
	var filters []*formula.Checked
	if strings.TrimSpace(filter) != "" {
		scope := tableScope{t: c.Table, names: true}
		ch, errs := formula.Compile(filter, scope)
		if errs != nil {
			return nil, &ValidationError{Field: "filter", Message: errs[0].Message, Formula: errs}
		}
		if ch.Type != formula.Bool && ch.Type != formula.Any && ch.Type != formula.Null {
			return nil, &ValidationError{Field: "filter", Message: "a filter must be true or false, this one gives " + ch.Type.String()}
		}
		filters = append(filters, ch)
	}
	rule, err := c.rule(actor)
	if err != nil {
		return nil, err
	}
	if rule != nil {
		filters = append(filters, rule)
	}
	return filters, nil
}

// pushdown turns filters into one SQL condition; ok is false when one of
// them has no SQL form.
func pushdown(db *store.DB, c *compiled, env formula.Env, filters []*formula.Checked) (where string, args []any, ok bool) {
	for _, f := range filters {
		w, ok := f.CompileSQL(string(db.Dialect), resolver{c: c, dialect: string(db.Dialect), env: env}, env)
		if !ok {
			return "", nil, false
		}
		if where != "" {
			where += " AND "
		}
		where += "(" + w.Where + ")"
		args = append(args, w.Args...)
	}
	return where, args, true
}

// scan loads and hydrates the rows all filters accept, in _id order. SQL
// narrows the rows when the filters have an SQL form, else every row is
// loaded and filtered in Go. It refuses to load more than MaxScan rows.
func (s *Service) scan(ctx context.Context, db *store.DB, c *compiled, actor rbac.Actor, filters []*formula.Checked, now time.Time) ([]*Row, error) {
	env := newRowEnv(c, &Row{Values: map[string]any{}}, actor, now)
	where, args, pushed := pushdown(db, c, env, filters)
	countQ := "SELECT COUNT(*) FROM " + db.Table(c.sqlName())
	if pushed && where != "" {
		countQ += " WHERE " + where
	}
	var n int
	if err := db.QueryRow(ctx, countQ, args...).Scan(&n); err != nil {
		return nil, err
	}
	if n > MaxScan {
		if pushed {
			return nil, invalid("sort", "this sort or grouping cannot run in the database and more than %d rows match; filter the rows further or choose another field", MaxScan)
		}
		return nil, invalid("filter", "this filter or sort cannot run in the database and the table has more than %d rows; simplify it", MaxScan)
	}
	rows, err := selectRows(ctx, db, db, c, where, args, "ORDER BY _id")
	if err != nil {
		return nil, err
	}
	if err := s.hydrate(ctx, db, db, c, rows, 0); err != nil {
		return nil, err
	}
	if pushed {
		return rows, nil
	}
	kept := rows[:0]
	for _, r := range rows {
		env := newRowEnv(c, r, actor, now)
		keep := true
		for _, f := range filters {
			if ok, err := f.EvalBool(env); err != nil || !ok {
				keep = false
				break
			}
		}
		if keep {
			kept = append(kept, r)
		}
	}
	return kept, nil
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func compareKeys(a, b any) int {
	switch x := a.(type) {
	case float64:
		if y, ok := b.(float64); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	case bool:
		if y, ok := b.(bool); ok {
			if x == y {
				return 0
			}
			if !x {
				return -1
			}
			return 1
		}
	case time.Time:
		if y, ok := b.(time.Time); ok {
			return x.Compare(y)
		}
	}
	return strings.Compare(formula.ToText(a), formula.ToText(b))
}

// GetRow returns one row.
func (s *Service) GetRow(ctx context.Context, actor rbac.Actor, appID, tableID string, id int64) (map[string]any, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(c.Permissions.Read, "reading "+c.Name); err != nil {
		return nil, err
	}
	rule, err := c.rule(actor)
	if err != nil {
		return nil, err
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	r, err := s.ruleRow(ctx, db, db, c, rule, actor, id)
	if err != nil {
		return nil, err
	}
	return s.shownJSON(ctx, db, db, c, r, actor)
}

// shownJSON is a row as the actor may see it: rowJSON with the labels of
// linked rows they may not read blanked. Row events keep the full row,
// for flows that run with the flow's own access.
func (s *Service) shownJSON(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, r *Row, actor rbac.Actor) (map[string]any, error) {
	cp := *r
	cp.Values = make(map[string]any, len(r.Values))
	for k, v := range r.Values {
		cp.Values[k] = v
	}
	if err := s.maskLinks(ctx, ex, db, c, []*Row{&cp}, actor); err != nil {
		return nil, err
	}
	return s.rowJSON(c, &cp, actor), nil
}

func (s *Service) getRow(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, actor rbac.Actor, id int64) (map[string]any, error) {
	r, err := s.loadRow(ctx, ex, db, c, id)
	if err != nil {
		return nil, err
	}
	return s.rowJSON(c, r, actor), nil
}

// prepared holds encoded values for a write, by field.
type prepared struct {
	cols  map[*Field]any
	links map[*Field][]int64
}

// prepare validates and encodes input values, keyed by field name.
func (s *Service) prepare(ctx context.Context, ex store.Execer, db *store.DB, c *compiled, values map[string]any, create bool, lenient bool) (*prepared, error) {
	p := &prepared{cols: map[*Field]any{}, links: map[*Field][]int64{}}
	for name, v := range values {
		if strings.HasPrefix(name, "_") {
			continue // system fields are read only
		}
		f := c.FieldByName(name)
		if f == nil {
			if lenient {
				continue
			}
			return nil, invalid(name, "there is no field named %q", name)
		}
		if !f.writable() {
			if lenient {
				continue
			}
			return nil, invalid(f.Name, "this field is computed and cannot be set")
		}
		if f.Type == TypeLink && f.LinkMany {
			ids, err := linkIDs(v)
			if err != nil {
				return nil, invalid(f.Name, "%s", err.Error())
			}
			p.links[f] = ids
			continue
		}
		enc, err := encode(f, v)
		if err != nil {
			return nil, invalid(f.Name, "%s", err.Error())
		}
		if txt, ok := enc.(string); ok && f.Indexed && f.Type == TypeText && len([]rune(txt)) > MaxIndexedText {
			return nil, invalid(f.Name, "[%s] is indexed, so it holds at most %d characters", f.Name, MaxIndexedText)
		}
		if f.Type == TypeSingleSelect && enc != nil && len(f.Options) > 0 && !slices.Contains(f.Options, enc.(string)) {
			return nil, invalid(f.Name, "%q is not one of the options", enc)
		}
		if f.Type == TypeMultiSelect && enc != nil && len(f.Options) > 0 {
			var items []string
			json.Unmarshal([]byte(enc.(string)), &items)
			for _, it := range items {
				if !slices.Contains(f.Options, it) {
					return nil, invalid(f.Name, "%q is not one of the options", it)
				}
			}
		}
		if f.Type == TypeLink && enc != nil {
			p.links[f] = []int64{enc.(int64)}
		}
		p.cols[f] = enc
	}
	// Linked rows must exist.
	for f, ids := range p.links {
		if len(ids) == 0 {
			continue
		}
		target := c
		if f.LinkTable != c.ID {
			var err error
			if target, err = s.loadCompiled(ctx, c.AppID, f.LinkTable); err != nil {
				return nil, err
			}
		}
		var n int
		for _, chunk := range chunks(ids, 500) {
			var k int
			if err := ex.QueryRow(ctx, "SELECT COUNT(*) FROM "+db.Table(target.sqlName())+" WHERE _id IN "+inList(len(chunk)), toArgs(chunk)...).Scan(&k); err != nil {
				return nil, err
			}
			n += k
		}
		if n != len(ids) {
			return nil, invalid(f.Name, "a linked row does not exist in %s", target.Name)
		}
		if !f.LinkMany {
			delete(p.links, f)
		}
	}
	// Required fields.
	for _, f := range c.Fields {
		if !f.Required {
			continue
		}
		v, setCol := p.cols[f]
		ids, setLinks := p.links[f]
		blank := (setCol && v == nil) || (setLinks && len(ids) == 0)
		missing := create && !setCol && !setLinks
		if blank || missing {
			return nil, invalid(f.Name, "a value is required")
		}
	}
	return p, nil
}

func (s *Service) writeLinks(ctx context.Context, tx *store.Tx, p *prepared, id int64) error {
	db := tx.DB()
	for f, ids := range p.links {
		if _, err := tx.Exec(ctx, "DELETE FROM "+db.Table(joinName(f))+" WHERE src = ?", id); err != nil {
			return err
		}
		for _, dst := range ids {
			if _, err := tx.Exec(ctx, "INSERT INTO "+db.Table(joinName(f))+" (src, dst) VALUES (?, ?)", id, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateRow adds a row from values keyed by field name.
func (s *Service) CreateRow(ctx context.Context, actor rbac.Actor, appID, tableID string, values map[string]any) (map[string]any, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(c.Permissions.Write, "adding rows to "+c.Name); err != nil {
		return nil, err
	}
	rule, err := c.rule(actor)
	if err != nil {
		return nil, err
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, err := s.insert(ctx, tx, c, values, false)
	if err != nil {
		return nil, err
	}
	r, err := s.loadRow(ctx, tx, db, c, id)
	if err != nil {
		return nil, err
	}
	if !s.allowed(c, rule, actor, r) {
		return nil, errOutsideRule(c)
	}
	row := s.rowJSON(c, r, actor)
	shown, err := s.shownJSON(ctx, tx, db, c, r, actor)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.emit(ctx, RowEvent{AppID: appID, TableID: c.ID, Kind: "created", Row: row, Actor: actor})
	return shown, nil
}

// emit tells OnRow about a change.
func (s *Service) emit(ctx context.Context, ev RowEvent) {
	s.touch(ev.AppID, ev.TableID)
	if s.OnRow != nil {
		s.OnRow(ctx, ev)
	}
}

func (s *Service) insert(ctx context.Context, tx *store.Tx, c *compiled, values map[string]any, lenient bool) (int64, error) {
	db := tx.DB()
	p, err := s.prepare(ctx, tx, db, c, values, true, lenient)
	if err != nil {
		return 0, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	cols := []string{"_created_at", "_updated_at"}
	marks := []string{"?", "?"}
	args := []any{now, now}
	tbl := db.Table(c.sqlName())
	for _, f := range c.Fields {
		if v, ok := p.cols[f]; ok {
			cols = append(cols, store.Q(f.Column))
			marks = append(marks, "?")
			args = append(args, v)
		}
		if f.Type == TypeAutoNumber {
			cols = append(cols, store.Q(f.Column))
			marks = append(marks, fmt.Sprintf("(SELECT COALESCE(MAX(%s), 0) + 1 FROM %s)", store.Q(f.Column), tbl))
		}
	}
	q := "INSERT INTO " + tbl + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(marks, ", ") + ") RETURNING _id"
	var id int64
	if err := tx.QueryRow(ctx, q, args...).Scan(&id); err != nil {
		return 0, err
	}
	if err := s.writeLinks(ctx, tx, p, id); err != nil {
		return 0, err
	}
	return id, s.recompute(ctx, tx, c, []int64{id})
}

// UpdateRow changes the given fields of a row.
func (s *Service) UpdateRow(ctx context.Context, actor rbac.Actor, appID, tableID string, id int64, values map[string]any) (map[string]any, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(c.Permissions.Write, "changing rows of "+c.Name); err != nil {
		return nil, err
	}
	rule, err := c.rule(actor)
	if err != nil {
		return nil, err
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var old map[string]any
	if s.OnRow != nil || rule != nil {
		r, err := s.ruleRow(ctx, tx, db, c, rule, actor, id)
		if err != nil {
			return nil, err
		}
		old = s.rowJSON(c, r, actor)
	}
	p, err := s.prepare(ctx, tx, db, c, values, false, false)
	if err != nil {
		return nil, err
	}
	sets := []string{"_updated_at = ?"}
	args := []any{s.now().UTC().Format(time.RFC3339Nano)}
	for f, v := range p.cols {
		sets = append(sets, store.Q(f.Column)+" = ?")
		args = append(args, v)
	}
	args = append(args, id)
	res, err := tx.Exec(ctx, "UPDATE "+db.Table(c.sqlName())+" SET "+strings.Join(sets, ", ")+" WHERE _id = ?", args...)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("row %w", ErrNotFound)
	}
	if err := s.writeLinks(ctx, tx, p, id); err != nil {
		return nil, err
	}
	if err := s.recompute(ctx, tx, c, []int64{id}); err != nil {
		return nil, err
	}
	r, err := s.loadRow(ctx, tx, db, c, id)
	if err != nil {
		return nil, err
	}
	if !s.allowed(c, rule, actor, r) {
		return nil, errOutsideRule(c)
	}
	row := s.rowJSON(c, r, actor)
	shown, err := s.shownJSON(ctx, tx, db, c, r, actor)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.emit(ctx, RowEvent{AppID: appID, TableID: c.ID, Kind: "updated", Row: row, Old: old, Actor: actor})
	return shown, nil
}

// DeleteRows deletes rows and every link that points at them.
func (s *Service) DeleteRows(ctx context.Context, actor rbac.Actor, appID, tableID string, ids []int64) (int, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return 0, err
	}
	if err := actor.Require(c.Permissions.Delete, "deleting rows of "+c.Name); err != nil {
		return 0, err
	}
	rule, err := c.rule(actor)
	if err != nil {
		return 0, err
	}
	all, err := s.List(ctx, appID)
	if err != nil {
		return 0, err
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return 0, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if rule != nil {
		// Rows outside the actor's row rule are left alone.
		var mine []int64
		for _, chunk := range chunks(ids, 500) {
			rows, err := selectRows(ctx, tx, db, c, "_id IN "+inList(len(chunk)), toArgs(chunk), "")
			if err != nil {
				return 0, err
			}
			if err := s.hydrate(ctx, tx, db, c, rows, 0); err != nil {
				return 0, err
			}
			for _, r := range rows {
				if s.allowed(c, rule, actor, r) {
					mine = append(mine, r.ID)
				}
			}
		}
		ids = mine
	}
	// Flows hear about deleted rows as they were, up to 1000 of them.
	var gone []map[string]any
	if s.OnRow != nil {
		for _, id := range ids {
			if len(gone) >= 1000 {
				break
			}
			if row, err := s.getRow(ctx, tx, db, c, actor, id); err == nil {
				gone = append(gone, row)
			} else if !IsNotFound(err) {
				return 0, err
			}
		}
	}
	deleted := 0
	for _, chunk := range chunks(ids, 500) {
		in, args := inList(len(chunk)), toArgs(chunk)
		for _, f := range c.Fields {
			if f.Type == TypeLink && f.LinkMany {
				if _, err := tx.Exec(ctx, "DELETE FROM "+db.Table(joinName(f))+" WHERE src IN "+in, args...); err != nil {
					return 0, err
				}
			}
		}
		for _, o := range all {
			for _, f := range o.Fields {
				if f.Type != TypeLink || f.LinkTable != c.ID {
					continue
				}
				if f.LinkMany {
					_, err = tx.Exec(ctx, "DELETE FROM "+db.Table(joinName(f))+" WHERE dst IN "+in, args...)
				} else {
					_, err = tx.Exec(ctx, "UPDATE "+db.Table(o.sqlName())+" SET "+store.Q(f.Column)+" = NULL WHERE "+store.Q(f.Column)+" IN "+in, args...)
				}
				if err != nil {
					return 0, err
				}
			}
		}
		res, err := tx.Exec(ctx, "DELETE FROM "+db.Table(c.sqlName())+" WHERE _id IN "+in, args...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	for _, row := range gone {
		s.emit(ctx, RowEvent{AppID: appID, TableID: c.ID, Kind: "deleted", Row: row, Actor: actor})
	}
	return deleted, nil
}

// IsNotFound reports whether err means something does not exist.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) }
