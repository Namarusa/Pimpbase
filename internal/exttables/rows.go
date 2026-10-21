package exttables

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// rowsCtx is a table's rows opened in an environment.
type rowsCtx struct {
	s    *Service
	t    *Table
	ds   *queries.Datasource
	src  extdb.Source
	rows extdb.Rows
	tg   *extdb.Target
	env  string
	g    *graph
}

func (s *Service) open(ctx context.Context, appID, id, env string) (*rowsCtx, error) {
	t, err := s.Get(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	g, err := s.graph(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.openTable(ctx, t, env, g)
}

func (s *Service) openTable(ctx context.Context, t *Table, env string, g *graph) (*rowsCtx, error) {
	ds, src, err := s.source(ctx, t, env)
	if err != nil {
		return nil, err
	}
	return &rowsCtx{s: s, t: t, ds: ds, src: src, rows: src.Rows(), tg: t.target(), env: env, g: g}, nil
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

// rowID is a row's _id: the value of a one-column key, or the key values
// as a JSON list, such as [3,"B"].
func (rc *rowsCtx) rowID(r map[string]any) any {
	switch len(rc.tg.Keys) {
	case 0:
		return nil
	case 1:
		return r[rc.tg.Keys[0]]
	}
	vals := make([]any, len(rc.tg.Keys))
	for i, k := range rc.tg.Keys {
		v := r[k]
		if t, ok := v.(time.Time); ok {
			v = t.UTC().Format(formula.DateLayout)
		}
		vals[i] = v
	}
	js, _ := json.Marshal(vals)
	return string(js)
}

// json is a row as the API shows it, keyed by field name, with its key as
// _id. Link fields are set by decorate.
func (rc *rowsCtx) json(r map[string]any) map[string]any {
	out := map[string]any{"_id": rc.rowID(r)}
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
		for _, k := range rc.tg.Keys {
			order = append(order, extdb.Order{Column: k, Desc: q.Desc})
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
		return page, rc.page(ctx, actor, page, got)
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
	var shown []map[string]any
	for i := q.Offset; i < len(kept) && i < q.Offset+q.Limit; i++ {
		shown = append(shown, kept[i])
	}
	return page, rc.page(ctx, actor, page, shown)
}

// page fills a page with rows as the API shows them, links included.
func (rc *rowsCtx) page(ctx context.Context, actor rbac.Actor, page *tables.Page, raws []map[string]any) error {
	for _, r := range raws {
		page.Rows = append(page.Rows, rc.json(r))
	}
	return rc.decorate(ctx, actor, raws, page.Rows)
}

// shown is one row as the API shows it, links included.
func (rc *rowsCtx) shown(ctx context.Context, actor rbac.Actor, r map[string]any) (map[string]any, error) {
	out := []map[string]any{rc.json(r)}
	if err := rc.decorate(ctx, actor, []map[string]any{r}, out); err != nil {
		return nil, err
	}
	return out[0], nil
}

// key reads a row's _id from the API into key values of the key columns'
// types.
func (rc *rowsCtx) key(raw string) ([]any, error) {
	notFound := fmt.Errorf("row %w", tables.ErrNotFound)
	keys := rc.tg.Keys
	if len(keys) == 0 {
		return nil, invalid("", "%s has no primary key, so its rows cannot be opened one by one", rc.t.Name)
	}
	parts := []any{raw}
	if len(keys) > 1 {
		parts = nil
		if err := json.Unmarshal([]byte(raw), &parts); err != nil || len(parts) != len(keys) {
			return nil, notFound
		}
	}
	out := make([]any, len(keys))
	for i, k := range keys {
		c := rc.t.column(k)
		v, err := extdb.Write(parts[i], &c.Column)
		if err != nil || v == nil {
			return nil, notFound
		}
		out[i] = v
	}
	return out, nil
}

// keyCond finds the row with the key values.
func (rc *rowsCtx) keyCond(key []any) *extdb.Cond {
	parts := make([]*extdb.Cond, len(key))
	for i, k := range rc.tg.Keys {
		parts[i] = rc.rows.In(rc.tg, k, []any{key[i]})
	}
	return and(parts)
}

// and joins conditions of one source.
func and(parts []*extdb.Cond) *extdb.Cond {
	if len(parts) == 1 {
		return parts[0]
	}
	if parts[0].Mongo != nil {
		list := make([]any, len(parts))
		for i, p := range parts {
			list[i] = p.Mongo
		}
		return &extdb.Cond{Mongo: map[string]any{"$and": list}}
	}
	out := &extdb.Cond{}
	for i, p := range parts {
		if i > 0 {
			out.SQL += " AND "
		}
		out.SQL += "(" + p.SQL + ")"
		out.Args = append(out.Args, p.Args...)
	}
	return out
}

// load reads one row by key, if the actor's row rule allows it.
func (rc *rowsCtx) load(ctx context.Context, key []any, actor rbac.Actor, now time.Time) (map[string]any, error) {
	got, err := rc.rows.Select(ctx, rc.tg, extdb.Select{Where: rc.keyCond(key), Limit: 1})
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
	return rc.shown(ctx, actor, r)
}

// values converts the values given for a write, by field name, into
// column values. Fields listing the rows that link here are read only and
// left out.
func (rc *rowsCtx) values(ctx context.Context, in map[string]any) (map[string]any, error) {
	reverse := map[string]bool{}
	for _, r := range rc.g.reverse(rc.t) {
		reverse[strings.ToLower(r.name)] = true
	}
	out := map[string]any{}
	for k, v := range in {
		if strings.HasPrefix(k, "_") || reverse[strings.ToLower(k)] {
			continue
		}
		c := rc.t.column(k)
		if c == nil || c.Hidden || c.Binary {
			return nil, invalid(k, "there is no field named %s", k)
		}
		if c.Generated {
			return nil, invalid(c.Name, "the database fills %s", c.Name)
		}
		if l := rc.g.link(rc.t, c.Name); l != nil {
			x, err := rc.linkValue(ctx, l, c, v)
			if err != nil {
				var ve *tables.ValidationError
				if errors.As(err, &ve) {
					return nil, err
				}
				if _, ok := err.(*queries.RunError); ok {
					return nil, err
				}
				return nil, invalid(c.Name, "%s", err)
			}
			out[c.Name] = x
			continue
		}
		x, err := extdb.Write(v, &c.Column)
		if err != nil {
			return nil, invalid(c.Name, "%s", err)
		}
		out[c.Name] = x
	}
	return out, nil
}

// emit tells OnRow about a change.
func (s *Service) emit(ctx context.Context, ev tables.RowEvent) {
	if s.OnRow != nil {
		s.OnRow(ctx, ev)
	}
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
	return s.createRow(ctx, rc, actor, in)
}

func (s *Service) createRow(ctx context.Context, rc *rowsCtx, actor rbac.Actor, in map[string]any) (map[string]any, error) {
	if err := actor.Require(rc.t.Permissions.Write, "adding rows to "+rc.t.Name); err != nil {
		return nil, err
	}
	if rc.readOnly() {
		return nil, invalid("", "%s is read only", rc.t.Name)
	}
	vals, err := rc.values(ctx, in)
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
	row, err := rc.shown(ctx, actor, r)
	if err != nil {
		return nil, err
	}
	s.emit(ctx, tables.RowEvent{AppID: rc.t.AppID, TableID: rc.t.ID, Kind: "created", Row: row, Actor: actor})
	return row, nil
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
	vals, err := rc.values(ctx, in)
	if err != nil {
		return nil, err
	}
	for _, k := range rc.tg.Keys {
		delete(vals, k)
	}
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
	var old map[string]any
	if s.OnRow != nil {
		if old, err = rc.shown(ctx, actor, cur); err != nil {
			return nil, err
		}
	}
	if err := rc.rows.Update(ctx, rc.tg, key, vals); err != nil {
		return nil, fail(err)
	}
	r, err := rc.load(ctx, key, rbac.Actor{Role: rbac.Admin, Builder: true}, now)
	if err != nil {
		return nil, err
	}
	row, err := rc.shown(ctx, actor, r)
	if err != nil {
		return nil, err
	}
	s.emit(ctx, tables.RowEvent{AppID: rc.t.AppID, TableID: rc.t.ID, Kind: "updated", Row: row, Old: old, Actor: actor})
	return row, nil
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
	var keys [][]any
	var gone []map[string]any
	for _, raw := range rowIDs {
		key, err := rc.key(raw)
		if err != nil {
			continue
		}
		if rule != nil || s.OnRow != nil {
			r, err := rc.load(ctx, key, actor, now)
			if err != nil {
				continue
			}
			if s.OnRow != nil {
				row, err := rc.shown(ctx, actor, r)
				if err != nil {
					return 0, err
				}
				gone = append(gone, row)
			}
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return 0, nil
	}
	n, err := rc.rows.Delete(ctx, rc.tg, keys)
	if err != nil {
		return n, fail(err)
	}
	for _, row := range gone {
		s.emit(ctx, tables.RowEvent{AppID: rc.t.AppID, TableID: rc.t.ID, Kind: "deleted", Row: row, Actor: actor})
	}
	return n, nil
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
