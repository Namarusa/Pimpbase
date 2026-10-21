package tables

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/formula"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/store"
)

// AggQuery groups the rows of a table and reduces each group to numbers,
// for charts and stat cards. It sees exactly the rows ListRows would show
// the same actor with the same filter.
type AggQuery struct {
	Filter string `json:"filter"`
	// GroupBy is a field name; empty puts every row in one group.
	GroupBy string `json:"groupBy"`
	// DateGroup buckets date fields by "day", "month" (the default) or
	// "year".
	DateGroup string     `json:"dateGroup"`
	Values    []AggValue `json:"values"`
	// SortBy "value" orders groups by their first value, largest first;
	// otherwise groups come in key order.
	SortBy string `json:"sortBy"`
	// Limit caps the groups returned: 200 by default, at most 1000.
	Limit int `json:"limit"`
}

// AggValue is one number per group: "count" counts rows, "sum", "avg",
// "min" and "max" reduce a number field, skipping blanks.
type AggValue struct {
	Op    string `json:"op"`
	Field string `json:"field"`
}

// AggGroup is one group: its key and one value per AggValue.
type AggGroup struct {
	// Key is the group's value of the GroupBy field: text, a number, true
	// or false, nil for blank, or the start of a date bucket such as
	// "2026-10" (month), "2026-10-20" (day) or "2026" (year).
	Key    any       `json:"key"`
	Values []float64 `json:"values"`
	Count  int       `json:"count"`
}

// AggResult holds the groups. Truncated says more groups exist than Limit.
type AggResult struct {
	Groups    []AggGroup `json:"groups"`
	Rows      int        `json:"rows"`
	Truncated bool       `json:"truncated"`
	// Engine says whether the grouping ran in SQL or in Go.
	Engine string `json:"engine"`
}

var aggOps = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}

// numeric reports whether a field holds numbers, which sum and friends
// need.
func numeric(f *Field) bool {
	switch f.Type {
	case TypeNumber, TypeAutoNumber:
		return true
	case TypeFormula:
		return f.ResultType == formula.Number
	}
	return false
}

// Aggregate groups and reduces rows in the database when the filter, the
// actor's row rule, the grouping and the values all have an SQL form, and
// in Go otherwise (up to MaxScan matching rows).
func (s *Service) Aggregate(ctx context.Context, actor rbac.Actor, appID, tableID string, q AggQuery) (*AggResult, error) {
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
	if q.Limit <= 0 {
		q.Limit = 200
	}
	if q.Limit > 1000 {
		q.Limit = 1000
	}
	if len(q.Values) == 0 {
		q.Values = []AggValue{{Op: "count"}}
	}
	if len(q.Values) > 10 {
		return nil, invalid("values", "ask for at most 10 values at once")
	}
	switch q.DateGroup {
	case "":
		q.DateGroup = "month"
	case "day", "month", "year":
	default:
		return nil, invalid("dateGroup", "group dates by day, month or year")
	}
	var group *Field
	if q.GroupBy != "" {
		if group = c.FieldByName(q.GroupBy); group == nil {
			return nil, invalid("groupBy", "there is no field named %q", q.GroupBy)
		}
		if group.Type == TypeAttachment {
			return nil, invalid("groupBy", "rows cannot be grouped by files")
		}
	}
	valueFields := make([]*Field, len(q.Values))
	for i, v := range q.Values {
		if !aggOps[v.Op] {
			return nil, invalid("values", "%q is not count, sum, avg, min or max", v.Op)
		}
		if v.Op == "count" {
			continue
		}
		f := c.FieldByName(v.Field)
		if f == nil {
			return nil, invalid("values", "there is no field named %q", v.Field)
		}
		valueFields[i] = f
	}
	filters, err := s.filters(c, actor, q.Filter)
	if err != nil {
		return nil, err
	}
	now := s.now()
	env := newRowEnv(c, &Row{Values: map[string]any{}}, actor, now)
	if where, args, ok := pushdown(db, c, env, filters); ok {
		// Link labels depend on what the actor may read, so the key holds
		// their roles as well as the query and the row rule's arguments.
		key := fmt.Sprintf("agg|%+v|%s|%v|%v|%v", q, where, args, actor.Chain(), actor.Builder)
		type answer struct {
			res *AggResult
			ok  bool
		}
		a, err := remember(s, c, key, func() (answer, error) {
			res, ok, err := s.aggregateSQL(ctx, db, c, actor, env, where, args, group, valueFields, q)
			return answer{res, ok}, err
		})
		if err != nil || a.ok {
			return a.res, err
		}
	}
	rows, err := s.scan(ctx, db, c, actor, filters, now)
	if err != nil {
		return nil, err
	}
	if err := s.maskLinks(ctx, db, db, c, rows, actor); err != nil {
		return nil, err
	}
	return s.aggregateGo(c, actor, now, rows, group, valueFields, q), nil
}

// groupSQL is the SQL of a group key, or ok=false.
func groupSQL(db *store.DB, c *compiled, env formula.Env, f *Field, dateGroup string) (expr string, args []any, ok bool) {
	res := c.resolver(db, env)
	switch {
	case f.Type == TypeMultiSelect:
		// The first option, as charts have always grouped them.
		if db.Dialect == store.Postgres {
			return "(" + store.Q(f.Column) + "::jsonb ->> 0)", nil, true
		}
		return "json_extract(" + store.Q(f.Column) + ", '$[0]')", nil, true
	case f.Type == TypeLink && !f.LinkMany:
		return store.Q(f.Column), nil, true
	case f.Type == TypeLink || f.Type == TypeAttachment:
		return "", nil, false
	}
	expr, args, ok = res.FieldSQL(f.Name)
	if !ok {
		return "", nil, false
	}
	if formulaType(f) == formula.Date {
		n := map[string]int{"day": 10, "month": 7, "year": 4}[dateGroup]
		expr = fmt.Sprintf("SUBSTR(%s, 1, %d)", expr, n)
	}
	return expr, args, true
}

func (s *Service) aggregateSQL(ctx context.Context, db *store.DB, c *compiled, actor rbac.Actor, env formula.Env, where string, whereArgs []any, group *Field, valueFields []*Field, q AggQuery) (*AggResult, bool, error) {
	res := c.resolver(db, env)
	// The inner query groups; the outer one orders, so that its ORDER BY
	// may use the column names on both databases.
	sel := []string{"COUNT(*) AS n"}
	var args []any
	if group != nil {
		key, keyArgs, ok := groupSQL(db, c, env, group, q.DateGroup)
		if !ok {
			return nil, false, nil
		}
		sel = append([]string{key + " AS k"}, sel...)
		args = append(args, keyArgs...)
	}
	for i, v := range q.Values {
		if v.Op == "count" {
			sel = append(sel, fmt.Sprintf("COUNT(*) AS v%d, COUNT(*) AS c%d", i, i))
			continue
		}
		f := valueFields[i]
		if !numeric(f) {
			return nil, false, nil
		}
		expr, exprArgs, ok := res.FieldSQL(f.Name)
		if !ok {
			return nil, false, nil
		}
		sel = append(sel, fmt.Sprintf("%s(%s) AS v%d, COUNT(%s) AS c%d", strings.ToUpper(v.Op), expr, i, expr, i))
		args = append(append(args, exprArgs...), exprArgs...)
	}
	query := "SELECT " + strings.Join(sel, ", ") + " FROM " + db.Table(c.sqlName())
	if where != "" {
		query += " WHERE " + where
		args = append(args, whereArgs...)
	}
	if group != nil {
		// Grouping by position keeps the key's arguments from being bound
		// twice.
		query = "SELECT * FROM (" + query + " GROUP BY 1) g ORDER BY "
		if q.SortBy == "value" {
			query += "v0 DESC, "
		}
		query += "(k IS NULL), k"
		if db.Dialect == store.Postgres && (formulaType(group) == formula.Text || group.Type == TypeMultiSelect) && group.Type != TypeLink {
			query += ` COLLATE "C"`
		}
		query += fmt.Sprintf(" LIMIT %d", q.Limit+1)
	}
	rs, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rs.Close()
	cols, _ := rs.Columns()
	var parts []part
	for rs.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, false, err
		}
		at := 0
		p := part{values: make([]float64, len(q.Values)), n: make([]int, len(q.Values))}
		if group != nil {
			p.key = raw[0]
			at = 1
		}
		p.count = int(toFloat(raw[at]))
		for i := range q.Values {
			p.values[i] = toFloat(raw[at+1+2*i])
			p.n[i] = int(toFloat(raw[at+2+2*i]))
		}
		parts = append(parts, p)
	}
	if err := rs.Err(); err != nil {
		return nil, false, err
	}
	out := &AggResult{Groups: []AggGroup{}, Engine: "sql"}
	if group == nil {
		g := AggGroup{Values: make([]float64, len(q.Values))}
		if len(parts) > 0 {
			g.Count, g.Values = parts[0].count, parts[0].values
		}
		out.Groups, out.Rows = []AggGroup{g}, g.Count
		return out, true, nil
	}
	for _, p := range parts {
		out.Rows += p.count
	}
	if len(parts) > q.Limit {
		parts, out.Truncated = parts[:q.Limit], true
		// Rows counts every matching row, not just those in the groups shown.
		if out.Rows, err = s.count(ctx, db, c, where, whereArgs); err != nil {
			return nil, false, err
		}
	}
	if group.Type == TypeLink {
		if err := s.linkKeys(ctx, db, c, actor, group, parts); err != nil {
			return nil, false, err
		}
		parts = merge(parts, q.Values)
		if q.SortBy == "value" {
			sort.SliceStable(parts, func(i, j int) bool { return parts[i].values[0] > parts[j].values[0] })
		} else {
			sort.SliceStable(parts, func(i, j int) bool { return keyLess(parts[i].key, parts[j].key) })
		}
	} else {
		for i := range parts {
			parts[i].key = sqlKey(group, parts[i].key)
		}
	}
	for _, p := range parts {
		out.Groups = append(out.Groups, AggGroup{Key: p.key, Count: p.count, Values: p.values})
	}
	return out, true, nil
}

// part is a group as SQL returns it; n counts the non-blank values behind
// each value, so groups can be merged.
type part struct {
	key    any
	count  int
	values []float64
	n      []int
}

// merge joins parts with equal keys, such as two linked rows with one
// label.
func merge(parts []part, ops []AggValue) []part {
	var out []part
	at := map[string]int{}
	for _, p := range parts {
		k := fmt.Sprintf("%T:%v", p.key, p.key)
		i, ok := at[k]
		if !ok {
			at[k] = len(out)
			out = append(out, p)
			continue
		}
		m := &out[i]
		for j, v := range ops {
			a, b := m.values[j], p.values[j]
			switch {
			case p.n[j] == 0:
			case m.n[j] == 0:
				m.values[j] = b
			case v.Op == "count" || v.Op == "sum":
				m.values[j] = a + b
			case v.Op == "avg":
				m.values[j] = (a*float64(m.n[j]) + b*float64(p.n[j])) / float64(m.n[j]+p.n[j])
			case v.Op == "min":
				m.values[j] = math.Min(a, b)
			case v.Op == "max":
				m.values[j] = math.Max(a, b)
			}
			m.n[j] += p.n[j]
		}
		m.count += p.count
	}
	return out
}

func keyLess(a, b any) bool {
	if (a == nil) != (b == nil) {
		return b == nil
	}
	return compareKeys(a, b) < 0
}

// linkKeys turns linked row ids into their labels, hiding those the actor
// may not see.
func (s *Service) linkKeys(ctx context.Context, db *store.DB, c *compiled, actor rbac.Actor, f *Field, parts []part) error {
	need := map[int64]bool{}
	for _, p := range parts {
		if p.key != nil {
			need[int64(toFloat(p.key))] = true
		}
	}
	labels, err := s.labels(ctx, db, db, c.AppID, f.LinkTable, need, c)
	if err != nil {
		return err
	}
	rows := make([]*Row, len(parts))
	for i, p := range parts {
		rows[i] = &Row{Values: map[string]any{}}
		if p.key == nil {
			continue
		}
		id := int64(toFloat(p.key))
		if lbl, ok := labels[id]; ok {
			rows[i].Values[f.ID] = LinkRef{ID: id, Label: lbl}
		}
	}
	if err := s.maskLinks(ctx, db, db, c, rows, actor); err != nil {
		return err
	}
	for i, r := range rows {
		parts[i].key = linkKey(r.Values[f.ID])
	}
	return nil
}

// HiddenLabel is the group key of linked rows the reader may not see.
const HiddenLabel = "Hidden row"

func linkKey(v any) any {
	switch l := v.(type) {
	case LinkRef:
		if l.Hidden {
			return HiddenLabel
		}
		return l.Label
	case []LinkRef:
		if len(l) == 0 {
			return nil
		}
		return linkKey(l[0])
	}
	return nil
}

// sqlKey converts a scanned key to its JSON value.
func sqlKey(f *Field, v any) any {
	if b, ok := v.([]byte); ok {
		v = string(b)
	}
	if v == nil {
		return nil
	}
	switch formulaType(f) {
	case formula.Number:
		return toFloat(v)
	case formula.Bool:
		switch x := v.(type) {
		case bool:
			return x
		default:
			return toFloat(x) != 0
		}
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	case int:
		return float64(x)
	case []byte:
		f, _ := strconv.ParseFloat(string(x), 64)
		return f
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

// goKey is the group key of a loaded row, matching what SQL gives.
func goKey(env *rowEnv, f *Field, dateGroup string) any {
	v := env.r.Values[f.ID]
	if f.Type == TypeLink {
		return linkKey(v)
	}
	if f.Type == TypeFormula && f.FormulaMode == Dynamic {
		v = env.value(f)
	}
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return nil
		}
		return x[0]
	case time.Time:
		s := x.UTC().Format(formula.DateLayout)
		return s[:map[string]int{"day": 10, "month": 7, "year": 4}[dateGroup]]
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
	case string:
		if x == "" {
			return nil
		}
	}
	return v
}

type acc struct {
	key   any
	count int
	sum   []float64
	n     []int
	min   []float64
	max   []float64
}

func (s *Service) aggregateGo(c *compiled, actor rbac.Actor, now time.Time, rows []*Row, group *Field, valueFields []*Field, q AggQuery) *AggResult {
	groups := map[string]*acc{}
	var order []*acc
	for _, r := range rows {
		env := newRowEnv(c, r, actor, now)
		var key any
		if group != nil {
			key = goKey(env, group, q.DateGroup)
		}
		k := fmt.Sprintf("%T:%v", key, key)
		a := groups[k]
		if a == nil {
			n := len(q.Values)
			a = &acc{key: key, sum: make([]float64, n), n: make([]int, n), min: make([]float64, n), max: make([]float64, n)}
			groups[k] = a
			order = append(order, a)
		}
		a.count++
		for i, v := range q.Values {
			if v.Op == "count" {
				continue
			}
			x, ok := env.value(valueFields[i]).(float64)
			if !ok || math.IsNaN(x) || math.IsInf(x, 0) {
				continue
			}
			if a.n[i] == 0 || x < a.min[i] {
				a.min[i] = x
			}
			if a.n[i] == 0 || x > a.max[i] {
				a.max[i] = x
			}
			a.sum[i] += x
			a.n[i]++
		}
	}
	out := &AggResult{Groups: []AggGroup{}, Rows: len(rows), Engine: "go"}
	for _, a := range order {
		g := AggGroup{Key: a.key, Count: a.count, Values: make([]float64, len(q.Values))}
		for i, v := range q.Values {
			switch v.Op {
			case "count":
				g.Values[i] = float64(a.count)
			case "sum":
				g.Values[i] = a.sum[i]
			case "avg":
				if a.n[i] > 0 {
					g.Values[i] = a.sum[i] / float64(a.n[i])
				}
			case "min":
				g.Values[i] = a.min[i]
			case "max":
				g.Values[i] = a.max[i]
			}
		}
		out.Groups = append(out.Groups, g)
	}
	if group == nil {
		if len(out.Groups) == 0 {
			out.Groups = append(out.Groups, AggGroup{Values: make([]float64, len(q.Values))})
		}
		return out
	}
	sort.SliceStable(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if q.SortBy == "value" && a.Values[0] != b.Values[0] {
			return a.Values[0] > b.Values[0]
		}
		return keyLess(a.Key, b.Key)
	})
	if len(out.Groups) > q.Limit {
		out.Groups, out.Truncated = out.Groups[:q.Limit], true
	}
	return out
}
