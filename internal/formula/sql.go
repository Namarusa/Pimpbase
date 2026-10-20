package formula

import (
	"strings"
	"time"
)

// SQLResolver maps field references to SQL for pushdown.
type SQLResolver interface {
	// FieldSQL returns an SQL expression for [name] that evaluates to the
	// field's raw value (NULL when blank), with any bound arguments, or
	// ok=false when the field cannot be expressed in SQL.
	FieldSQL(name string) (expr string, args []any, ok bool)
}

// SQL is a compiled boolean condition.
type SQL struct {
	Where string
	Args  []any
}

// CompileSQL turns a boolean formula into an SQL condition with the same
// meaning as Eval, for dialect "sqlite" or "postgres". Parts that do not
// depend on the row (literals, Current.User, TODAY()) are evaluated in Go
// and bound as arguments. ok is false when some part has no SQL form; the
// caller then filters rows in Go instead.
func (c *Checked) CompileSQL(dialect string, res SQLResolver, env Env) (out SQL, ok bool) {
	if c.Type != Bool && c.Type != Null && c.Type != Any {
		return SQL{}, false
	}
	comp := &compiler{c: c, dialect: dialect, res: res, env: env}
	e, ok := comp.node(c.Root)
	if !ok {
		return SQL{}, false
	}
	e = comp.zero(e, Bool)
	return SQL{Where: e.sql, Args: e.args}, true
}

// CompileValueSQL compiles a formula of any pushable type to an SQL value
// expression whose blank is NULL. It is used to inline formula fields into
// other formulas.
func (c *Checked) CompileValueSQL(dialect string, res SQLResolver, env Env) (expr string, args []any, ok bool) {
	comp := &compiler{c: c, dialect: dialect, res: res, env: env}
	e, ok := comp.node(c.Root)
	if !ok {
		return "", nil, false
	}
	return e.sql, e.args, true
}

type compiler struct {
	c       *Checked
	dialect string
	res     SQLResolver
	env     Env
}

// sqlExpr is a piece of SQL with its bound arguments in order of
// appearance. nullable means it can be NULL.
type sqlExpr struct {
	sql      string
	args     []any
	nullable bool
}

// cat joins strings and expressions into one expression, keeping the
// arguments in the order their placeholders appear.
func cat(nullable bool, parts ...any) sqlExpr {
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

func (comp *compiler) pg() bool { return comp.dialect == "postgres" }

// zero replaces a blank with the zero value of t, as Eval does for operands.
func (comp *compiler) zero(e sqlExpr, t Type) sqlExpr {
	if !e.nullable {
		return e
	}
	switch t {
	case Number:
		return cat(false, "COALESCE(", e, ", 0)")
	case Text:
		return cat(false, "COALESCE(", e, ", '')")
	case Bool:
		return cat(false, "COALESCE(", e, ", FALSE)")
	}
	return e
}

func (comp *compiler) cast(t Type) string {
	switch t {
	case Number:
		if comp.pg() {
			return "DOUBLE PRECISION"
		}
		return "REAL"
	}
	return "TEXT"
}

// param binds a constant computed in Go.
func (comp *compiler) param(v any, t Type) (sqlExpr, bool) {
	switch x := v.(type) {
	case nil:
		if t == Bool {
			return sqlExpr{"CAST(NULL AS BOOLEAN)", nil, true}, true
		}
		return sqlExpr{"CAST(NULL AS " + comp.cast(t) + ")", nil, true}, true
	case bool:
		if x {
			return sqlExpr{"TRUE", nil, false}, true
		}
		return sqlExpr{"FALSE", nil, false}, true
	case float64:
		return sqlExpr{"CAST(? AS " + comp.cast(Number) + ")", []any{x}, false}, true
	case string:
		return sqlExpr{"CAST(? AS TEXT)", []any{x}, false}, true
	case time.Time:
		return sqlExpr{"CAST(? AS TEXT)", []any{x.UTC().Format(DateLayout)}, false}, true
	}
	return sqlExpr{}, false
}

func constant(n Node) bool {
	c := true
	Walk(n, func(n Node) {
		switch n.(type) {
		case *FieldRef, *LinkField:
			c = false
		}
	})
	return c
}

func (comp *compiler) node(n Node) (sqlExpr, bool) {
	t := comp.c.types[n]
	if constant(n) {
		ev := &evaluator{env: comp.env, c: comp.c}
		v, err := ev.eval(n)
		if err != nil {
			return sqlExpr{}, false
		}
		return comp.param(v, t)
	}
	if t == List || t == Any {
		return sqlExpr{}, false
	}
	switch n := n.(type) {
	case *FieldRef:
		s, args, ok := comp.res.FieldSQL(n.Name)
		if !ok {
			return sqlExpr{}, false
		}
		e := sqlExpr{s, args, true}
		if t == Bool {
			// Booleans are stored as integers.
			return cat(true, "(", e, " <> 0)"), true
		}
		return e, true
	case *Unary:
		x, ok := comp.node(n.X)
		if !ok {
			return sqlExpr{}, false
		}
		if n.Op == "NOT" {
			return cat(false, "(NOT ", comp.zero(x, Bool), ")"), true
		}
		return cat(false, "(-", comp.zero(x, Number), ")"), true
	case *Binary:
		return comp.binary(n)
	case *In:
		xt := comp.c.types[n.X]
		if xt == Date || xt == Null || !sameKind(comp.c, n.X, n.List...) {
			return sqlExpr{}, false
		}
		for _, item := range n.List {
			if comp.c.types[item] == Null {
				return sqlExpr{}, false
			}
		}
		x, ok := comp.node(n.X)
		if !ok {
			return sqlExpr{}, false
		}
		if len(n.List) == 0 {
			return comp.param(n.Not, Bool)
		}
		parts := []any{"(", comp.zero(x, xt), " IN ("}
		if n.Not {
			parts[2] = " NOT IN ("
		}
		for i, item := range n.List {
			e, ok := comp.node(item)
			if !ok {
				return sqlExpr{}, false
			}
			if i > 0 {
				parts = append(parts, ", ")
			}
			parts = append(parts, comp.zero(e, xt))
		}
		parts = append(parts, "))")
		return cat(false, parts...), true
	case *LinkField:
		return comp.linkFieldSQL(n, t)
	case *Call:
		if e, handled, ok := comp.callLinkSQL(n); handled {
			return e, ok
		}
		return comp.call(n, t)
	}
	return sqlExpr{}, false
}

// sameKind reports whether all nodes share one static type, so SQL compares
// them the way Go does.
func sameKind(c *Checked, first Node, rest ...Node) bool {
	t := c.types[first]
	for _, n := range rest {
		u := c.types[n]
		if u != t && u != Null && t != Null {
			return false
		}
	}
	return true
}

func (comp *compiler) binary(n *Binary) (sqlExpr, bool) {
	lt, rt := comp.c.types[n.L], comp.c.types[n.R]
	switch n.Op {
	case "=", "<>", "<", "<=", ">", ">=":
		if lt == Null || rt == Null {
			return comp.param(false, Bool) // nothing equals or orders against NULL
		}
		if !sameKind(comp.c, n.L, n.R) {
			return sqlExpr{}, false
		}
	case "&":
		if lt != Text && lt != Null || rt != Text && rt != Null {
			return sqlExpr{}, false // number and date formatting differ in SQL
		}
	case "^":
		return sqlExpr{}, false
	}
	l, ok := comp.node(n.L)
	if !ok {
		return sqlExpr{}, false
	}
	r, ok := comp.node(n.R)
	if !ok {
		return sqlExpr{}, false
	}
	switch n.Op {
	case "AND", "OR":
		return cat(false, "(", comp.zero(l, Bool), " "+n.Op+" ", comp.zero(r, Bool), ")"), true
	case "&":
		return cat(false, "(", comp.zero(l, Text), " || ", comp.zero(r, Text), ")"), true
	case "+", "-", "*":
		return cat(false, "(", comp.zero(l, Number), " "+n.Op+" ", comp.zero(r, Number), ")"), true
	case "/":
		return cat(true, "(", comp.zero(l, Number), " / NULLIF(", comp.zero(r, Number), ", 0))"), true
	}
	// Comparisons of one type.
	t := lt
	op := " " + n.Op + " "
	rs := comp.zero(r, t)
	if t == Text && comp.pg() && n.Op != "=" && n.Op != "<>" {
		rs = cat(false, rs, ` COLLATE "C"`) // byte order, as in Go and SQLite
	}
	cmp := cat(false, "(", comp.zero(l, t), op, rs, ")")
	if t == Date {
		return cat(false, "COALESCE(", cmp, ", FALSE)"), true
	}
	return cmp, true
}

func (comp *compiler) call(n *Call, t Type) (sqlExpr, bool) {
	args := make([]sqlExpr, len(n.Args))
	types := make([]Type, len(n.Args))
	for i, a := range n.Args {
		types[i] = comp.c.types[a]
		if n.Name == "IF" && i > 0 && types[i] != t && types[i] != Null {
			return sqlExpr{}, false
		}
		e, ok := comp.node(a)
		if !ok {
			return sqlExpr{}, false
		}
		args[i] = e
	}
	allText := func() bool {
		for _, t := range types {
			if t != Text && t != Null {
				return false
			}
		}
		return true
	}
	switch n.Name {
	case "IF":
		els := sqlExpr{"NULL", nil, true}
		if len(args) > 2 {
			els = args[2]
		}
		return cat(args[1].nullable || els.nullable, "(CASE WHEN ", comp.zero(args[0], Bool), " THEN ", args[1], " ELSE ", els, " END)"), true
	case "AND", "OR":
		parts := []any{"("}
		for i, a := range args {
			if i > 0 {
				parts = append(parts, " "+n.Name+" ")
			}
			parts = append(parts, comp.zero(a, Bool))
		}
		return cat(false, append(parts, ")")...), true
	case "NOT":
		return cat(false, "(NOT ", comp.zero(args[0], Bool), ")"), true
	case "ISBLANK":
		switch types[0] {
		case Text, Date:
			return cat(false, "COALESCE(", args[0], ", '') = ''"), true
		case Number, Bool:
			return cat(false, "(", args[0], " IS NULL)"), true
		}
	case "CONCAT":
		if !allText() {
			return sqlExpr{}, false
		}
		parts := []any{"("}
		for i, a := range args {
			if i > 0 {
				parts = append(parts, " || ")
			}
			parts = append(parts, comp.zero(a, Text))
		}
		return cat(false, append(parts, ")")...), true
	case "LEN":
		if allText() {
			return cat(false, "LENGTH(", comp.zero(args[0], Text), ")"), true
		}
	case "TRIM":
		if allText() {
			return cat(false, "TRIM(", comp.zero(args[0], Text), ", ' ')"), true
		}
	case "UPPER", "LOWER":
		// SQLite only changes ASCII letters, so only Postgres matches Go.
		if allText() && comp.pg() {
			return cat(false, n.Name+"(", comp.zero(args[0], Text), ")"), true
		}
	case "CONTAINS":
		if allText() {
			fn := "INSTR("
			if comp.pg() {
				fn = "STRPOS("
			}
			return cat(false, "("+fn, comp.zero(args[0], Text), ", ", comp.zero(args[1], Text), ") > 0)"), true
		}
	case "STARTSWITH":
		if allText() {
			b := comp.zero(args[1], Text)
			return cat(false, "(SUBSTR(", comp.zero(args[0], Text), ", 1, LENGTH(", b, ")) = ", b, ")"), true
		}
	case "ABS":
		return cat(false, "ABS(", comp.zero(args[0], Number), ")"), true
	case "SIGN":
		return cat(false, "CAST(SIGN(", comp.zero(args[0], Number), ") AS "+comp.cast(Number)+")"), true
	case "COALESCE":
		// Text and dates count "" as blank, as Go does.
		if t != Number && t != Text && t != Date || !sameKind(comp.c, n.Args[0], n.Args[1:]...) {
			break
		}
		parts := []any{"COALESCE("}
		for i, a := range args {
			if i > 0 {
				parts = append(parts, ", ")
			}
			if t == Number {
				parts = append(parts, a)
			} else {
				parts = append(parts, "NULLIF(", a, ", '')")
			}
		}
		return cat(true, append(parts, ")")...), true
	case "FIND":
		if len(args) == 2 && allText() {
			fn := "INSTR("
			if comp.pg() {
				fn = "STRPOS("
			}
			return cat(false, "CAST("+fn, comp.zero(args[1], Text), ", ", comp.zero(args[0], Text), ") AS "+comp.cast(Number)+")"), true
		}
	case "YEAR", "MONTH", "DAY", "HOUR", "MINUTE", "QUARTER":
		// Dates are DateLayout text in SQL: 2006-01-02T15:04:05Z.
		if types[0] != Date {
			break
		}
		pos := map[string]string{"YEAR": "1, 4", "MONTH": "6, 2", "DAY": "9, 2", "HOUR": "12, 2", "MINUTE": "15, 2", "QUARTER": "6, 2"}[n.Name]
		part := cat(true, "CAST(SUBSTR(", args[0], ", "+pos+") AS INTEGER)")
		if n.Name == "QUARTER" {
			part = cat(true, "((", part, " + 2) / 3)")
		}
		return cat(true, "CAST(", part, " AS "+comp.cast(Number)+")"), true
	case "REGEXMATCH", "REGEXEXTRACT", "REGEXREPLACE":
		// SQLite runs Go's own regexp through registered functions;
		// Postgres regular expressions differ, so those stay in Go. The
		// pattern must be valid, or SQL would fail where Go gives blank.
		if comp.pg() || !constant(n.Args[1]) {
			break
		}
		ev := &evaluator{env: comp.env, c: comp.c}
		pat, err := ev.eval(n.Args[1])
		if err != nil {
			break
		}
		if _, err := compileRegexp(ToText(pat)); err != nil {
			break
		}
		fn := "bb_" + strings.ToLower(n.Name) + "("
		parts := []any{fn}
		for i, a := range args {
			if i > 0 {
				parts = append(parts, ", ")
			}
			if types[i] != Text && types[i] != Null {
				return sqlExpr{}, false
			}
			parts = append(parts, comp.zero(a, Text))
		}
		parts = append(parts, ")")
		if n.Name == "REGEXMATCH" {
			return cat(false, append([]any{"("}, append(parts, " <> 0)")...)...), true
		}
		return cat(n.Name == "REGEXEXTRACT", parts...), true
	}
	return sqlExpr{}, false
}
