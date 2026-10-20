package tables

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

// TestAggregate checks that grouping in SQL gives what grouping the same
// rows in Go gives, and that row rules narrow both.
func TestAggregate(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		svc, app := setup(t, s)
		reps := must[*Table](t)(svc.CreateTable(ctx, app, "Reps", []FieldInput{{Name: "Name", Type: TypeText}}))
		deals := must[*Table](t)(svc.CreateTable(ctx, app, "Deals", []FieldInput{
			{Name: "Title", Type: TypeText},
			{Name: "Stage", Type: TypeSingleSelect, Options: []string{"Lead", "Won", "Lost"}},
			{Name: "Amount", Type: TypeNumber},
			{Name: "Closed", Type: TypeDate},
			{Name: "Hot", Type: TypeBoolean},
			{Name: "Tags", Type: TypeMultiSelect, Options: []string{"b", "a"}},
			{Name: "Rep", Type: TypeLink, LinkTable: reps.ID},
			{Name: "Owner", Type: TypeText},
			{Name: "Double", Type: TypeFormula, Formula: "[Amount] * 2"},
			{Name: "Size", Type: TypeFormula, Formula: `IF([Amount] > 500, "big", "small")`},
		}))
		var repIDs []int64
		for _, n := range []string{"Ann", "Bo", "Ann"} { // two reps share a label
			r := must[map[string]any](t)(svc.CreateRow(ctx, admin, app, reps.ID, map[string]any{"Name": n}))
			repIDs = append(repIDs, r["_id"].(int64))
		}
		stages := []any{"Lead", "Won", "Lost", nil}
		for i := 0; i < 60; i++ {
			v := map[string]any{
				"Title": fmt.Sprintf("Deal %d", i),
				"Stage": stages[i%4],
				"Owner": []string{"kim@x.test", "sam@x.test"}[i%2],
				"Hot":   i%3 == 0,
				"Rep":   repIDs[i%3],
			}
			if i%7 != 0 {
				v["Amount"] = float64(i*37%1000) + 0.25
			}
			if i%5 != 0 {
				v["Closed"] = time.Date(2025+i%2, time.Month(1+i%12), 1+i%28, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
			}
			if i%4 == 1 {
				v["Tags"] = []any{"a", "b"}
			} else if i%4 == 2 {
				v["Tags"] = []any{"b"}
			}
			must[map[string]any](t)(svc.CreateRow(ctx, admin, app, deals.ID, v))
		}

		values := []AggValue{{Op: "count"}, {Op: "sum", Field: "Amount"}, {Op: "avg", Field: "Amount"}, {Op: "min", Field: "Double"}, {Op: "max", Field: "Amount"}}
		cases := []AggQuery{
			{Values: values},
			{GroupBy: "Stage", Values: values},
			{GroupBy: "Stage", Values: values, SortBy: "value"},
			{GroupBy: "Closed", Values: values},
			{GroupBy: "Closed", DateGroup: "year", Values: values},
			{GroupBy: "Closed", DateGroup: "day", Values: values, Limit: 5},
			{GroupBy: "Hot", Values: values},
			{GroupBy: "Tags", Values: values},
			{GroupBy: "Rep", Values: values},
			{GroupBy: "Amount", Values: values[:2], Limit: 7},
			{GroupBy: "Size", Values: values, Filter: `[Stage] <> "Lost"`},
			{GroupBy: "Stage", Values: values, Filter: `[Amount] > 300`},
		}
		ruled := rbac.Actor{UserID: "usr_k", Email: "kim@x.test", Role: rbac.Basic}
		for _, actor := range []rbac.Actor{admin, ruled} {
			if actor.UserID == ruled.UserID {
				must[*Table](t)(svc.UpdateTable(ctx, app, deals.ID, TablePatch{Permissions: &Permissions{
					Read: rbac.Basic, Write: rbac.Basic,
					Rows: []RowRule{{Role: rbac.Basic, Filter: `[Owner] = Current.User.Email`}},
				}}))
			}
			for _, q := range cases {
				name := fmt.Sprintf("%s/%s/%s/%s/%s", actor.Role, q.GroupBy, q.DateGroup, q.SortBy, q.Filter)
				got, err := svc.Aggregate(ctx, actor, app, deals.ID, q)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if got.Engine != "sql" {
					t.Errorf("%s: ran in %s", name, got.Engine)
				}
				want := goAggregate(t, svc, actor, app, deals.ID, q)
				if !sameAgg(got, want) {
					t.Errorf("%s:\n sql %+v\n  go %+v", name, got, want)
				}
			}
		}

		// The row rule shows Kim her 30 deals only.
		got := must[*AggResult](t)(svc.Aggregate(ctx, ruled, app, deals.ID, AggQuery{}))
		if got.Groups[0].Values[0] != 30 {
			t.Errorf("ruled count = %v, want 30", got.Groups[0].Values[0])
		}
		// A filter without an SQL form falls back to Go with the same answer.
		q := AggQuery{GroupBy: "Stage", Values: values, Filter: `CONTAINS([Rep], "n")`}
		got = must[*AggResult](t)(svc.Aggregate(ctx, ruled, app, deals.ID, q))
		if got.Engine != "go" || !sameAgg(got, goAggregate(t, svc, ruled, app, deals.ID, q)) {
			t.Errorf("go fallback: %+v", got)
		}
		// Linked rows Kim may not read are grouped under one hidden label.
		must[*Table](t)(svc.UpdateTable(ctx, app, reps.ID, TablePatch{Permissions: &Permissions{Read: rbac.Power, Write: rbac.Power}}))
		got = must[*AggResult](t)(svc.Aggregate(ctx, ruled, app, deals.ID, AggQuery{GroupBy: "Rep"}))
		if len(got.Groups) != 1 || got.Groups[0].Key != HiddenLabel || got.Groups[0].Count != 30 {
			t.Errorf("hidden reps: %+v", got.Groups)
		}
		// No read access, no numbers.
		if _, err := svc.Aggregate(ctx, rbac.Actor{Role: rbac.Public}, app, deals.ID, AggQuery{}); !errors.Is(err, rbac.ErrForbidden) {
			t.Errorf("public: %v", err)
		}
		if _, err := svc.Aggregate(ctx, admin, app, deals.ID, AggQuery{Values: []AggValue{{Op: "median", Field: "Amount"}}}); !isInvalid(err, "values") {
			t.Errorf("bad op: %v", err)
		}
	})
}

// goAggregate groups the rows the actor sees in Go.
func goAggregate(t *testing.T, svc *Service, actor rbac.Actor, app, table string, q AggQuery) *AggResult {
	t.Helper()
	ctx := context.Background()
	c := must[*compiled](t)(svc.loadCompiled(ctx, app, table))
	db := must[*store.DB](t)(svc.store.AppDB(ctx, app))
	filters := must[[]*formula.Checked](t)(svc.filters(c, actor, q.Filter))
	all := must[[]*Row](t)(selectRows(ctx, db, db, c, "", nil, "ORDER BY _id"))
	if err := svc.hydrate(ctx, db, db, c, all, 0); err != nil {
		t.Fatal(err)
	}
	var rows []*Row
	for _, r := range all {
		keep := true
		for _, f := range filters {
			if ok, err := f.EvalBool(newRowEnv(c, r, actor, svc.now())); err != nil || !ok {
				keep = false
			}
		}
		if keep {
			rows = append(rows, r)
		}
	}
	if err := svc.maskLinks(ctx, db, db, c, rows, actor); err != nil {
		t.Fatal(err)
	}
	if q.Limit <= 0 {
		q.Limit = 200
	}
	if len(q.Values) == 0 {
		q.Values = []AggValue{{Op: "count"}}
	}
	if q.DateGroup == "" {
		q.DateGroup = "month"
	}
	var group *Field
	if q.GroupBy != "" {
		group = c.FieldByName(q.GroupBy)
	}
	vf := make([]*Field, len(q.Values))
	for i, v := range q.Values {
		vf[i] = c.FieldByName(v.Field)
	}
	return svc.aggregateGo(c, actor, svc.now(), rows, group, vf, q)
}

func sameAgg(a, b *AggResult) bool {
	if a.Rows != b.Rows || a.Truncated != b.Truncated || len(a.Groups) != len(b.Groups) {
		return false
	}
	for i := range a.Groups {
		x, y := a.Groups[i], b.Groups[i]
		if !reflect.DeepEqual(x.Key, y.Key) || x.Count != y.Count {
			return false
		}
		for j := range x.Values {
			if math.Abs(x.Values[j]-y.Values[j]) > 1e-6 {
				return false
			}
		}
	}
	return true
}
