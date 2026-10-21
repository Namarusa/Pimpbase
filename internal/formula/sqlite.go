package formula

import (
	"database/sql/driver"
	"fmt"
	"strings"

	"modernc.org/sqlite"
)

// SQLite gets Go's regular expressions and text case as functions, so that
// formulas with REGEXMATCH, REGEXEXTRACT, REGEXREPLACE, LOWER and UPPER
// filter in the database with exactly the meaning they have in Go. (SQLite's
// own LOWER and UPPER change ASCII letters only.)
func init() {
	for name, fn := range map[string]func(string) string{"bb_lower": strings.ToLower, "bb_upper": strings.ToUpper} {
		sqlite.MustRegisterDeterministicScalarFunction(name, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			switch v := args[0].(type) {
			case nil:
				return nil, nil
			case string:
				return fn(v), nil
			case []byte:
				return fn(string(v)), nil
			}
			return args[0], nil
		})
	}
	text := func(v driver.Value) string {
		switch x := v.(type) {
		case nil:
			return ""
		case string:
			return x
		case []byte:
			return string(x)
		}
		return fmt.Sprint(v)
	}
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(sqlite.RegisterDeterministicScalarFunction("bb_regexmatch", 2, func(_ *sqlite.FunctionContext, a []driver.Value) (driver.Value, error) {
		re, err := compileRegexp(text(a[1]))
		if err != nil {
			return nil, err
		}
		if re.MatchString(text(a[0])) {
			return int64(1), nil
		}
		return int64(0), nil
	}))
	must(sqlite.RegisterDeterministicScalarFunction("bb_regexextract", 2, func(_ *sqlite.FunctionContext, a []driver.Value) (driver.Value, error) {
		re, err := compileRegexp(text(a[1]))
		if err != nil {
			return nil, err
		}
		v := regexExtract(re, text(a[0]))
		if v == nil {
			return nil, nil
		}
		return v.(string), nil
	}))
	must(sqlite.RegisterDeterministicScalarFunction("bb_regexreplace", 3, func(_ *sqlite.FunctionContext, a []driver.Value) (driver.Value, error) {
		re, err := compileRegexp(text(a[1]))
		if err != nil {
			return nil, err
		}
		return regexReplace(re, text(a[0]), text(a[2])), nil
	}))
}
