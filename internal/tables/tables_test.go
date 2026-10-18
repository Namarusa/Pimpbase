package tables

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/store/storetest"
)

var admin = rbac.Actor{UserID: "usr_1", Email: "ada@example.com", Name: "Ada", Role: rbac.Admin, Builder: true}

func setup(t *testing.T, s *store.Store) (*Service, string) {
	a, err := apps.NewService(s).Create(context.Background(), "Shop", "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(s)
	svc.now = func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) }
	return svc, a.ID
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func TestTablesEndToEnd(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		svc, app := setup(t, s)

		customers := must[*Table](t)(svc.CreateTable(ctx, app, "Customers", []FieldInput{
			{Name: "Name", Type: TypeText, Required: true},
			{Name: "Tier", Type: TypeSingleSelect, Options: []string{"Gold", "Silver"}},
		}))
		orders := must[*Table](t)(svc.CreateTable(ctx, app, "Orders", []FieldInput{
			{Name: "Ref", Type: TypeAutoNumber},
			{Name: "Item", Type: TypeText},
			{Name: "Qty", Type: TypeNumber},
			{Name: "Unit price", Type: TypeNumber},
			{Name: "Paid", Type: TypeBoolean},
			{Name: "Due", Type: TypeDate},
			{Name: "Tags", Type: TypeMultiSelect, Options: []string{"rush", "gift"}},
			{Name: "Customer", Type: TypeLink, LinkTable: customers.ID},
			{Name: "Total", Type: TypeFormula, Formula: "[Qty] * [Unit price]"},
			{Name: "Big", Type: TypeFormula, Formula: `IF([Total] > 50, "big", "small")`, FormulaMode: Static},
			{Name: "Who", Type: TypeFormula, Formula: `[Customer] & " / " & [Item]`},
		}))
		if orders.PrimaryField != orders.FieldByName("Ref").ID {
			t.Errorf("primary field = %s, want Ref", orders.PrimaryField)
		}

		ada := must[map[string]any](t)(svc.CreateRow(ctx, admin, app, customers.ID, map[string]any{"Name": "Ada", "Tier": "Gold"}))
		bob := must[map[string]any](t)(svc.CreateRow(ctx, admin, app, customers.ID, map[string]any{"name": "Bob"}))
		if _, err := svc.CreateRow(ctx, admin, app, customers.ID, map[string]any{"Tier": "Gold"}); !isInvalid(err, "Name") {
			t.Errorf("missing required: %v", err)
		}
		if _, err := svc.CreateRow(ctx, admin, app, customers.ID, map[string]any{"Name": "X", "Tier": "Bronze"}); !isInvalid(err, "Tier") {
			t.Errorf("bad option: %v", err)
		}

		rows := []map[string]any{
			{"Item": "Desk", "Qty": 2, "Unit price": 40, "Paid": true, "Due": "2026-10-01", "Tags": []any{"rush"}, "Customer": ada["_id"]},
			{"Item": "Lamp", "Qty": 1, "Unit price": 20.5, "Paid": false, "Customer": map[string]any{"_id": bob["_id"]}},
			{"Item": "Chair", "Qty": "3", "Unit price": "15", "Due": "2026-11-30"},
			{"Item": nil, "Qty": nil},
		}
		for _, r := range rows {
			for k, v := range r {
				if n, ok := v.(int); ok {
					r[k] = float64(n)
				}
			}
			must[map[string]any](t)(svc.CreateRow(ctx, admin, app, orders.ID, r))
		}

		page := must[*Page](t)(svc.ListRows(ctx, admin, app, orders.ID, Query{}))
		if page.Total != 4 || page.Engine != "sql" {
			t.Fatalf("total %d engine %s", page.Total, page.Engine)
		}
		desk := page.Rows[0]
		if desk["Ref"] != 1.0 || desk["Total"] != 80.0 || desk["Big"] != "big" || desk["Paid"] != true {
			t.Errorf("desk = %v", desk)
		}
		if l, ok := desk["Customer"].(LinkRef); !ok || l.Label != "Ada" {
			t.Errorf("desk customer = %#v", desk["Customer"])
		}
		if desk["Who"] != "Ada / Desk" || desk["Due"] != "2026-10-01T00:00:00Z" {
			t.Errorf("who %v due %v", desk["Who"], desk["Due"])
		}
		if page.Rows[3]["Ref"] != 4.0 || page.Rows[3]["Total"] != 0.0 {
			t.Errorf("blank row = %v", page.Rows[3])
		}

		ids := func(p *Page) string {
			var out []any
			for _, r := range p.Rows {
				out = append(out, r["Item"])
			}
			return fmt.Sprint(out)
		}
		cases := []struct {
			q      Query
			want   string
			engine string
		}{
			{Query{Filter: "[Total] > 30"}, "[Desk Chair]", "sql"},
			{Query{Filter: `[Paid]`}, "[Desk]", "sql"},
			{Query{Filter: `NOT [Paid]`}, "[Lamp Chair <nil>]", "sql"},
			{Query{Filter: `[Due] < TODAY()`}, "[Desk]", "sql"},
			{Query{Filter: `ISBLANK([Item])`}, "[<nil>]", "sql"},
			{Query{Filter: `[Big] = "small"`}, "[Lamp Chair <nil>]", "sql"},
			{Query{Filter: `CONTAINS([Tags], "rush")`}, "[Desk]", "go"},
			{Query{Filter: `[Customer] = "Bob"`}, "[Lamp]", "go"},
			{Query{Sort: "Unit price", Desc: true}, "[Desk Lamp Chair <nil>]", "sql"},
			{Query{Sort: "Item"}, "[Chair Desk Lamp <nil>]", "sql"},
			{Query{Sort: "Total", Desc: true}, "[Desk Chair Lamp <nil>]", "go"},
			{Query{Sort: "Item", Limit: 2, Offset: 1}, "[Desk Lamp]", "sql"},
		}
		for _, c := range cases {
			p, err := svc.ListRows(ctx, admin, app, orders.ID, c.q)
			if err != nil {
				t.Errorf("%+v: %v", c.q, err)
				continue
			}
			if got := ids(p); got != c.want || p.Engine != c.engine {
				t.Errorf("%+v: got %s via %s, want %s via %s", c.q, got, p.Engine, c.want, c.engine)
			}
		}
		if _, err := svc.ListRows(ctx, admin, app, orders.ID, Query{Filter: "[Qty] +"}); !isInvalid(err, "filter") {
			t.Errorf("bad filter: %v", err)
		}

		// Update recomputes static formulas.
		lampID := page.Rows[1]["_id"].(int64)
		lamp := must[map[string]any](t)(svc.UpdateRow(ctx, admin, app, orders.ID, lampID, map[string]any{"Qty": 10.0}))
		if lamp["Big"] != "big" || lamp["Total"] != 205.0 {
			t.Errorf("updated lamp = %v", lamp)
		}

		// Renaming a field rewrites formulas.
		qty := orders.FieldByName("Qty")
		name := "Quantity"
		orders = must[*Table](t)(svc.UpdateField(ctx, app, orders.ID, qty.ID, FieldPatch{Name: &name}))
		if f := orders.FieldByName("Total"); f.Formula != "[Quantity] * [Unit price]" {
			t.Errorf("renamed formula = %s", f.Formula)
		}
		// Fields used by formulas cannot be deleted.
		if _, err := svc.DeleteField(ctx, app, orders.ID, qty.ID); !isInvalid(err, "") {
			t.Errorf("delete used field: %v", err)
		}
		// A formula cycle is rejected.
		f := "[Big] & \"x\""
		if _, err := svc.UpdateField(ctx, app, orders.ID, orders.FieldByName("Total").ID, FieldPatch{Formula: &f}); err == nil {
			t.Errorf("cycle accepted")
		}
		// Changing a static formula's type rebuilds its column.
		f = "[Quantity] * 2"
		orders = must[*Table](t)(svc.UpdateField(ctx, app, orders.ID, orders.FieldByName("Big").ID, FieldPatch{Formula: &f}))
		got := must[map[string]any](t)(svc.GetRow(ctx, admin, app, orders.ID, lampID))
		if got["Big"] != 20.0 {
			t.Errorf("retyped static formula = %v", got["Big"])
		}

		// Converting text to number.
		item := orders.FieldByName("Item")
		num := TypeNumber
		if _, err := svc.UpdateField(ctx, app, orders.ID, item.ID, FieldPatch{Type: &num}); !isInvalid(err, "type") {
			t.Errorf("lossy conversion allowed: %v", err)
		}
		orders = must[*Table](t)(svc.UpdateField(ctx, app, orders.ID, item.ID, FieldPatch{Type: &num, Force: true}))
		// Converting text to a select collects the options.
		sel := TypeSingleSelect
		cust := must[*Table](t)(svc.UpdateField(ctx, app, customers.ID, customers.FieldByName("Name").ID, FieldPatch{Type: &sel}))
		if fmt.Sprint(cust.FieldByName("Name").Options) != "[Ada Bob]" {
			t.Errorf("options = %v", cust.FieldByName("Name").Options)
		}

		// Many links, and deleting linked rows.
		tags := must[*Table](t)(svc.CreateTable(ctx, app, "Labels", []FieldInput{{Name: "Label", Type: TypeText}}))
		l1 := must[map[string]any](t)(svc.CreateRow(ctx, admin, app, tags.ID, map[string]any{"Label": "eco"}))
		l2 := must[map[string]any](t)(svc.CreateRow(ctx, admin, app, tags.ID, map[string]any{"Label": "new"}))
		if _, _, err := svc.AddField(ctx, app, orders.ID, FieldInput{Name: "Labels", Type: TypeLink, LinkTable: tags.ID, LinkMany: true}); err != nil {
			t.Fatal(err)
		}
		r := must[map[string]any](t)(svc.UpdateRow(ctx, admin, app, orders.ID, lampID, map[string]any{"Labels": []any{l1["_id"], l2["_id"], l1["_id"]}}))
		if fmt.Sprint(r["Labels"]) != "[{"+fmt.Sprint(l1["_id"])+" eco} {"+fmt.Sprint(l2["_id"])+" new}]" {
			t.Errorf("labels = %v", r["Labels"])
		}
		if _, err := svc.UpdateRow(ctx, admin, app, orders.ID, lampID, map[string]any{"Labels": []any{999.0}}); !isInvalid(err, "Labels") {
			t.Errorf("missing link target: %v", err)
		}
		must[int](t)(svc.DeleteRows(ctx, admin, app, tags.ID, []int64{l1["_id"].(int64)}))
		must[int](t)(svc.DeleteRows(ctx, admin, app, customers.ID, []int64{bob["_id"].(int64)}))
		r = must[map[string]any](t)(svc.GetRow(ctx, admin, app, orders.ID, lampID))
		if len(r["Labels"].([]LinkRef)) != 1 || r["Customer"] != nil {
			t.Errorf("after deletes: labels %v customer %v", r["Labels"], r["Customer"])
		}
		if err := svc.DeleteTable(ctx, app, tags.ID); !isInvalid(err, "") {
			t.Errorf("deleted a linked table: %v", err)
		}

		// Permissions.
		basic := rbac.Actor{UserID: "usr_2", Role: rbac.Basic}
		public := rbac.Actor{Role: rbac.Public}
		if _, err := svc.ListRows(ctx, public, app, orders.ID, Query{}); !errors.Is(err, rbac.ErrForbidden) {
			t.Errorf("public read: %v", err)
		}
		power := rbac.Power
		must[*Table](t)(svc.UpdateTable(ctx, app, orders.ID, TablePatch{Permissions: &Permissions{Read: rbac.Basic, Write: power}}))
		if _, err := svc.ListRows(ctx, basic, app, orders.ID, Query{}); err != nil {
			t.Errorf("basic read: %v", err)
		}
		if _, err := svc.CreateRow(ctx, basic, app, orders.ID, map[string]any{}); !errors.Is(err, rbac.ErrForbidden) {
			t.Errorf("basic write: %v", err)
		}
	})
}

func isInvalid(err error, field string) bool {
	var v *ValidationError
	return errors.As(err, &v) && (field == "" || v.Field == field)
}
