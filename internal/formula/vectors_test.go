package formula

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The vectors are formulas with the results Go must give. They are also
// written to testdata/vectors.json, which the browser's formula engine
// (web/builder/src/lib/formula) is tested against, so the two engines
// give the same answers. Run with -update after changing them.

var update = flag.Bool("update", false, "rewrite testdata/vectors.json")

type vector struct {
	src  string
	want any // errWant when evaluating must fail
}

type errWant struct{}

var failed = errWant{}

func d(s string) time.Time {
	t, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return t
}

func l(v ...any) []any { return v }

// vectorScope and vectorEnv are what the vectors can refer to.
var vectorScope = testScope{
	"qty": Number, "price": Number, "blank": Number, "name": Text, "note": Text, "word": Text,
	"done": Bool, "due": Date, "at": Date, "nodate": Date, "tags": List, "nums": List, "unit price": Number,
}

var vectorEnv = testEnv{
	"qty": 3.0, "price": 2.5, "blank": nil, "name": "Ada", "note": nil, "word": "hello world",
	"done": true, "due": d("2026-10-01"), "at": d("2026-10-18T15:30:45Z"), "nodate": nil,
	"tags": l("red", "blue", "red"), "nums": l(3.0, 1.0, nil, 2.0), "unit price": 4.0,
}

var vectors = []vector{
	// Operators and basics.
	{"1 + 2 * 3", 7.0}, {"2 ^ 3 ^ 2", 512.0}, {"-[qty] + 1", -2.0}, {"[qty] / 0", nil}, {`"a" & 1 & TRUE`, "a1true"},
	{"[qty] * [price]", 7.5}, {"[blank] + 1", 1.0}, {"[note] & \"!\"", "!"}, {"[qty] > 2 AND NOT [done]", false},
	{"[qty] IN (1, 2, 3)", true}, {"[name] NOT IN (\"Bo\")", true}, {"[blank] = 0", true}, {"[due] = NULL", false},
	{`[due] < "2026-10-02"`, true}, {`"10" > 9`, true}, {`"abc" < "abd"`, true}, {"[unit price] * 2", 8.0},
	{"Current.User.Email", "ada@example.com"}, {"Current.User.[Email]", "ada@example.com"}, {`VALUE("x") + 1`, failed}, {"[nodate] < [due]", false}, {"[nodate] = NULL", false},

	// Logic.
	{`IF([done], "y", "n")`, "y"}, {`IF(FALSE, 1)`, nil}, {"AND([done], [qty] > 1)", true}, {"OR(FALSE, [blank] > 0)", false},
	{"NOT([done])", false}, {"ISBLANK([note])", true}, {`ISBLANK("")`, true}, {"ISBLANK([tags])", false}, {"BLANKVALUE([note], \"-\")", "-"},
	{"IFERROR(VALUE(\"x\"), 0)", 0.0}, {"IFERROR(1, 0)", 1.0},
	{`SWITCH([qty], 1, "one", 3, "three", "many")`, "three"}, {`SWITCH([qty], 1, "one", "many")`, "many"}, {`SWITCH([qty], 1, "one")`, nil},
	{`SWITCH([name], "ada", 1, "Ada", 2)`, 2.0},
	{`IFS([qty] > 5, "big", [qty] > 1, "mid", TRUE, "small")`, "mid"}, {`IFS(FALSE, 1)`, nil},
	{"COALESCE([note], [blank], [name])", "Ada"}, {`COALESCE("", [note])`, nil}, {"COALESCE([blank], 5)", 5.0},
	{"ISNUMBER([qty])", true}, {`ISNUMBER(" 12.5 ")`, true}, {`ISNUMBER("1e3")`, true}, {`ISNUMBER("12a")`, false}, {`ISNUMBER("0x10")`, false}, {"ISNUMBER([note])", false},

	// Text.
	{`CONCAT("a", 1, [blank], TRUE)`, "a1true"}, {"UPPER([name])", "ADA"}, {"LOWER([name])", "ada"}, {`TRIM("  a b  ")`, "a b"},
	{"LEN([word])", 11.0}, {`LEN("éé")`, 2.0}, {"LEFT([word], 4)", "hell"}, {"RIGHT([word], 3)", "rld"}, {"MID([word], 7, 3)", "wor"},
	{`MID("abc", 0, 2)`, "ab"}, {`LEFT("abc", -1)`, ""}, {`CONTAINS([word], "lo w")`, true}, {`CONTAINS([tags], "blue")`, true},
	{`STARTSWITH([word], "he")`, true}, {`ENDSWITH([word], "x")`, false}, {`REPLACE([word], "o", "0")`, "hell0 w0rld"},
	{"TEXT(1.50)", "1.5"}, {"TEXT([due])", "2026-10-01"}, {"TEXT([at])", "2026-10-18T15:30:45Z"}, {`VALUE(" 42 ")`, 42.0}, {`VALUE("")`, nil},
	{`SPLIT("a,b,,c", ",")`, l("a", "b", "", "c")}, {`SPLIT("", ",")`, l()}, {`JOIN([tags], "/")`, "red/blue/red"},
	{"PROPER([word])", "Hello World"}, {`PROPER("o'neil MCDONALD 2nd")`, "O'neil Mcdonald 2nd"},
	{`PADLEFT("7", 3, "0")`, "007"}, {`PADLEFT("abcd", 3, "0")`, "abcd"}, {`PADRIGHT("ab", 5, "xy")`, "abxyx"}, {`PADLEFT("a", 3)`, "  a"},
	{`REPT("ab", 3)`, "ababab"}, {`REPT("x", 0)`, ""}, {`REPT("x", -2)`, ""},
	{`FIND("o", [word])`, 5.0}, {`FIND("o", [word], 6)`, 8.0}, {`FIND("z", [word])`, 0.0}, {`FIND("", "abc")`, 1.0}, {`FIND("é", "aéb")`, 2.0},
	{`FIND("a", "abc", 9)`, 0.0},
	{`REGEXMATCH([word], "^h.*d$")`, true}, {`REGEXMATCH([name], "(?i)^ADA$")`, true}, {`REGEXMATCH([name], "^ADA$")`, false},
	{`REGEXEXTRACT("Order #1234 shipped", "[0-9]+")`, "1234"}, {`REGEXEXTRACT("id=42;", "id=([0-9]+)")`, "42"}, {`REGEXEXTRACT("abc", "[0-9]")`, nil},
	{`REGEXREPLACE("2026-10-18", "([0-9]+)-([0-9]+)-([0-9]+)", "$3/$2/$1")`, "18/10/2026"}, {`REGEXREPLACE("a.b.c", "\.", "$$")`, "a$b$c"},
	{`REGEXREPLACE("aaa", "a", "$0$0")`, "aaaaaa"}, {`REGEXREPLACE("cab ab", "\bab", "X")`, "cab X"}, {`REGEXMATCH("a", "(")`, failed},
	{`FORMATDATE([at], "YYYY-MM-DD HH:mm:ss")`, "2026-10-18 15:30:45"}, {`FORMATDATE([at], "D MMM YY")`, "18 Oct 26"},
	{`FORMATDATE([at], "dddd D MMMM YYYY")`, "Sunday 18 October 2026"}, {`FORMATDATE([at], "ddd h:mm A")`, "Sun 3:30 PM"},
	{`FORMATDATE([at], "[Week of] M/D")`, "Week of 10/18"}, {`FORMATDATE([due], "hh a")`, "12 am"},
	{`FORMATDATE([at], "HH:mm", "Europe/Paris")`, "17:30"}, {`FORMATDATE([at], "D HH:mm", "America/New_York")`, "18 11:30"},
	{`FORMATDATE([at], "HH", "Mars/Base")`, failed}, {`FORMATDATE([nodate], "YYYY")`, nil},
	{"FORMATNUMBER(1234567.891, 2)", "1,234,567.89"}, {"FORMATNUMBER(-0.004, 2)", "0.00"}, {"FORMATNUMBER(-1234.5)", "-1,235"},
	{`FORMATNUMBER(1234.5, 1, " ")`, "1 234.5"}, {`FORMATNUMBER(999, 0, "")`, "999"}, {"FORMATNUMBER(0.125, 2)", "0.13"}, {"FORMATNUMBER([blank], 2)", nil},

	// Numbers.
	{"ROUND(2.345, 2)", 2.35}, {"ROUND(-2.5)", -3.0}, {"ROUND(1234, -2)", 1200.0}, {"FLOOR(-1.5)", -2.0}, {"CEILING(1.2)", 2.0},
	{"ABS(-3)", 3.0}, {"SQRT(16)", 4.0}, {"SQRT(-1)", failed}, {"POWER(2, 10)", 1024.0}, {"MOD(-7, 3)", 2.0}, {"MOD(7, 0)", nil},
	{"SUM([nums], 4)", 10.0}, {"AVERAGE([nums])", 2.0}, {"MIN([nums], 0.5)", 0.5}, {"MAX(1, [qty])", 3.0}, {"COUNT([nums])", 3.0},
	{"ROUNDUP(1.1, 1)", 1.1}, {"ROUNDUP(1.11, 1)", 1.2}, {"ROUNDUP(-1.11, 1)", -1.2}, {"ROUNDUP(2)", 2.0}, {"ROUNDUP(2.0001)", 3.0},
	{"ROUNDDOWN(1.99, 1)", 1.9}, {"ROUNDDOWN(-1.99)", -1.0}, {"ROUNDDOWN(1234, -2)", 1200.0}, {"ROUNDDOWN(0.3 * 3, 1)", 0.9},
	{"SIGN(-4)", -1.0}, {"SIGN(0)", 0.0}, {"SIGN([qty])", 1.0}, {"LN(1)", 0.0}, {"LN(0)", failed}, {"LOG(1000)", 3.0}, {"LOG(8, 2)", 3.0},
	{"LOG(8, 1)", failed}, {"EXP(0)", 1.0}, {"ROUND(PI(), 4)", 3.1416}, {"RANDBETWEEN(5, 5)", 5.0}, {"RANDBETWEEN(3, 1)", failed},

	// Dates.
	{"TODAY()", d("2026-10-18")}, {"NOW()", d("2026-10-18T15:30:00Z")}, {"DATE(2026, 2, 30)", d("2026-03-02")}, {`DATEVALUE("2026-10-18 08:00:00")`, d("2026-10-18T08:00:00Z")},
	{"YEAR([due])", 2026.0}, {"MONTH([due])", 10.0}, {"DAY([due])", 1.0}, {"WEEKDAY([due])", 4.0}, {"YEAR([nodate])", nil},
	{`DATEADD([due], 1, "month")`, d("2026-11-01")}, {`DATEADD([due], -2, "weeks")`, d("2026-09-17")}, {`DATEADD([at], 90, "minutes")`, d("2026-10-18T17:00:45Z")},
	{`DATEDIFF([due], [at], "days")`, 17.0}, {`DATEDIFF(DATE(2026, 1, 31), DATE(2026, 2, 28), "months")`, 0.0}, {`DATEDIFF([due], DATE(2028, 10, 1), "years")`, 2.0},
	{`DATEADD([due], 1, "fortnight")`, failed},
	{"EOMONTH([due])", d("2026-10-31")}, {"EOMONTH([due], 4)", d("2027-02-28")}, {"EOMONTH(DATE(2024, 1, 15), 1)", d("2024-02-29")}, {"EOMONTH([at], -1)", d("2026-09-30")},
	{"WEEKNUM(DATE(2026, 1, 1))", 1.0}, {"WEEKNUM(DATE(2027, 1, 1))", 53.0}, {"WEEKNUM([at])", 42.0},
	{"QUARTER([due])", 4.0}, {"QUARTER(DATE(2026, 3, 31))", 1.0}, {"HOUR([at])", 15.0}, {"MINUTE([at])", 30.0}, {"HOUR([due])", 0.0},
	{"WORKDAYS(DATE(2026, 10, 19), DATE(2026, 10, 25))", 5.0}, {"WORKDAYS(DATE(2026, 10, 24), DATE(2026, 10, 25))", 0.0},
	{"WORKDAYS(DATE(2026, 10, 30), DATE(2026, 10, 19))", -10.0}, {"WORKDAYS([due], [at])", 12.0}, {"WORKDAYS([nodate], [due])", nil},
	{"ADDWORKDAYS(DATE(2026, 10, 16), 1)", d("2026-10-19")}, {"ADDWORKDAYS(DATE(2026, 10, 19), -1)", d("2026-10-16")},
	{"ADDWORKDAYS(DATE(2026, 10, 17), 0)", d("2026-10-17")}, {"ADDWORKDAYS([at], 10)", d("2026-10-30")},

	// Lists.
	{"LIST(1, \"a\", [note])", l(1.0, "a", nil)}, {"LIST()", l()}, {"FIRST([tags])", "red"}, {"LAST([nums])", 2.0}, {"FIRST(LIST())", nil},
	{"FIRST([name])", "Ada"}, {"INDEX([tags], 2)", "blue"}, {"INDEX([tags], -1)", "red"}, {"INDEX([tags], 9)", nil}, {"INDEX([tags], 0)", nil},
	{"SLICE([tags], 2)", l("blue", "red")}, {"SLICE([tags], 1, 2)", l("red", "blue")}, {"SLICE([tags], 5, 2)", l()}, {"SLICE([tags], 0, 1)", l("red")},
	{"SORT([nums])", l(1.0, 2.0, 3.0, nil)}, {"SORT([nums], TRUE)", l(3.0, 2.0, 1.0, nil)}, {"SORT([tags])", l("blue", "red", "red")},
	{"SORT(LIST(10, \"9\"))", l(10.0, "9")}, {"SORT(LIST([at], [due]))", l(d("2026-10-01"), d("2026-10-18T15:30:45Z"))},
	{"UNIQUE([tags])", l("red", "blue")}, {"UNIQUE(LIST(1, \"1\", 1))", l(1.0, "1")}, {"COUNT(UNIQUE([tags]))", 2.0}, {"JOIN(SORT(UNIQUE([tags])), \", \")", "blue, red"},

	// Other.
	{"LEN(UUID())", 36.0}, {`REGEXMATCH(UUID(), "^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")`, true},
}

func TestVectors(t *testing.T) {
	for _, v := range vectors {
		c, errs := Compile(v.src, vectorScope)
		if errs != nil {
			t.Errorf("%s: %v", v.src, errs[0])
			continue
		}
		got, err := c.Eval(vectorEnv)
		if v.want == failed {
			if err == nil {
				t.Errorf("%s: gave %#v, want an error", v.src, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", v.src, err)
			continue
		}
		if !same(got, v.want) {
			t.Errorf("%s: got %#v, want %#v", v.src, got, v.want)
		}
	}

	// Every function has at least one vector, so the browser engine is
	// tested on all of them.
	for name := range functions {
		if strings.HasSuffix(name, "OF") && functions[name].rollup {
			continue // rollups only run in tables
		}
		found := false
		for _, v := range vectors {
			if strings.Contains(v.src, name+"(") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no vector uses %s", name)
		}
	}

	js, err := vectorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/vectors.json"
	if *update {
		if err := os.WriteFile(path, js, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	old, err := os.ReadFile(path)
	if err != nil || string(old) != string(js) {
		t.Errorf("%s is out of date; run go test ./internal/formula -run TestVectors -update", path)
	}
}

func same(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.Abs(x-y) < 1e-12*math.Max(1, math.Abs(y)))
	case time.Time:
		y, ok := b.(time.Time)
		return ok && x.Equal(y)
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !same(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// jsonValue encodes a formula value for the browser tests: dates become
// {"date": "..."} so they are not mistaken for text.
func jsonValue(v any) any {
	switch x := v.(type) {
	case time.Time:
		return map[string]string{"date": x.UTC().Format(DateLayout)}
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonValue(e)
		}
		return out
	case errWant:
		return map[string]bool{"error": true}
	}
	return v
}

func vectorsJSON() ([]byte, error) {
	fields := map[string]any{}
	types := map[string]string{}
	for k, v := range vectorEnv {
		fields[k] = jsonValue(v)
		types[k] = strings.ToLower(vectorScope[k].String())
	}
	var cases []map[string]any
	for _, v := range vectors {
		cases = append(cases, map[string]any{"src": v.src, "want": jsonValue(v.want)})
	}
	out := map[string]any{
		"comment": "Generated by internal/formula/vectors_test.go; do not edit.",
		"now":     vectorEnv.Now().Format(DateLayout),
		"user":    map[string]string{"email": fmt.Sprint(vectorEnv.Name([]string{"Current", "User", "Email"}))},
		"fields":  fields,
		"types":   types,
		"cases":   cases,
	}
	b, err := json.MarshalIndent(out, "", " ")
	return append(b, '\n'), err
}
