package extdb

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/interp"
)

// BindSQL turns a query the builder wrote into SQL with placeholders and
// the values of its {{ bindings }} as parameters. No value is ever written
// into the SQL text, so a binding cannot change what the statement does.
// A binding inside quotes or a comment is refused, because the database
// would read the placeholder as text there. A list becomes one
// placeholder per item, for IN ({{ params.ids }}).
func BindSQL(kind, src string, scope interp.Scope) (string, []any, error) {
	t, err := interp.Parse(src)
	if err != nil {
		return "", nil, errorf("%s", err)
	}
	var b strings.Builder
	var args []any
	lx := &sqlLexer{kind: kind}
	err = t.Each(scope, func(text string) error {
		lx.feed(text)
		b.WriteString(text)
		return nil
	}, func(e *interp.Expr, v any) error {
		if where := lx.inside(); where != "" {
			return errorf("{{ %s }} is inside %s; write it without quotes, as in WHERE name = {{ %s }}: Pimpbase sends it as a parameter", strings.TrimSpace(e.Source), where, strings.TrimSpace(e.Source))
		}
		add := func(v any) {
			args = append(args, sqlArg(v))
			b.WriteString(placeholder(kind, len(args)))
		}
		if list, ok := v.([]any); ok {
			if len(list) == 0 {
				b.WriteString("NULL")
				return nil
			}
			for i, x := range list {
				if i > 0 {
					b.WriteString(", ")
				}
				add(x)
			}
			return nil
		}
		add(v)
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return b.String(), args, nil
}

// CheckSQL reports bindings placed where they cannot be parameters.
func CheckSQL(kind, src string) error {
	_, _, err := BindSQL(kind, src, checkScope{})
	return err
}

// checkScope gives every binding a blank, for checking placement only.
type checkScope struct{}

func (checkScope) Lookup([]string) (any, error) { return nil, nil }

func placeholder(kind string, n int) string {
	switch kind {
	case "postgres":
		return "$" + itoa(n)
	case "mssql":
		return "@p" + itoa(n)
	}
	return "?"
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// sqlArg converts a binding's value to a driver parameter.
func sqlArg(v any) any {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return int64(x)
		}
		return x
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return f
	case map[string]any, []any:
		js, _ := json.Marshal(x)
		return string(js)
	case time.Time:
		return x.UTC()
	case nil, bool, string, int64:
		return x
	}
	return interp.ToText(v)
}

// sqlLexer follows SQL text far enough to know whether the end of it is
// inside a string, a quoted name or a comment.
type sqlLexer struct {
	kind  string
	state byte // 0, '\'', '"', '`', '[', '-' (line comment), '*' (block comment)
	prev  byte
}

func (l *sqlLexer) feed(s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch l.state {
		case 0:
			switch {
			case c == '\'' || c == '"' || c == '`' && l.kind == "mysql" || c == '[' && l.kind == "mssql":
				l.state = c
			case c == '-' && l.prev == '-':
				l.state = '-'
			case c == '*' && l.prev == '/':
				l.state = '*'
				c = 0 // so "/*/" does not close
			}
		case '\'', '"', '`':
			if c == l.state {
				l.state = 0
				c = 0 // a doubled quote reopens on the next one
			} else if c == '\\' && l.kind == "mysql" && i+1 < len(s) {
				i++
				c = 0
			}
		case '[':
			if c == ']' {
				l.state = 0
			}
		case '-':
			if c == '\n' {
				l.state = 0
			}
		case '*':
			if c == '/' && l.prev == '*' {
				l.state = 0
				c = 0
			}
		}
		l.prev = c
	}
}

func (l *sqlLexer) inside() string {
	switch l.state {
	case '\'':
		return "quotes"
	case '"', '`', '[':
		return "a quoted name"
	case '-', '*':
		return "a comment"
	}
	return ""
}

// QueryLike reports whether a statement reads rows, so it is sent as a
// query rather than executed for a row count.
func QueryLike(sql string) bool {
	s := strings.TrimSpace(stripComments(sql))
	s = strings.TrimLeft(s, "(")
	word := strings.ToUpper(firstWord(s))
	switch word {
	case "SELECT", "WITH", "SHOW", "EXPLAIN", "DESCRIBE", "DESC", "VALUES", "TABLE", "EXEC", "EXECUTE", "CALL", "PRAGMA":
		return true
	}
	up := strings.ToUpper(s)
	return strings.Contains(up, " RETURNING ") || strings.Contains(up, "\nRETURNING ") || strings.Contains(up, " OUTPUT ")
}

func firstWord(s string) string {
	for i, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return s[:i]
		}
	}
	return s
}

func stripComments(s string) string {
	for {
		s = strings.TrimSpace(s)
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			return ""
		}
		return s
	}
}
