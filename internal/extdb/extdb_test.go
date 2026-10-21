package extdb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pimpbase/pimpbase/internal/formula"
	"github.com/pimpbase/pimpbase/internal/interp"
)

// The database tests run against real servers named by environment
// variables, for example:
//
//	PIMPBASE_TEST_EXT_POSTGRES=postgres://postgres:pw@localhost:5432/shop?sslmode=disable
//	PIMPBASE_TEST_EXT_MYSQL=root:pw@tcp(localhost:3306)/shop?parseTime=true
//	PIMPBASE_TEST_EXT_MSSQL=sqlserver://sa:pw@localhost:1433?database=master
//	PIMPBASE_TEST_EXT_MONGO=mongodb://localhost:27017/shop
func source(t *testing.T, kind, env string) Source {
	uri := os.Getenv(env)
	if uri == "" {
		t.Skipf("%s is not set", env)
	}
	d, err := Lookup(kind)
	if err != nil {
		t.Skip(err)
	}
	src, err := d.Open(Settings{URI: uri})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	if err := src.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return src
}

func exec(t *testing.T, src Source, sql string) {
	t.Helper()
	if _, err := src.Run(context.Background(), Statement{SQL: sql}); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

var sqlSetup = map[string][]string{
	"postgres": {
		`DROP TABLE IF EXISTS bb_lines`,
		`DROP TABLE IF EXISTS bb_items`,
		`DROP TYPE IF EXISTS bb_status`,
		`CREATE TYPE bb_status AS ENUM ('open', 'won', 'lost')`,
		`CREATE TABLE bb_items (id SERIAL PRIMARY KEY, name VARCHAR(80) NOT NULL, notes TEXT, price NUMERIC(10,2), qty INTEGER, active BOOLEAN, due DATE, status bb_status, ref UUID)`,
	},
	"mysql": {
		`DROP TABLE IF EXISTS bb_lines`,
		`DROP TABLE IF EXISTS bb_items`,
		"CREATE TABLE bb_items (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(80) NOT NULL, notes TEXT, price DECIMAL(10,2), qty INT, active TINYINT(1), due DATE, status ENUM('open','won','lost'), ref CHAR(36))",
	},
	"mssql": {
		`IF OBJECT_ID('dbo.bb_lines') IS NOT NULL DROP TABLE dbo.bb_lines`,
		`IF OBJECT_ID('dbo.bb_items') IS NOT NULL DROP TABLE dbo.bb_items`,
		`CREATE TABLE dbo.bb_items (id INT IDENTITY(1,1) PRIMARY KEY, name NVARCHAR(80) NOT NULL, notes NVARCHAR(MAX), price DECIMAL(10,2), qty INT, active BIT, due DATE, status NVARCHAR(10), ref UNIQUEIDENTIFIER)`,
	},
}

func TestSQLSources(t *testing.T) {
	for kind, env := range map[string]string{"postgres": "PIMPBASE_TEST_EXT_POSTGRES", "mysql": "PIMPBASE_TEST_EXT_MYSQL", "mssql": "PIMPBASE_TEST_EXT_MSSQL"} {
		t.Run(kind, func(t *testing.T) {
			src := source(t, kind, env)
			ctx := context.Background()
			for _, q := range sqlSetup[kind] {
				exec(t, src, q)
			}
			ts, err := src.Tables(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := ""
			for _, x := range ts {
				if x.Name == "bb_items" {
					found = x.Schema
				}
			}
			if found == "" {
				t.Fatalf("bb_items not listed: %+v", ts)
			}
			info, err := src.Describe(ctx, found, "bb_items")
			if err != nil {
				t.Fatal(err)
			}
			types := map[string]string{}
			for _, c := range info.Columns {
				types[c.Name] = c.Type
			}
			want := map[string]string{"id": "autonumber", "name": "text", "notes": "longtext", "price": "number", "qty": "number", "active": "boolean", "due": "date"}
			for k, v := range want {
				if types[k] != v {
					t.Errorf("column %s is %s, want %s (%+v)", k, types[k], v, info.Columns)
				}
			}
			if kind != "mssql" && types["status"] != "single_select" {
				t.Errorf("status is %s", types["status"])
			}
			if strings.Join(info.PrimaryKey, ",") != "id" {
				t.Errorf("primary key %v", info.PrimaryKey)
			}
			tg := &Target{Schema: found, Name: "bb_items", Columns: info.Columns, Keys: []string{"id"}}
			rows := src.Rows()
			due, _ := formula.ParseDate("2026-11-01")
			var keys [][]any
			for i, r := range []map[string]any{
				{"name": "Apple", "price": 1.5, "qty": int64(10), "active": true, "due": due, "status": "open"},
				{"name": "Banana", "price": 0.25, "qty": int64(0), "active": false, "status": "won"},
				{"name": "Cherry O'Hara", "notes": "red", "qty": nil, "active": nil},
			} {
				k, err := rows.Insert(ctx, tg, r)
				if err != nil {
					t.Fatalf("insert %d: %v", i, err)
				}
				keys = append(keys, k)
			}
			if keys[0][0] != 1.0 || keys[2][0] != 3.0 {
				t.Fatalf("keys %v", keys)
			}
			check := func(src2, want string) {
				t.Helper()
				ch, errs := formula.Compile(src2, scopeOf(tg))
				if errs != nil {
					t.Fatal(errs[0])
				}
				cond, ok := rows.Compile(tg, ch, testEnv{})
				if !ok {
					t.Fatalf("%s has no SQL form", src2)
				}
				got, err := rows.Select(ctx, tg, Select{Where: cond, Order: []Order{{Column: "name"}}})
				if err != nil {
					t.Fatalf("%s: %v (%s)", src2, err, cond.SQL)
				}
				var names []string
				for _, r := range got {
					names = append(names, r["name"].(string))
				}
				if strings.Join(names, ",") != want {
					t.Errorf("%s gave %v, want %s (%s %v)", src2, names, want, cond.SQL, cond.Args)
				}
				n, err := rows.Count(ctx, tg, cond)
				if err != nil || n != len(names) {
					t.Errorf("count %s = %d, %v", src2, n, err)
				}
			}
			check(`[qty] > 5`, "Apple")
			check(`[qty] < 5`, "Banana,Cherry O'Hara")
			check(`[qty] = 0`, "Banana,Cherry O'Hara")
			check(`[qty] <> 0`, "Apple")
			check(`[active]`, "Apple")
			check(`NOT [active]`, "Banana,Cherry O'Hara")
			check(`[active] = FALSE`, "Banana,Cherry O'Hara")
			check(`CONTAINS(LOWER(TEXT([name])), "an")`, "Banana")
			check(`OR(CONTAINS(LOWER(TEXT([name])), "o'h"), CONTAINS(LOWER(TEXT([qty])), "10"))`, "Apple,Cherry O'Hara")
			check(`STARTSWITH([name], "Ch")`, "Cherry O'Hara")
			check(`ISBLANK([notes])`, "Apple,Banana")
			check(`[due] < DATE(2027, 1, 1)`, "Apple")
			check(`[due] > DATE(2020, 1, 1) OR [price] >= 1`, "Apple")
			check(`[status] IN ("open", "won")`, "Apple,Banana")
			check(`[name] = Current.User.Name`, "Banana")
			check(`[price] * 4 = 1`, "Banana")

			got, err := rows.Select(ctx, tg, Select{Order: []Order{{Column: "qty", Desc: true}}, Limit: 2, Offset: 0})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0]["name"] != "Apple" || got[1]["name"] != "Banana" {
				t.Fatalf("sorted page %v", got)
			}
			if got[0]["price"] != 1.5 || got[0]["active"] != true || got[0]["status"] != "open" {
				t.Errorf("values %v", got[0])
			}
			if d, ok := got[0]["due"].(time.Time); !ok || !d.Equal(due) {
				t.Errorf("due %v", got[0]["due"])
			}
			if err := rows.Update(ctx, tg, keys[1], map[string]any{"qty": int64(7), "notes": "yellow"}); err != nil {
				t.Fatal(err)
			}
			check(`[qty] > 5`, "Apple,Banana")
			if n, err := rows.Delete(ctx, tg, [][]any{keys[0], keys[2]}); err != nil || n != 2 {
				t.Fatalf("delete %d %v", n, err)
			}
			if err := rows.Update(ctx, tg, []any{int64(99)}, map[string]any{"qty": int64(1)}); kind != "mysql" && err != ErrNoRow {
				t.Errorf("update of a missing row: %v", err)
			}

			// A query the builder wrote, with bindings as parameters.
			scope := interp.Map{"params": map[string]any{"name": "Banana' OR 1=1 --", "ids": []any{1.0, 2.0, 3.0}}}
			q, args, err := BindSQL(kind, `SELECT name, qty FROM bb_items WHERE name = {{ params.name }} OR id IN ({{ params.ids }})`, scope)
			if err != nil {
				t.Fatal(err)
			}
			res, err := src.Run(ctx, Statement{SQL: q, Args: args})
			if err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			if len(res.Rows) != 1 || res.Rows[0]["name"] != "Banana" || res.Rows[0]["qty"] != 7.0 {
				t.Errorf("query rows %v", res.Rows)
			}
			q, args, _ = BindSQL(kind, `UPDATE bb_items SET qty = {{ params.qty }} WHERE id = {{ params.id }}`, interp.Map{"params": map[string]any{"qty": 3.0, "id": 2.0}})
			res, err = src.Run(ctx, Statement{SQL: q, Args: args})
			if err != nil || res.RowsAffected != 1 || res.Query {
				t.Errorf("update: %+v %v", res, err)
			}
			if _, _, err := BindSQL(kind, `SELECT * FROM bb_items WHERE name = '{{ params.name }}'`, scope); err == nil {
				t.Error("a binding inside quotes was accepted")
			}
			testCompositeKeys(t, src, kind, found)
		})
	}
}

var compositeSetup = map[string][]string{
	"postgres": {
		`DROP TABLE IF EXISTS bb_lines`,
		`CREATE TABLE bb_lines (item_id INTEGER NOT NULL REFERENCES bb_items(id), line INTEGER NOT NULL, note VARCHAR(40), PRIMARY KEY (item_id, line))`,
	},
	"mysql": {
		`DROP TABLE IF EXISTS bb_lines`,
		`CREATE TABLE bb_lines (item_id INT NOT NULL, line INT NOT NULL, note VARCHAR(40), PRIMARY KEY (item_id, line), FOREIGN KEY (item_id) REFERENCES bb_items(id))`,
	},
	"mssql": {
		`IF OBJECT_ID('dbo.bb_lines') IS NOT NULL DROP TABLE dbo.bb_lines`,
		`CREATE TABLE dbo.bb_lines (item_id INT NOT NULL REFERENCES dbo.bb_items(id), line INT NOT NULL, note NVARCHAR(40), PRIMARY KEY (item_id, line))`,
	},
}

// testCompositeKeys writes rows of a table keyed by two columns, one of
// them a foreign key.
func testCompositeKeys(t *testing.T, src Source, kind, schema string) {
	ctx := context.Background()
	for _, q := range compositeSetup[kind] {
		exec(t, src, q)
	}
	info, err := src.Describe(ctx, schema, "bb_lines")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(info.PrimaryKey, ",") != "item_id,line" {
		t.Fatalf("primary key %v", info.PrimaryKey)
	}
	if ref := info.Columns[0].References; ref == nil || ref.Table != "bb_items" || ref.Column != "id" {
		t.Errorf("item_id references %+v", ref)
	}
	if info.Columns[1].References != nil {
		t.Errorf("line references %+v", info.Columns[1].References)
	}
	tg := &Target{Schema: schema, Name: "bb_lines", Columns: info.Columns, Keys: info.PrimaryKey}
	rows := src.Rows()
	items, err := rows.Select(ctx, &Target{Schema: schema, Name: "bb_items", Columns: []Column{{Name: "id", Type: "autonumber", DBType: "int4"}}, Keys: []string{"id"}}, Select{Limit: 1})
	if err != nil || len(items) == 0 {
		t.Fatalf("items %v %v", items, err)
	}
	item := int64(items[0]["id"].(float64))
	for line := int64(1); line <= 3; line++ {
		key, err := rows.Insert(ctx, tg, map[string]any{"item_id": item, "line": line, "note": "n"})
		if err != nil {
			t.Fatal(err)
		}
		if len(key) != 2 || key[0] != float64(item) || key[1] != float64(line) {
			t.Errorf("key %v", key)
		}
	}
	if err := rows.Update(ctx, tg, []any{item, int64(2)}, map[string]any{"note": "second"}); err != nil {
		t.Fatal(err)
	}
	if n, err := rows.Delete(ctx, tg, [][]any{{item, int64(1)}, {item, int64(3)}}); err != nil || n != 2 {
		t.Fatalf("delete %d %v", n, err)
	}
	got, err := rows.Select(ctx, tg, Select{})
	if err != nil || len(got) != 1 || got[0]["note"] != "second" || got[0]["line"] != 2.0 {
		t.Errorf("left %v %v", got, err)
	}
	exec(t, src, compositeSetup[kind][0])
}

func TestMongoSource(t *testing.T) {
	src := source(t, "mongodb", "PIMPBASE_TEST_EXT_MONGO")
	ctx := context.Background()
	exec := func(op, filter, doc string) *Result {
		t.Helper()
		r, err := src.Run(ctx, Statement{Collection: "bb_items", Operation: op, Filter: filter, Document: doc})
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		return r
	}
	exec("deleteMany", `{"_id": {"$exists": true}}`, "")
	exec("insertMany", "", `[{"name": "Apple", "qty": 10, "active": true, "due": {"$date": "2026-11-01T00:00:00Z"}}, {"name": "Banana", "qty": 0, "active": false}, {"name": "Cherry", "notes": "red"}]`)
	info, err := src.Describe(ctx, "", "bb_items")
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, c := range info.Columns {
		types[c.Name] = c.Type
	}
	if types["qty"] != "number" || types["active"] != "boolean" || types["due"] != "date" || types["name"] != "text" {
		t.Fatalf("types %v", types)
	}
	tg := &Target{Name: "bb_items", Columns: info.Columns, Keys: []string{"_id"}}
	rows := src.Rows()
	check := func(f, want string) {
		t.Helper()
		ch, errs := formula.Compile(f, scopeOf(tg))
		if errs != nil {
			t.Fatal(errs[0])
		}
		cond, ok := rows.Compile(tg, ch, testEnv{})
		if !ok {
			t.Fatalf("%s has no Mongo form", f)
		}
		got, err := rows.Select(ctx, tg, Select{Where: cond, Order: []Order{{Column: "name"}}})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, r := range got {
			names = append(names, r["name"].(string))
		}
		if strings.Join(names, ",") != want {
			t.Errorf("%s gave %v, want %s (%v)", f, names, want, cond.Mongo)
		}
	}
	check(`[qty] > 5`, "Apple")
	check(`[qty] < 5`, "Banana,Cherry")
	check(`[qty] <> 0`, "Apple")
	check(`[active]`, "Apple")
	check(`NOT [active]`, "Banana,Cherry")
	check(`CONTAINS(LOWER(TEXT([name])), "an")`, "Banana")
	check(`CONTAINS(LOWER(TEXT([qty])), "10")`, "Apple")
	check(`ISBLANK([notes])`, "Apple,Banana")
	check(`[due] < DATE(2027, 1, 1)`, "Apple")
	check(`[name] IN ("Apple", "Cherry")`, "Apple,Cherry")
	key, err := rows.Insert(ctx, tg, map[string]any{"name": "Date", "qty": 4.0})
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Update(ctx, tg, key, map[string]any{"qty": 40.0}); err != nil {
		t.Fatal(err)
	}
	check(`[qty] > 5`, "Apple,Date")
	if n, err := rows.Delete(ctx, tg, [][]any{key}); err != nil || n != 1 {
		t.Fatalf("delete %d %v", n, err)
	}
	r := exec("find", `{"qty": {"$gte": 0}}`, "")
	if len(r.Rows) != 2 {
		t.Errorf("find %v", r.Rows)
	}
	r = exec("aggregate", `[{"$group": {"_id": null, "total": {"$sum": "$qty"}}}]`, "")
	if len(r.Rows) != 1 || r.Rows[0]["total"] != 10.0 {
		t.Errorf("aggregate %v", r.Rows)
	}
}

type testScope struct{ t *Target }

func scopeOf(t *Target) testScope { return testScope{t} }

func (s testScope) FieldType(name string) (formula.Type, bool) {
	c := s.t.Column(name)
	if c == nil {
		return formula.Any, false
	}
	return FormulaType(c.Type), true
}

func (s testScope) NameType(parts []string) (formula.Type, bool) { return formula.Text, true }

type testEnv struct{}

func (testEnv) Field(string) any  { return nil }
func (testEnv) Name([]string) any { return "Banana" }
func (testEnv) Now() time.Time    { return time.Now() }
