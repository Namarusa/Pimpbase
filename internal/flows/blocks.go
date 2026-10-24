package flows

import (
	"context"
	"regexp"
	"strings"

	"github.com/pimpbase/pimpbase/internal/email"
	"github.com/pimpbase/pimpbase/internal/formula"
	"github.com/pimpbase/pimpbase/internal/interp"
)

// Step texts are interp templates with {{#each}} and {{#if}} blocks; see
// interp.RenderBlocks.

type overlay = interp.Overlay

func checkText(src string) error { return interp.CheckBlocks(src) }

func render(src string, mode interp.Mode, s interp.Scope) (string, error) {
	return interp.RenderBlocks(src, mode, s)
}

func value(src string, s interp.Scope) (any, error) { return interp.BlocksValue(src, s) }

func asList(v any) []any { return interp.AsList(v) }

func truthy(v any) bool { return interp.Truthy(v) }

var bindingRe = regexp.MustCompile(`\{\{[\s\S]*?\}\}`)

// fillFormula puts bound values into a formula as literals, so a filter
// such as [Customer] = {{ trigger.row.Customer }} stays a valid formula
// whatever the value holds.
func fillFormula(src string, s interp.Scope) (string, error) {
	var ferr error
	out := bindingRe.ReplaceAllStringFunc(src, func(m string) string {
		if ferr != nil {
			return ""
		}
		t, err := interp.Parse(m)
		if err != nil {
			ferr = err
			return ""
		}
		v, err := t.Value(s)
		if err != nil {
			ferr = err
			return ""
		}
		return literal(v)
	})
	return out, ferr
}

func literal(v any) string {
	switch x := v.(type) {
	case nil:
		return `""`
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case float64:
		return formula.FormatNumber(x)
	case int64, int:
		return interp.ToText(x)
	}
	return `"` + strings.ReplaceAll(interp.ToText(v), `"`, `""`) + `"`
}

type noMailer struct{}

func (noMailer) Send(context.Context, *email.Mail) (*email.Receipt, error) {
	return nil, email.ErrNotSetUp
}

func oneLine(s string) string { return email.OneLine(s) }
