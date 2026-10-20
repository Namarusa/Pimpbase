package formula

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// The differential test checks that every filter means the same thing in
// SQL as in Go, on SQLite and (when BUBIBASE_TEST_POSTGRES is set) Postgres.

var diffScope = testScope{"n": Number, "m": Number, "t": Text, "u": Text, "b": Bool, "d": Date}

var cols = map[string]string{"n": "n", "m": "m", "t": "t", "u": "u", "b": "b", "d": "d"}

type colResolver struct{}

func (colResolver) FieldSQL(name string) (string, []any, bool) {
	c, ok := cols[strings.ToLower(name)]
	return `"` + c + `"`, nil, ok
}

type row struct {
	id   int
	n, m any
	t, u any
	b    any
	d    any
}

func date(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }

var rows = []row{
	{1, 1.0, 2.0, "apple", "Apple", true, date(2026, 1, 1)},
	{2, nil, 0.0, nil, "", nil, nil},
	{3, -3.5, nil, "", nil, false, date(2026, 10, 18)},
	{4, 0.0, 0.0, "Zebra", "zebra", true, date(2025, 12, 31)},
	{5, 10.0, 3.0, "a b ", "a b", false, nil},
	{6, 2.0, 2.0, "éclair", "Éclair", nil, date(2026, 10, 19)},
}

var predicates = []string{
	"[n] > 1", "[n] >= 0", "[n] = 0", "[n] <> 0", "[n] < [m]", "[n] = [m]",
	"[n] + [m] > 2", "[n] * 2 = [m] * 2", "[n] / [m] > 0.4", "[n] / [m] = 0", "ISBLANK([n] / [m])",
	"-[n] > 0", "ABS([n]) > 3", "[n] IN (0, 1, 2)", "[n] NOT IN (0, 1)",
	`[t] = "apple"`, `[t] = ""`, `[t] <> ""`, `[t] < "b"`, `[t] > [u]`, `[t] >= "Z"`,
	`[t] & [u] = "appleApple"`, `CONCAT([t], "-", [u]) = "-"`, `CONTAINS([t], "b")`, `STARTSWITH([u], "a")`,
	`LEN([t]) > 3`, `TRIM([t]) = [u]`, `UPPER([t]) = "APPLE"`, `[t] IN ("apple", "")`,
	"[b]", "NOT [b]", "[b] = TRUE", "[b] = FALSE", "[b] AND [n] > 0", "[b] OR [n] < 0", "AND([b], [n] > 0, [m] >= 0)",
	"OR(NOT [b], [n] = 0)",
	"ISBLANK([n])", "ISBLANK([t])", "ISBLANK([b])", "ISBLANK([d])", "NOT ISBLANK([d])",
	"[d] < TODAY()", "[d] >= DATE(2026, 1, 1)", "NOT ([d] < TODAY())", "[d] = DATE(2026,10,18)", "[d] <> DATE(2026,10,18)",
	"[n] = NULL", "NOT ([n] = NULL)", "[t] = NULL",
	`IF([b], [n], [m]) > 1`, `IF([n] > 0, "pos", "neg") = "pos"`, `ISBLANK(IF([b], [n]))`, `NOT IF([b], [n] > 0)`,
	`IF([n] > 0, [t], [u]) = ""`, `[n] > 0 AND [t] <> "" OR [b]`,
	// Column against constant, either way round, which keeps the column
	// bare for indexes.
	`1 < [n]`, `0 >= [n]`, `[n] <= -1`, `[n] < 0.5`, `NOT ([n] > 1)`, `NOT (0 = [n])`, `[n] <> 2`,
	`"b" > [t]`, `[t] <> "apple"`, `NOT ([t] = "apple")`, `[t] < ""`, `[t] >= ""`, `"" = [t]`, `[u] <= "a b"`,
	`[d] > DATE(2026, 1, 1)`, `DATE(2026, 1, 1) <= [d]`, `NOT ([d] <> DATE(2026, 10, 18))`, `NOT (DATE(2026, 1, 1) > [d])`,
	`LOWER([u]) = "éclair"`, `CONTAINS(UPPER([t]), "ÉC")`, `CONTAINS(LOWER(TEXT([u])), "zeb")`, `TEXT([t]) = ""`, `ISBLANK(TEXT([t]))`,
	// These fall back to Go but must still work.
	`YEAR([d]) = 2026`, `[n] ^ 2 > 4`, `CONTAINS(TEXT([n]), "1")`, `[t] = 1`,
}

func openDBs(t *testing.T) map[string]*sql.DB {
	dbs := map[string]*sql.DB{}
	lite, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	lite.SetMaxOpenConns(1)
	dbs["sqlite"] = lite
	if dsn := os.Getenv("BUBIBASE_TEST_POSTGRES"); dsn != "" {
		pg, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		dbs["postgres"] = pg
	}
	return dbs
}

func TestSQLMatchesEval(t *testing.T) {
	for dialect, db := range openDBs(t) {
		t.Run(dialect, func(t *testing.T) {
			defer db.Close()
			real, ph := "REAL", func(i int) string { return "?" }
			if dialect == "postgres" {
				real = "DOUBLE PRECISION"
				ph = func(i int) string { return fmt.Sprintf("$%d", i) }
			}
			mustExec(t, db, `DROP TABLE IF EXISTS diff`)
			mustExec(t, db, `CREATE TABLE diff (id INTEGER PRIMARY KEY, n `+real+`, m `+real+`, t TEXT, u TEXT, b INTEGER, d TEXT)`)
			for _, r := range rows {
				b := any(nil)
				if r.b != nil {
					b = map[bool]int{false: 0, true: 1}[r.b.(bool)]
				}
				d := any(nil)
				if r.d != nil {
					d = r.d.(time.Time).Format(DateLayout)
				}
				mustExec(t, db, fmt.Sprintf(`INSERT INTO diff VALUES (%s, %s, %s, %s, %s, %s, %s)`, ph(1), ph(2), ph(3), ph(4), ph(5), ph(6), ph(7)),
					r.id, r.n, r.m, r.t, r.u, b, d)
			}
			pushed := 0
			for _, p := range predicates {
				c, errs := Compile(p, diffScope)
				if errs != nil {
					t.Fatalf("%s: %v", p, errs[0])
				}
				var want []int
				for _, r := range rows {
					ok, err := c.EvalBool(testEnv{"n": r.n, "m": r.m, "t": r.t, "u": r.u, "b": r.b, "d": r.d})
					if err != nil {
						continue // rows that fail to evaluate never match
					}
					if ok {
						want = append(want, r.id)
					}
				}
				q, ok := c.CompileSQL(dialect, colResolver{}, env)
				if !ok {
					continue
				}
				pushed++
				where := q.Where
				if dialect == "postgres" {
					for i := 1; strings.Contains(where, "?"); i++ {
						where = strings.Replace(where, "?", fmt.Sprintf("$%d", i), 1)
					}
				}
				rs, err := db.Query(`SELECT id FROM diff WHERE `+where+` ORDER BY id`, q.Args...)
				if err != nil {
					t.Errorf("%s: %v\n  SQL: %s", p, err, where)
					continue
				}
				var got []int
				for rs.Next() {
					var id int
					rs.Scan(&id)
					got = append(got, id)
				}
				rs.Close()
				sort.Ints(got)
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Errorf("%s: SQL gives %v, Go gives %v\n  SQL: %s %v", p, got, want, where, q.Args)
				}
			}
			if pushed < len(predicates)-6 {
				t.Errorf("only %d of %d filters compiled to SQL", pushed, len(predicates))
			}
		})
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
