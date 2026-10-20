package exttables

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/extdb"
	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/queries"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/tables"
)

// MaxScan is the most rows a filter without a database form may read.
const MaxScan = 50_000

// rows opens a table's rows for an actor in an environment.
type rowsCtx struct {
	t    *Table
	ds   *queries.Datasource
	src  extdb.Source
	rows extdb.Rows
	tg   *extdb.Target
}

func (s *Service) open(ctx context.Context, appID, id, env string) (*rowsCtx, error) {
	t, err := s.Get(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	ds, src, err := s.source(ctx, t, env)
	if err != nil {
		return nil, err
	}
	return &rowsCtx{t: t, ds: ds, src: src, rows: src.Rows(), tg: t.target()}, nil
}

func (rc *rowsCtx) readOnly() bool {
	return !rc.t.Writable() || rc.ds.Config.DB != nil && rc.ds.Config.DB.ReadOnly
}

// fail turns a driver error into one safe to show.
func fail(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, extdb.ErrNoRow) {
		return fmt.Errorf("row %w", tables.ErrNotFound)
	}
	if errors.Is(err, extdb.ErrReadOnly) {
		return invalid("", "this data source is read only")
	}
	var de *extdb.Error
	if errors.As(err, &de) {
		return &queries.RunError{Message: "the database answered: " + de.Message}
	}
	return err
}

// json is a row as the API shows it, keyed by field name, with its key as
// _id.
func (rc *rowsCtx) json(r map[string]any) map[string]any {
	out := map[string]any{"_id": r[rc.tg.Key]}
	for _, c := range rc.t.visible() {
		v := r[c.Name]
		switch x := v.(type) {
		case time.Time:
			v = x.UTC().Format(formula.DateLayout)
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				v = nil
			}
		}
		out[c.Name] = v
	}
	return out
}

// conds compiles the filter and the actor's row rule.
func (rc *rowsCtx) conds(filter string, actor rbac.Actor) ([]*formula.Checked, error) {
	var out []*formula.Checked
	if f, err := compileCond(rc.t, "filter", filter); err != nil {
		return nil, err
	} else if f != nil {
		out = append(out, f)
	}
	if r, err := rc.t.rule(actor); err != nil {
		return nil, err
	} else if r != nil {
		out = append(out, r)
	}
	return out, nil
}

// push compiles conditions for the database; ok is false when one of
// them has no database form.
func (rc *rowsCtx) push(cs []*formula.Checked, e formula.Env) (*extdb.Cond, bool) {
	if len(cs) == 0 {
		return nil, true
	}
	var parts []*extdb.Cond
	for _, c := range cs {
		p, ok := rc.rows.Compile(rc.tg, c, e)
		if !ok {
			return nil, false
		}
		parts = append(parts, p)
	}
	if len(parts) == 1 {
		return parts[0], true
	}
	if parts[0].Mongo != nil {
		list := make([]any, len(parts))
		for i, p := range parts {
			list[i] = p.Mongo
		}
		return &extdb.Cond{Mongo: map[string]any{"$and": list}}, true
	}
	out := &extdb.Cond{}
	for i, p := range parts {
		if i > 0 {
			out.SQL += " AND "
		}
		out.SQL += "(" + p.SQL + ")"
		out.Args = append(out.Args, p.Args...)
	}
	return out, true
}

func matches(cs []*formula.Checked, e formula.Env) bool {
	for _, c := range cs {
		if ok, err := c.EvalBool(e); err != nil || !ok {
			return false
		}
	}
	return true
}

// ListRows returns a page of rows that match the query. Env picks the
// connection settings: development in the builder, production in apps.
func (s *Service) ListRows(ctx context.Context, actor rbac.Actor, env, appID, id string, q tables.Query) (*tables.Page, error) {
	rc, err := s.open(ctx, appID, id, env)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(rc.t.Permissions.Read, "reading "+rc.t.Name); err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 100
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	cs, err := rc.conds(q.Filter, actor)
	if err != nil {
		return nil, err
	}
	var order []extdb.Order
	switch q.Sort {
	case "", "_id", "_createdAt", "_updatedAt":
		if rc.tg.Key != "" {
			order = []extdb.Order{{Column: rc.tg.Key, Desc: q.Desc}}
		}
	default:
		c := rc.t.column(q.Sort)
		if c == nil || c.Hidden || c.Binary {
			return nil, invalid("sort", "there is no field named %q", q.Sort)
		}
		order = []extdb.Order{{Column: c.Name, Desc: q.Desc}}
	}
	now := s.now()
	blank := &rowEnv{t: rc.t, row: map[string]any{}, actor: actor, now: now}
	if where, ok := rc.push(cs, blank); ok {
		total, err := rc.rows.Count(ctx, rc.tg, where)
		if err != nil {
			return nil, fail(err)
		}
		got, err := rc.rows.Select(ctx, rc.tg, extdb.Select{Where: where, Order: order, Limit: q.Limit, Offset: q.Offset})
		if err != nil {
			return nil, fail(err)
		}
		page := &tables.Page{Rows: []map[string]any{}, Total: total, Engine: "sql"}
		for _, r := range got {
			page.Rows = append(page.Rows, rc.json(r))
		}
		return page, nil
	}

	// Go path: read the table, then filter and page.
	n, err := rc.rows.Count(ctx, rc.tg, nil)
	if err != nil {
		return nil, fail(err)
	}
	if n > MaxScan {
		return nil, invalid("filter", "this filter cannot run in the database and the table has more than %d rows; simplify it", MaxScan)
	}
	all, err := rc.rows.Select(ctx, rc.tg, extdb.Select{Order: order})
	if err != nil {
		return nil, fail(err)
	}
	kept := all[:0]
	for _, r := range all {
		if matches(cs, &rowEnv{t: rc.t, row: r, actor: actor, now: now}) {
			kept = append(kept, r)
		}
	}
	page := &tables.Page{Rows: []map[string]any{}, Total: len(kept), Engine: "go"}
	for i := q.Offset; i < len(kept) && i < q.Offset+q.Limit; i++ {
		page.Rows = append(page.Rows, rc.json(kept[i]))
	}
	return page, nil
}

// key reads a row key from the API into the key column's type.
func (rc *rowsCtx) key(raw string) (any, error) {
	if rc.tg.Key == "" {
		return nil, invalid("", "%s has no primary key, so its rows cannot be opened one by one", rc.t.Name)
	}
	c := rc.t.column(rc.tg.Key)
	if c.Type == "number" || c.Type == "autonumber" {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("row %w", tables.ErrNotFound)
		}
		return extdb.Write(f, &c.Column)
	}
	return raw, nil
}

// load reads one row by key, if the actor's row rule allows it.
func (rc *rowsCtx) load(ctx context.Context, key any, actor rbac.Actor, now time.Time) (map[string]any, error) {
	where := &extdb.Cond{}
	if rc.ds.Kind == "mongodb" {
		where = nil
	}
	var got []map[string]any
	var err error
	if where != nil {
		where.SQL = keyEquals(rc.ds.Kind, rc.tg.Key)
		where.Args = []any{key}
		got, err = rc.rows.Select(ctx, rc.tg, extdb.Select{Where: where, Limit: 1})
	} else {
		got, err = rc.mongoGet(ctx, key)
	}
	if err != nil {
		return nil, fail(err)
	}
	if len(got) == 0 {
		return nil, fmt.Errorf("row %w", tables.ErrNotFound)
	}
	rule, err := rc.t.rule(actor)
	if err != nil {
		return nil, err
	}
	if rule != nil && !matches([]*formula.Checked{rule}, &rowEnv{t: rc.t, row: got[0], actor: actor, now: now}) {
		return nil, fmt.Errorf("row %w", tables.ErrNotFound)
	}
	return got[0], nil
}

// mongoGet finds a document by key through a formula, so the driver turns
// the key into an ObjectID where it is one.
func (rc *rowsCtx) mongoGet(ctx context.Context, key any) ([]map[string]any, error) {
	ch, errs := formula.Compile("["+rc.tg.Key+"] = Current.User.Id", scope{rc.t})
	if errs != nil {
		return nil, errs[0]
	}
	where, ok := rc.rows.Compile(rc.tg, ch, keyEnv{key})
	if !ok {
		return nil, errors.New("the key has no filter form")
	}
	return rc.rows.Select(ctx, rc.tg, extdb.Select{Where: where, Limit: 1})
}

// keyEnv gives a key as Current.User.Id, for mongoGet.
type keyEnv struct{ key any }

func (k keyEnv) Field(string) any  { return nil }
func (k keyEnv) Name([]string) any { return fmt.Sprint(k.key) }
func (k keyEnv) Now() time.Time    { return time.Now() }

func keyEquals(kind, key string) string {
	switch kind {
	case "mysql":
		return "`" + strings.ReplaceAll(key, "`", "``") + "` = ?"
	case "mssql":
		return "[" + strings.ReplaceAll(key, "]", "]]") + "] = ?"
	}
	return `"` + strings.ReplaceAll(key, `"`, `""`) + `" = ?`
}

// GetRow returns one row.
func (s *Service) GetRow(ctx context.Context, actor rbac.Actor, env, appID, id, rowID string) (map[string]any, error) {
	rc, err := s.open(ctx, appID, id, env)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(rc.t.Permissions.Read, "reading "+rc.t.Name); err != nil {
		return nil, err
	}
	key, err := rc.key(rowID)
	if err != nil {
		return nil, err
	}
	r, err := rc.load(ctx, key, actor, s.now())
	if err != nil {
		return nil, err
	}
	return rc.json(r), nil
}

// values converts the values given for a write, by field name, into
// column values.
func (rc *rowsCtx) values(in map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range in {
		if strings.HasPrefix(k, "_") {
			continue
		}
		c := rc.t.column(k)
		if c == nil || c.Hidden || c.Binary {
			return nil, invalid(k, "there is no field named %s", k)
		}
		if c.Generated {
			return nil, invalid(c.Name, "the database fills %s", c.Name)
		}
		x, err := extdb.Write(v, &c.Column)
		if err != nil {
			return nil, invalid(c.Name, "%s", err)
		}
		out[c.Name] = x
	}
	return out, nil
}

// formulaRow is a written row as formulas read it.
func formulaRow(t *Table, vals map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range vals {
		c := t.column(k)
		if c == nil {
			continue
		}
		switch x := v.(type) {
		case int64:
			out[k] = float64(x)
		default:
			out[k] = extdb.Read(x, &c.Column)
		}
	}
	return out
}

func errOutsideRule(t *Table) error {
	return fmt.Errorf("%w: the row must match your row rule for %s", rbac.ErrForbidden, t.Name)
}

// CreateRow adds a row.
func (s *Service) CreateRow(ctx context.Context, actor rbac.Actor, env, appID, id string, in map[string]any) (map[string]any, error) {
	rc, err := s.open(ctx, appID, id, env)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(rc.t.Permissions.Write, "adding rows to "+rc.t.Name); err != nil {
		return nil, err
	}
	if rc.readOnly() {
		return nil, invalid("", "%s is read only", rc.t.Name)
	}
	vals, err := rc.values(in)
	if err != nil {
		return nil, err
	}
	for _, c := range rc.t.Columns {
		if required(&c.Column) && !c.Hidden {
			if v, ok := vals[c.Name]; !ok || v == nil {
				return nil, invalid(c.Name, "%s is required", c.Name)
			}
		}
	}
	now := s.now()
	rule, err := rc.t.rule(actor)
	if err != nil {
		return nil, err
	}
	if rule != nil && !matches([]*formula.Checked{rule}, &rowEnv{t: rc.t, row: formulaRow(rc.t, vals), actor: actor, now: now}) {
		return nil, errOutsideRule(rc.t)
	}
	key, err := rc.rows.Insert(ctx, rc.tg, vals)
	if err != nil {
		return nil, fail(err)
	}
	if key == nil {
		return map[string]any{}, nil
	}
	r, err := rc.load(ctx, key, rbac.Actor{Role: rbac.Admin, Builder: true}, now)
	if err != nil {
		return nil, err
	}
	return rc.json(r), nil
}

// UpdateRow changes the fields given.
func (s *Service) UpdateRow(ctx context.Context, actor rbac.Actor, env, appID, id, rowID string, in map[string]any) (map[string]any, error) {
	rc, err := s.open(ctx, appID, id, env)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(rc.t.Permissions.Write, "changing rows of "+rc.t.Name); err != nil {
		return nil, err
	}
	if rc.readOnly() {
		return nil, invalid("", "%s is read only", rc.t.Name)
	}
	key, err := rc.key(rowID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	cur, err := rc.load(ctx, key, actor, now)
	if err != nil {
		return nil, err
	}
	vals, err := rc.values(in)
	if err != nil {
		return nil, err
	}
	delete(vals, rc.tg.Key)
	for _, c := range rc.t.Columns {
		if v, ok := vals[c.Name]; ok && v == nil && required(&c.Column) {
			return nil, invalid(c.Name, "%s is required", c.Name)
		}
	}
	rule, err := rc.t.rule(actor)
	if err != nil {
		return nil, err
	}
	if rule != nil {
		merged := map[string]any{}
		for k, v := range cur {
			merged[k] = v
		}
		for k, v := range formulaRow(rc.t, vals) {
			merged[k] = v
		}
		if !matches([]*formula.Checked{rule}, &rowEnv{t: rc.t, row: merged, actor: actor, now: now}) {
			return nil, errOutsideRule(rc.t)
		}
	}
	if err := rc.rows.Update(ctx, rc.tg, key, vals); err != nil {
		return nil, fail(err)
	}
	r, err := rc.load(ctx, key, rbac.Actor{Role: rbac.Admin, Builder: true}, now)
	if err != nil {
		return nil, err
	}
	return rc.json(r), nil
}

// DeleteRows deletes rows by key; rows the actor may not see are left.
func (s *Service) DeleteRows(ctx context.Context, actor rbac.Actor, env, appID, id string, rowIDs []string) (int, error) {
	rc, err := s.open(ctx, appID, id, env)
	if err != nil {
		return 0, err
	}
	if err := actor.Require(rc.t.Permissions.Delete, "deleting rows of "+rc.t.Name); err != nil {
		return 0, err
	}
	if rc.readOnly() {
		return 0, invalid("", "%s is read only", rc.t.Name)
	}
	now := s.now()
	rule, err := rc.t.rule(actor)
	if err != nil {
		return 0, err
	}
	var keys []any
	for _, raw := range rowIDs {
		key, err := rc.key(raw)
		if err != nil {
			continue
		}
		if rule != nil {
			if _, err := rc.load(ctx, key, actor, now); err != nil {
				continue
			}
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return 0, nil
	}
	n, err := rc.rows.Delete(ctx, rc.tg, keys)
	return n, fail(err)
}

// Schema lists the tables of a database data source.
func (s *Service) Schema(ctx context.Context, appID, dsID string) ([]extdb.TableInfo, error) {
	ds, err := s.queries.Datasource(ctx, appID, dsID)
	if err != nil {
		return nil, err
	}
	if !ds.Database() {
		return nil, invalid("datasourceId", "only database data sources have tables")
	}
	src, err := s.runner.Source(ctx, ds, queries.Development)
	if err != nil {
		return nil, err
	}
	ts, err := src.Tables(ctx)
	if err != nil {
		return nil, fail(err)
	}
	return ts, nil
}

// Describe reads the columns of one table of a data source.
func (s *Service) Describe(ctx context.Context, appID, dsID, schema, name string) (*extdb.TableInfo, error) {
	ds, err := s.queries.Datasource(ctx, appID, dsID)
	if err != nil {
		return nil, err
	}
	src, err := s.runner.Source(ctx, ds, queries.Development)
	if err != nil {
		return nil, err
	}
	info, err := src.Describe(ctx, schema, name)
	return info, fail(err)
}
