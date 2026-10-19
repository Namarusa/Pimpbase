// Package interp fills {{ bindings }} in the text of API queries: their
// URL, query string, headers and body.
//
// A binding is a dotted path with optional pipes, the same syntax screens
// use ({{ params.id }}, {{ env.API_KEY }}, {{ user.email | lower }}), or a
// formula after "=" ({{ = CONCAT("Bearer ", env.TOKEN) }}) using the
// functions of table formulas; in a formula, [name] is the parameter name.
//
// Where a value lands decides how it is escaped: path segments and query
// values are URL-escaped, headers lose line breaks, JSON bodies get JSON
// (a quoted string inside "…", a typed value outside), XML bodies get
// escaped text. The raw pipe skips the escaping.
package interp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/xmltree"
)

// Template is text with bindings, parsed once.
type Template struct {
	Source string
	parts  []part
}

type part struct {
	lit  string
	expr *Expr
}

// Expr is one binding.
type Expr struct {
	Source  string
	Path    []string
	Pipes   []Pipe
	Formula *formula.Checked
}

type Pipe struct {
	Name string
	Arg  string
	// hasArg is true when the pipe was written with ":".
	hasArg bool
}

// Error locates a mistake in a template.
type Error struct {
	Pos     int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Parse reads a template.
func Parse(src string) (*Template, error) {
	t := &Template{Source: src}
	rest, pos := src, 0
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			if rest != "" {
				t.parts = append(t.parts, part{lit: rest})
			}
			return t, nil
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			return nil, &Error{pos + i, "a {{ binding }} is not closed with }}"}
		}
		if i > 0 {
			t.parts = append(t.parts, part{lit: rest[:i]})
		}
		inner := rest[i+2 : i+j]
		e, err := parseExpr(inner)
		if err != nil {
			return nil, &Error{pos + i, fmt.Sprintf("in {{%s}}: %s", inner, err)}
		}
		t.parts = append(t.parts, part{expr: e})
		pos += i + j + 2
		rest = rest[i+j+2:]
	}
}

// MustParse parses a template known to be valid.
func MustParse(src string) *Template {
	t, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return t
}

func parseExpr(src string) (*Expr, error) {
	s := strings.TrimSpace(src)
	e := &Expr{Source: s}
	if f, ok := strings.CutPrefix(s, "="); ok {
		c, errs := formula.Compile(f, anyScope{})
		if len(errs) > 0 {
			return nil, fmt.Errorf("%s", errs[0].Message)
		}
		e.Formula = c
		return e, nil
	}
	segs := splitPipes(s)
	path := strings.TrimSpace(segs[0])
	if path == "" {
		return nil, fmt.Errorf("the binding is empty")
	}
	for _, p := range strings.Split(path, ".") {
		if p = strings.TrimSpace(p); p != "" {
			e.Path = append(e.Path, p)
		}
	}
	for _, raw := range segs[1:] {
		name, arg, has := strings.Cut(strings.TrimSpace(raw), ":")
		name = strings.TrimSpace(name)
		if _, ok := pipes[name]; !ok {
			return nil, fmt.Errorf("there is no pipe named %q", name)
		}
		arg = strings.TrimSpace(arg)
		if len(arg) >= 2 && arg[0] == '"' && arg[len(arg)-1] == '"' {
			arg = arg[1 : len(arg)-1]
		}
		e.Pipes = append(e.Pipes, Pipe{Name: name, Arg: arg, hasArg: has})
	}
	return e, nil
}

// splitPipes splits on "|" outside double quotes.
func splitPipes(s string) []string {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case '|':
			if !quoted {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// Paths lists the paths the template reads, formulas included.
func (t *Template) Paths() [][]string {
	var out [][]string
	for _, p := range t.parts {
		if p.expr == nil {
			continue
		}
		if p.expr.Formula == nil {
			out = append(out, p.expr.Path)
			continue
		}
		formula.Walk(p.expr.Formula.Root, func(n formula.Node) {
			switch n := n.(type) {
			case *formula.NameRef:
				out = append(out, n.Parts)
			case *formula.FieldRef:
				out = append(out, []string{"params", n.Name})
			}
		})
	}
	return out
}

// HasBindings reports whether the template holds any binding.
func (t *Template) HasBindings() bool {
	for _, p := range t.parts {
		if p.expr != nil {
			return true
		}
	}
	return false
}

// Scope supplies values to bindings.
type Scope interface {
	Lookup(path []string) (any, error)
}

// Lazy is a value computed when a binding reaches it; it receives the rest
// of the path. Queries that read other queries' results use it.
type Lazy func(rest []string) (any, error)

// Map is a Scope over nested maps and lists.
type Map map[string]any

func (m Map) Lookup(path []string) (any, error) { return Walk(map[string]any(m), path) }

// Walk follows a path through maps and lists. Map keys match exactly, or
// else ignoring case (HTTP headers); list items are numbered from 0.
func Walk(v any, path []string) (any, error) {
	for i, key := range path {
		switch cur := v.(type) {
		case Lazy:
			return cur(path[i:])
		case map[string]any:
			next, ok := cur[key]
			if !ok {
				for k, x := range cur {
					if strings.EqualFold(k, key) {
						next, ok = x, true
						break
					}
				}
			}
			v = next
		case Map:
			v = cur[key]
		case []any:
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || n >= len(cur) {
				return nil, nil
			}
			v = cur[n]
		default:
			return nil, nil
		}
	}
	if l, ok := v.(Lazy); ok {
		return l(nil)
	}
	return v, nil
}

// Mode is where a template's text goes.
type Mode int

const (
	Text   Mode = iota // as is
	URL                // a URL: path parts and query values are escaped
	Header             // a header value: no line breaks
	JSON               // a JSON document
	XML                // an XML document
)

// raw marks a value the raw, json or xml pipes produced, which the
// renderer inserts without escaping.
type raw struct {
	s    string
	mode Mode // the mode it is safe in; Text means every mode
}

// Render fills the bindings, escaping each value for mode.
func (t *Template) Render(mode Mode, s Scope) (string, error) {
	var b strings.Builder
	inString := false // JSON: inside a "…" literal
	for i, p := range t.parts {
		if p.expr == nil {
			b.WriteString(p.lit)
			if mode == JSON {
				inString = jsonInString(p.lit, inString)
			}
			continue
		}
		v, err := p.expr.eval(s)
		if err != nil {
			return "", err
		}
		if r, ok := v.(raw); ok && (r.mode == Text || r.mode == mode) && !(mode == JSON && inString && r.mode == JSON) {
			if mode == Header {
				b.WriteString(stripBreaks(r.s))
			} else {
				b.WriteString(r.s)
			}
			continue
		}
		switch mode {
		case Text:
			b.WriteString(ToText(v))
		case Header:
			b.WriteString(stripBreaks(ToText(v)))
		case URL:
			sofar := b.String()
			switch {
			case i == 0:
				// A leading binding is a base address, such as {{ env.API_URL }}.
				b.WriteString(ToText(v))
			case strings.Contains(sofar, "?"):
				b.WriteString(url.QueryEscape(ToText(v)))
			default:
				b.WriteString(url.PathEscape(ToText(v)))
			}
		case JSON:
			if inString {
				js, _ := marshal(ToText(v))
				b.Write(js[1 : len(js)-1])
			} else {
				js, err := marshal(v)
				if err != nil {
					return "", fmt.Errorf("{{ %s }} cannot be written as JSON: %w", p.expr.Source, err)
				}
				b.Write(js)
			}
		case XML:
			b.WriteString(escapeXML(ToText(v)))
		}
	}
	return b.String(), nil
}

// Value fills a template that is exactly one binding with the bound value,
// keeping its type; any other template gives text.
func (t *Template) Value(s Scope) (any, error) {
	if len(t.parts) == 1 && t.parts[0].expr != nil {
		v, err := t.parts[0].expr.eval(s)
		if r, ok := v.(raw); ok {
			return r.s, err
		}
		return v, err
	}
	return t.Render(Text, s)
}

// jsonInString tracks whether text leaves the writer inside a JSON string.
func jsonInString(lit string, in bool) bool {
	for i := 0; i < len(lit); i++ {
		switch {
		case in && lit[i] == '\\':
			i++
		case lit[i] == '"':
			in = !in
		}
	}
	return in
}

func stripBreaks(s string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
}

func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func marshal(v any) ([]byte, error) {
	switch x := v.(type) {
	case raw:
		v = x.s
	case time.Time:
		return json.Marshal(x.UTC().Format(formula.DateLayout))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return []byte("null"), nil
		}
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}

// ToText shows a value as text: numbers without exponents, true and false,
// dates in ISO form, lists and objects as JSON.
func ToText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case raw:
		return x.s
	case xmltree.Raw:
		return string(x)
	case float64:
		return formula.FormatNumber(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		return formula.FormatDate(x)
	default:
		js, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(js)
	}
}

func (e *Expr) eval(s Scope) (any, error) {
	var v any
	var err error
	if e.Formula != nil {
		env := &formulaEnv{s: s}
		v, err = e.Formula.Eval(env)
		if err == nil {
			err = env.err
		}
		if err != nil {
			return nil, fmt.Errorf("{{ %s }}: %w", e.Source, err)
		}
	} else {
		v, err = s.Lookup(e.Path)
		if err != nil {
			return nil, err
		}
	}
	for _, p := range e.Pipes {
		v, err = pipes[p.Name](v, p)
		if err != nil {
			return nil, fmt.Errorf("{{ %s }}: %w", e.Source, err)
		}
	}
	return v, nil
}

var pipes map[string]func(v any, p Pipe) (any, error)

func init() {
	pipes = map[string]func(any, Pipe) (any, error){
		"upper": func(v any, _ Pipe) (any, error) { return strings.ToUpper(ToText(v)), nil },
		"lower": func(v any, _ Pipe) (any, error) { return strings.ToLower(ToText(v)), nil },
		"trim":  func(v any, _ Pipe) (any, error) { return strings.TrimSpace(ToText(v)), nil },
		"default": func(v any, p Pipe) (any, error) {
			if isBlank(v) {
				return p.Arg, nil
			}
			return v, nil
		},
		"number": func(v any, p Pipe) (any, error) {
			f, ok := toNumber(v)
			if !ok {
				return nil, nil
			}
			if p.hasArg {
				d, _ := strconv.Atoi(p.Arg)
				return raw{strconv.FormatFloat(f, 'f', d, 64), Text}, nil
			}
			return f, nil
		},
		"count": func(v any, _ Pipe) (any, error) {
			switch x := v.(type) {
			case []any:
				return float64(len(x)), nil
			case nil:
				return 0.0, nil
			}
			return 1.0, nil
		},
		"first": func(v any, _ Pipe) (any, error) {
			if l, ok := v.([]any); ok {
				if len(l) == 0 {
					return nil, nil
				}
				return l[0], nil
			}
			return v, nil
		},
		"last": func(v any, _ Pipe) (any, error) {
			if l, ok := v.([]any); ok {
				if len(l) == 0 {
					return nil, nil
				}
				return l[len(l)-1], nil
			}
			return v, nil
		},
		"join": func(v any, p Pipe) (any, error) {
			sep := ","
			if p.hasArg {
				sep = p.Arg
			}
			l, ok := v.([]any)
			if !ok {
				return ToText(v), nil
			}
			parts := make([]string, len(l))
			for i, x := range l {
				parts[i] = ToText(x)
			}
			return strings.Join(parts, sep), nil
		},
		"urlencode": func(v any, _ Pipe) (any, error) { return raw{url.QueryEscape(ToText(v)), Text}, nil },
		"base64":    func(v any, _ Pipe) (any, error) { return base64.StdEncoding.EncodeToString([]byte(ToText(v))), nil },
		"raw":       func(v any, _ Pipe) (any, error) { return raw{ToText(v), Text}, nil },
		"json": func(v any, _ Pipe) (any, error) {
			js, err := marshal(v)
			if err != nil {
				return nil, err
			}
			return raw{string(js), JSON}, nil
		},
		"xml": func(v any, p Pipe) (any, error) {
			if s, ok := v.(string); ok && !p.hasArg {
				return raw{escapeXML(s), XML}, nil
			}
			return raw{xmltree.Encode(p.Arg, plain(v)), XML}, nil
		},
	}
}

// Pipes lists the pipe names, for the editor's help.
func Pipes() []string {
	return []string{"upper", "lower", "trim", "default", "number", "count", "first", "last", "join", "urlencode", "base64", "json", "xml", "raw"}
}

// plain converts json.Number and the like for the XML writer.
func plain(v any) any {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, y := range x {
			out[k] = plain(y)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = plain(y)
		}
		return out
	}
	return v
}

func isBlank(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	}
	return false
}

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// anyScope lets formulas name anything; values are checked as they run.
type anyScope struct{}

func (anyScope) FieldType(string) (formula.Type, bool)  { return formula.Any, true }
func (anyScope) NameType([]string) (formula.Type, bool) { return formula.Any, true }

// formulaEnv feeds bound values to a formula, converted to formula types.
// The first lookup error is kept, since formulas cannot return one.
type formulaEnv struct {
	s   Scope
	err error
}

func (e *formulaEnv) Field(name string) any { return e.Name([]string{"params", name}) }

func (e *formulaEnv) Name(parts []string) any {
	v, err := e.s.Lookup(parts)
	if err != nil {
		if e.err == nil {
			e.err = err
		}
		return nil
	}
	return toFormula(v)
}

func (*formulaEnv) Now() time.Time { return time.Now() }

func toFormula(v any) any {
	switch x := v.(type) {
	case nil, string, bool, time.Time:
		return x
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case raw:
		return x.s
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = toFormula(y)
		}
		return out
	default:
		return ToText(x)
	}
}
