package interp

import (
	"fmt"
	"regexp"
	"strings"
)

// Texts with blocks are templates plus two blocks, for emails and PDFs:
//
//	{{#each steps.Orders.rows}} {{ loop.item.Name }} ({{ loop.number }}) {{/each}}
//	{{#if trigger.row.Paid}} paid {{else}} due {{/if}}
//
// Inside each, loop.item is the current element, loop.index counts from 0
// and loop.number from 1, as in a Loop step; the short names item, index
// and number work too. Blocks nest, and the innermost loop wins.

type block struct {
	text string // a plain template, when kind is ""
	kind string // "each" or "if"
	expr *Template
	body []*block
	alt  []*block // the else part of an if
}

var blockTag = regexp.MustCompile(`\{\{\s*(#each|#if|else|/each|/if)\b\s*([^}]*?)\s*\}\}`)

// parseBlocks reads a text with blocks.
func parseBlocks(src string) ([]*block, error) {
	type frame struct {
		b      *block
		inElse bool
	}
	root := &block{}
	stack := []*frame{{b: root}}
	add := func(b *block) {
		f := stack[len(stack)-1]
		if f.inElse {
			f.b.alt = append(f.b.alt, b)
		} else {
			f.b.body = append(f.b.body, b)
		}
	}
	at := 0
	for _, m := range blockTag.FindAllStringSubmatchIndex(src, -1) {
		if m[0] > at {
			lit := src[at:m[0]]
			if _, err := Parse(lit); err != nil {
				return nil, err
			}
			add(&block{text: lit})
		}
		at = m[1]
		tag, arg := src[m[2]:m[3]], strings.TrimSpace(src[m[4]:m[5]])
		top := stack[len(stack)-1]
		switch tag {
		case "#each", "#if":
			if arg == "" {
				return nil, fmt.Errorf("{{%s}} needs a value, such as {{%s steps.Find.rows}}", tag, tag)
			}
			t, err := Parse("{{ " + arg + " }}")
			if err != nil {
				return nil, err
			}
			b := &block{kind: tag[1:], expr: t}
			add(b)
			stack = append(stack, &frame{b: b})
		case "else":
			if len(stack) == 1 || top.b.kind != "if" || top.inElse {
				return nil, fmt.Errorf("{{else}} belongs inside {{#if}} … {{/if}}")
			}
			top.inElse = true
		case "/each", "/if":
			if len(stack) == 1 || top.b.kind != tag[1:] {
				return nil, fmt.Errorf("{{%s}} does not close an open {{#%s}}", tag, tag[1:])
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 1 {
		return nil, fmt.Errorf("{{#%s}} is not closed with {{/%s}}", stack[len(stack)-1].b.kind, stack[len(stack)-1].b.kind)
	}
	if at < len(src) {
		if _, err := Parse(src[at:]); err != nil {
			return nil, err
		}
		root.body = append(root.body, &block{text: src[at:]})
	}
	return root.body, nil
}

// Overlay adds names in front of a scope.
type Overlay struct {
	Names map[string]any
	Base  Scope
}

func (o Overlay) Lookup(path []string) (any, error) {
	if len(path) > 0 {
		if _, ok := o.Names[path[0]]; ok {
			return Walk(o.Names, path)
		}
	}
	return o.Base.Lookup(path)
}

// renderBlocks fills a text with blocks.
func renderBlocks(bs []*block, mode Mode, s Scope, b *strings.Builder) error {
	for _, bl := range bs {
		switch bl.kind {
		case "":
			t, err := Parse(bl.text)
			if err != nil {
				return err
			}
			out, err := t.Render(mode, s)
			if err != nil {
				return err
			}
			b.WriteString(out)
		case "if":
			v, err := bl.expr.Value(s)
			if err != nil {
				return err
			}
			part := bl.alt
			if Truthy(v) {
				part = bl.body
			}
			if err := renderBlocks(part, mode, s, b); err != nil {
				return err
			}
		case "each":
			v, err := bl.expr.Value(s)
			if err != nil {
				return err
			}
			list := AsList(v)
			for i, item := range list {
				inner := Overlay{Names: map[string]any{"item": item, "index": float64(i), "number": float64(i + 1),
					"loop": map[string]any{"item": item, "index": float64(i), "number": float64(i + 1), "count": float64(len(list))}}, Base: s}
				if err := renderBlocks(bl.body, mode, inner, b); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// CheckBlocks validates a text with blocks.
func CheckBlocks(src string) error {
	_, err := parseBlocks(src)
	return err
}

// RenderBlocks fills a text with blocks and bindings.
func RenderBlocks(src string, mode Mode, s Scope) (string, error) {
	bs, err := parseBlocks(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := renderBlocks(bs, mode, s, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

// BlocksValue fills a text keeping the type of a lone binding:
// "{{ trigger.row._id }}" gives a number, "{{ steps.Find.rows }}" a list.
func BlocksValue(src string, s Scope) (any, error) {
	if !strings.Contains(src, "{{#") {
		t, err := Parse(src)
		if err != nil {
			return nil, err
		}
		return t.Value(s)
	}
	return RenderBlocks(src, Text, s)
}

// AsList reads a value as a list: nothing is empty, one value is a list of it.
func AsList(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	}
	return []any{v}
}

// Truthy is how conditions read a value: blank, false, 0, "false", "no"
// and empty lists are false.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case int64:
		return x != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "", "false", "no", "0", "off":
			return false
		}
		return true
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
