package flows

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pimpbase/pimpbase/internal/interp"
)

func TestCron(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	at := func(s string) time.Time {
		tm, err := time.ParseInLocation("2006-01-02 15:04", s, paris)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	cases := []struct {
		expr, when string
		want       bool
	}{
		{"*/15 * * * *", "2026-10-20 10:45", true},
		{"*/15 * * * *", "2026-10-20 10:46", false},
		{"30 8 * * mon-fri", "2026-10-20 08:30", true}, // a Tuesday
		{"30 8 * * mon-fri", "2026-10-25 08:30", false},
		{"0 9 1 * *", "2026-11-01 09:00", true},
		{"0 9 1,15 * *", "2026-11-15 09:00", true},
		{"0 0 * * 7", "2026-10-25 00:00", true},    // 7 is Sunday
		{"0 9 13 * fri", "2026-10-16 09:00", true}, // either day field matches
		{"@daily", "2026-10-20 00:00", true},
		{"0 12 * jan-mar *", "2026-10-20 12:00", false},
	}
	for _, c := range cases {
		s, err := ParseCron(c.expr)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got := s.Match(at(c.when)); got != c.want {
			t.Errorf("%s at %s = %v, want %v", c.expr, c.when, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "5-1 * * * *", "*/0 * * * *", "* * * foo *"} {
		if _, err := ParseCron(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	next, err := NextRuns("30 8 * * mon-fri", "Europe/Paris", at("2026-10-23 09:00"), 3)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range next {
		got = append(got, n.Format("Mon 02 15:04"))
	}
	if strings.Join(got, ", ") != "Mon 26 08:30, Tue 27 08:30, Wed 28 08:30" {
		t.Errorf("next = %v", got)
	}
}

func TestBlocks(t *testing.T) {
	scope := interp.Map{
		"steps": map[string]any{"Find": map[string]any{"rows": []any{
			map[string]any{"Name": "Dune", "Qty": 2.0},
			map[string]any{"Name": `Emma "the novel"`, "Qty": 1.0},
		}}},
		"trigger": map[string]any{"row": map[string]any{"Paid": true, "Customer": `Bob "B" Smith`}},
	}
	out, err := render("{{#each steps.Find.rows}}{{ number }}. {{ item.Name }} x{{ item.Qty }}\n{{/each}}{{#if trigger.row.Paid}}paid{{else}}due{{/if}}", interp.Text, scope)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1. Dune x2\n2. Emma \"the novel\" x1\npaid" {
		t.Errorf("render = %q", out)
	}
	js, err := render(`{"names": [{{#each steps.Find.rows}}"{{ item.Name }}",{{/each}}""]}`, interp.JSON, scope)
	if err != nil || js != `{"names": ["Dune","Emma \"the novel\"",""]}` {
		t.Errorf("json = %q, %v", js, err)
	}
	same, err := render("{{#each steps.Find.rows}}{{ loop.number }}/{{ loop.count }} {{ loop.item[\"Name\"] }};{{/each}}", interp.Text, scope)
	if err != nil || same != "1/2 Dune;2/2 Emma \"the novel\";" {
		t.Errorf("loop names = %q, %v", same, err)
	}
	v, err := value("{{ steps.Find.rows }}", scope)
	if list, ok := v.([]any); err != nil || !ok || len(list) != 2 {
		t.Errorf("value = %v, %v", v, err)
	}
	f, err := fillFormula(`[Customer] = {{ trigger.row.Customer }} AND [Paid] = {{ trigger.row.Paid }}`, scope)
	if err != nil || f != `[Customer] = "Bob ""B"" Smith" AND [Paid] = TRUE` {
		t.Errorf("formula = %q, %v", f, err)
	}
	for _, bad := range []string{"{{#each x}}", "{{/if}}", "{{#if}}a{{/if}}", "{{else}}", "{{#each a}}{{/if}}", "{{ unclosed"} {
		if err := checkText(bad); err == nil {
			t.Errorf("%q passed the check", bad)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		l    any
		op   string
		r    any
		want bool
	}{
		{"10", "gt", "9", true}, // numbers compare as numbers
		{"b", "gt", "a", true},
		{5.0, "equals", "5", true},
		{"Done", "equals", "done", false},
		{"Hello World", "contains", "world", true},
		{[]any{"red", "blue"}, "contains", "blue", true},
		{[]any{"red"}, "not_contains", "blue", true},
		{"", "empty", nil, true},
		{0.0, "not_empty", nil, true},
		{"yes", "true", nil, true},
		{"0", "false", nil, true},
	}
	for _, c := range cases {
		got, err := compare(c.l, c.op, c.r)
		if err != nil || got != c.want {
			t.Errorf("%v %s %v = %v, %v", c.l, c.op, c.r, got, err)
		}
	}
}

func TestCheck(t *testing.T) {
	f := &Flow{Name: "Invoices", Definition: Definition{
		Trigger: Trigger{Type: OnSchedule, Cron: "0 8 * * *", Timezone: "Europe/Paris"},
		Steps: []*Step{
			{Name: "Due", Type: "find_rows", Inputs: map[string]any{"table": "tbl_x"}},
			{Name: "Each", Type: "loop", Inputs: map[string]any{"items": "{{ steps.Due.rows }}"}, Do: []*Step{
				{Name: "Mail", Type: "send_email", Inputs: map[string]any{"to": "{{ loop.item.Email }}"}},
			}},
		},
	}}
	if err := f.Check(); err != nil {
		t.Fatal(err)
	}
	if f.Steps[1].Do[0].ID == "" {
		t.Error("steps get ids")
	}
	f.Steps[1].Do[0].Name = "due"
	if err := f.Check(); err == nil || !strings.Contains(err.Error(), "two steps are named") {
		t.Errorf("duplicate names: %v", err)
	}
	f.Steps[1].Do[0].Name = "Mail"
	f.Steps[1].Do[0].Inputs["to"] = "{{ loop.item.Email"
	if err := f.Check(); err == nil {
		t.Error("a broken binding passed")
	}
	f.Steps[1].Do[0].Inputs["to"] = "x"
	f.Trigger.Timezone = "Mars/Olympus"
	if err := f.Check(); err == nil {
		t.Error("an unknown zone passed")
	}
}

func TestPDF(t *testing.T) {
	d := &Document{
		Title:  "Invoice INV-7 — Société Générale",
		Footer: "Acme SARL · 1 rue de Rivoli, Paris",
		Accent: "#0f766e",
		Content: "## Bill to\n**Bob Smith**, 12 *Main* Street\n\n" +
			"| Item | Qty | Price |\n|---|---:|---:|\n| Dune, a long title that wraps around the cell because it is long | 2 | €9.50 |\n| Emma | 1 | €4.00 |\n\n" +
			"- first\n  - nested\n1. one\n---\nThanks!\n\\newpage\nSecond page",
	}
	data, err := d.PDF()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.Contains(data, []byte("%%EOF")) {
		t.Fatalf("not a PDF: %q", data[:20])
	}
	if n := bytes.Count(data, []byte("/Type /Page\n")); n != 2 {
		t.Errorf("pages = %d, want 2", n)
	}
}

func TestTrimLog(t *testing.T) {
	rows := make([]any, 400)
	for i := range rows {
		rows[i] = map[string]any{"id": float64(i), "name": strings.Repeat("é", 300)}
	}
	var cut bool
	out := trimLog(map[string]any{"rows": rows, "ok": true}, &cut)
	m, ok := out.(map[string]any)
	if !cut || !ok || m["ok"] != true {
		t.Fatalf("trimLog = %v, cut %v", out, cut)
	}
	list := m["rows"].([]any)
	if len(list) == 0 || len(list) > 20 || list[0].(map[string]any)["id"] != 0.0 {
		t.Errorf("rows kept: %d", len(list))
	}
	if name := list[0].(map[string]any)["name"].(string); !utf8.ValidString(name) || len(name) > 1004 {
		t.Errorf("name cut badly: %d bytes", len(name))
	}
	cut = false
	if trimLog(map[string]any{"a": 1.0}, &cut); cut {
		t.Error("a small value was marked as cut")
	}
}
