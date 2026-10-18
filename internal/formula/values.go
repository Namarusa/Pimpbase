package formula

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Type is the static type of an expression.
type Type int

const (
	Any Type = iota // unknown until run time
	Null
	Number
	Text
	Bool
	Date
	List
)

func (t Type) String() string {
	return [...]string{"Any", "Blank", "Number", "Text", "Boolean", "Date", "List"}[t]
}

func (t Type) MarshalText() ([]byte, error) { return []byte(strings.ToLower(t.String())), nil }

// DateLayout is how dates are stored and compared as text. It sorts in time
// order, which lets SQL compare stored dates as plain strings.
const DateLayout = "2006-01-02T15:04:05Z"

// Values at run time are nil (blank), float64, string, bool, time.Time or
// []any.

// FormatNumber prints a number the way formulas show it: no exponent, no
// trailing zeros.
func FormatNumber(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return ""
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// FormatDate prints a date: just the day when there is no time of day.
func FormatDate(t time.Time) string {
	t = t.UTC()
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format(DateLayout)
}

// ParseDate accepts a full timestamp or a plain day.
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Truncate(time.Second), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date (use YYYY-MM-DD)", s)
}

func typeOf(v any) Type {
	switch v.(type) {
	case nil:
		return Null
	case float64:
		return Number
	case string:
		return Text
	case bool:
		return Bool
	case time.Time:
		return Date
	case []any:
		return List
	}
	return Any
}

func toNumber(v any) (float64, error) {
	switch v := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return v, nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("expected a number, got %q", v)
		}
		return f, nil
	}
	return 0, fmt.Errorf("expected a number, got a %s", typeOf(v))
}

// ToText converts any value to its text form.
func ToText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case float64:
		return FormatNumber(v)
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case time.Time:
		return FormatDate(v)
	case []any:
		parts := make([]string, len(v))
		for i, x := range v {
			parts[i] = ToText(x)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

func toBool(v any) (bool, error) {
	switch v := v.(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case float64:
		return v != 0, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "false", "0", "no":
			return false, nil
		case "true", "1", "yes":
			return true, nil
		}
		return false, fmt.Errorf("expected true or false, got %q", v)
	case []any:
		return len(v) > 0, nil
	}
	return false, fmt.Errorf("expected true or false, got a %s", typeOf(v))
}

func toDate(v any) (time.Time, bool, error) {
	switch v := v.(type) {
	case nil:
		return time.Time{}, false, nil
	case time.Time:
		return v, true, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return time.Time{}, false, nil
		}
		t, err := ParseDate(v)
		return t, err == nil, err
	}
	return time.Time{}, false, fmt.Errorf("expected a date, got a %s", typeOf(v))
}

func toList(v any) []any {
	switch v := v.(type) {
	case nil:
		return nil
	case []any:
		return v
	}
	return []any{v}
}

// zeroOf is the value a blank stands for when used as a t.
func zeroOf(t Type) (any, bool) {
	switch t {
	case Number:
		return 0.0, true
	case Text:
		return "", true
	case Bool:
		return false, true
	}
	return nil, false
}

// compare orders a and b. known is false when either side is a blank that
// has no zero value (a blank date, NULL), in which case every comparison is
// false, as in SQL.
func compare(a any, at Type, b any, bt Type) (cmp int, known bool, err error) {
	if a == nil {
		if a, known = zeroOf(at); !known {
			return 0, false, nil
		}
	}
	if b == nil {
		if b, known = zeroOf(bt); !known {
			return 0, false, nil
		}
	}
	switch x := a.(type) {
	case float64:
		y, err := toNumber(b)
		if err != nil {
			return 0, false, err
		}
		return cmpOrdered(x, y), true, nil
	case string:
		if _, ok := b.(string); !ok {
			if _, isNum := b.(float64); isNum {
				y := b.(float64)
				xf, err := toNumber(x)
				if err != nil {
					return 0, false, fmt.Errorf("cannot compare text with a number")
				}
				return cmpOrdered(xf, y), true, nil
			}
			if bt, ok := b.(time.Time); ok {
				xt, ok, err := toDate(x)
				if err != nil || !ok {
					return 0, false, err
				}
				return cmpOrdered(xt.Unix(), bt.Unix()), true, nil
			}
			return 0, false, fmt.Errorf("cannot compare text with a %s", typeOf(b))
		}
		return strings.Compare(x, b.(string)), true, nil
	case bool:
		y, err := toBool(b)
		if err != nil {
			return 0, false, err
		}
		xi, yi := 0, 0
		if x {
			xi = 1
		}
		if y {
			yi = 1
		}
		return cmpOrdered(xi, yi), true, nil
	case time.Time:
		y, ok, err := toDate(b)
		if err != nil || !ok {
			return 0, false, err
		}
		return cmpOrdered(x.Unix(), y.Unix()), true, nil
	}
	return 0, false, fmt.Errorf("cannot compare a %s", typeOf(a))
}

func cmpOrdered[T int | int64 | float64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// unify is the type of a value that may come from either branch.
func unify(a, b Type) Type {
	switch {
	case a == b:
		return a
	case a == Null:
		return b
	case b == Null:
		return a
	}
	return Any
}
