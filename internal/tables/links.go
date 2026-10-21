package tables

import (
	"context"
	"errors"
	"fmt"

	"github.com/pimpbase/pimpbase/internal/formula"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/store"
)

// Formulas read linked rows as [Link].[Field] and through rollups such as
// SUMOF([Lines], [Amount]). This file keeps those formulas working when the
// linked table changes, loads linked rows for formulas run in Go, and
// gives the SQL for them.

// linkingTables returns the other tables of the app that link to t.
func (s *Service) linkingTables(ctx context.Context, t *Table) ([]*Table, error) {
	all, err := s.List(ctx, t.AppID)
	if err != nil {
		return nil, err
	}
	var out []*Table
	for _, o := range all {
		if o.ID == t.ID {
			continue
		}
		for _, f := range o.Fields {
			if f.Type == TypeLink && f.LinkTable == t.ID {
				out = append(out, o)
				break
			}
		}
	}
	return out, nil
}

// renameLinked rewrites the formulas and row rules of tables linking to t
// (t itself included, for links to itself) after t's field old is renamed.
// Other tables are saved right away.
func (s *Service) renameLinked(ctx context.Context, t *Table, old, new string) error {
	others, err := s.linkingTables(ctx, t)
	if err != nil {
		return err
	}
	rewrite := func(o *Table) bool {
		changed := false
		for _, l := range o.Fields {
			if l.Type != TypeLink || l.LinkTable != t.ID {
				continue
			}
			fix := func(src string) string {
				out := formula.RenameLinkedField(src, l.Name, old, new)
				changed = changed || out != src
				return out
			}
			for _, f := range o.Fields {
				if f.Type == TypeFormula {
					f.Formula = fix(f.Formula)
				}
			}
			for i := range o.Permissions.Rows {
				o.Permissions.Rows[i].Filter = fix(o.Permissions.Rows[i].Filter)
			}
		}
		return changed
	}
	rewrite(t)
	for _, o := range others {
		if rewrite(o) {
			if err := s.saveMeta(ctx, o, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkDependents refuses a change to t that would break a formula or row
// rule of another table reading t's fields through a link.
func (s *Service) checkDependents(ctx context.Context, t *Table) error {
	others, err := s.linkingTables(ctx, t)
	if err != nil {
		return err
	}
	lookup := s.lookup(ctx, t.AppID)
	withT := func(id string) (*Table, error) {
		if id == t.ID {
			return t, nil
		}
		return lookup(id)
	}
	for _, o := range others {
		c, err := compile(o, withT)
		if err == nil {
			err = checkPermissions(c, &o.Permissions)
		}
		if err != nil {
			var v *ValidationError
			if errors.As(err, &v) {
				what := "a row rule"
				if v.Field != "" && v.Field != "permissions" {
					what = "the formula [" + v.Field + "]"
				}
				return invalid("", "%s in table %s reads this table and would stop working (%s); change it first", what, o.Name, v.Message)
			}
			return err
		}
	}
	return nil
}

// checkLinkedReads refuses filters that read rows linked through a table
// the actor may not read in full: the filter could reveal what is in rows
// their access rules hide.
func checkLinkedReads(c *compiled, ch *formula.Checked, actor rbac.Actor) error {
	if actor.Builder || actor.Role == rbac.Admin {
		return nil
	}
	for _, name := range ch.Links() {
		f := c.FieldByName(name)
		if f == nil {
			continue
		}
		tc := c.targets[f.ID]
		if tc == nil || !actor.Has(tc.Permissions.Read) || tc.RowRuleFor(actor) != "" {
			return invalid("filter", "this filter reads the rows linked through [%s], and you cannot see all of them; a builder can add a formula field for it instead", f.Name)
		}
	}
	return nil
}

// linkLoader loads, for a batch of rows, the rows linked to them. It loads
// one link field for the whole batch the first time a formula asks.
type linkLoader struct {
	s      *Service
	ctx    context.Context
	ex     store.Execer
	db     *store.DB
	c      *compiled
	rows   []*Row
	loaded map[string]map[int64][]*Row // link field id -> row id -> linked rows
	errs   map[string]error
}

func (l *linkLoader) linked(f *Field, id int64) ([]*Row, *compiled, error) {
	tc := l.c.targets[f.ID]
	if tc == nil {
		return nil, nil, fmt.Errorf("the table [%s] links to cannot be read here", f.Name)
	}
	if err := l.errs[f.ID]; err != nil {
		return nil, nil, err
	}
	if _, ok := l.loaded[f.ID]; !ok {
		if err := l.load(f, tc); err != nil {
			l.errs[f.ID] = err
			return nil, nil, err
		}
	}
	return l.loaded[f.ID][id], tc, nil
}

func (l *linkLoader) load(f *Field, tc *compiled) error {
	refs := func(r *Row) []LinkRef {
		switch v := r.Values[f.ID].(type) {
		case LinkRef:
			return []LinkRef{v}
		case []LinkRef:
			return v
		}
		return nil
	}
	need := map[int64]bool{}
	for _, r := range l.rows {
		for _, ref := range refs(r) {
			need[ref.ID] = true
		}
	}
	var ids []int64
	for id := range need {
		ids = append(ids, id)
	}
	byID := map[int64]*Row{}
	for _, chunk := range chunks(ids, 500) {
		rows, err := selectRows(l.ctx, l.ex, l.db, tc, "_id IN "+inList(len(chunk)), toArgs(chunk), "")
		if err != nil {
			return err
		}
		if err := l.s.hydrate(l.ctx, l.ex, l.db, tc, rows, 0); err != nil {
			return err
		}
		for _, r := range rows {
			byID[r.ID] = r
		}
	}
	out := map[int64][]*Row{}
	for _, r := range l.rows {
		for _, ref := range refs(r) {
			if lr := byID[ref.ID]; lr != nil {
				out[r.ID] = append(out[r.ID], lr)
			}
		}
	}
	l.loaded[f.ID] = out
	return nil
}

// Linked gives formulas the rows linked through [name].
func (e *rowEnv) Linked(name string) ([]formula.Env, error) {
	f := e.c.FieldByName(name)
	if f == nil || f.Type != TypeLink {
		return nil, fmt.Errorf("[%s] is not a link", name)
	}
	if e.r.links == nil {
		return nil, fmt.Errorf("linked rows are not loaded here")
	}
	rows, tc, err := e.r.links.linked(f, e.r.ID)
	if err != nil {
		return nil, err
	}
	out := make([]formula.Env, len(rows))
	for i, r := range rows {
		out[i] = newRowEnv(tc, r, e.actor, e.now)
	}
	return out, nil
}

// LinkSQL gives the subquery over rows linked through [name], one link
// deep: the linked table's own links are not followed.
func (r resolver) LinkSQL(name, alias string) (string, string, []any, formula.SQLResolver, bool) {
	if r.self != "" || r.db == nil {
		return "", "", nil, nil, false
	}
	f := r.c.FieldByName(name)
	if f == nil || f.Type != TypeLink {
		return "", "", nil, nil, false
	}
	tc := r.c.targets[f.ID]
	if tc == nil {
		return "", "", nil, nil, false
	}
	outer := r.db.Table(r.c.sqlName())
	target := resolver{c: tc, dialect: r.dialect, env: r.env, depth: r.depth + 1, db: r.db, self: alias}
	if f.LinkMany {
		j := alias + "_j"
		from := fmt.Sprintf("%s AS %s JOIN %s AS %s ON %s.dst = %s._id", r.db.Table(tc.sqlName()), alias, r.db.Table(joinName(f)), j, j, alias)
		return from, j + ".src = " + outer + "._id", nil, target, true
	}
	from := r.db.Table(tc.sqlName()) + " AS " + alias
	return from, alias + "._id = " + outer + "." + store.Q(f.Column), nil, target, true
}
