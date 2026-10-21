package formula

import (
	"crypto/rand"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	_ "time/tzdata" // FORMATDATE time zones work without system zone files
)

// moreFunctions adds the functions beyond the spreadsheet basics: switches,
// regular expressions, formatting, more maths, working days and lists.
// web/builder/src/lib/formula mirrors every one of them; the shared test
// vectors in testdata/vectors.json keep the two the same.
func moreFunctions() map[string]*function {
	return map[string]*function{
		// Logic
		"SWITCH": {category: "Logic", sig: "SWITCH(value, match1, result1, ..., default)", doc: "Returns the result next to the first match that equals the value, else the default (blank when omitted).",
			params: []Type{Any}, min: 3, variadic: true,
			retFn: func(a []Type) Type {
				t := Null
				for i := 2; i < len(a); i += 2 {
					t = unify(t, a[i])
				}
				if len(a)%2 == 0 {
					t = unify(t, a[len(a)-1])
				}
				return t
			},
			lazy: func(ev *evaluator, args []Node) (any, error) {
				x, err := ev.eval(args[0])
				if err != nil {
					return nil, err
				}
				xt := ev.c.types[args[0]]
				i := 1
				for ; i+1 < len(args); i += 2 {
					m, err := ev.eval(args[i])
					if err != nil {
						return nil, err
					}
					c, known, err := compare(x, xt, m, ev.c.types[args[i]])
					if err != nil {
						return nil, err
					}
					if known && c == 0 {
						return ev.eval(args[i+1])
					}
				}
				if i < len(args) {
					return ev.eval(args[i])
				}
				return nil, nil
			}},
		"IFS": {category: "Logic", sig: "IFS(condition1, value1, condition2, value2, ...)", doc: "Returns the value next to the first true condition (blank when none is).",
			params: []Type{Bool, Any}, min: 2, variadic: true,
			check: func(a []Type) string {
				if len(a)%2 != 0 {
					return "IFS takes pairs of a condition and a value"
				}
				return ""
			},
			argType: func(i int) Type {
				if i%2 == 0 {
					return Bool
				}
				return Any
			},
			retFn: func(a []Type) Type {
				t := Null
				for i := 1; i < len(a); i += 2 {
					t = unify(t, a[i])
				}
				return t
			},
			lazy: func(ev *evaluator, args []Node) (any, error) {
				for i := 0; i+1 < len(args); i += 2 {
					c, err := ev.eval(args[i])
					if err != nil {
						return nil, err
					}
					ok, err := toBool(c)
					if err != nil {
						return nil, err
					}
					if ok {
						return ev.eval(args[i+1])
					}
				}
				return nil, nil
			}},
		"COALESCE": {category: "Logic", sig: "COALESCE(a, b, ...)", doc: "The first value that is not blank.", params: []Type{Any}, min: 1, variadic: true,
			retFn: firstType,
			eval: func(_ Env, a []any) (any, error) {
				for _, v := range a {
					if !blank(v) {
						return v, nil
					}
				}
				return nil, nil
			}},
		"ISNUMBER": {category: "Logic", sig: "ISNUMBER(value)", doc: "True when the value is a number, or text that reads as one.", params: []Type{Any}, min: 1, ret: Bool,
			eval: func(_ Env, a []any) (any, error) {
				switch v := a[0].(type) {
				case float64:
					return true, nil
				case string:
					_, ok := parseNumber(v)
					return ok, nil
				}
				return false, nil
			}},

		// Text
		"PROPER": {category: "Text", sig: "PROPER(text)", doc: "Capitalises the first letter of every word.", params: []Type{Any}, min: 1, ret: Text,
			eval: text1(func(s string) any {
				var b strings.Builder
				prev := false
				for _, r := range s {
					if prev {
						b.WriteRune(unicode.ToLower(r))
					} else {
						b.WriteRune(unicode.ToUpper(r))
					}
					prev = unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\''
				}
				return b.String()
			})},
		"PADLEFT": {category: "Text", sig: "PADLEFT(text, length, pad)", doc: "Adds pad (a space when omitted) before the text until it is length characters long.", params: []Type{Any, Number, Text}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) { return pad(a, true) }},
		"PADRIGHT": {category: "Text", sig: "PADRIGHT(text, length, pad)", doc: "Adds pad (a space when omitted) after the text until it is length characters long.", params: []Type{Any, Number, Text}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) { return pad(a, false) }},
		"REPT": {category: "Text", sig: "REPT(text, count)", doc: "Repeats the text count times.", params: []Type{Any, Number}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				n, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				s := ToText(a[0])
				k := int(math.Max(math.Trunc(n), 0))
				if k*utf8.RuneCountInString(s) > maxText {
					return nil, fmt.Errorf("REPT would make more than %d characters", maxText)
				}
				return strings.Repeat(s, k), nil
			}},
		"FIND": {category: "Text", sig: "FIND(find, text, start)", doc: "Position of find in the text (1 is the first character), from start when given; 0 when it is not there.", params: []Type{Any, Any, Number}, min: 2, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				start := 1.0
				if len(a) > 2 {
					var err error
					if start, err = toNumber(a[2]); err != nil {
						return nil, err
					}
				}
				r := []rune(ToText(a[1]))
				st := int(math.Max(math.Trunc(start), 1)) - 1
				if st > len(r) {
					return 0.0, nil
				}
				i := strings.Index(string(r[st:]), ToText(a[0]))
				if i < 0 {
					return 0.0, nil
				}
				return float64(st + utf8.RuneCountInString(string(r[st:])[:i]) + 1), nil
			}},
		"REGEXMATCH": {category: "Text", sig: "REGEXMATCH(text, pattern)", doc: "True when the text matches the regular expression. Start the pattern with (?i) to ignore case.", params: []Type{Any, Text}, min: 2, ret: Bool,
			eval: func(_ Env, a []any) (any, error) {
				re, err := compileRegexp(ToText(a[1]))
				if err != nil {
					return nil, err
				}
				return re.MatchString(ToText(a[0])), nil
			}},
		"REGEXEXTRACT": {category: "Text", sig: "REGEXEXTRACT(text, pattern)", doc: "The first part of the text that matches, or its first (group) when the pattern has one; blank when nothing matches.", params: []Type{Any, Text}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				re, err := compileRegexp(ToText(a[1]))
				if err != nil {
					return nil, err
				}
				return regexExtract(re, ToText(a[0])), nil
			}},
		"REGEXREPLACE": {category: "Text", sig: "REGEXREPLACE(text, pattern, replacement)", doc: "Replaces every match; $1 in the replacement stands for the first (group).", params: []Type{Any, Text, Any}, min: 3, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				re, err := compileRegexp(ToText(a[1]))
				if err != nil {
					return nil, err
				}
				return regexReplace(re, ToText(a[0]), ToText(a[2])), nil
			}},
		"FORMATDATE": {category: "Text", sig: "FORMATDATE(date, pattern, timezone)", doc: `Writes a date with a pattern such as "D MMM YYYY" or "YYYY-MM-DD HH:mm"; text in [brackets] is kept as is. The time zone (UTC when omitted) is a name such as "Europe/Paris".`, params: []Type{Date, Text, Text}, min: 2, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				t, ok, err := toDate(a[0])
				if !ok {
					return nil, err
				}
				if len(a) > 2 && ToText(a[2]) != "" {
					loc, err := time.LoadLocation(ToText(a[2]))
					if err != nil {
						return nil, fmt.Errorf("unknown time zone %q", ToText(a[2]))
					}
					t = t.In(loc)
				} else {
					t = t.UTC()
				}
				return formatDate(t, ToText(a[1])), nil
			}},
		"FORMATNUMBER": {category: "Text", sig: "FORMATNUMBER(number, decimals, separator)", doc: `Writes a number with a fixed number of decimals and a thousands separator ("," when omitted, "" for none).`, params: []Type{Number, Number, Text}, min: 1, ret: Text,
			eval: func(_ Env, a []any) (any, error) {
				if a[0] == nil {
					return nil, nil
				}
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
				sep := ","
				if len(a) > 2 {
					sep = ToText(a[2])
				}
				return formatNumber(x, int(math.Min(math.Max(math.Trunc(d), 0), 10)), sep), nil
			}},

		// Numbers
		"ROUNDUP": {category: "Number", sig: "ROUNDUP(number, digits)", doc: "Rounds away from zero to a number of decimal places (0 when omitted).", params: []Type{Number, Number}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) { return roundDir(a, math.Ceil) }},
		"ROUNDDOWN": {category: "Number", sig: "ROUNDDOWN(number, digits)", doc: "Rounds towards zero to a number of decimal places (0 when omitted).", params: []Type{Number, Number}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) { return roundDir(a, math.Floor) }},
		"SIGN": {category: "Number", sig: "SIGN(number)", doc: "1 for a positive number, -1 for a negative one, 0 for zero.", params: []Type{Number}, min: 1, ret: Number,
			eval: num1(func(x float64) float64 {
				switch {
				case x > 0:
					return 1
				case x < 0:
					return -1
				}
				return 0
			})},
		"LN": {category: "Number", sig: "LN(number)", doc: "Natural logarithm.", params: []Type{Number}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				x, err := toNumber(a[0])
				if err != nil {
					return nil, err
				}
				if x <= 0 {
					return nil, fmt.Errorf("LN needs a number above 0")
				}
				return math.Log(x), nil
			}},
		"LOG": {category: "Number", sig: "LOG(number, base)", doc: "Logarithm in a base (10 when omitted).", params: []Type{Number, Number}, min: 1, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				x, err := toNumber(a[0])
				if err != nil {
					return nil, err
				}
				base := 10.0
				if len(a) > 1 {
					if base, err = toNumber(a[1]); err != nil {
						return nil, err
					}
				}
				if x <= 0 || base <= 0 || base == 1 {
					return nil, fmt.Errorf("LOG needs a number and a base above 0, and a base other than 1")
				}
				if base == 10 {
					return math.Log10(x), nil
				}
				return math.Log(x) / math.Log(base), nil
			}},
		"EXP": {category: "Number", sig: "EXP(number)", doc: "e raised to the power of the number.", params: []Type{Number}, min: 1, ret: Number, eval: num1(math.Exp)},
		"PI":  {category: "Number", sig: "PI()", doc: "The number π.", min: 0, ret: Number, eval: func(Env, []any) (any, error) { return math.Pi, nil }},
		"RANDBETWEEN": {category: "Number", sig: "RANDBETWEEN(low, high)", doc: "A random whole number from low to high.", params: []Type{Number, Number}, min: 2, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				lo, err := toNumber(a[0])
				if err != nil {
					return nil, err
				}
				hi, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				l, h := math.Ceil(lo), math.Floor(hi)
				if h < l {
					return nil, fmt.Errorf("RANDBETWEEN needs low at most high")
				}
				var b [8]byte
				rand.Read(b[:])
				u := float64(uint64(b[0])<<40|uint64(b[1])<<32|uint64(b[2])<<24|uint64(b[3])<<16|uint64(b[4])<<8|uint64(b[5])) / (1 << 48)
				return l + math.Floor(u*(h-l+1)), nil
			}},

		// Dates
		"EOMONTH": {category: "Date", sig: "EOMONTH(date, months)", doc: "The last day of the month, months after the date (0 when omitted).", params: []Type{Date, Number}, min: 1, ret: Date,
			eval: func(_ Env, a []any) (any, error) {
				t, ok, err := toDate(a[0])
				if !ok {
					return nil, err
				}
				m := 0.0
				if len(a) > 1 {
					if m, err = toNumber(a[1]); err != nil {
						return nil, err
					}
				}
				t = t.UTC()
				return time.Date(t.Year(), t.Month()+time.Month(int(m))+1, 0, 0, 0, 0, 0, time.UTC), nil
			}},
		"WEEKNUM": {category: "Date", sig: "WEEKNUM(date)", doc: "The ISO week of the year, 1 to 53 (weeks start on Monday).", params: []Type{Date}, min: 1, ret: Number,
			eval: datePart(func(t time.Time) int { _, w := t.ISOWeek(); return w })},
		"QUARTER": {category: "Date", sig: "QUARTER(date)", doc: "The quarter of the year, 1 to 4.", params: []Type{Date}, min: 1, ret: Number,
			eval: datePart(func(t time.Time) int { return (int(t.Month())-1)/3 + 1 })},
		"HOUR":   {category: "Date", sig: "HOUR(date)", doc: "The hour, 0 to 23 (UTC).", params: []Type{Date}, min: 1, ret: Number, eval: datePart(func(t time.Time) int { return t.Hour() })},
		"MINUTE": {category: "Date", sig: "MINUTE(date)", doc: "The minute, 0 to 59.", params: []Type{Date}, min: 1, ret: Number, eval: datePart(func(t time.Time) int { return t.Minute() })},
		"WORKDAYS": {category: "Date", sig: "WORKDAYS(start, end)", doc: "Monday-to-Friday days from start to end, counting both; negative when end is before start.", params: []Type{Date, Date}, min: 2, ret: Number,
			eval: func(_ Env, a []any) (any, error) {
				s, ok1, err := toDate(a[0])
				if err != nil {
					return nil, err
				}
				e, ok2, err := toDate(a[1])
				if err != nil || !ok1 || !ok2 {
					return nil, err
				}
				return float64(workdays(dayNumber(s), dayNumber(e))), nil
			}},
		"ADDWORKDAYS": {category: "Date", sig: "ADDWORKDAYS(date, days)", doc: "The date that many Monday-to-Friday days later (earlier when negative).", params: []Type{Date, Number}, min: 2, ret: Date,
			eval: func(_ Env, a []any) (any, error) {
				t, ok, err := toDate(a[0])
				if !ok {
					return nil, err
				}
				n, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				k := int(math.Trunc(n))
				if k > 100000 || k < -100000 {
					return nil, fmt.Errorf("ADDWORKDAYS takes at most 100000 days")
				}
				d := dayNumber(t)
				step := 1
				if k < 0 {
					step, k = -1, -k
				}
				for k > 0 {
					d += int64(step)
					if weekday(d) < 5 {
						k--
					}
				}
				return time.Unix(d*86400, 0).UTC(), nil
			}},

		// Lists
		"LIST": {category: "List", sig: "LIST(a, b, ...)", doc: "Makes a list of the values.", params: []Type{Any}, min: 0, variadic: true, ret: List,
			eval: func(_ Env, a []any) (any, error) { return append([]any{}, a...), nil }},
		"FIRST": {category: "List", sig: "FIRST(list)", doc: "The first item (blank for an empty list).", params: []Type{Any}, min: 1, ret: Any,
			eval: func(_ Env, a []any) (any, error) { return listItem(a[0], 1), nil }},
		"LAST": {category: "List", sig: "LAST(list)", doc: "The last item (blank for an empty list).", params: []Type{Any}, min: 1, ret: Any,
			eval: func(_ Env, a []any) (any, error) { return listItem(a[0], -1), nil }},
		"INDEX": {category: "List", sig: "INDEX(list, position)", doc: "The item at a position (1 is the first, -1 the last); blank outside the list.", params: []Type{Any, Number}, min: 2, ret: Any,
			eval: func(_ Env, a []any) (any, error) {
				n, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				return listItem(a[0], int(math.Trunc(n))), nil
			}},
		"SLICE": {category: "List", sig: "SLICE(list, start, count)", doc: "Count items from position start (to the end when count is omitted).", params: []Type{Any, Number, Number}, min: 2, ret: List,
			eval: func(_ Env, a []any) (any, error) {
				l := toList(a[0])
				st, err := toNumber(a[1])
				if err != nil {
					return nil, err
				}
				i := int(math.Max(math.Trunc(st), 1)) - 1
				if i > len(l) {
					i = len(l)
				}
				j := len(l)
				if len(a) > 2 {
					n, err := toNumber(a[2])
					if err != nil {
						return nil, err
					}
					if k := i + int(math.Max(math.Trunc(n), 0)); k < j {
						j = k
					}
				}
				return append([]any{}, l[i:j]...), nil
			}},
		"SORT": {category: "List", sig: "SORT(list, descending)", doc: "The items in order: numbers by size, dates by time, the rest as text. Blanks go last.", params: []Type{Any, Bool}, min: 1, ret: List,
			eval: func(_ Env, a []any) (any, error) {
				desc := false
				if len(a) > 1 {
					var err error
					if desc, err = toBool(a[1]); err != nil {
						return nil, err
					}
				}
				return sortList(toList(a[0]), desc), nil
			}},
		"UNIQUE": {category: "List", sig: "UNIQUE(list)", doc: "The items without repeats, in their first order.", params: []Type{Any}, min: 1, ret: List,
			eval: func(_ Env, a []any) (any, error) {
				seen := map[string]bool{}
				out := []any{}
				for _, v := range toList(a[0]) {
					k := fmt.Sprintf("%d:%s", typeOf(v), ToText(v))
					if !seen[k] {
						seen[k] = true
						out = append(out, v)
					}
				}
				return out, nil
			}},

		// Other
		"UUID": {category: "Other", sig: "UUID()", doc: "A new random identifier.", min: 0, ret: Text,
			eval: func(Env, []any) (any, error) {
				var b [16]byte
				rand.Read(b[:])
				b[6] = b[6]&0x0f | 0x40
				b[8] = b[8]&0x3f | 0x80
				return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
			}},
	}
}

// maxText caps text that functions build.
const maxText = 100_000

func blank(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	}
	return false
}

func pad(a []any, left bool) (any, error) {
	s := ToText(a[0])
	n, err := toNumber(a[1])
	if err != nil {
		return nil, err
	}
	p := " "
	if len(a) > 2 {
		p = ToText(a[2])
	}
	want := int(math.Trunc(n))
	if want > maxText {
		return nil, fmt.Errorf("cannot pad to more than %d characters", maxText)
	}
	have := utf8.RuneCountInString(s)
	if p == "" || have >= want {
		return s, nil
	}
	pr := []rune(p)
	fill := make([]rune, want-have)
	for i := range fill {
		fill[i] = pr[i%len(pr)]
	}
	if left {
		return string(fill) + s, nil
	}
	return s + string(fill), nil
}

// roundDir rounds by digits with ceil (away from zero) or floor (towards
// zero), first snapping away float noise such as 1.1*10 = 11.000000000000002.
func roundDir(a []any, fn func(float64) float64) (any, error) {
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
	p := math.Pow(10, math.Trunc(d))
	y := math.Abs(x) * p
	if r := math.Round(y); math.Abs(y-r) <= 1e-9*math.Max(1, y) {
		y = r
	}
	y = fn(y) / p
	if x < 0 {
		y = -y
	}
	return y, nil
}

var regexps sync.Map // pattern -> *regexp.Regexp

func compileRegexp(p string) (*regexp.Regexp, error) {
	if re, ok := regexps.Load(p); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(p)
	if err != nil {
		msg := err.Error()
		if i := strings.LastIndex(msg, ": "); i >= 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("the pattern is not a valid regular expression (%s)", strings.TrimPrefix(msg, "error parsing regexp: "))
	}
	regexps.Store(p, re)
	return re, nil
}

func regexExtract(re *regexp.Regexp, s string) any {
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return nil
	}
	if len(m) > 2 {
		if m[2] < 0 {
			return nil
		}
		return s[m[2]:m[3]]
	}
	return s[m[0]:m[1]]
}

// regexReplace replaces matches; in the replacement $1 to $9 are groups,
// $0 the whole match and $$ a dollar sign. Other text is kept as written.
func regexReplace(re *regexp.Regexp, s, repl string) string {
	return re.ReplaceAllStringFunc(s, func(match string) string {
		sub := re.FindStringSubmatch(match)
		var b strings.Builder
		for i := 0; i < len(repl); i++ {
			c := repl[i]
			if c == '$' && i+1 < len(repl) {
				n := repl[i+1]
				if n == '$' {
					b.WriteByte('$')
					i++
					continue
				}
				if n >= '0' && n <= '9' {
					if k := int(n - '0'); k < len(sub) {
						b.WriteString(sub[k])
					}
					i++
					continue
				}
			}
			b.WriteByte(c)
		}
		return b.String()
	})
}

var monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
var dayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// dateTokens are the pattern tokens, longest first.
var dateTokens = []string{"YYYY", "MMMM", "dddd", "MMM", "ddd", "YY", "MM", "DD", "HH", "hh", "mm", "ss", "M", "D", "H", "h", "m", "s", "A", "a"}

func formatDate(t time.Time, pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); {
		if pattern[i] == '[' {
			if j := strings.IndexByte(pattern[i:], ']'); j > 0 {
				b.WriteString(pattern[i+1 : i+j])
				i += j + 1
				continue
			}
		}
		tok := ""
		for _, k := range dateTokens {
			if strings.HasPrefix(pattern[i:], k) {
				tok = k
				break
			}
		}
		if tok == "" {
			b.WriteByte(pattern[i])
			i++
			continue
		}
		h12 := t.Hour() % 12
		if h12 == 0 {
			h12 = 12
		}
		switch tok {
		case "YYYY":
			fmt.Fprintf(&b, "%04d", t.Year())
		case "YY":
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case "MMMM":
			b.WriteString(monthNames[t.Month()-1])
		case "MMM":
			b.WriteString(monthNames[t.Month()-1][:3])
		case "MM":
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case "M":
			fmt.Fprintf(&b, "%d", int(t.Month()))
		case "DD":
			fmt.Fprintf(&b, "%02d", t.Day())
		case "D":
			fmt.Fprintf(&b, "%d", t.Day())
		case "dddd":
			b.WriteString(dayNames[t.Weekday()])
		case "ddd":
			b.WriteString(dayNames[t.Weekday()][:3])
		case "HH":
			fmt.Fprintf(&b, "%02d", t.Hour())
		case "H":
			fmt.Fprintf(&b, "%d", t.Hour())
		case "hh":
			fmt.Fprintf(&b, "%02d", h12)
		case "h":
			fmt.Fprintf(&b, "%d", h12)
		case "mm":
			fmt.Fprintf(&b, "%02d", t.Minute())
		case "m":
			fmt.Fprintf(&b, "%d", t.Minute())
		case "ss":
			fmt.Fprintf(&b, "%02d", t.Second())
		case "s":
			fmt.Fprintf(&b, "%d", t.Second())
		case "A", "a":
			ampm := "AM"
			if t.Hour() >= 12 {
				ampm = "PM"
			}
			if tok == "a" {
				ampm = strings.ToLower(ampm)
			}
			b.WriteString(ampm)
		}
		i += len(tok)
	}
	return b.String()
}

func formatNumber(x float64, decimals int, sep string) string {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return ""
	}
	x = roundTo(x, float64(decimals))
	s := fmt.Sprintf("%.*f", decimals, math.Abs(x))
	whole, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		whole, frac = s[:i], s[i:]
	}
	var b strings.Builder
	if x < 0 && strings.Trim(s, "0.") != "" {
		b.WriteByte('-')
	}
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	b.WriteString(frac)
	return b.String()
}

// dayNumber counts days since 1970-01-01 (UTC), ignoring the time of day.
func dayNumber(t time.Time) int64 {
	return int64(math.Floor(float64(t.Unix()) / 86400))
}

// weekday of a day number: 0 Monday ... 6 Sunday.
func weekday(d int64) int64 { return ((d+3)%7 + 7) % 7 }

func workdays(a, b int64) int64 {
	sign := int64(1)
	if b < a {
		a, b, sign = b, a, -1
	}
	n := b - a + 1
	weeks := n / 7
	count := weeks * 5
	for d := a + weeks*7; d <= b; d++ {
		if weekday(d) < 5 {
			count++
		}
	}
	return sign * count
}

// listItem returns item i of a list, 1-based, negative from the end. A
// value that is not a list counts as a list of itself.
func listItem(v any, i int) any {
	l := toList(v)
	if i < 0 {
		i = len(l) + i + 1
	}
	if i < 1 || i > len(l) {
		return nil
	}
	return l[i-1]
}

func sortList(l []any, desc bool) []any {
	out := append([]any{}, l...)
	allNum, allDate := true, true
	for _, v := range out {
		if v == nil {
			continue
		}
		if _, ok := v.(float64); !ok {
			allNum = false
		}
		if _, ok := v.(time.Time); !ok {
			allDate = false
		}
	}
	key := func(v any) (float64, string) {
		switch x := v.(type) {
		case float64:
			if allNum {
				return x, ""
			}
		case time.Time:
			if allDate {
				return float64(x.Unix()), ""
			}
		}
		return 0, ToText(v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a == nil) != (b == nil) {
			return b == nil
		}
		if a == nil {
			return false
		}
		an, as := key(a)
		bn, bs := key(b)
		c := 0
		if allNum || allDate {
			c = cmpOrdered(an, bn)
		} else {
			c = strings.Compare(as, bs)
		}
		if desc {
			return c > 0
		}
		return c < 0
	})
	return out
}
