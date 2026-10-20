package formula

import (
	"fmt"
	"math"
	"time"
)

// Env supplies values while a formula runs.
type Env interface {
	// Field returns the raw value of [name] in the current row: nil when
	// blank, else float64, string, bool, time.Time or []any.
	Field(name string) any
	// Name returns the value of a dotted name such as Current.User.Email.
	Name(parts []string) any
	Now() time.Time
}

// Eval runs a checked formula. Blank numbers count as 0 and blank text as
// "" wherever a value is needed; comparisons with a blank date are false.
// Division by zero gives a blank result.
func (c *Checked) Eval(env Env) (any, error) {
	ev := &evaluator{env: env, c: c}
	v, err := ev.eval(c.Root)
	if err != nil {
		return nil, err
	}
	if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
		return nil, nil
	}
	return v, nil
}

// EvalBool runs a formula used as a condition: anything not true is false.
func (c *Checked) EvalBool(env Env) (bool, error) {
	v, err := c.Eval(env)
	if err != nil {
		return false, err
	}
	b, err := toBool(v)
	return b, err
}

type evaluator struct {
	env Env
	c   *Checked
}

func (ev *evaluator) fail(n Node, err error) error {
	if _, ok := err.(*Error); ok {
		return err
	}
	s, e := n.Span()
	return &Error{Start: s, End: e, Message: err.Error()}
}

func (ev *evaluator) eval(n Node) (any, error) {
	switch n := n.(type) {
	case *NumberLit:
		return n.Value, nil
	case *StringLit:
		return n.Value, nil
	case *BoolLit:
		return n.Value, nil
	case *NullLit:
		return nil, nil
	case *FieldRef:
		return ev.env.Field(n.Name), nil
	case *NameRef:
		return ev.env.Name(n.Parts), nil
	case *Unary:
		x, err := ev.eval(n.X)
		if err != nil {
			return nil, err
		}
		if n.Op == "NOT" {
			b, err := toBool(x)
			if err != nil {
				return nil, ev.fail(n.X, err)
			}
			return !b, nil
		}
		f, err := toNumber(x)
		if err != nil {
			return nil, ev.fail(n.X, err)
		}
		return -f, nil
	case *Binary:
		return ev.binary(n)
	case *In:
		x, err := ev.eval(n.X)
		if err != nil {
			return nil, err
		}
		xt := ev.c.types[n.X]
		for _, item := range n.List {
			v, err := ev.eval(item)
			if err != nil {
				return nil, err
			}
			c, known, err := compare(x, xt, v, ev.c.types[item])
			if err != nil {
				return nil, ev.fail(n, err)
			}
			if !known {
				if _, ok := zeroOf(xt); x == nil && !ok {
					return false, nil // a blank date is neither in nor out of a list
				}
				continue // a NULL item matches nothing
			}
			if c == 0 {
				return !n.Not, nil
			}
		}
		return n.Not, nil
	case *Call:
		f := functions[n.Name]
		if f.lazy != nil {
			v, err := f.lazy(ev, n.Args)
			if err != nil {
				return nil, ev.fail(n, err)
			}
			return v, nil
		}
		args := make([]any, len(n.Args))
		for i, a := range n.Args {
			v, err := ev.eval(a)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		v, err := f.eval(ev.env, args)
		if err != nil {
			return nil, ev.fail(n, err)
		}
		return v, nil
	}
	return nil, ev.fail(n, fmt.Errorf("unsupported expression"))
}

func (ev *evaluator) binary(n *Binary) (any, error) {
	l, err := ev.eval(n.L)
	if err != nil {
		return nil, err
	}
	switch n.Op { // short-circuit
	case "AND", "OR":
		lb, err := toBool(l)
		if err != nil {
			return nil, ev.fail(n.L, err)
		}
		if (n.Op == "AND") != lb {
			return lb, nil
		}
		r, err := ev.eval(n.R)
		if err != nil {
			return nil, err
		}
		rb, err := toBool(r)
		if err != nil {
			return nil, ev.fail(n.R, err)
		}
		return rb, nil
	}
	r, err := ev.eval(n.R)
	if err != nil {
		return nil, err
	}
	switch n.Op {
	case "&":
		return ToText(l) + ToText(r), nil
	case "+", "-", "*", "/", "^":
		a, err := toNumber(l)
		if err != nil {
			return nil, ev.fail(n.L, err)
		}
		b, err := toNumber(r)
		if err != nil {
			return nil, ev.fail(n.R, err)
		}
		switch n.Op {
		case "+":
			return a + b, nil
		case "-":
			return a - b, nil
		case "*":
			return a * b, nil
		case "/":
			if b == 0 {
				return nil, nil
			}
			return a / b, nil
		default:
			return math.Pow(a, b), nil
		}
	}
	c, known, err := compare(l, ev.c.types[n.L], r, ev.c.types[n.R])
	if err != nil {
		return nil, ev.fail(n, err)
	}
	if !known {
		return false, nil
	}
	switch n.Op {
	case "=":
		return c == 0, nil
	case "<>":
		return c != 0, nil
	case "<":
		return c < 0, nil
	case "<=":
		return c <= 0, nil
	case ">":
		return c > 0, nil
	case ">=":
		return c >= 0, nil
	}
	return nil, ev.fail(n, fmt.Errorf("unknown operator %s", n.Op))
}

// EvalNode runs one part of a checked formula, such as a constant operand
// that a database filter binds as a parameter.
func (c *Checked) EvalNode(n Node, env Env) (any, error) {
	ev := &evaluator{env: env, c: c}
	return ev.eval(n)
}

// Constant reports whether a part of a formula does not depend on the
// row: it has no [field] in it.
func Constant(n Node) bool { return constant(n) }
