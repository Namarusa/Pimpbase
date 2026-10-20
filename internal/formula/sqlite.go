package formula

import (
	"database/sql/driver"
	"strings"

	"modernc.org/sqlite"
)

// SQLite's own LOWER and UPPER change ASCII letters only. These do what Go
// does, so text functions can run in SQLite with the meaning they have in
// formulas.
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
}
