// Package formula implements Bubibase's expression language: an Excel-style
// syntax with [Field] references, used for formula fields, filters and every
// other computed value. Source text is parsed into an AST, type checked
// against a scope, then either evaluated in Go or compiled to SQL.
package formula

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tNumber
	tString
	tField // [Name]
	tIdent
	tLParen
	tRParen
	tComma
	tDot
	tOp // + - * / ^ & = <> != < <= > >=
)

type token struct {
	kind       tokenKind
	text       string // operator or identifier text, decoded string, field name
	start, end int    // byte offsets in the source
}

// Error is a problem in a formula, with the byte range it applies to.
type Error struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s (at %d)", e.Message, e.Start) }

func errAt(start, end int, format string, args ...any) *Error {
	return &Error{Start: start, End: end, Message: fmt.Sprintf(format, args...)}
}

func lex(src string) ([]token, *Error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9' || (c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9'):
			start := i
			for i < len(src) && (src[i] >= '0' && src[i] <= '9' || src[i] == '.') {
				i++
			}
			if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
				j := i + 1
				if j < len(src) && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < len(src) && src[j] >= '0' && src[j] <= '9' {
					i = j
					for i < len(src) && src[i] >= '0' && src[i] <= '9' {
						i++
					}
				}
			}
			toks = append(toks, token{tNumber, src[start:i], start, i})
		case c == '"':
			start := i
			i++
			var b strings.Builder
			closed := false
			for i < len(src) {
				if src[i] == '"' {
					if i+1 < len(src) && src[i+1] == '"' { // "" escapes a quote
						b.WriteByte('"')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				b.WriteByte(src[i])
				i++
			}
			if !closed {
				return nil, errAt(start, len(src), "text is missing its closing quote")
			}
			toks = append(toks, token{tString, b.String(), start, i})
		case c == '[':
			start := i
			end := strings.IndexByte(src[i:], ']')
			if end < 0 {
				return nil, errAt(start, len(src), "field reference is missing its closing ]")
			}
			name := strings.TrimSpace(src[i+1 : i+end])
			if name == "" {
				return nil, errAt(start, i+end+1, "empty field reference")
			}
			i += end + 1
			toks = append(toks, token{tField, name, start, i})
		case c == '(':
			toks = append(toks, token{tLParen, "(", i, i + 1})
			i++
		case c == ')':
			toks = append(toks, token{tRParen, ")", i, i + 1})
			i++
		case c == ',':
			toks = append(toks, token{tComma, ",", i, i + 1})
			i++
		case c == '.':
			toks = append(toks, token{tDot, ".", i, i + 1})
			i++
		case strings.IndexByte("+-*/^&", c) >= 0:
			toks = append(toks, token{tOp, string(c), i, i + 1})
			i++
		case c == '=':
			n := 1
			if i+1 < len(src) && src[i+1] == '=' { // accept == as =
				n = 2
			}
			toks = append(toks, token{tOp, "=", i, i + n})
			i += n
		case c == '!' && i+1 < len(src) && src[i+1] == '=':
			toks = append(toks, token{tOp, "<>", i, i + 2})
			i += 2
		case c == '<' || c == '>':
			op := string(c)
			if i+1 < len(src) && (src[i+1] == '=' || (c == '<' && src[i+1] == '>')) {
				op += string(src[i+1])
			}
			toks = append(toks, token{tOp, op, i, i + len(op)})
			i += len(op)
		case c == '_' || unicode.IsLetter(rune(c)) || c >= 0x80:
			start := i
			for i < len(src) {
				r := rune(src[i])
				if src[i] >= 0x80 || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
					i++
					continue
				}
				break
			}
			toks = append(toks, token{tIdent, src[start:i], start, i})
		default:
			return nil, errAt(i, i+1, "unexpected character %q", c)
		}
	}
	toks = append(toks, token{tEOF, "", len(src), len(src)})
	return toks, nil
}

// RenameField rewrites every reference to the field old of the current
// row to [new], leaving the rest of the text exactly as written. Fields of
// linked tables keep their names. Text that does not parse is returned
// unchanged.
func RenameField(src, old, new string) string {
	n, err := Parse(src)
	if err != nil {
		return src
	}
	return rewrite(src, outerRefs(n), old, new)
}

// RenameLinkedField rewrites references to the field old of the table that
// the link field [link] points to: [link].[old] and [old] inside rollups
// over [link].
func RenameLinkedField(src, link, old, new string) string {
	n, err := Parse(src)
	if err != nil {
		return src
	}
	return rewrite(src, linkedRefs(n, link), old, new)
}

// LinkedFieldRefs returns the fields of the table behind [link] that src
// reads.
func LinkedFieldRefs(src, link string) []string {
	n, err := Parse(src)
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range linkedRefs(n, link) {
		out = append(out, r.name)
	}
	return out
}

func rewrite(src string, refs []ref, old, new string) string {
	sort.Slice(refs, func(i, j int) bool { return refs[i].start < refs[j].start })
	var b strings.Builder
	last := 0
	for _, r := range refs {
		if strings.EqualFold(r.name, old) && r.start >= last {
			b.WriteString(src[last:r.start])
			b.WriteString("[" + new + "]")
			last = r.end
		}
	}
	b.WriteString(src[last:])
	return b.String()
}
