package tables

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// TestScale times reads on a big table. It runs only when
// PIMPBASE_SCALE_ROWS names a row count, e.g.
//
//	PIMPBASE_SCALE_ROWS=1000000 go test ./internal/tables -run TestScale -v
func TestScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("PIMPBASE_SCALE_ROWS"))
	if n <= 0 {
		t.Skip("set PIMPBASE_SCALE_ROWS to run")
	}
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		svc, app := setup(t, s)
		deals := must[*Table](t)(svc.CreateTable(ctx, app, "Deals", []FieldInput{
			{Name: "Title", Type: TypeText},
			{Name: "Stage", Type: TypeSingleSelect, Options: []string{"Lead", "Qualified", "Won", "Lost"}},
			{Name: "Amount", Type: TypeNumber},
			{Name: "Closed", Type: TypeDate},
			{Name: "Owner", Type: TypeText},
		}))
		start := time.Now()
		seed(t, s, app, deals, n)
		t.Logf("seeded %d rows in %v", n, time.Since(start).Round(time.Millisecond))

		ruled := rbac.Actor{UserID: "usr_k", Email: "owner7@x.test", Role: rbac.Basic}
		must[*Table](t)(svc.UpdateTable(ctx, app, deals.ID, TablePatch{Permissions: &Permissions{
			Read: rbac.Basic, Write: rbac.Basic,
			Rows: []RowRule{{Role: rbac.Basic, Filter: `[Owner] = Current.User.Email`}},
		}}))
		run := func(label string) {
			measure := func(name string, fn func() (string, error)) {
				best := time.Duration(1 << 62)
				var info string
				for i := 0; i < 3; i++ {
					if !strings.Contains(name, "cached") {
						svc.touch(app, deals.ID) // measure without remembered answers
					}
					st := time.Now()
					got, err := fn()
					if err != nil {
						t.Fatalf("%s: %v", name, err)
					}
					if d := time.Since(st); d < best {
						best = d
					}
					info = got
				}
				t.Logf("%-8s %-44s %8.1f ms  %s", label, name, float64(best.Microseconds())/1000, info)
			}
			page := func(a rbac.Actor, q Query) func() (string, error) {
				return func() (string, error) {
					p, err := svc.ListRows(ctx, a, app, deals.ID, q)
					if err != nil {
						return "", err
					}
					return fmt.Sprintf("total %d via %s", p.Total, p.Engine), nil
				}
			}
			agg := func(a rbac.Actor, q AggQuery) func() (string, error) {
				return func() (string, error) {
					r, err := svc.Aggregate(ctx, a, app, deals.ID, q)
					if err != nil {
						return "", err
					}
					return fmt.Sprintf("%d groups over %d rows via %s", len(r.Groups), r.Rows, r.Engine), nil
				}
			}
			measure("first page of 50", page(admin, Query{Limit: 50}))
			measure("next page, count cached", page(admin, Query{Limit: 50, Offset: 50}))
			measure("page of 50 at the middle", page(admin, Query{Limit: 50, Offset: n / 2}))
			measure("sort by Amount desc", page(admin, Query{Limit: 50, Sort: "Amount", Desc: true}))
			measure(`filter [Owner] = "owner7@x.test"`, page(admin, Query{Limit: 50, Filter: `[Owner] = "owner7@x.test"`}))
			measure(`filter [Stage] = "Won", sort Amount`, page(admin, Query{Limit: 50, Filter: `[Stage] = "Won"`, Sort: "Amount", Desc: true}))
			measure(`search "deal 4242" in Title and Stage`, page(admin, Query{Limit: 50, Filter: `OR(CONTAINS(LOWER([Title]), "deal 4242"), CONTAINS(LOWER([Stage]), "deal 4242"))`}))
			measure("row rule: owner's first page", page(ruled, Query{Limit: 50}))
			measure("stat: sum Amount", agg(admin, AggQuery{Values: []AggValue{{Op: "sum", Field: "Amount"}}}))
			measure("chart: sum Amount by Stage", agg(admin, AggQuery{GroupBy: "Stage", Values: []AggValue{{Op: "sum", Field: "Amount"}}}))
			measure("chart: count by month of Closed", agg(admin, AggQuery{GroupBy: "Closed", Values: []AggValue{{Op: "count"}}}))
			measure("chart by Stage, row rule", agg(ruled, AggQuery{GroupBy: "Stage", Values: []AggValue{{Op: "sum", Field: "Amount"}}}))
			measure("same chart again within 10 s (cached)", agg(admin, AggQuery{GroupBy: "Stage", Values: []AggValue{{Op: "sum", Field: "Amount"}}}))
		}
		run("plain")
		for _, name := range []string{"Owner", "Stage", "Amount"} {
			f := deals.FieldByName(name)
			st := time.Now()
			if _, err := svc.UpdateField(ctx, app, deals.ID, f.ID, FieldPatch{Indexed: ptr(true)}); err != nil {
				t.Fatal(err)
			}
			t.Logf("indexed %s in %v", name, time.Since(st).Round(time.Millisecond))
		}
		run("indexed")
	})
}

func ptr[T any](v T) *T { return &v }

// seed inserts n rows straight into the table's SQL table.
func seed(t *testing.T, s *store.Store, app string, tb *Table, n int) {
	ctx := context.Background()
	db := must[*store.DB](t)(s.AppDB(ctx, app))
	cols := []string{"_created_at", "_updated_at"}
	for _, name := range []string{"Title", "Stage", "Amount", "Closed", "Owner"} {
		cols = append(cols, store.Q(tb.FieldByName(name).Column))
	}
	stages := []string{"Lead", "Qualified", "Won", "Lost"}
	r := rand.New(rand.NewPCG(1, 2))
	const batch = 500
	for done := 0; done < n; {
		tx := must[*store.Tx](t)(db.Begin(ctx))
		for b := 0; b < 20 && done < n; b++ {
			k := min(batch, n-done)
			ph := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
			q := "INSERT INTO " + db.Table(tb.sqlName()) + " (" + strings.Join(cols, ", ") + ") VALUES " + strings.TrimSuffix(strings.Repeat(ph+", ", k), ", ")
			args := make([]any, 0, k*len(cols))
			for i := 0; i < k; i++ {
				id := done + i
				now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(id) * time.Second).Format(time.RFC3339Nano)
				closed := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, r.IntN(1000)).Format("2006-01-02T15:04:05Z")
				args = append(args, now, now, fmt.Sprintf("Deal %d", id), stages[r.IntN(4)], float64(r.IntN(100000))/100, closed, fmt.Sprintf("owner%d@x.test", r.IntN(1000)))
			}
			if _, err := tx.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
			done += k
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if db.Dialect == store.Postgres {
		db.Exec(ctx, "ANALYZE "+db.Table(tb.sqlName()))
	}
}
