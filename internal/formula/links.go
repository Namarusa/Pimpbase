package formula

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"
)

// Formulas can read rows linked to the current row, one link deep:
//
//	[Customer].[City]               a field of the linked row
//	[Lines].[Amount]                a list, when the link holds many rows
//	SUMOF([Lines], [Qty] * [Price]) a rollup: the expression runs on each
//	COUNTOF([Lines], [Paid])        linked row, the condition picks rows
//
// Both forms compile to SQL subqueries when the parts do, so they filter
// and sort in the database.

// LinkField is [Link].[Field].
type LinkField struct {
	span
	Link, Field string
	// many is set by the checker when the link holds many rows.
	many bool
	// Where [Link] ends and [Field] starts in the source.
	linkEnd, fieldStart int
}

// LinkScope is a Scope whose fields may link to rows of other tables.
type LinkScope interface {
	Scope
	// Link returns the scope of the table the link field [name] points to
	// and whether it holds many rows; ok is false when [name] is no link.
	Link(name string) (target Scope, many bool, ok bool)
}

// LinkEnv is an Env whose row may have linked rows.
type LinkEnv interface {
	Env
	// Linked returns the rows linked through [name].
	Linked(name string) ([]Env, error)
}

// LinkResolver is an SQLResolver that can reach linked rows.
type LinkResolver interface {
	SQLResolver
	// LinkSQL returns the FROM and WHERE of a subquery over the rows
	// linked through [name] to the outer row, with alias naming the linked
	// table inside it, and a resolver for the linked table's fields.
	LinkSQL(name, alias string) (from, where string, args []any, target SQLResolver, ok bool)
}

// rollups: name -> how many arguments after the link, and whether the
// first one is a value (the others count rows).
var rollupDocs = []struct {
	name, sig, doc string
	value          bool
}{
	{"SUMOF", "SUMOF(link, value, condition)", "Adds the value over the linked rows, only those where the condition is true when one is given.", true},
	{"AVERAGEOF", "AVERAGEOF(link, value, condition)", "Mean of the value over the linked rows, ignoring blanks.", true},
	{"MINOF", "MINOF(link, value, condition)", "Smallest value over the linked rows.", true},
	{"MAXOF", "MAXOF(link, value, condition)", "Largest value over the linked rows.", true},
	{"COUNTOF", "COUNTOF(link, condition)", "Number of linked rows, only those where the condition is true when one is given.", false},
}

func rollupFunctions() map[string]*function {
	out := map[string]*function{}
	for _, r := range rollupDocs {
		r := r
		max := 2
		if r.value {
			max = 3
		}
		params := make([]Type, max)
		for i := range params {
			params[i] = Any
		}
		out[r.name] = &function{category: "Links", sig: r.sig, doc: r.doc + " Inside, [Field] names a field of the linked table.",
			params: params, min: max - 1, ret: Number, rollup: true,
			lazy: func(ev *evaluator, args []Node) (any, error) { return ev.rollup(r.name, args) }}
	}
	return out
}

// checkLink types [Link].[Field].
func (ch *checker) checkLink(n *LinkField) Type {
	target, many, ok := ch.link(n, n.Link)
	if !ok {
		return Any
	}
	t, ok := target.FieldType(n.Field)
	if !ok {
		ch.fail(n, "the table [%s] links to has no field named [%s]", n.Link, n.Field)
		return Any
	}
	n.many = many
	if many {
		ch.linkTypes[n] = t
		return List
	}
	return t
}

func (ch *checker) link(n Node, name string) (Scope, bool, bool) {
	ls, ok := ch.scope.(LinkScope)
	if !ok {
		ch.fail(n, "linked rows cannot be read here")
		return nil, false, false
	}
	target, many, ok := ls.Link(name)
	if !ok {
		if _, isField := ch.scope.FieldType(name); isField {
			ch.fail(n, "[%s] does not link to another table", name)
		} else {
			ch.fail(n, "there is no field named [%s]", name)
		}
		return nil, false, false
	}
	return target, many, true
}

// checkRollup types SUMOF(link, value, condition) and friends: arguments
// after the link are checked against the linked table.
func (ch *checker) checkRollup(n *Call, f *function) Type {
	if len(n.Args) < f.min || len(n.Args) > len(f.params) {
		ch.fail(n, "%s takes %s", n.Name, f.arity())
		return Number
	}
	ref, ok := n.Args[0].(*FieldRef)
	if !ok {
		ch.fail(n.Args[0], "the first argument of %s must be a link field such as [Lines]", n.Name)
		return Number
	}
	ch.types[ref] = List
	target, _, ok := ch.link(ref, ref.Name)
	if !ok {
		return Number
	}
	outer := ch.scope
	ch.scope = target
	defer func() { ch.scope = outer }()
	rest := n.Args[1:]
	if n.Name != "COUNTOF" {
		if t := ch.check(rest[0]); !accepts(Number, t) {
			ch.fail(rest[0], "the value of %s must be a number, got %s", n.Name, t)
		}
		rest = rest[1:]
	}
	if len(rest) > 0 {
		if t := ch.check(rest[0]); !accepts(Bool, t) {
			ch.fail(rest[0], "the condition of %s must be true or false, got %s", n.Name, t)
		}
	}
	return Number
}

func (ev *evaluator) linked(n Node, name string) ([]Env, error) {
	le, ok := ev.env.(LinkEnv)
	if !ok {
		return nil, ev.fail(n, fmt.Errorf("linked rows cannot be read here"))
	}
	rows, err := le.Linked(name)
	if err != nil {
		return nil, ev.fail(n, err)
	}
	return rows, nil
}

func (ev *evaluator) linkField(n *LinkField) (any, error) {
	rows, err := ev.linked(n, n.Link)
	if err != nil {
		return nil, err
	}
	if !n.many {
		if len(rows) == 0 {
			return nil, nil
		}
		return rows[0].Field(n.Field), nil
	}
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r.Field(n.Field)
	}
	return out, nil
}

func (ev *evaluator) rollup(name string, args []Node) (any, error) {
	rows, err := ev.linked(args[0], args[0].(*FieldRef).Name)
	if err != nil {
		return nil, err
	}
	value, cond := Node(nil), Node(nil)
	rest := args[1:]
	if name != "COUNTOF" {
		value, rest = rest[0], rest[1:]
	}
	if len(rest) > 0 {
		cond = rest[0]
	}
	var nums []float64
	count := 0
	for _, r := range rows {
		inner := &evaluator{env: r, c: ev.c}
		if cond != nil {
			c, err := inner.eval(cond)
			if err != nil {
				return nil, err
			}
			if ok, err := toBool(c); err != nil || !ok {
				if err != nil {
					return nil, err
				}
				continue
			}
		}
		count++
		if value == nil {
			continue
		}
		v, err := inner.eval(value)
		if err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		x, err := toNumber(v)
		if err != nil {
			return nil, err
		}
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			nums = append(nums, x)
		}
	}
	switch name {
	case "COUNTOF":
		return float64(count), nil
	case "SUMOF":
		s := 0.0
		for _, x := range nums {
			s += x
		}
		return s, nil
	}
	if len(nums) == 0 {
		return nil, nil
	}
	switch name {
	case "AVERAGEOF":
		s := 0.0
		for _, x := range nums {
			s += x
		}
		return s / float64(len(nums)), nil
	case "MINOF":
		sort.Float64s(nums)
		return nums[0], nil
	}
	sort.Float64s(nums)
	return nums[len(nums)-1], nil
}

var aliases atomic.Uint64

// alias names a linked table in a subquery, uniquely.
func (comp *compiler) alias() string {
	return fmt.Sprintf("l%d", aliases.Add(1))
}

// within returns a compiler for expressions about the linked table.
func (comp *compiler) within(res SQLResolver) *compiler {
	inner := *comp
	inner.res = res
	return &inner
}

func (comp *compiler) linkSQL(name string) (from, where string, args []any, inner *compiler, ok bool) {
	lr, isLink := comp.res.(LinkResolver)
	if !isLink {
		return "", "", nil, nil, false
	}
	from, where, args, target, ok := lr.LinkSQL(name, comp.alias())
	if !ok {
		return "", "", nil, nil, false
	}
	return from, where, args, comp.within(target), true
}

// linkFieldSQL compiles [Link].[Field] of a link to one row.
func (comp *compiler) linkFieldSQL(n *LinkField, t Type) (sqlExpr, bool) {
	if n.many {
		return sqlExpr{}, false
	}
	from, where, args, inner, ok := comp.linkSQL(n.Link)
	if !ok {
		return sqlExpr{}, false
	}
	s, fargs, ok := inner.res.FieldSQL(n.Field)
	if !ok {
		return sqlExpr{}, false
	}
	value := sqlExpr{s, fargs, true}
	if t == Bool {
		value = cat(true, "(", value, " <> 0)")
	}
	return cat(true, "(SELECT ", value, " FROM "+from+" WHERE ", sqlExpr{where, args, false}, ")"), true
}

// rollupSQL compiles a rollup to a subquery. value and cond are compiled
// by the compiler for the linked table; either may be nil.
func (comp *compiler) rollupSQL(name, link string, value, cond func(*compiler) (sqlExpr, bool)) (sqlExpr, bool) {
	from, where, args, inner, ok := comp.linkSQL(link)
	if !ok {
		return sqlExpr{}, false
	}
	agg := map[string]string{"SUMOF": "SUM", "AVERAGEOF": "AVG", "MINOF": "MIN", "MAXOF": "MAX", "COUNTOF": "COUNT"}[name]
	sel := sqlExpr{"*", nil, false}
	if value != nil {
		if sel, ok = value(inner); !ok {
			return sqlExpr{}, false
		}
	}
	w := sqlExpr{where, args, false}
	if cond != nil {
		c, ok := cond(inner)
		if !ok {
			return sqlExpr{}, false
		}
		w = cat(false, w, " AND ", inner.zero(c, Bool))
	}
	sub := cat(true, "(SELECT "+agg+"(", sel, ") FROM "+from+" WHERE ", w, ")")
	switch name {
	case "SUMOF", "COUNTOF":
		return cat(false, "CAST(COALESCE(", sub, ", 0) AS "+comp.cast(Number)+")"), true
	}
	return cat(true, "CAST(", sub, " AS "+comp.cast(Number)+")"), true
}

// callLinkSQL compiles rollups, and SUM, AVERAGE, MIN, MAX and COUNT over
// [Link].[Field] or a link itself. ok is false when n is neither.
func (comp *compiler) callLinkSQL(n *Call) (e sqlExpr, handled, ok bool) {
	f := functions[n.Name]
	if f.rollup {
		ref := n.Args[0].(*FieldRef)
		rest := n.Args[1:]
		var value, cond func(*compiler) (sqlExpr, bool)
		if n.Name != "COUNTOF" {
			v := rest[0]
			value = func(in *compiler) (sqlExpr, bool) { return in.node(v) }
			rest = rest[1:]
		}
		if len(rest) > 0 {
			c := rest[0]
			cond = func(in *compiler) (sqlExpr, bool) { return in.node(c) }
		}
		e, ok := comp.rollupSQL(n.Name, ref.Name, value, cond)
		return e, true, ok
	}
	agg := map[string]string{"SUM": "SUMOF", "AVERAGE": "AVERAGEOF", "MIN": "MINOF", "MAX": "MAXOF", "COUNT": "COUNTOF"}[n.Name]
	if agg == "" || len(n.Args) != 1 {
		return sqlExpr{}, false, false
	}
	switch a := n.Args[0].(type) {
	case *LinkField:
		if !a.many {
			return sqlExpr{}, false, false
		}
		field := a.Field
		value := func(in *compiler) (sqlExpr, bool) {
			s, args, ok := in.res.FieldSQL(field)
			return sqlExpr{s, args, true}, ok
		}
		if agg == "COUNTOF" {
			// COUNT skips blanks; "" is blank for text.
			cond := func(in *compiler) (sqlExpr, bool) {
				s, args, ok := in.res.FieldSQL(field)
				return cat(false, "(COALESCE(CAST(", sqlExpr{s, args, true}, " AS TEXT), '') <> '')"), ok
			}
			e, ok := comp.rollupSQL(agg, a.Link, nil, cond)
			return e, true, ok
		}
		if comp.linkedType(a) != Number {
			return sqlExpr{}, true, false
		}
		e, ok := comp.rollupSQL(agg, a.Link, value, nil)
		return e, true, ok
	case *FieldRef:
		if agg != "COUNTOF" {
			return sqlExpr{}, false, false
		}
		if lr, ok := comp.res.(LinkResolver); ok {
			if _, _, _, _, isLink := lr.LinkSQL(a.Name, "probe"); isLink {
				e, ok := comp.rollupSQL(agg, a.Name, nil, nil)
				return e, true, ok
			}
		}
	}
	return sqlExpr{}, false, false
}

// linkedType is the type of the field a many link reads, which the
// checker only records as List.
func (comp *compiler) linkedType(n *LinkField) Type {
	if t, ok := comp.c.linkTypes[n]; ok {
		return t
	}
	return Any
}

// UsesLinks reports whether the formula reads linked rows.
func (c *Checked) UsesLinks() bool { return len(c.Links()) > 0 }

// Links returns the link fields whose rows the formula reads.
func (c *Checked) Links() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if k := strings.ToLower(name); !seen[k] {
			seen[k] = true
			out = append(out, name)
		}
	}
	Walk(c.Root, func(n Node) {
		switch n := n.(type) {
		case *LinkField:
			add(n.Link)
		case *Call:
			if f := functions[n.Name]; f != nil && f.rollup && len(n.Args) > 0 {
				if ref, ok := n.Args[0].(*FieldRef); ok {
					add(ref.Name)
				}
			}
		}
	})
	return out
}
