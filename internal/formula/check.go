package formula

import (
	"strings"
)

// Scope tells the checker what names mean where a formula lives.
type Scope interface {
	// FieldType returns the type of the field [name] of the current row.
	FieldType(name string) (Type, bool)
	// NameType returns the type of a dotted name such as Current.User.Email.
	NameType(parts []string) (Type, bool)
}

// Checked is a parsed, type-checked formula ready to evaluate or compile.
type Checked struct {
	Source string
	Root   Node
	Type   Type
	types  map[Node]Type
	// linkTypes are the field types behind [Link].[Field] of many links.
	linkTypes map[*LinkField]Type
}

// TypeOf returns the static type of a node in the formula.
func (c *Checked) TypeOf(n Node) Type { return c.types[n] }

// Compile parses and checks src. It returns every error found, with
// positions for the editor.
func Compile(src string, scope Scope) (*Checked, []*Error) {
	root, err := Parse(src)
	if err != nil {
		return nil, []*Error{err}
	}
	return Check(src, root, scope)
}

// Check type checks a parsed formula.
func Check(src string, root Node, scope Scope) (*Checked, []*Error) {
	ch := &checker{scope: scope, types: map[Node]Type{}, linkTypes: map[*LinkField]Type{}}
	t := ch.check(root)
	if len(ch.errs) > 0 {
		return nil, ch.errs
	}
	return &Checked{Source: src, Root: root, Type: t, types: ch.types, linkTypes: ch.linkTypes}, nil
}

type checker struct {
	scope     Scope
	types     map[Node]Type
	linkTypes map[*LinkField]Type
	errs      []*Error
}

func (ch *checker) fail(n Node, format string, args ...any) {
	s, e := n.Span()
	ch.errs = append(ch.errs, errAt(s, e, format, args...))
}

func accepts(want, got Type) bool {
	return want == Any || got == Any || got == Null || want == got
}

func (ch *checker) check(n Node) Type {
	t := ch.typeOf(n)
	ch.types[n] = t
	return t
}

func (ch *checker) typeOf(n Node) Type {
	switch n := n.(type) {
	case *NumberLit:
		return Number
	case *StringLit:
		return Text
	case *BoolLit:
		return Bool
	case *NullLit:
		return Null
	case *FieldRef:
		t, ok := ch.scope.FieldType(n.Name)
		if !ok {
			ch.fail(n, "there is no field named [%s]", n.Name)
			return Any
		}
		return t
	case *LinkField:
		return ch.checkLink(n)
	case *NameRef:
		t, ok := ch.scope.NameType(n.Parts)
		if !ok {
			ch.fail(n, "unknown name %s", strings.Join(n.Parts, "."))
			return Any
		}
		return t
	case *Unary:
		x := ch.check(n.X)
		if n.Op == "NOT" {
			if !accepts(Bool, x) {
				ch.fail(n.X, "NOT needs true or false, got %s", x)
			}
			return Bool
		}
		if !accepts(Number, x) {
			ch.fail(n.X, "cannot negate %s", x)
		}
		return Number
	case *Binary:
		l, r := ch.check(n.L), ch.check(n.R)
		switch n.Op {
		case "+", "-", "*", "/", "^":
			for _, side := range []struct {
				n Node
				t Type
			}{{n.L, l}, {n.R, r}} {
				if !accepts(Number, side.t) {
					hint := ""
					if side.t == Text && n.Op == "+" {
						hint = " (use & to join text)"
					} else if side.t == Date {
						hint = " (use DATEADD or DATEDIFF for dates)"
					}
					ch.fail(side.n, "%s needs numbers, got %s%s", n.Op, side.t, hint)
				}
			}
			return Number
		case "&":
			for _, side := range []struct {
				n Node
				t Type
			}{{n.L, l}, {n.R, r}} {
				if side.t == List {
					ch.fail(side.n, "& cannot join a list (use JOIN)")
				}
			}
			return Text
		case "AND", "OR":
			if !accepts(Bool, l) {
				ch.fail(n.L, "%s needs true or false, got %s", n.Op, l)
			}
			if !accepts(Bool, r) {
				ch.fail(n.R, "%s needs true or false, got %s", n.Op, r)
			}
			return Bool
		default: // comparisons
			if !comparable(l, r) {
				ch.fail(n, "cannot compare %s with %s", l, r)
			}
			return Bool
		}
	case *In:
		x := ch.check(n.X)
		for _, item := range n.List {
			if t := ch.check(item); !comparable(x, t) {
				ch.fail(item, "cannot compare %s with %s", x, t)
			}
		}
		return Bool
	case *Call:
		f, ok := functions[n.Name]
		if !ok {
			ch.fail(n, "unknown function %s", n.Name)
			for _, a := range n.Args {
				ch.check(a)
			}
			return Any
		}
		if f.rollup {
			return ch.checkRollup(n, f)
		}
		args := make([]Type, len(n.Args))
		for i, a := range n.Args {
			args[i] = ch.check(a)
		}
		if len(args) < f.min || (!f.variadic && len(args) > len(f.params)) {
			ch.fail(n, "%s takes %s", n.Name, f.arity())
			return f.result(args)
		}
		if f.check != nil {
			if msg := f.check(args); msg != "" {
				ch.fail(n, "%s", msg)
				return f.result(args)
			}
		}
		for i, t := range args {
			want := f.param(i)
			if !accepts(want, t) {
				ch.fail(n.Args[i], "argument %d of %s must be %s, got %s", i+1, n.Name, want, t)
			}
		}
		return f.result(args)
	}
	ch.fail(n, "unsupported expression")
	return Any
}

func comparable(a, b Type) bool {
	if a == List || b == List {
		return false
	}
	return a == b || a == Any || b == Any || a == Null || b == Null ||
		// Text compares with dates and numbers by parsing it.
		(a == Text && (b == Date || b == Number)) || (b == Text && (a == Date || a == Number))
}
