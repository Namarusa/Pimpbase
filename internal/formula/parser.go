package formula

import (
	"strconv"
	"strings"
)

// Node is an expression in the syntax tree.
type Node interface {
	Span() (start, end int)
}

type span struct{ start, end int }

func (s span) Span() (int, int) { return s.start, s.end }

type (
	NumberLit struct {
		span
		Value float64
	}
	StringLit struct {
		span
		Value string
	}
	BoolLit struct {
		span
		Value bool
	}
	NullLit struct{ span }
	// FieldRef is [Name]: a field of the current row.
	FieldRef struct {
		span
		Name string
	}
	// NameRef is a dotted name such as Current.User.Email.
	NameRef struct {
		span
		Parts []string
	}
	Unary struct {
		span
		Op string // "-" or "NOT"
		X  Node
	}
	Binary struct {
		span
		Op   string // + - * / ^ & = <> < <= > >= AND OR
		L, R Node
	}
	Call struct {
		span
		Name string // upper case
		Args []Node
	}
	In struct {
		span
		X    Node
		List []Node
		Not  bool
	}
)

// Parse parses a formula. A leading "=" is allowed, as in spreadsheets.
func Parse(src string) (Node, *Error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if t := p.peek(); t.kind == tOp && t.text == "=" {
		p.next()
	}
	if p.peek().kind == tEOF {
		return nil, errAt(0, len(src), "the formula is empty")
	}
	n, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, errAt(t.start, t.end, "unexpected %q", tokenText(src, t))
	}
	return n, nil
}

func tokenText(src string, t token) string { return src[t.start:t.end] }

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}

func keyword(t token) string {
	if t.kind != tIdent {
		return ""
	}
	switch k := strings.ToUpper(t.text); k {
	case "AND", "OR", "NOT", "IN", "TRUE", "FALSE", "NULL":
		return k
	}
	return ""
}

// Binding powers, lowest first.
const (
	bpOr = iota + 1
	bpAnd
	bpNot
	bpCompare
	bpConcat
	bpAdd
	bpMul
	bpPow
	bpUnary
)

func infix(t token) (op string, bp int) {
	if t.kind == tOp {
		switch t.text {
		case "=", "<>", "<", "<=", ">", ">=":
			return t.text, bpCompare
		case "&":
			return "&", bpConcat
		case "+", "-":
			return t.text, bpAdd
		case "*", "/":
			return t.text, bpMul
		case "^":
			return "^", bpPow
		}
	}
	switch keyword(t) {
	case "OR":
		return "OR", bpOr
	case "AND":
		return "AND", bpAnd
	case "IN":
		return "IN", bpCompare
	case "NOT": // NOT IN
		return "NOT IN", bpCompare
	}
	return "", 0
}

func (p *parser) expr(minBP int) (Node, *Error) {
	left, err := p.prefix()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		op, bp := infix(t)
		if bp == 0 || bp <= minBP {
			return left, nil
		}
		p.next()
		ls, _ := left.Span()
		if op == "IN" || op == "NOT IN" {
			if op == "NOT IN" {
				if keyword(p.peek()) != "IN" {
					u := p.peek()
					return nil, errAt(u.start, u.end, "expected IN after NOT")
				}
				p.next()
			}
			list, end, err := p.parenList()
			if err != nil {
				return nil, err
			}
			left = &In{span{ls, end}, left, list, op == "NOT IN"}
			continue
		}
		nextBP := bp
		if op == "^" { // right associative: 2^3^2 = 2^(3^2)
			nextBP = bp - 1
		}
		right, err := p.expr(nextBP)
		if err != nil {
			return nil, err
		}
		_, re := right.Span()
		left = &Binary{span{ls, re}, op, left, right}
	}
}

func (p *parser) parenList() ([]Node, int, *Error) {
	t := p.next()
	if t.kind != tLParen {
		return nil, 0, errAt(t.start, t.end, "expected ( to start a list")
	}
	var items []Node
	if p.peek().kind == tRParen {
		return nil, p.next().end, nil
	}
	for {
		n, err := p.expr(0)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, n)
		t := p.next()
		if t.kind == tRParen {
			return items, t.end, nil
		}
		if t.kind != tComma {
			return nil, 0, errAt(t.start, t.end, "expected , or )")
		}
	}
}

func (p *parser) prefix() (Node, *Error) {
	t := p.next()
	switch t.kind {
	case tNumber:
		v, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, errAt(t.start, t.end, "invalid number %q", t.text)
		}
		return &NumberLit{span{t.start, t.end}, v}, nil
	case tString:
		return &StringLit{span{t.start, t.end}, t.text}, nil
	case tField:
		if p.peek().kind == tDot && p.toks[p.pos+1].kind == tField {
			p.next()
			f := p.next()
			return &LinkField{span: span{t.start, f.end}, Link: t.text, Field: f.text, linkEnd: t.end, fieldStart: f.start}, nil
		}
		return &FieldRef{span{t.start, t.end}, t.text}, nil
	case tLParen:
		n, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tRParen {
			return nil, errAt(c.start, c.end, "expected )")
		}
		return n, nil
	case tOp:
		if t.text == "-" || t.text == "+" {
			x, err := p.expr(bpUnary - 1)
			if err != nil {
				return nil, err
			}
			if t.text == "+" {
				return x, nil
			}
			_, e := x.Span()
			return &Unary{span{t.start, e}, "-", x}, nil
		}
	case tIdent:
		switch keyword(t) {
		case "TRUE", "FALSE":
			return &BoolLit{span{t.start, t.end}, keyword(t) == "TRUE"}, nil
		case "NULL":
			return &NullLit{span{t.start, t.end}}, nil
		case "NOT":
			if p.peek().kind == tLParen { // NOT(x) reads as a function call
				break
			}
			x, err := p.expr(bpNot)
			if err != nil {
				return nil, err
			}
			_, e := x.Span()
			return &Unary{span{t.start, e}, "NOT", x}, nil
		}
		if p.peek().kind == tLParen {
			args, end, err := p.parenList()
			if err != nil {
				return nil, err
			}
			return &Call{span{t.start, end}, strings.ToUpper(t.text), args}, nil
		}
		parts := []string{t.text}
		end := t.end
		for p.peek().kind == tDot {
			p.next()
			id := p.next()
			if id.kind != tIdent && id.kind != tField { // a.[name with spaces]
				return nil, errAt(id.start, id.end, "expected a name after .")
			}
			parts = append(parts, id.text)
			end = id.end
		}
		return &NameRef{span{t.start, end}, parts}, nil
	case tEOF:
		return nil, errAt(t.start, t.end, "the formula ends too early")
	}
	return nil, errAt(t.start, t.end, "unexpected %q", t.text)
}

// FieldRefs returns the distinct fields of the current row that n uses.
// Fields of linked tables, inside rollups or after [Link]., are left out;
// the link fields themselves are in.
func FieldRefs(n Node) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range outerRefs(n) {
		if !seen[strings.ToLower(r.name)] {
			seen[strings.ToLower(r.name)] = true
			out = append(out, r.name)
		}
	}
	return out
}

// ref is a field name in the source and the byte range of its [brackets].
type ref struct {
	name       string
	start, end int
}

// outerRefs lists the references to fields of the current row.
func outerRefs(n Node) []ref {
	var out []ref
	var walk func(n Node)
	walk = func(n Node) {
		switch n := n.(type) {
		case *FieldRef:
			out = append(out, ref{n.Name, n.start, n.end})
		case *LinkField:
			out = append(out, ref{n.Link, n.start, n.linkEnd})
		case *Unary:
			walk(n.X)
		case *Binary:
			walk(n.L)
			walk(n.R)
		case *In:
			walk(n.X)
			for _, a := range n.List {
				walk(a)
			}
		case *Call:
			if f := functions[n.Name]; f != nil && f.rollup && len(n.Args) > 0 {
				walk(n.Args[0])
				return
			}
			for _, a := range n.Args {
				walk(a)
			}
		}
	}
	walk(n)
	return out
}

// linkedRefs lists the references to fields of the table that link
// points to: [link].[x], and [x] inside rollups over link.
func linkedRefs(n Node, link string) []ref {
	var out []ref
	var walk func(n Node, inside bool)
	walk = func(n Node, inside bool) {
		switch n := n.(type) {
		case *FieldRef:
			if inside {
				out = append(out, ref{n.Name, n.start, n.end})
			}
		case *LinkField:
			if !inside && strings.EqualFold(n.Link, link) {
				out = append(out, ref{n.Field, n.fieldStart, n.end})
			}
		case *Unary:
			walk(n.X, inside)
		case *Binary:
			walk(n.L, inside)
			walk(n.R, inside)
		case *In:
			walk(n.X, inside)
			for _, a := range n.List {
				walk(a, inside)
			}
		case *Call:
			if f := functions[n.Name]; f != nil && f.rollup && len(n.Args) > 0 && !inside {
				r, ok := n.Args[0].(*FieldRef)
				in := ok && strings.EqualFold(r.Name, link)
				for _, a := range n.Args[1:] {
					walk(a, in)
				}
				return
			}
			for _, a := range n.Args {
				walk(a, inside)
			}
		}
	}
	walk(n, false)
	return out
}

// Walk calls fn for n and every node below it.
func Walk(n Node, fn func(Node)) {
	fn(n)
	switch n := n.(type) {
	case *Unary:
		Walk(n.X, fn)
	case *Binary:
		Walk(n.L, fn)
		Walk(n.R, fn)
	case *Call:
		for _, a := range n.Args {
			Walk(a, fn)
		}
	case *In:
		Walk(n.X, fn)
		for _, a := range n.List {
			Walk(a, fn)
		}
	}
}
