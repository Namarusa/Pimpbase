package exttables

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/extdb"
	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/tables"
)

// Link makes a column of an external table a link: the column holds the
// key of a row of another table, external (its single-column primary
// key) or built-in (the row's _id). The linked table, when external,
// shows the rows pointing at each of its rows in a field of its own.
type Link struct {
	Column string `json:"column"`
	// Table is the linked table's id.
	Table string `json:"table"`
	// Reverse names the field of the linked table listing the rows that
	// point at a row; empty uses this table's name.
	Reverse string `json:"reverse,omitempty"`
	// Detected links come from a foreign key of the database.
	Detected bool `json:"detected,omitempty"`
}

// LinkRef is a linked row as rows show it: its key and label.
type LinkRef struct {
	ID    any    `json:"_id"`
	Label string `json:"label"`
	// Hidden marks a row the reader may not see; its label is blank.
	Hidden bool `json:"hidden,omitempty"`
}

// graph knows the tables of an app that links may point at.
type graph struct {
	ext     map[string]*Table
	all     []*Table
	builtin map[string]string // id to name
}

func (s *Service) graph(ctx context.Context, appID string) (*graph, error) {
	ts, err := s.List(ctx, appID)
	if err != nil {
		return nil, err
	}
	g := &graph{ext: map[string]*Table{}, all: ts, builtin: map[string]string{}}
	for _, t := range ts {
		g.ext[t.ID] = t
	}
	rows, err := s.store.Meta.Query(ctx, `SELECT id, name FROM app_tables WHERE app_id = ?`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		g.builtin[id] = name
	}
	return g, rows.Err()
}

// link is the link of a column whose table still exists, or nil.
func (g *graph) link(t *Table, column string) *Link {
	if g == nil {
		return nil
	}
	for i := range t.Links {
		l := &t.Links[i]
		if !strings.EqualFold(l.Column, column) {
			continue
		}
		if u := g.ext[l.Table]; u != nil && u.Key() != "" {
			return l
		}
		if _, ok := g.builtin[l.Table]; ok {
			return l
		}
	}
	return nil
}

// reverseLink is a field listing the rows of another external table whose
// column points at a row.
type reverseLink struct {
	name   string
	from   *Table
	column string
}

func (g *graph) reverse(t *Table) []reverseLink {
	if g == nil || t.Key() == "" {
		return nil
	}
	taken := map[string]bool{}
	for _, c := range t.Columns {
		taken[strings.ToLower(c.Name)] = true
	}
	from := append([]*Table{}, g.all...)
	sort.Slice(from, func(i, j int) bool { return from[i].Name < from[j].Name })
	var out []reverseLink
	for _, u := range from {
		for _, l := range u.Links {
			c := u.column(l.Column)
			if l.Table != t.ID || c == nil || c.Hidden || c.Binary {
				continue
			}
			name := strings.TrimSpace(l.Reverse)
			if name == "" {
				name = u.Name
			}
			if taken[strings.ToLower(name)] {
				name = u.Name + " (" + c.Name + ")"
			}
			for n := 2; taken[strings.ToLower(name)]; n++ {
				name = fmt.Sprintf("%s (%s) %d", u.Name, c.Name, n)
			}
			taken[strings.ToLower(name)] = true
			out = append(out, reverseLink{name: name, from: u, column: c.Name})
		}
	}
	return out
}

// checkLinks validates links set by a builder.
func (s *Service) checkLinks(ctx context.Context, t *Table, in []Link) ([]Link, error) {
	g, err := s.graph(ctx, t.AppID)
	if err != nil {
		return nil, err
	}
	out := []Link{}
	seen := map[string]bool{}
	for _, l := range in {
		c := t.column(l.Column)
		if c == nil || c.Binary {
			return nil, invalid("links", "there is no column named %s", l.Column)
		}
		if seen[strings.ToLower(c.Name)] {
			return nil, invalid("links", "%s links to two tables", c.Name)
		}
		seen[strings.ToLower(c.Name)] = true
		switch {
		case g.ext[l.Table] != nil:
			u := g.ext[l.Table]
			if u.Key() == "" {
				return nil, invalid("links", "%s cannot be linked to: it has no single-column primary key", u.Name)
			}
		case g.builtin[l.Table] != "":
		default:
			// Accept a table's name too.
			found := ""
			for _, u := range g.all {
				if strings.EqualFold(u.Name, l.Table) && u.Key() != "" {
					found = u.ID
				}
			}
			for id, name := range g.builtin {
				if strings.EqualFold(name, l.Table) {
					found = id
				}
			}
			if found == "" {
				return nil, invalid("links", "there is no table %s to link %s to", l.Table, c.Name)
			}
			l.Table = found
		}
		if strings.ContainsAny(l.Reverse, "[]") || strings.HasPrefix(strings.TrimSpace(l.Reverse), "_") {
			return nil, invalid("links", "a field name cannot contain [ or ] or start with _")
		}
		out = append(out, Link{Column: c.Name, Table: l.Table, Reverse: strings.TrimSpace(l.Reverse), Detected: l.Detected})
	}
	return out, nil
}

// detectLinks adds links for the foreign keys between a table and the
// other tables imported from the same data source, both ways.
func (s *Service) detectLinks(ctx context.Context, t *Table) error {
	ts, err := s.List(ctx, t.AppID)
	if err != nil {
		return err
	}
	points := func(c *Column, u *Table) bool {
		r := c.References
		return r != nil && u.DatasourceID == t.DatasourceID && u.Key() != "" &&
			strings.EqualFold(r.Table, u.Source) && strings.EqualFold(r.Column, u.Key()) &&
			(r.Schema == "" || u.Schema == "" || strings.EqualFold(r.Schema, u.Schema))
	}
	has := func(x *Table, column string) bool {
		for _, l := range x.Links {
			if strings.EqualFold(l.Column, column) {
				return true
			}
		}
		return false
	}
	changed := false
	for i := range t.Columns {
		c := &t.Columns[i]
		if c.References == nil || has(t, c.Name) {
			continue
		}
		for _, u := range ts {
			if points(c, u) {
				t.Links = append(t.Links, Link{Column: c.Name, Table: u.ID, Detected: true})
				changed = true
				break
			}
		}
	}
	if changed {
		if _, err := s.save(ctx, t); err != nil {
			return err
		}
	}
	for _, u := range ts {
		if u.ID == t.ID || u.DatasourceID != t.DatasourceID {
			continue
		}
		uc := false
		for i := range u.Columns {
			c := &u.Columns[i]
			r := c.References
			if r == nil || has(u, c.Name) || t.Key() == "" || !strings.EqualFold(r.Table, t.Source) || !strings.EqualFold(r.Column, t.Key()) ||
				(r.Schema != "" && t.Schema != "" && !strings.EqualFold(r.Schema, t.Schema)) {
				continue
			}
			u.Links = append(u.Links, Link{Column: c.Name, Table: t.ID, Detected: true})
			uc = true
		}
		if uc {
			if _, err := s.save(ctx, u); err != nil {
				return err
			}
		}
	}
	return nil
}

// keyText gives a key value as text, so keys read from different tables
// compare equal: 3 and 3.0 both give "3".
func keyText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	case time.Time:
		return x.UTC().Format(formula.DateLayout)
	case string:
		return x
	}
	return fmt.Sprint(v)
}

// label is the text a row is shown by.
func (rc *rowsCtx) label(r map[string]any) string {
	if c := rc.t.column(rc.t.PrimaryField); c != nil {
		if s := keyText(r[c.Name]); s != "" {
			return s
		}
	}
	parts := make([]string, len(rc.tg.Keys))
	for i, k := range rc.tg.Keys {
		parts[i] = keyText(r[k])
	}
	return "#" + strings.Join(parts, "-")
}

// access reports whether the actor may read rows of a table, and the row
// rule they read it under.
func access(t *Table, actor rbac.Actor) (bool, *formula.Checked, error) {
	if !actor.Builder && !actor.Has(t.Permissions.Read) {
		return false, nil, nil
	}
	rule, err := t.rule(actor)
	return true, rule, err
}

// fetch reads the rows whose column holds one of the values.
func (rc *rowsCtx) fetch(ctx context.Context, column string, values []any, limit int) ([]map[string]any, error) {
	var out []map[string]any
	for start := 0; start < len(values); start += 500 {
		end := min(start+500, len(values))
		got, err := rc.rows.Select(ctx, rc.tg, extdb.Select{Where: rc.rows.In(rc.tg, column, values[start:end]), Limit: limit})
		if err != nil {
			return nil, fail(err)
		}
		out = append(out, got...)
	}
	return out, nil
}

// maxReverse caps the rows a reverse link field reads for one page.
const maxReverse = 5000

// decorate sets the link fields of rows read: raws are the rows as read,
// outs the same rows as the API shows them.
func (rc *rowsCtx) decorate(ctx context.Context, actor rbac.Actor, raws, outs []map[string]any) error {
	if len(raws) == 0 || rc.g == nil {
		return nil
	}
	now := rc.s.now()
	for _, c := range rc.t.visible() {
		l := rc.g.link(rc.t, c.Name)
		if l == nil {
			continue
		}
		var vals []any
		seen := map[string]bool{}
		for _, r := range raws {
			if v := r[c.Name]; v != nil && !seen[keyText(v)] {
				seen[keyText(v)] = true
				vals = append(vals, v)
			}
		}
		refs := map[string]LinkRef{}
		if u := rc.g.ext[l.Table]; u != nil {
			urc, err := rc.s.openTable(ctx, u, rc.env, rc.g)
			if err != nil {
				return err
			}
			readable, rule, err := access(u, actor)
			if err != nil {
				return err
			}
			got, err := urc.fetch(ctx, u.Key(), vals, 0)
			if err != nil {
				return err
			}
			for _, r := range got {
				ref := LinkRef{ID: urc.rowID(r), Label: urc.label(r)}
				if !readable || (rule != nil && !matches([]*formula.Checked{rule}, &rowEnv{t: u, row: r, actor: actor, now: now})) {
					ref = LinkRef{ID: ref.ID, Hidden: true}
				}
				refs[keyText(r[u.Key()])] = ref
			}
		} else {
			var ids []int64
			for _, v := range vals {
				if f, ok := v.(float64); ok && f == math.Trunc(f) {
					ids = append(ids, int64(f))
				}
			}
			got, err := rc.s.tables.Labels(ctx, actor, rc.t.AppID, l.Table, ids)
			if err != nil {
				return err
			}
			for id, ref := range got {
				refs[strconv.FormatInt(id, 10)] = LinkRef{ID: float64(id), Label: ref.Label, Hidden: ref.Hidden}
			}
		}
		for i, r := range raws {
			v := r[c.Name]
			if v == nil {
				outs[i][c.Name] = nil
				continue
			}
			ref, ok := refs[keyText(v)]
			if !ok {
				ref = LinkRef{ID: v, Label: keyText(v)}
			}
			outs[i][c.Name] = ref
		}
	}
	for _, rev := range rc.g.reverse(rc.t) {
		key := rc.t.Key()
		var keys []any
		for _, r := range raws {
			if v := r[key]; v != nil {
				keys = append(keys, v)
			}
		}
		urc, err := rc.s.openTable(ctx, rev.from, rc.env, rc.g)
		if err != nil {
			return err
		}
		readable, rule, err := access(rev.from, actor)
		if err != nil {
			return err
		}
		got, err := urc.fetch(ctx, rev.column, keys, maxReverse)
		if err != nil {
			return err
		}
		by := map[string][]LinkRef{}
		for _, r := range got {
			ref := LinkRef{ID: urc.rowID(r), Label: urc.label(r)}
			if !readable || (rule != nil && !matches([]*formula.Checked{rule}, &rowEnv{t: rev.from, row: r, actor: actor, now: now})) {
				ref = LinkRef{ID: ref.ID, Hidden: true}
			}
			k := keyText(r[rev.column])
			by[k] = append(by[k], ref)
		}
		for i, r := range raws {
			list := by[keyText(r[key])]
			if list == nil {
				list = []LinkRef{}
			}
			outs[i][rev.name] = list
		}
	}
	return nil
}

// unwrapLink reads the row a link value names: a key, an object with _id,
// a LinkRef, or a list of one of these.
func unwrapLink(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		return unwrapLink(x["_id"])
	case LinkRef:
		return x.ID, nil
	case tables.LinkRef:
		return float64(x.ID), nil
	case []any:
		switch len(x) {
		case 0:
			return nil, nil
		case 1:
			return unwrapLink(x[0])
		}
		return nil, fmt.Errorf("links to one row only")
	case string:
		if strings.TrimSpace(x) == "" {
			return nil, nil
		}
	}
	return v, nil
}

// linkValue turns a value written to a link column into the linked row's
// key, as the column stores it. A text that is no key is looked up as a
// row's label, so exported CSV files import again.
func (rc *rowsCtx) linkValue(ctx context.Context, l *Link, c *Column, v any) (any, error) {
	v, err := unwrapLink(v)
	if err != nil || v == nil {
		return nil, err
	}
	if u := rc.g.ext[l.Table]; u != nil {
		urc, err := rc.s.openTable(ctx, u, rc.env, rc.g)
		if err != nil {
			return nil, err
		}
		kc := u.column(u.Key())
		if k, err := extdb.Write(v, &kc.Column); err == nil {
			got, err := urc.fetch(ctx, kc.Name, []any{k}, 1)
			if err != nil {
				return nil, err
			}
			if len(got) == 1 {
				return extdb.Write(got[0][kc.Name], &c.Column)
			}
		}
		if s, ok := v.(string); ok {
			if pc := u.column(u.PrimaryField); pc != nil {
				got, err := urc.fetch(ctx, pc.Name, []any{s}, 2)
				if err != nil {
					return nil, err
				}
				if len(got) == 1 {
					return extdb.Write(got[0][kc.Name], &c.Column)
				}
				if len(got) > 1 {
					return nil, fmt.Errorf("several rows of %s are labelled %q; give the key", u.Name, s)
				}
			}
		}
		return nil, fmt.Errorf("%s has no row %v", u.Name, v)
	}
	name := rc.g.builtin[l.Table]
	if f, ok := number(v); ok && f == math.Trunc(f) && f > 0 {
		got, err := rc.s.tables.Labels(ctx, rbac.Actor{Role: rbac.Admin, Builder: true}, rc.t.AppID, l.Table, []int64{int64(f)})
		if err != nil {
			return nil, err
		}
		if len(got) == 1 {
			return extdb.Write(f, &c.Column)
		}
	}
	if s, ok := v.(string); ok {
		found, err := rc.s.tables.FindByLabel(ctx, rc.t.AppID, l.Table, s, 2)
		if err != nil {
			return nil, err
		}
		if len(found) == 1 {
			return extdb.Write(float64(found[0]), &c.Column)
		}
		if len(found) > 1 {
			return nil, fmt.Errorf("several rows of %s are labelled %q; give the _id", name, s)
		}
	}
	return nil, fmt.Errorf("%s has no row %v", name, v)
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	if n, ok := v.(interface{ Float64() (float64, error) }); ok {
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
