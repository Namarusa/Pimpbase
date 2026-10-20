package formula

import (
	"database/sql/driver"
	"fmt"

	"modernc.org/sqlite"
)

// SQLite gets Go's regular expressions as functions, so that formulas with
// REGEXMATCH, REGEXEXTRACT and REGEXREPLACE filter in the database with
// exactly the meaning they have in Go.
func init() {
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
