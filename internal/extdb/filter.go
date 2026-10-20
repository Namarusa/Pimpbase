package extdb

import (
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/formula"
)

// sqlCompiler turns a condition formula into SQL for a table whose columns
// keep their database types. Constant parts (literals, Current.User,
// TODAY()) are worked out first and sent as parameters. Blanks follow the
// formula rules: a blank number is 0 and blank text is "" in comparisons,
// and nothing compares with a blank date. Anything else has no SQL form,
// and the caller filters in Go.
type sqlCompiler struct {
	d   *dialect
	t   *Target
	c   *formula.Checked
	env formula.Env
}

type sqlExpr struct {
	sql      string
	args     []any
	nullable bool
}

func join(nullable bool, parts ...any) sqlExpr {
	var b strings.Builder
	var args []any
	for _, p := range parts {
		switch p := p.(type) {
		case string:
			b.WriteString(p)
		case sqlExpr:
			b.WriteString(p.sql)
			args = append(args, p.args...)
		}
	}
	return sqlExpr{b.String(), args, nullable}
}

var (
	sqlTrue  = sqlExpr{sql: "1=1"}
	sqlFalse = sqlExpr{sql: "1=0"}
)

func (c *sqlCompiler) typ(n formula.Node) formula.Type { return c.c.TypeOf(n) }

// constant evaluates a part of the formula that does not read the row.
func (c *sqlCompiler) constant(n formula.Node) (any, bool) {
	if !formula.Constant(n) {
		return nil, false
	}
	v, err := c.c.EvalNode(n, c.env)
	if err != nil {
		return nil, false
	}
	return v, true
}

// column is the SQL of [name] and its column.
func (c *sqlCompiler) column(name string) (*Column, string, bool) {
	col := c.t.Column(name)
	if col == nil || col.Binary {
		return nil, "", false
	}
	return col, c.d.quote(col.Name), true
}

// pred compiles a condition; the result is never NULL.
func (c *sqlCompiler) pred(n formula.Node) (sqlExpr, bool) {
	if v, ok := c.constant(n); ok {
		if b, isBool := v.(bool); isBool && b {
			return sqlTrue, true
		}
		if v == nil || v == false {
			return sqlFalse, true
		}
		return sqlExpr{}, false
	}
	switch n := n.(type) {
	case *formula.FieldRef:
		col, q, ok := c.column(n.Name)
		if !ok || col.Type != "boolean" {
			return sqlExpr{}, false
		}
		return sqlExpr{sql: c.d.boolTrue(q)}, true
	case *formula.Unary:
		if n.Op != "NOT" {
			return sqlExpr{}, false
		}
		x, ok := c.pred(n.X)
		if !ok {
			return sqlExpr{}, false
		}
		return join(false, "(NOT ", x, ")"), true
	case *formula.Binary:
		switch n.Op {
		case "AND", "OR":
			l, ok := c.pred(n.L)
			if !ok {
				return sqlExpr{}, false
			}
			r, ok := c.pred(n.R)
			if !ok {
				return sqlExpr{}, false
			}
			return join(false, "(", l, " "+n.Op+" ", r, ")"), true
		case "=", "<>", "<", "<=", ">", ">=":
			return c.compare(n)
		}
	case *formula.In:
		return c.in(n)
	case *formula.Call:
		return c.call(n)
	}
	return sqlExpr{}, false
}

func (c *sqlCompiler) call(n *formula.Call) (sqlExpr, bool) {
	switch n.Name {
	case "AND", "OR":
		parts := []any{"("}
		for i, a := range n.Args {
			p, ok := c.pred(a)
			if !ok {
				return sqlExpr{}, false
			}
			if i > 0 {
				parts = append(parts, " "+n.Name+" ")
			}
			parts = append(parts, p)
		}
		return join(false, append(parts, ")")...), true
	case "NOT":
		if len(n.Args) != 1 {
			return sqlExpr{}, false
		}
		p, ok := c.pred(n.Args[0])
		if !ok {
			return sqlExpr{}, false
		}
		return join(false, "(NOT ", p, ")"), true
	case "ISBLANK":
		if len(n.Args) != 1 {
			return sqlExpr{}, false
		}
		x, ok := c.value(n.Args[0])
		if !ok {
			return sqlExpr{}, false
		}
		if c.typ(n.Args[0]) == formula.Text {
			return join(false, "(", x, " IS NULL OR ", x, " = '')"), true
		}
		return join(false, "(", x, " IS NULL)"), true
	case "CONTAINS", "STARTSWITH", "ENDSWITH":
		if len(n.Args) != 2 || c.typ(n.Args[0]) != formula.Text || c.typ(n.Args[1]) != formula.Text && c.typ(n.Args[1]) != formula.Null {
			return sqlExpr{}, false
		}
		a, ok := c.value(n.Args[0])
		if !ok {
			return sqlExpr{}, false
		}
		if v, ok := c.constant(n.Args[1]); ok {
			s := formula.ToText(v)
			if s == "" {
				return sqlTrue, true
			}
			pat := escapeLike(s)
			switch n.Name {
			case "CONTAINS":
				pat = "%" + pat + "%"
			case "STARTSWITH":
				pat += "%"
			default:
				pat = "%" + pat
			}
			return join(false, "(", a, " LIKE ? ESCAPE '!')", sqlExpr{args: []any{pat}}), true
		}
		if n.Name != "CONTAINS" || c.d.kind == "mssql" {
			return sqlExpr{}, false
		}
		b, ok := c.value(n.Args[1])
		if !ok {
			return sqlExpr{}, false
		}
		return sqlExpr{sql: c.d.contains("COALESCE("+a.sql+", '')", "COALESCE("+b.sql+", '')"), args: append(append([]any{}, a.args...), b.args...)}, true
	}
	return sqlExpr{}, false
}

// escapeLike escapes LIKE wildcards with "!".
func escapeLike(s string) string {
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_", "[", "![")
	return r.Replace(s)
}

// value compiles a number, text or date; the result may be NULL.
func (c *sqlCompiler) value(n formula.Node) (sqlExpr, bool) {
	t := c.typ(n)
	if v, ok := c.constant(n); ok {
		return c.param(v)
	}
	switch n := n.(type) {
	case *formula.FieldRef:
		col, q, ok := c.column(n.Name)
		if !ok {
			return sqlExpr{}, false
		}
		switch col.Type {
		case "boolean":
			return sqlExpr{}, false
		case "text", "longtext", "single_select":
			if !c.charType(col) {
				q = "CAST(" + q + " AS " + c.d.textType + ")"
			}
		}
		return sqlExpr{sql: q, nullable: true}, true
	case *formula.Unary:
		if n.Op != "-" || t != formula.Number {
			return sqlExpr{}, false
		}
		x, ok := c.value(n.X)
		if !ok {
			return sqlExpr{}, false
		}
		return join(false, "(-", c.zero(x, formula.Number), ")"), true
	case *formula.Binary:
		if t != formula.Number || c.typ(n.L) != formula.Number || c.typ(n.R) != formula.Number {
			return sqlExpr{}, false
		}
		l, ok := c.value(n.L)
		if !ok {
			return sqlExpr{}, false
		}
		r, ok := c.value(n.R)
		if !ok {
			return sqlExpr{}, false
		}
		switch n.Op {
		case "+", "-", "*":
			return join(false, "(", c.zero(l, formula.Number), " "+n.Op+" ", c.zero(r, formula.Number), ")"), true
		case "/":
			return join(true, "(", c.zero(l, formula.Number), " / NULLIF(", c.zero(r, formula.Number), ", 0))"), true
		}
	case *formula.Call:
		if len(n.Args) != 1 {
			return sqlExpr{}, false
		}
		at := c.typ(n.Args[0])
		switch n.Name {
		case "LOWER", "UPPER", "TRIM":
			if at != formula.Text {
				return sqlExpr{}, false
			}
			x, ok := c.value(n.Args[0])
			if !ok {
				return sqlExpr{}, false
			}
			return join(x.nullable, n.Name+"(", x, ")"), true
		case "TEXT":
			x, ok := c.value(n.Args[0])
			if !ok {
				return sqlExpr{}, false
			}
			switch at {
			case formula.Text:
				return x, true
			case formula.Number:
				return join(x.nullable, "CAST(", x, " AS "+c.d.textType+")"), true
			}
		}
	}
	return sqlExpr{}, false
}

func (c *sqlCompiler) charType(col *Column) bool {
	t := strings.ToLower(col.DBType)
	if i := strings.IndexAny(t, "( "); i >= 0 && !strings.HasPrefix(t, "character varying") {
		t = t[:i]
	}
	for _, ct := range c.d.charTypes {
		if t == ct {
			return true
		}
	}
	return false
}

func (c *sqlCompiler) param(v any) (sqlExpr, bool) {
	switch x := v.(type) {
	case nil:
		return sqlExpr{sql: "NULL", nullable: true}, true
	case float64, string:
		return sqlExpr{sql: "?", args: []any{x}}, true
	case time.Time:
		return sqlExpr{sql: "?", args: []any{x.UTC()}}, true
	}
	return sqlExpr{}, false
}

func (c *sqlCompiler) zero(e sqlExpr, t formula.Type) sqlExpr {
	if !e.nullable {
		return e
	}
	switch t {
	case formula.Number:
		return join(false, "COALESCE(", e, ", 0)")
	case formula.Text:
		return join(false, "COALESCE(", e, ", '')")
	}
	return e
}

func zeroValue(t formula.Type) (any, bool) {
	switch t {
	case formula.Number:
		return 0.0, true
	case formula.Text:
		return "", true
	}
	return nil, false
}

// holds works out "a op b" in Go for constants of one type.
func holds(op string, a, b any) bool {
	var cmp int
	switch x := a.(type) {
	case float64:
		y, _ := b.(float64)
		switch {
		case x < y:
			cmp = -1
		case x > y:
			cmp = 1
		}
	case string:
		y, _ := b.(string)
		cmp = strings.Compare(x, y)
	default:
		return false
	}
	switch op {
	case "=":
		return cmp == 0
	case "<>":
		return cmp != 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	}
	return false
}

func flip(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op
}

func (c *sqlCompiler) compare(n *formula.Binary) (sqlExpr, bool) {
	lt, rt := c.typ(n.L), c.typ(n.R)
	if lt == formula.Null || rt == formula.Null {
		return sqlFalse, true
	}
	if lt != rt {
		return sqlExpr{}, false
	}
	if lt == formula.Bool {
		// [done] = TRUE and the like.
		if n.Op != "=" && n.Op != "<>" {
			return sqlExpr{}, false
		}
		field, other := n.L, n.R
		if formula.Constant(field) {
			field, other = other, field
		}
		v, ok := c.constant(other)
		b, isBool := v.(bool)
		if !ok || !isBool && v != nil {
			return sqlExpr{}, false
		}
		p, ok := c.pred(field)
		if !ok {
			return sqlExpr{}, false
		}
		if b == (n.Op == "=") {
			return p, true
		}
		return join(false, "(NOT ", p, ")"), true
	}
	if lt != formula.Number && lt != formula.Text && lt != formula.Date {
		return sqlExpr{}, false
	}
	op := n.Op
	x, k := n.L, n.R
	if formula.Constant(x) && !formula.Constant(k) {
		x, k, op = k, x, flip(op)
	}
	xe, ok := c.value(x)
	if !ok {
		return sqlExpr{}, false
	}
	if kv, isConst := c.constant(k); isConst {
		if lt == formula.Date {
			if kv == nil {
				return sqlFalse, true
			}
			ke, ok := c.param(kv)
			if !ok {
				return sqlExpr{}, false
			}
			if xe.nullable {
				return join(false, "(", xe, " IS NOT NULL AND ", xe, " "+op+" ", ke, ")"), true
			}
			return join(false, "(", xe, " "+op+" ", ke, ")"), true
		}
		z, _ := zeroValue(lt)
		if kv == nil {
			kv = z
		}
		ke, ok := c.param(kv)
		if !ok {
			return sqlExpr{}, false
		}
		cmp := join(false, "(", xe, " "+op+" ", ke, ")")
		if xe.nullable && holds(op, z, kv) {
			if lt == formula.Text {
				// A blank is NULL or ''; '' already compares in SQL.
				return join(false, "(", xe, " IS NULL OR ", cmp, ")"), true
			}
			return join(false, "(", xe, " IS NULL OR ", cmp, ")"), true
		}
		return cmp, true
	}
	ke, ok := c.value(k)
	if !ok {
		return sqlExpr{}, false
	}
	if lt == formula.Date {
		return join(false, "(", xe, " IS NOT NULL AND ", ke, " IS NOT NULL AND ", xe, " "+op+" ", ke, ")"), true
	}
	return join(false, "(", c.zero(xe, lt), " "+op+" ", c.zero(ke, lt), ")"), true
}

func (c *sqlCompiler) in(n *formula.In) (sqlExpr, bool) {
	xt := c.typ(n.X)
	if xt != formula.Number && xt != formula.Text {
		return sqlExpr{}, false
	}
	x, ok := c.value(n.X)
	if !ok {
		return sqlExpr{}, false
	}
	if len(n.List) == 0 {
		if n.Not {
			return sqlTrue, true
		}
		return sqlFalse, true
	}
	z, _ := zeroValue(xt)
	hasZero := false
	parts := []any{}
	for i, item := range n.List {
		v, ok := c.constant(item)
		if !ok {
			return sqlExpr{}, false
		}
		if v == nil {
			v = z
		}
		if _, isList := v.([]any); isList {
			return sqlExpr{}, false
		}
		if (xt == formula.Number) != isNumber(v) {
			return sqlExpr{}, false
		}
		hasZero = hasZero || v == z
		e, _ := c.param(v)
		if i > 0 {
			parts = append(parts, ", ")
		}
		parts = append(parts, e)
	}
	list := join(false, append([]any{"("}, append(parts, ")")...)...)
	switch {
	case !n.Not && hasZero && x.nullable:
		return join(false, "(", x, " IS NULL OR ", x, " IN ", list, ")"), true
	case !n.Not:
		return join(false, "(", x, " IN ", list, ")"), true
	case hasZero && x.nullable:
		return join(false, "(", x, " IS NOT NULL AND ", x, " NOT IN ", list, ")"), true
	case x.nullable:
		return join(false, "(", x, " IS NULL OR ", x, " NOT IN ", list, ")"), true
	}
	return join(false, "(", x, " NOT IN ", list, ")"), true
}

func isNumber(v any) bool { _, ok := v.(float64); return ok }
