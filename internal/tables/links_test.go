package tables

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

func TestLinkedFormulas(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		svc, app := setup(t, s)

		customers := must[*Table](t)(svc.CreateTable(ctx, app, "Customers", []FieldInput{
			{Name: "Name", Type: TypeText}, {Name: "City", Type: TypeText}, {Name: "Credit", Type: TypeNumber},
		}))
		lines := must[*Table](t)(svc.CreateTable(ctx, app, "Lines", []FieldInput{
			{Name: "Item", Type: TypeText}, {Name: "Qty", Type: TypeNumber}, {Name: "Price", Type: TypeNumber}, {Name: "Shipped", Type: TypeBoolean},
			{Name: "Amount", Type: TypeFormula, Formula: "[Qty] * [Price]"},
		}))
		orders := must[*Table](t)(svc.CreateTable(ctx, app, "Orders", []FieldInput{
			{Name: "Ref", Type: TypeText},
			{Name: "Customer", Type: TypeLink, LinkTable: customers.ID},
			{Name: "Lines", Type: TypeLink, LinkTable: lines.ID, LinkMany: true},
			{Name: "City", Type: TypeFormula, Formula: "[Customer].[City]"},
			{Name: "Total", Type: TypeFormula, Formula: "SUMOF([Lines], [Qty] * [Price])"},
			{Name: "Open", Type: TypeFormula, Formula: "COUNTOF([Lines], NOT [Shipped])"},
			{Name: "Biggest", Type: TypeFormula, Formula: "MAX([Lines].[Amount])"},
			{Name: "Items", Type: TypeFormula, Formula: `JOIN([Lines].[Item], ", ")`},
		}))
		row := func(tbl *Table, v map[string]any) map[string]any {
			for k, x := range v {
				if n, ok := x.(int); ok {
					v[k] = float64(n)
				}
			}
			return must[map[string]any](t)(svc.CreateRow(ctx, admin, app, tbl.ID, v))
		}
		paris := row(customers, map[string]any{"Name": "Ada", "City": "Paris", "Credit": 100})
		lyon := row(customers, map[string]any{"Name": "Bo", "City": "Lyon", "Credit": 5})
		l1 := row(lines, map[string]any{"Item": "Desk", "Qty": 2, "Price": 40, "Shipped": true})
		l2 := row(lines, map[string]any{"Item": "Lamp", "Qty": 1, "Price": 20})
		l3 := row(lines, map[string]any{"Item": "Pen", "Qty": 10, "Price": 1})
		o1 := row(orders, map[string]any{"Ref": "A", "Customer": paris["_id"], "Lines": []any{l1["_id"], l2["_id"]}})
		row(orders, map[string]any{"Ref": "B", "Customer": lyon["_id"], "Lines": []any{l3["_id"]}})
		row(orders, map[string]any{"Ref": "C"})

		if o1["City"] != "Paris" || o1["Total"] != 100.0 || o1["Open"] != 1.0 || o1["Biggest"] != 80.0 || o1["Items"] != "Desk, Lamp" {
			t.Errorf("order A: %v", o1)
		}

		list := func(actor rbac.Actor, filter, sort string) ([]string, string, error) {
			p, err := svc.ListRows(ctx, actor, app, orders.ID, Query{Filter: filter, Sort: sort})
			if err != nil {
				return nil, "", err
			}
			var refs []string
			for _, r := range p.Rows {
				refs = append(refs, fmt.Sprint(r["Ref"]))
			}
			return refs, p.Engine, nil
		}
		// Each filter runs in SQL, and as the same filter forced into Go
		// (the ^ 1 has no SQL form), with the same rows.
		for filter, want := range map[string]string{
			`[Total] > 50`:                             "[A]",
			`[City] = "Lyon"`:                          "[B]",
			`[Customer].[Credit] >= 100`:               "[A]",
			`ISBLANK([Customer].[City])`:               "[C]",
			`SUMOF([Lines], [Qty], [Price] < 30) = 11`: "[]",
			`COUNTOF([Lines]) = 0`:                     "[C]",
			`COUNT([Lines]) = 1`:                       "[B]",
			`SUM([Lines].[Amount]) = 10`:               "[B]",
			`[Open] = 1 AND [Biggest] > 50`:            "[A]",
			`MAXOF([Lines], [Amount], [Shipped]) = 80`: "[A]",
		} {
			got, engine, err := list(admin, filter, "")
			if err != nil {
				t.Errorf("%s: %v", filter, err)
				continue
			}
			if fmt.Sprint(got) != want || engine != "sql" {
				t.Errorf("%s: %v by %s, want %s by sql", filter, got, engine, want)
			}
			got, engine, err = list(admin, "("+filter+") AND LEN([Ref]) ^ 1 > 0", "")
			if err != nil || fmt.Sprint(got) != want || engine != "go" {
				t.Errorf("%s in Go: %v by %s (%v), want %s", filter, got, engine, err, want)
			}
		}
		if got, engine, _ := list(admin, "", "Total"); fmt.Sprint(got) != "[C B A]" || engine != "sql" {
			t.Errorf("sort by a rollup: %v by %s", got, engine)
		}

		// Renaming a field of Lines rewrites the formulas reading it.
		qty := lines.FieldByName("Qty")
		name := "Units"
		must[*Table](t)(svc.UpdateField(ctx, app, lines.ID, qty.ID, FieldPatch{Name: &name}))
		o := must[*Table](t)(svc.Get(ctx, app, orders.ID))
		if f := o.FieldByName("Total").Formula; f != "SUMOF([Lines], [Units] * [Price])" {
			t.Errorf("after rename: %s", f)
		}
		if f := must[*Table](t)(svc.Get(ctx, app, lines.ID)).FieldByName("Amount").Formula; f != "[Units] * [Price]" {
			t.Errorf("own formula after rename: %s", f)
		}
		if got := must[map[string]any](t)(svc.GetRow(ctx, admin, app, orders.ID, o1["_id"].(int64))); got["Total"] != 100.0 {
			t.Errorf("total after rename: %v", got["Total"])
		}
		// Deleting or retyping a field another table reads is refused.
		if _, err := svc.DeleteField(ctx, app, customers.ID, customers.FieldByName("City").ID); err == nil || !strings.Contains(err.Error(), "the formula [City] in table Orders") {
			t.Errorf("delete a field read through a link: %v", err)
		}
		text := TypeText
		if _, err := svc.UpdateField(ctx, app, lines.ID, lines.FieldByName("Shipped").ID, FieldPatch{Type: &text}); err == nil || !strings.Contains(err.Error(), "[Open]") {
			t.Errorf("retype a field read through a link: %v", err)
		}
		// Stored formulas cannot read linked rows.
		if _, _, err := svc.AddField(ctx, app, orders.ID, FieldInput{Name: "Kept", Type: TypeFormula, Formula: "[Customer].[City]", FormulaMode: Static}); err == nil || !strings.Contains(err.Error(), "make it dynamic") {
			t.Errorf("static formula over links: %v", err)
		}

		// Row rules may read linked rows.
		perms := orders.Permissions
		perms.Rows = []RowRule{{Role: rbac.Basic, Filter: `[Customer].[City] = "Paris"`}}
		must[*Table](t)(svc.UpdateTable(ctx, app, orders.ID, TablePatch{Permissions: &perms}))
		basic := rbac.Actor{UserID: "usr_2", Role: rbac.Basic}
		if got, engine, err := list(basic, "", ""); err != nil || fmt.Sprint(got) != "[A]" || engine != "sql" {
			t.Errorf("row rule over a link: %v by %s (%v)", got, engine, err)
		}
		if _, err := svc.GetRow(ctx, basic, app, orders.ID, o1["_id"].(int64)+1); err == nil {
			t.Error("row rule over a link lets basic read order B")
		}

		// A user whose access to Lines is limited may not filter through
		// it, which would reveal hidden lines; builders may.
		lp := lines.Permissions
		lp.Rows = []RowRule{{Role: rbac.Basic, Filter: `[Shipped]`}}
		must[*Table](t)(svc.UpdateTable(ctx, app, lines.ID, TablePatch{Permissions: &lp}))
		if _, _, err := list(basic, `SUMOF([Lines], [Price]) > 30`, ""); err == nil || !strings.Contains(err.Error(), "cannot see all of them") {
			t.Errorf("filter through a limited link: %v", err)
		}
		if _, _, err := list(basic, `[Customer].[Credit] > 0`, ""); err != nil {
			t.Errorf("filter through an open link: %v", err)
		}
	})
}
