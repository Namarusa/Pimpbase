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
	`COALESCE([n], [m]) > 1`, `COALESCE([t], [u]) = "Apple"`, `ISBLANK(COALESCE([t], [u]))`, `COALESCE([d], DATE(2026, 5, 5)) > DATE(2026, 3, 1)`,
	`SIGN([n]) = -1`, `SIGN([n] - [m]) >= 0`, `FIND("b", [t]) = 3`, `FIND("", [t]) = 1`, `FIND("x", [t]) = 0`,
	`YEAR([d]) = 2026`, `MONTH([d]) = 10`, `DAY([d]) > 18`, `HOUR([d]) = 0`, `MINUTE([d]) = 0`, `QUARTER([d]) = 4`, `ISBLANK(YEAR([d]))`,
	`REGEXMATCH([t], "^[a-z]+$")`, `REGEXMATCH([u], "(?i)^a")`, `REGEXEXTRACT([t], "([aeiou])") = "a"`, `ISBLANK(REGEXEXTRACT([t], "q"))`,
	`REGEXREPLACE([t], "[aeiou]", "_") = "_ppl_"`, `REGEXREPLACE([u], "(b)", "<$1>") = "a <b>"`,
	// These fall back to Go but must still work.
	`REGEXMATCH([t], [u])`, `PROPER([t]) = [u]`, `PADLEFT(TEXT([n]), 3, "0") = "001"`, `WEEKNUM([d]) = 42`, `CONTAINS(SORT(LIST([t], [u])), "Apple")`,
	`[n] ^ 2 > 4`, `CONTAINS(TEXT([n]), "1")`, `[t] = 1`,
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
			goOnly := 11
			if dialect == "postgres" {
				goOnly += 6 // regular expressions stay in Go there
			}
			if pushed < len(predicates)-goOnly {
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

// Linked rows: each diff row may link many rows of lines (through the
// join table lj) and one row of people (through the cust column).

type linkScope struct{ testScope }

var linesScope = testScope{"amt": Number, "st": Text, "paid": Bool}
var peopleScope = testScope{"nm": Text, "age": Number}

func (s linkScope) Link(name string) (Scope, bool, bool) {
	switch strings.ToLower(name) {
	case "lines":
		return linesScope, true, true
	case "cust":
		return peopleScope, false, true
	}
	return nil, false, false
}

func (s linkScope) FieldType(name string) (Type, bool) {
	switch strings.ToLower(name) {
	case "lines":
		return List, true
	case "cust":
		return Text, true
	}
	return s.testScope.FieldType(name)
}

type linkColResolver struct{}

func (linkColResolver) FieldSQL(name string) (string, []any, bool) {
	return colResolver{}.FieldSQL(name)
}

func (linkColResolver) LinkSQL(name, alias string) (string, string, []any, SQLResolver, bool) {
	switch strings.ToLower(name) {
	case "lines":
		return "lines AS " + alias + " JOIN lj AS " + alias + "_j ON " + alias + "_j.dst = " + alias + ".id", alias + "_j.src = diff.id", nil, aliasResolver{alias, linesScope}, true
	case "cust":
		return "people AS " + alias, alias + ".id = diff.cust", nil, aliasResolver{alias, peopleScope}, true
	}
	return "", "", nil, nil, false
}

type aliasResolver struct {
	alias string
	cols  testScope
}

func (r aliasResolver) FieldSQL(name string) (string, []any, bool) {
	if _, ok := r.cols[strings.ToLower(name)]; !ok {
		return "", nil, false
	}
	return r.alias + `."` + strings.ToLower(name) + `"`, nil, true
}

type linkTestEnv struct {
	testEnv
	linked map[string][]Env
}

func (e linkTestEnv) Field(name string) any {
	switch strings.ToLower(name) {
	case "lines":
		out := []any{}
		for range e.linked["lines"] {
			out = append(out, "line") // labels are never blank
		}
		return out
	case "cust":
		if c := e.linked["cust"]; len(c) > 0 {
			return c[0].Field("nm")
		}
		return nil
	}
	return e.testEnv.Field(name)
}

func (e linkTestEnv) Linked(name string) ([]Env, error) { return e.linked[strings.ToLower(name)], nil }

type line struct {
	id     int
	parent int
	amt    any
	st     any
	paid   any
}

var lines = []line{
	{1, 1, 5.0, "x", true}, {2, 1, 7.5, "", false}, {3, 3, -2.0, "y", nil},
	{4, 4, nil, nil, true}, {5, 5, 1.0, "x", false}, {6, 5, 4.0, "z", true}, {7, 5, nil, "x", nil},
}

var people = []struct {
	id  int
	nm  any
	age any
}{{1, "Bo", 31.0}, {2, nil, 40.0}, {3, "Cy", nil}}

var custOf = map[int]any{1: 1, 2: 2, 3: 3, 5: 1, 6: 99} // 6 links a missing row

var linkPredicates = []string{
	`SUMOF([lines], [amt]) > 10`, `SUMOF([lines], [amt]) = 0`, `SUM([lines].[amt]) > 4`, `SUM([lines].[amt]) = 0`,
	`COUNTOF([lines]) = 2`, `COUNTOF([lines], [paid]) >= 1`, `COUNT([lines]) = 0`, `COUNT([lines].[st]) = 2`, `COUNT([lines].[amt]) = 2`,
	`AVERAGEOF([lines], [amt], [st] <> "") > 2`, `ISBLANK(AVERAGEOF([lines], [amt]))`, `AVERAGE([lines].[amt]) >= 2.5`,
	`MINOF([lines], [amt]) < 0`, `ISBLANK(MAXOF([lines], [amt]))`, `MAX([lines].[amt]) = 7.5`, `MIN([lines].[amt]) = 1`,
	`SUMOF([lines], [amt], [paid] AND [amt] > 1) = 5`, `SUMOF([lines], [amt] * 2, [st] IN ("x", "z")) = 10`,
	`COUNTOF([lines], NOT [paid]) = 1`, `COUNTOF([lines], ISBLANK([st])) > 0`,
	`[cust].[nm] = "Bo"`, `[cust].[age] > 30`, `ISBLANK([cust].[nm])`, `[cust].[age] + [n] > 31`, `UPPER([cust].[nm]) = "CY"`,
	`[cust] = "Bo"`, `SUMOF([lines], [amt]) > [n]`,
	// In Go only.
	`CONTAINS([lines].[st], "x")`, `JOIN([lines].[st], "") = "x"`, `SUMOF([lines], VALUE(TEXT([amt]))) > 1`,
}

func TestLinksSQLMatchesEval(t *testing.T) {
	scope := linkScope{diffScope}
	for dialect, db := range openDBs(t) {
		t.Run(dialect, func(t *testing.T) {
			defer db.Close()
			real, ph := "REAL", func(i int) string { return "?" }
			if dialect == "postgres" {
				real = "DOUBLE PRECISION"
				ph = func(i int) string { return fmt.Sprintf("$%d", i) }
			}
			for _, q := range []string{`DROP TABLE IF EXISTS diff`, `DROP TABLE IF EXISTS lines`, `DROP TABLE IF EXISTS lj`, `DROP TABLE IF EXISTS people`} {
				mustExec(t, db, q)
			}
			mustExec(t, db, `CREATE TABLE diff (id INTEGER PRIMARY KEY, n `+real+`, cust INTEGER)`)
			mustExec(t, db, `CREATE TABLE lines (id INTEGER PRIMARY KEY, amt `+real+`, st TEXT, paid INTEGER)`)
			mustExec(t, db, `CREATE TABLE lj (src INTEGER, dst INTEGER)`)
			mustExec(t, db, `CREATE TABLE people (id INTEGER PRIMARY KEY, nm TEXT, age `+real+`)`)
			envs := map[int]linkTestEnv{}
			lineEnvs := map[int]Env{}
			for _, l := range lines {
				var paid any
				if l.paid != nil {
					paid = map[bool]int{false: 0, true: 1}[l.paid.(bool)]
				}
				mustExec(t, db, fmt.Sprintf(`INSERT INTO lines VALUES (%s, %s, %s, %s)`, ph(1), ph(2), ph(3), ph(4)), l.id, l.amt, l.st, paid)
				mustExec(t, db, fmt.Sprintf(`INSERT INTO lj VALUES (%s, %s)`, ph(1), ph(2)), l.parent, l.id)
				lineEnvs[l.id] = testEnv{"amt": l.amt, "st": l.st, "paid": l.paid}
			}
			mustExec(t, db, fmt.Sprintf(`INSERT INTO lj VALUES (%s, %s)`, ph(1), ph(2)), 2, 999) // a link to a deleted row
			personEnvs := map[any]Env{}
			for _, p := range people {
				mustExec(t, db, fmt.Sprintf(`INSERT INTO people VALUES (%s, %s, %s)`, ph(1), ph(2), ph(3)), p.id, p.nm, p.age)
				personEnvs[p.id] = testEnv{"nm": p.nm, "age": p.age}
			}
			for _, r := range rows {
				mustExec(t, db, fmt.Sprintf(`INSERT INTO diff VALUES (%s, %s, %s)`, ph(1), ph(2), ph(3)), r.id, r.n, custOf[r.id])
				e := linkTestEnv{testEnv{"n": r.n}, map[string][]Env{"lines": {}}}
				for _, l := range lines {
					if l.parent == r.id {
						e.linked["lines"] = append(e.linked["lines"], lineEnvs[l.id])
					}
				}
				if p, ok := personEnvs[custOf[r.id]]; ok {
					e.linked["cust"] = []Env{p}
				}
				envs[r.id] = e
			}
			pushed := 0
			for _, p := range linkPredicates {
				c, errs := Compile(p, scope)
				if errs != nil {
					t.Fatalf("%s: %v", p, errs[0])
				}
				var want []int
				for _, r := range rows {
					if ok, err := c.EvalBool(envs[r.id]); err == nil && ok {
						want = append(want, r.id)
					}
				}
				q, ok := c.CompileSQL(dialect, linkColResolver{}, env)
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
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Errorf("%s: SQL gives %v, Go gives %v\n  SQL: %s %v", p, got, want, where, q.Args)
				}
			}
			if pushed < len(linkPredicates)-5 {
				t.Errorf("only %d of %d link filters compiled to SQL", pushed, len(linkPredicates))
			}
		})
	}
}
