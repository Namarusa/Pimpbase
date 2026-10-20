package formula

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type function struct {
	category string
	sig      string
	doc      string
	params   []Type
	min      int
	variadic bool // the last param repeats
	ret      Type
	retFn    func(args []Type) Type
	eval     func(env Env, args []any) (any, error)
	// lazy functions get their arguments unevaluated (IF, AND, OR...).
	lazy func(ev *evaluator, args []Node) (any, error)
	// check reports a problem with the argument count beyond min.
	check func(args []Type) string
	// rollup functions read rows linked through their first argument.
	rollup bool
	// argType overrides params for functions whose arguments alternate.
	argType func(i int) Type
}

func (f *function) param(i int) Type {
	if f.argType != nil {
		return f.argType(i)
	}
	if i >= len(f.params) {
		return f.params[len(f.params)-1]
	}
	return f.params[i]
}

func (f *function) result(args []Type) Type {
	if f.retFn != nil {
		return f.retFn(args)
	}
	return f.ret
}

func (f *function) arity() string {
	switch {
	case f.variadic:
		return fmt.Sprintf("at least %d argument(s)", f.min)
	case f.min == len(f.params):
		return fmt.Sprintf("%d argument(s)", f.min)
	}
	return fmt.Sprintf("%d to %d arguments", f.min, len(f.params))
}

// FunctionInfo describes a function for the formula editor.
type FunctionInfo struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
}

// Functions lists every function, sorted by name.
func Functions() []FunctionInfo {
	out := make([]FunctionInfo, 0, len(functions))
	for name, f := range functions {
		out = append(out, FunctionInfo{name, f.category, f.sig, f.doc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func num1(fn func(float64) float64) func(Env, []any) (any, error) {
	return func(_ Env, a []any) (any, error) {
		x, err := toNumber(a[0])
		if err != nil {
			return nil, err
		}
		return fn(x), nil
	}
}

func text1(fn func(string) any) func(Env, []any) (any, error) {
	return func(_ Env, a []any) (any, error) { return fn(ToText(a[0])), nil }
}

func firstType(args []Type) Type {
	t := Null
	for _, a := range args {
		t = unify(t, a)
	}
	return t
}

// numbers flattens arguments that may be numbers or lists of numbers.
func numbers(args []any) ([]float64, error) {
	var out []float64
	for _, a := range args {
		for _, v := range toList(a) {
			if v == nil {
				continue
			}
			f, err := toNumber(v)
			if err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	return out, nil
}

func roundTo(x float64, digits float64) float64 {
	p := math.Pow(10, math.Trunc(digits))
	return math.Round(x*p) / p // half away from zero, as in spreadsheets
}

func runeSlice(s string, start, n int) string {
	r := []rune(s)
	if start < 0 {
		start = 0
	}
	if start > len(r) {
		return ""
	}
	end := start + n
	if n < 0 || end > len(r) {
		end = len(r)
	}
	return string(r[start:end])
}

func dateArg(a any) (time.Time, bool, error) {
	t, ok, err := toDate(a)
	return t, ok, err
}

func addDate(t time.Time, n float64, unit string) (time.Time, error) {
	i := int(n)
	switch strings.ToLower(strings.TrimSuffix(strings.ToLower(unit), "s")) {
	case "second":
		return t.Add(time.Duration(n * float64(time.Second))), nil
	case "minute":
		return t.Add(time.Duration(n * float64(time.Minute))), nil
	case "hour":
		return t.Add(time.Duration(n * float64(time.Hour))), nil
	case "day":
		return t.AddDate(0, 0, i), nil
	case "week":
		return t.AddDate(0, 0, 7*i), nil
	case "month":
		return t.AddDate(0, i, 0), nil
	case "year":
		return t.AddDate(i, 0, 0), nil
	}
	return t, fmt.Errorf("unknown unit %q (use days, weeks, months, years, hours, minutes or seconds)", unit)
}

func diffDates(a, b time.Time, unit string) (float64, error) {
	d := b.Sub(a)
	switch strings.TrimSuffix(strings.ToLower(unit), "s") {
	case "second":
		return math.Trunc(d.Seconds()), nil
	case "minute":
		return math.Trunc(d.Minutes()), nil
	case "hour":
		return math.Trunc(d.Hours()), nil
	case "day":
		return math.Trunc(d.Hours() / 24), nil
	case "week":
		return math.Trunc(d.Hours() / 24 / 7), nil
	case "month", "year":
		months := (b.Year()-a.Year())*12 + int(b.Month()-a.Month())
		// Count only whole months.
		if months > 0 && b.AddDate(0, -months, 0).Before(a) {
			months--
		} else if months < 0 && b.AddDate(0, -months, 0).After(a) {
			months++
		}
		if strings.HasPrefix(strings.ToLower(unit), "year") {
			return math.Trunc(float64(months) / 12), nil
		}
		return float64(months), nil
	}
	return 0, fmt.Errorf("unknown unit %q (use days, weeks, months, years, hours, minutes or seconds)", unit)
}

var functions map[string]*function

func init() {
	functions = map[string]*function{
		// Logic
		"IF": {category: "Logic", sig: "IF(condition, then, else)", doc: "Returns then when the condition is true, otherwise else (blank when omitted).",
			params: []Type{Bool, Any, Any}, min: 2,
			retFn: func(a []Type) Type {
				if len(a) < 3 {
					return unify(a[1], Null)
				}
				return unify(a[1], a[2])
			},
			lazy: func(ev *evaluator, args []Node) (any, error) {
				c, err := ev.eval(args[0])
				if err != nil {
					return nil, err
				}
				ok, err := toBool(c)
				if err != nil {
					return nil, err
				}
				if ok {
					return ev.eval(args[1])
				}
				if len(args) > 2 {
					return ev.eval(args[2])
				}
				return nil, nil
			}},
		"AND": {category: "Logic", sig: "AND(a, b, ...)", doc: "True when every argument is true.", params: []Type{Bool}, min: 1, variadic: true, ret: Bool,
			lazy: func(ev *evaluator, args []Node) (any, error) {
				for _, a := range args {
					v, err := ev.eval(a)
					if err != nil {
						return nil, err
					}
					if b, err := toBool(v); err != nil || !b {
						return false, err
					}
				}
				return true, nil
			}},
		"OR": {category: "Logic", sig: "OR(a, b, ...)", doc: "True when any argument is true.", params: []Type{Bool}, min: 1, variadic: true, ret: Bool,
			lazy: func(ev *evaluator, args []Node) (any, error) {
				for _, a := range args {
					v, err := ev.eval(a)
					if err != nil {
						return nil, err
					}
					if b, err := toBool(v); err != nil || b {
						return b, err
					}
				}
				return false, nil
			}},
		"NOT": {category: "Logic", sig: "NOT(value)", doc: "Reverses true and false.", params: []Type{Bool}, min: 1, ret: Bool,
			eval: func(_ Env, a []any) (any, error) {
				b, err := toBool(a[0])
				return !b, err
			}},
		"ISBLANK": {category: "Logic", sig: "ISBLANK(value)", doc: "True when the value is blank or empty text.", params: []Type{Any}, min: 1, ret: Bool,
			eval: func(_ Env, a []any) (any, error) {
				switch v := a[0].(type) {
				case nil:
					return true, nil
				case string:
					return v == "", nil
				case []any:
					return len(v) == 0, nil
				}
				return false, nil
			}},
		"BLANKVALUE": {category: "Logic", sig: "BLANKVALUE(value, fallback)", doc: "Returns the fallback when the value is blank.", params: []Type{Any, Any}, min: 2,
			retFn: func(a []Type) Type { return unify(a[0], a[1]) },
			eval: func(_ Env, a []any) (any, error) {
				if a[0] == nil || a[0] == "" {
					return a[1], nil
				}
				return a[0], nil
			}},
		"IFERROR": {category: "Logic", sig: "IFERROR(value, fallback)", doc: "Returns the fallback when computing the value fails.", params: []Type{Any, Any}, min: 2,
			retFn: func(a []Type) Type { return unify(a[0], a[1]) },
			lazy: func(ev *evaluator, args []Node) (any, error) {
				v, err := ev.eval(args[0])
				if err != nil {
					return ev.eval(args[1])
				}
				return v, nil
			}},

		// Text
		"CONCAT": {category: "Text", sig: "CONCAT(a, b, ...)", doc: "Joins values into one text.", params: []Type{Any}, min: 1, variadic: true, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				var b strings.Builder
				for _, v := range a {
					b.WriteString(ToText(v))
				}
				return b.String(), nil
			}},
		"UPPER": {category: "Text", sig: "UPPER(text)", doc: "Converts text to upper case.", params: []Type{Any}, min: 1, ret: Text, eval: text1(func(s string) any { return strings.ToUpper(s) })},
		"LOWER": {category: "Text", sig: "LOWER(text)", doc: "Converts text to lower case.", params: []Type{Any}, min: 1, ret: Text, eval: text1(func(s string) any { return strings.ToLower(s) })},
		"TRIM":  {category: "Text", sig: "TRIM(text)", doc: "Removes spaces at both ends.", params: []Type{Any}, min: 1, ret: Text, eval: text1(func(s string) any { return strings.Trim(s, " ") })},
		"LEN":   {category: "Text", sig: "LEN(text)", doc: "Number of characters.", params: []Type{Any}, min: 1, ret: Number, eval: text1(func(s string) any { return float64(utf8.RuneCountInString(s)) })},
		"LEFT": {category: "Text", sig: "LEFT(text, count)", doc: "The first characters of the text.", params: []Type{Any, Number}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				n, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				return runeSlice(ToText(a[0]), 0, int(math.Max(n, 0))), nil
			}},
		"RIGHT": {category: "Text", sig: "RIGHT(text, count)", doc: "The last characters of the text.", params: []Type{Any, Number}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				n, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				s := ToText(a[0])
				l := utf8.RuneCountInString(s)
				k := int(math.Max(n, 0))
				if k > l {
					k = l
				}
				return runeSlice(s, l-k, k), nil
			}},
		"MID": {category: "Text", sig: "MID(text, start, count)", doc: "Characters from position start (1 is the first).", params: []Type{Any, Number, Number}, min: 3, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				st, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				n, err := toNumber(a[2])
				if err != nil {
					return nil, err
				}
				return runeSlice(ToText(a[0]), int(st)-1, int(math.Max(n, 0))), nil
			}},
		"CONTAINS": {category: "Text", sig: "CONTAINS(text or list, value)", doc: "True when the text contains the value, or the list has it as an item.", params: []Type{Any, Any}, min: 2, ret: Bool,
			eval: func(_ Env, a []any) (any, error) {
				if l, ok := a[0].([]any); ok {
					want := ToText(a[1])
					for _, v := range l {
						if ToText(v) == want {
							return true, nil
						}
					}
					return false, nil
				}
				return strings.Contains(ToText(a[0]), ToText(a[1])), nil
			}},
		"STARTSWITH": {category: "Text", sig: "STARTSWITH(text, prefix)", doc: "True when the text starts with the prefix.", params: []Type{Any, Any}, min: 2, ret: Bool,
			eval: func(_ Env, a []any) (any, error) { return strings.HasPrefix(ToText(a[0]), ToText(a[1])), nil }},
		"ENDSWITH": {category: "Text", sig: "ENDSWITH(text, suffix)", doc: "True when the text ends with the suffix.", params: []Type{Any, Any}, min: 2, ret: Bool,
			eval: func(_ Env, a []any) (any, error) { return strings.HasSuffix(ToText(a[0]), ToText(a[1])), nil }},
		"REPLACE": {category: "Text", sig: "REPLACE(text, find, replacement)", doc: "Replaces every occurrence of find.", params: []Type{Any, Any, Any}, min: 3, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				find := ToText(a[1])
				if find == "" {
					return ToText(a[0]), nil
				}
				return strings.ReplaceAll(ToText(a[0]), find, ToText(a[2])), nil
			}},
		"TEXT": {category: "Text", sig: "TEXT(value)", doc: "Converts a value to text.", params: []Type{Any}, min: 1, ret: Text,
			eval: func(_ Env, a []any) (any, error) { return ToText(a[0]), nil }},
		"VALUE": {category: "Text", sig: "VALUE(text)", doc: "Converts text to a number.", params: []Type{Any}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				if a[0] == nil || a[0] == "" {
					return nil, nil
				}
				return toNumber(a[0])
			}},
		"SPLIT": {category: "Text", sig: "SPLIT(text, separator)", doc: "Splits text into a list.", params: []Type{Any, Text}, min: 2, ret: List,
			eval: func(_ Env, a []any) (any, error) {
				s := ToText(a[0])
				if s == "" {
					return []any{}, nil
				}
				parts := strings.Split(s, ToText(a[1]))
				out := make([]any, len(parts))
				for i, p := range parts {
					out[i] = p
				}
				return out, nil
			}},
		"JOIN": {category: "Text", sig: "JOIN(list, separator)", doc: "Joins list items with a separator.", params: []Type{Any, Text}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				l := toList(a[0])
				parts := make([]string, len(l))
				for i, v := range l {
					parts[i] = ToText(v)
				}
				return strings.Join(parts, ToText(a[1])), nil
			}},

		// Numbers
		"ROUND": {category: "Number", sig: "ROUND(number, digits)", doc: "Rounds to a number of decimal places (0 when omitted).", params: []Type{Number, Number}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				x, err := toNumber(a[0])
				if err != nil {
					return nil, err
				}
				d := 0.0
				if len(a) > 1 {
					if d, err = toNumber(a[1]); err != nil {
						return nil, err
					}
				}
				return roundTo(x, d), nil
			}},
		"FLOOR":   {category: "Number", sig: "FLOOR(number)", doc: "Rounds down to a whole number.", params: []Type{Number}, min: 1, ret: Number, eval: num1(math.Floor)},
		"CEILING": {category: "Number", sig: "CEILING(number)", doc: "Rounds up to a whole number.", params: []Type{Number}, min: 1, ret: Number, eval: num1(math.Ceil)},
		"ABS":     {category: "Number", sig: "ABS(number)", doc: "The number without its sign.", params: []Type{Number}, min: 1, ret: Number, eval: num1(math.Abs)},
		"SQRT": {category: "Number", sig: "SQRT(number)", doc: "Square root.", params: []Type{Number}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				x, err := toNumber(a[0])
				if err != nil {
					return nil, err
				}
				if x < 0 {
					return nil, fmt.Errorf("SQRT of a negative number")
				}
				return math.Sqrt(x), nil
			}},
		"POWER": {category: "Number", sig: "POWER(number, exponent)", doc: "Raises a number to a power.", params: []Type{Number, Number}, min: 2, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				x, err := toNumber(a[0])
				if err != nil {
					return nil, err
				}
				y, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				return math.Pow(x, y), nil
			}},
		"MOD": {category: "Number", sig: "MOD(number, divisor)", doc: "Remainder after division, with the sign of the divisor.", params: []Type{Number, Number}, min: 2, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				x, err := toNumber(a[0])
				if err != nil {
					return nil, err
				}
				y, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				if y == 0 {
					return nil, nil
				}
				return x - y*math.Floor(x/y), nil
			}},
		"SUM": {category: "Number", sig: "SUM(a, b, ...)", doc: "Adds numbers or lists of numbers.", params: []Type{Any}, min: 1, variadic: true, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				ns, err := numbers(a)
				if err != nil {
					return nil, err
				}
				s := 0.0
				for _, n := range ns {
					s += n
				}
				return s, nil
			}},
		"AVERAGE": {category: "Number", sig: "AVERAGE(a, b, ...)", doc: "Mean of numbers, ignoring blanks.", params: []Type{Any}, min: 1, variadic: true, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				ns, err := numbers(a)
				if err != nil || len(ns) == 0 {
					return nil, err
				}
				s := 0.0
				for _, n := range ns {
					s += n
				}
				return s / float64(len(ns)), nil
			}},
		"MIN": {category: "Number", sig: "MIN(a, b, ...)", doc: "Smallest number, ignoring blanks.", params: []Type{Any}, min: 1, variadic: true, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				ns, err := numbers(a)
				if err != nil || len(ns) == 0 {
					return nil, err
				}
				m := ns[0]
				for _, n := range ns[1:] {
					m = math.Min(m, n)
				}
				return m, nil
			}},
		"MAX": {category: "Number", sig: "MAX(a, b, ...)", doc: "Largest number, ignoring blanks.", params: []Type{Any}, min: 1, variadic: true, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				ns, err := numbers(a)
				if err != nil || len(ns) == 0 {
					return nil, err
				}
				m := ns[0]
				for _, n := range ns[1:] {
					m = math.Max(m, n)
				}
				return m, nil
			}},
		"COUNT": {category: "Number", sig: "COUNT(list)", doc: "Number of non-blank items in a list.", params: []Type{Any}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				n := 0
				for _, v := range toList(a[0]) {
					if v != nil && v != "" {
						n++
					}
				}
				return float64(n), nil
			}},

		// Dates
		"TODAY": {category: "Date", sig: "TODAY()", doc: "Today's date (UTC).", min: 0, ret: Date,
			eval: func(env Env, _ []any) (any, error) { return env.Now().UTC().Truncate(24 * time.Hour), nil }},
		"NOW": {category: "Date", sig: "NOW()", doc: "The current date and time (UTC).", min: 0, ret: Date,
			eval: func(env Env, _ []any) (any, error) { return env.Now().UTC().Truncate(time.Second), nil }},
		"DATE": {category: "Date", sig: "DATE(year, month, day)", doc: "Builds a date.", params: []Type{Number, Number, Number}, min: 3, ret: Date,
			eval: func(_ Env, a []any) (any, error) {
				var p [3]float64
				for i := range p {
					f, err := toNumber(a[i])
					if err != nil {
						return nil, err
					}
					p[i] = f
				}
				return time.Date(int(p[0]), time.Month(int(p[1])), int(p[2]), 0, 0, 0, 0, time.UTC), nil
			}},
		"DATEVALUE": {category: "Date", sig: "DATEVALUE(text)", doc: "Reads a date from text such as 2026-10-18.", params: []Type{Any}, min: 1, ret: Date,
			eval: func(_ Env, a []any) (any, error) {
				t, ok, err := toDate(a[0])
				if !ok {
					return nil, err
				}
				return t, nil
			}},
		"YEAR":    {category: "Date", sig: "YEAR(date)", doc: "The year of a date.", params: []Type{Date}, min: 1, ret: Number, eval: datePart(func(t time.Time) int { return t.Year() })},
		"MONTH":   {category: "Date", sig: "MONTH(date)", doc: "The month of a date, 1 to 12.", params: []Type{Date}, min: 1, ret: Number, eval: datePart(func(t time.Time) int { return int(t.Month()) })},
		"DAY":     {category: "Date", sig: "DAY(date)", doc: "The day of the month.", params: []Type{Date}, min: 1, ret: Number, eval: datePart(func(t time.Time) int { return t.Day() })},
		"WEEKDAY": {category: "Date", sig: "WEEKDAY(date)", doc: "Day of the week, 1 (Monday) to 7 (Sunday).", params: []Type{Date}, min: 1, ret: Number, eval: datePart(func(t time.Time) int { return (int(t.Weekday())+6)%7 + 1 })},
		"DATEADD": {category: "Date", sig: "DATEADD(date, amount, unit)", doc: "Adds an amount of days, weeks, months, years, hours, minutes or seconds.", params: []Type{Date, Number, Text}, min: 3, ret: Date,
			eval: func(_ Env, a []any) (any, error) {
				t, ok, err := dateArg(a[0])
				if !ok {
					return nil, err
				}
				n, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				return addDate(t, n, ToText(a[2]))
			}},
		"DATEDIFF": {category: "Date", sig: "DATEDIFF(start, end, unit)", doc: "Whole units from start to end.", params: []Type{Date, Date, Text}, min: 3, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				s, ok1, err := dateArg(a[0])
				if err != nil {
					return nil, err
				}
				e, ok2, err := dateArg(a[1])
				if err != nil || !ok1 || !ok2 {
					return nil, err
				}
				return diffDates(s, e, ToText(a[2]))
			}},
	}
	for name, f := range moreFunctions() {
		functions[name] = f
	}
}

func datePart(fn func(time.Time) int) func(Env, []any) (any, error) {
	return func(_ Env, a []any) (any, error) {
		t, ok, err := toDate(a[0])
		if !ok {
			return nil, err
		}
		return float64(fn(t.UTC())), nil
	}
}
