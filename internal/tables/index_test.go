package tables

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// TestIndexedFields checks that indexing a field changes no answer: pages
// sorted by it, blanks last, and filters on it.
func TestIndexedFields(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		svc, app := setup(t, s)
		tb := must[*Table](t)(svc.CreateTable(ctx, app, "Items", []FieldInput{
			{Name: "Name", Type: TypeText},
			{Name: "Price", Type: TypeNumber},
			{Name: "Code", Type: TypeText},
			{Name: "Notes", Type: TypeLongText},
		}))
		for i := 0; i < 37; i++ {
			v := map[string]any{"Name": fmt.Sprintf("Item %02d", i)}
			if i%4 != 0 {
				v["Price"] = float64((i * 13) % 10)
				v["Code"] = []string{"b", "a", "C", "é", "B"}[i%5]
			}
			must[map[string]any](t)(svc.CreateRow(ctx, admin, app, tb.ID, v))
		}
		queries := []Query{}
		for _, sort := range []string{"Price", "Code"} {
			for _, desc := range []bool{false, true} {
				for _, off := range []int{0, 10, 25, 30, 36, 40} {
					queries = append(queries, Query{Sort: sort, Desc: desc, Limit: 7, Offset: off})
					queries = append(queries, Query{Sort: sort, Desc: desc, Limit: 7, Offset: off, Filter: `[Price] <> 3`})
				}
			}
		}
		queries = append(queries, Query{Filter: `[Code] = "a"`}, Query{Filter: `[Code] = ""`}, Query{Filter: `[Price] >= 4`}, Query{Filter: `[Price] < 4`})
		ids := func(q Query) []any {
			p := must[*Page](t)(svc.ListRows(ctx, admin, app, tb.ID, q))
			out := []any{p.Total}
			for _, r := range p.Rows {
				out = append(out, r["_id"])
			}
			return out
		}
		var before [][]any
		for _, q := range queries {
			before = append(before, ids(q))
		}
		for _, name := range []string{"Price", "Code"} {
			tb = must[*Table](t)(svc.UpdateField(ctx, app, tb.ID, tb.FieldByName(name).ID, FieldPatch{Indexed: ptr(true)}))
		}
		if !tb.FieldByName("Price").Indexed {
			t.Fatal("Price not indexed")
		}
		for i, q := range queries {
			if got := ids(q); !reflect.DeepEqual(got, before[i]) {
				t.Errorf("%+v: indexed %v, plain %v", q, got, before[i])
			}
		}
		if _, err := svc.UpdateField(ctx, app, tb.ID, tb.FieldByName("Notes").ID, FieldPatch{Indexed: ptr(true)}); !isInvalid(err, "indexed") {
			t.Errorf("long text indexed: %v", err)
		}
		// Retyping and deleting indexed fields drops their index with them.
		num := TypeNumber
		tb = must[*Table](t)(svc.UpdateField(ctx, app, tb.ID, tb.FieldByName("Code").ID, FieldPatch{Type: &num, Force: true}))
		if !tb.FieldByName("Code").Indexed {
			t.Error("retyped field lost its index")
		}
		must[*Table](t)(svc.DeleteField(ctx, app, tb.ID, tb.FieldByName("Code").ID))
		tb = must[*Table](t)(svc.UpdateField(ctx, app, tb.ID, tb.FieldByName("Price").ID, FieldPatch{Indexed: ptr(false)}))
		must[*Table](t)(svc.DeleteField(ctx, app, tb.ID, tb.FieldByName("Price").ID))
		// Single links are indexed from the start.
		other := must[*Table](t)(svc.CreateTable(ctx, app, "Other", []FieldInput{{Name: "Item", Type: TypeLink, LinkTable: tb.ID}}))
		if !other.FieldByName("Item").Indexed {
			t.Error("link not indexed")
		}
	})
}
