package extdb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bubibase/bubibase/internal/formula"
)

// Target is the table that row operations work on.
type Target struct {
	Schema string
	Name   string
	// Columns are the columns read and written.
	Columns []Column
	// Key is the primary key column; empty when the table has none.
	Key string
}

// Column finds a column by name, ignoring case.
func (t *Target) Column(name string) *Column {
	for i := range t.Columns {
		if strings.EqualFold(t.Columns[i].Name, name) {
			return &t.Columns[i]
		}
	}
	return nil
}

// Cond is a filter compiled for one source.
type Cond struct {
	SQL  string
	Args []any
	// Mongo is a filter document.
	Mongo any
}

// Order sorts by one column.
type Order struct {
	Column string
	Desc   bool
}

// Select picks rows.
type Select struct {
	Where  *Cond
	Order  []Order
	Limit  int // 0 reads every row
	Offset int
}

// Rows reads and writes the rows of tables in one source. Values going in
// and out are nil, float64, bool, string or time.Time, by column type.
type Rows interface {
	// Compile turns a condition formula on the table's columns into a
	// filter the database runs, or gives ok false when some part of it
	// has no database form.
	Compile(t *Target, c *formula.Checked, env formula.Env) (cond *Cond, ok bool)
	Count(ctx context.Context, t *Target, where *Cond) (int, error)
	Select(ctx context.Context, t *Target, s Select) ([]map[string]any, error)
	// Insert adds a row and returns its key.
	Insert(ctx context.Context, t *Target, values map[string]any) (any, error)
	Update(ctx context.Context, t *Target, key any, values map[string]any) error
	Delete(ctx context.Context, t *Target, keys []any) (int, error)
}

// Read converts a value a driver read into the form of a column type.
func Read(v any, c *Column) any {
	if v == nil {
		return nil
	}
	if b, ok := v.([]byte); ok {
		if isGUID(c.DBType) && len(b) == 16 {
			return guid(b)
		}
		if c.Binary || !utf8.Valid(b) {
			return base64.StdEncoding.EncodeToString(b)
		}
		v = string(b)
	}
	switch c.Type {
	case "number", "autonumber":
		switch x := v.(type) {
		case int64:
			if x > 1<<53 || x < -(1<<53) {
				return strconv.FormatInt(x, 10)
			}
			return float64(x)
		case int32:
			return float64(x)
		case int:
			return float64(x)
		case float64:
			return x
		case float32:
			return float64(x)
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return x
			}
			// Decimals with more digits than a float holds stay text.
			if digits(x) > 15 {
				return x
			}
			return f
		case bool:
			if x {
				return 1.0
			}
			return 0.0
		}
	case "boolean":
		switch x := v.(type) {
		case bool:
			return x
		case int64:
			return x != 0
		case int32:
			return x != 0
		case float64:
			return x != 0
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "1", "t", "true", "y", "yes", "on":
				return true
			}
			return false
		}
	case "date":
		switch x := v.(type) {
		case time.Time:
			return x.UTC()
		case string:
			if t, err := formula.ParseDate(x); err == nil {
				return t
			}
			return x
		}
	}
	return text(v)
}

func digits(s string) int {
	n := 0
	for _, r := range strings.TrimLeft(s, "-+0") {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}

// text gives any value read as text.
func text(v any) any {
	switch x := v.(type) {
	case string:
		return x
	case time.Time:
		return x.UTC().Format(formula.DateLayout)
	case float64:
		return formula.FormatNumber(x)
	case float32:
		return formula.FormatNumber(float64(x))
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	case map[string]any, []any:
		js, _ := json.Marshal(x)
		return string(js)
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}

func isGUID(dbType string) bool { return strings.EqualFold(dbType, "uniqueidentifier") }

// guid formats a SQL Server uniqueidentifier, whose first three groups are
// stored little-endian.
func guid(b []byte) string {
	return fmt.Sprintf("%02X%02X%02X%02X-%02X%02X-%02X%02X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		b[3], b[2], b[1], b[0], b[5], b[4], b[7], b[6], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

// Write converts a value given for a column into a driver parameter.
func Write(v any, c *Column) (any, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" && c.Type != "text" && c.Type != "longtext" {
		return nil, nil
	}
	switch c.Type {
	case "number", "autonumber":
		var f float64
		switch x := v.(type) {
		case float64:
			f = x
		case json.Number:
			if integer(c.DBType) {
				if n, err := x.Int64(); err == nil {
					return n, nil
				}
			}
			var err error
			if f, err = x.Float64(); err != nil {
				return nil, fmt.Errorf("%q is not a number", x)
			}
		case string:
			var err error
			if f, err = strconv.ParseFloat(strings.TrimSpace(x), 64); err != nil {
				return nil, fmt.Errorf("%q is not a number", x)
			}
			if !integer(c.DBType) && digits(x) > 15 {
				return strings.TrimSpace(x), nil // keep a long decimal exact
			}
		case bool:
			if x {
				f = 1
			}
		default:
			return nil, fmt.Errorf("is not a number")
		}
		if integer(c.DBType) {
			if f != math.Trunc(f) {
				return nil, fmt.Errorf("%s takes whole numbers", c.Name)
			}
			return int64(f), nil
		}
		return f, nil
	case "boolean":
		switch x := v.(type) {
		case bool:
			return x, nil
		case float64:
			return x != 0, nil
		case json.Number:
			return x.String() != "0", nil
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "yes", "1", "on", "t", "y":
				return true, nil
			case "false", "no", "0", "off", "f", "n":
				return false, nil
			}
		}
		return nil, fmt.Errorf("is not true or false")
	case "date":
		switch x := v.(type) {
		case time.Time:
			return x.UTC(), nil
		case string:
			t, err := formula.ParseDate(x)
			if err != nil {
				return nil, fmt.Errorf("%q is not a date", x)
			}
			return t, nil
		}
		return nil, fmt.Errorf("is not a date")
	case "single_select":
		s := fmt.Sprint(v)
		if len(c.Options) > 0 {
			for _, o := range c.Options {
				if o == s {
					return s, nil
				}
			}
			return nil, fmt.Errorf("%q is not one of the choices", s)
		}
		return s, nil
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case float64:
		return formula.FormatNumber(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case map[string]any, []any:
		js, _ := json.Marshal(x)
		return string(js), nil
	}
	return fmt.Sprint(v), nil
}

// integer reports whether a database type holds whole numbers only.
func integer(dbType string) bool {
	t := strings.ToLower(dbType)
	if i := strings.IndexAny(t, "( "); i >= 0 {
		t = t[:i]
	}
	switch t {
	case "int", "integer", "int2", "int4", "int8", "smallint", "bigint", "tinyint", "mediumint", "serial", "bigserial", "smallserial", "int32", "int64", "long":
		return true
	}
	return false
}

// FormulaType is the type formulas see for a column type.
func FormulaType(fieldType string) formula.Type {
	switch fieldType {
	case "number", "autonumber":
		return formula.Number
	case "boolean":
		return formula.Bool
	case "date":
		return formula.Date
	}
	return formula.Text
}
