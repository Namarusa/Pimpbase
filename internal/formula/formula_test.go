package formula

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type testScope map[string]Type

func (s testScope) FieldType(name string) (Type, bool) {
	t, ok := s[strings.ToLower(name)]
	return t, ok
}

func (s testScope) NameType(parts []string) (Type, bool) {
	if strings.EqualFold(strings.Join(parts, "."), "Current.User.Email") {
		return Text, true
	}
	return Any, false
}

type testEnv map[string]any

func (e testEnv) Field(name string) any { return e[strings.ToLower(name)] }
func (e testEnv) Name(parts []string) any {
	return "ada@example.com"
}
func (e testEnv) Now() time.Time { return time.Date(2026, 10, 18, 15, 30, 0, 0, time.UTC) }

var scope = testScope{
	"qty": Number, "price": Number, "blank": Number, "name": Text, "note": Text,
	"done": Bool, "due": Date, "tags": List, "unit price": Number,
}

var env = testEnv{
	"qty": 3.0, "price": 2.5, "blank": nil, "name": "Ada", "note": nil,
	"done": true, "due": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	"tags": []any{"red", "blue"}, "unit price": 4.0,
}

func eval(t *testing.T, src string) any {
	t.Helper()
	c, errs := Compile(src, scope)
	if errs != nil {
		t.Fatalf("%s: %v", src, errs[0])
	}
	v, err := c.Eval(env)
	if err != nil {
		t.Fatalf("%s: eval: %v", src, err)
	}
	return v
}

func TestEval(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"1 + 2 * 3", 7.0},
		{"(1 + 2) * 3", 9.0},
		{"2 ^ 3 ^ 2", 512.0},
		{"-2 ^ 2", 4.0},
		{"=[Qty] * [Price]", 7.5},
		{"[Unit price] * [qty]", 12.0},
		{"[Blank] + 1", 1.0},
		{"[Qty] / 0", nil},
		{`[Name] & " " & [Note] & "!"`, "Ada !"},
		{`"say ""hi"""`, `say "hi"`},
		{`IF([Qty] > 2, "many", "few")`, "many"},
		{`IF([Qty] > 5, "many")`, nil},
		{"[Qty] > 2 AND NOT [Done]", false},
		{"[Qty] > 2 OR 1 / 0 > 1", true},
		{`[Name] IN ("Bob", "Ada")`, true},
		{`[Name] NOT IN ("Bob", "Ada")`, false},
		{"ISBLANK([Blank])", true},
		{"ISBLANK([Note])", true},
		{"ISBLANK([Qty])", false},
		{"[Blank] = 0", true},
		{`[Note] = ""`, true},
		{"BLANKVALUE([Blank], 9)", 9.0},
		{"ROUND(2.345, 2)", 2.35},
		{"ROUND(-2.5)", -3.0},
		{"MOD(-7, 3)", 2.0},
		{"SUM(1, 2, [Blank], 3)", 6.0},
		{"AVERAGE(2, 4)", 3.0},
		{"MAX(1, 7, 3)", 7.0},
		{`LEN("héllo")`, 5.0},
		{`LEFT("héllo", 2)`, "hé"},
		{`RIGHT("hello", 3)`, "llo"},
		{`MID("hello", 2, 3)`, "ell"},
		{`UPPER([Name])`, "ADA"},
		{`CONTAINS([Tags], "red")`, true},
		{`CONTAINS([Name], "d")`, true},
		{`JOIN([Tags], "+")`, "red+blue"},
		{`COUNT(SPLIT("a,b,c", ","))`, 3.0},
		{`TEXT(1.50)`, "1.5"},
		{`VALUE("42")`, 42.0},
		{"YEAR([Due])", 2026.0},
		{"WEEKDAY(DATE(2026, 10, 18))", 7.0},
		{`DATEDIFF([Due], TODAY(), "days")`, 17.0},
		{`DATEDIFF(DATE(2026,1,31), DATE(2026,3,30), "months")`, 1.0},
		{`DATEADD([Due], 1, "month")`, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{`[Due] < TODAY()`, true},
		{`IFERROR(VALUE("x"), -1)`, -1.0},
		{`Current.User.Email`, "ada@example.com"},
		{`not true`, false},
	}
	for _, c := range cases {
		got := eval(t, c.src)
		if gt, ok := got.(time.Time); ok {
			if !gt.Equal(c.want.(time.Time)) {
				t.Errorf("%s = %v, want %v", c.src, got, c.want)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s = %#v, want %#v", c.src, got, c.want)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		src, msg string
		start    int
	}{
		{"[Qty] + ", "ends too early", 8},
		{"[Nope] + 1", "no field named [Nope]", 0},
		{`[Qty] + "a"`, "+ needs numbers, got Text (use & to join text)", 8},
		{"FOO(1)", "unknown function FOO", 0},
		{"ROUND()", "ROUND takes 1 to 2 arguments", 0},
		{`"open`, "missing its closing quote", 0},
		{`[Qty] = "x" AND [Done] = 1`, "cannot compare Boolean with Number", 16},
		{"1 2", `unexpected "2"`, 2},
		{"", "empty", 0},
	}
	for _, c := range cases {
		_, errs := Compile(c.src, scope)
		if len(errs) == 0 {
			t.Errorf("%q: no error", c.src)
			continue
		}
		if !strings.Contains(errs[0].Message, c.msg) || errs[0].Start != c.start {
			t.Errorf("%q: got %q at %d, want %q at %d", c.src, errs[0].Message, errs[0].Start, c.msg, c.start)
		}
	}
}

func TestRenameField(t *testing.T) {
	got := RenameField(`[Qty]*[qty] & "[Qty]"`, "Qty", "Quantity")
	if want := `[Quantity]*[Quantity] & "[Qty]"`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTypes(t *testing.T) {
	for src, want := range map[string]Type{
		"[Qty] * 2":            Number,
		`IF([Done], "a", "b")`: Text,
		`IF([Done], 1)`:        Number,
		`IF([Done], 1, "a")`:   Any,
		"[Due]":                Date,
		"[Qty] > 1":            Bool,
	} {
		c, errs := Compile(src, scope)
		if errs != nil {
			t.Fatal(errs[0])
		}
		if c.Type != want {
			t.Errorf("%s: type %s, want %s", src, c.Type, want)
		}
	}
}

func TestRenameAcrossLinks(t *testing.T) {
	src := `[Qty] + SUMOF([Lines], [Qty], [Qty] > 0) + [Lines].[Qty] + [Cust].[Qty]`
	if got, want := RenameField(src, "Qty", "N"), `[N] + SUMOF([Lines], [Qty], [Qty] > 0) + [Lines].[Qty] + [Cust].[Qty]`; got != want {
		t.Errorf("RenameField: got %s, want %s", got, want)
	}
	if got, want := RenameField(src, "Lines", "Items"), `[Qty] + SUMOF([Items], [Qty], [Qty] > 0) + [Items].[Qty] + [Cust].[Qty]`; got != want {
		t.Errorf("RenameField of a link: got %s, want %s", got, want)
	}
	if got, want := RenameLinkedField(src, "lines", "Qty", "Units"), `[Qty] + SUMOF([Lines], [Units], [Units] > 0) + [Lines].[Units] + [Cust].[Qty]`; got != want {
		t.Errorf("RenameLinkedField: got %s, want %s", got, want)
	}
	if got := fmt.Sprint(LinkedFieldRefs(src, "Cust")); got != "[Qty]" {
		t.Errorf("LinkedFieldRefs: %s", got)
	}
	n, _ := Parse(src)
	if got := fmt.Sprint(FieldRefs(n)); got != "[Qty Lines Cust]" {
		t.Errorf("FieldRefs: %s", got)
	}
}

func TestLinkErrors(t *testing.T) {
	s := linkScope{diffScope}
	for src, msg := range map[string]string{
		"[n].[x]":               "[n] does not link to another table",
		"[nope].[x]":            "no field named [nope]",
		"[cust].[x]":            "has no field named [x]",
		"SUMOF([lines], [st])":  "the value of SUMOF must be a number",
		"SUMOF([n], 1)":         "[n] does not link",
		"SUMOF(1, 1)":           "must be a link field",
		"COUNTOF([lines], [n])": "no field named [n]",
		"SUMOF([lines])":        "SUMOF takes 2 to 3 arguments",
	} {
		_, errs := Compile(src, s)
		if len(errs) == 0 || !strings.Contains(errs[0].Message, msg) {
			t.Errorf("%s: got %v, want %q", src, errs, msg)
		}
	}
	if _, errs := Compile("[cust].[nm]", scope); len(errs) == 0 || !strings.Contains(errs[0].Message, "cannot be read here") {
		t.Errorf("links outside tables: %v", errs)
	}
}
