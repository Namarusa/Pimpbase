package rbac

import (
	"slices"
	"testing"
)

func TestChainAndHas(t *testing.T) {
	set := NewSet([]Def{
		{ID: "SALES", Name: "Sales", Inherits: Basic},
		{ID: "LEADS", Name: "Leads", Inherits: "SALES"},
		{ID: "AUDIT", Name: "Audit", Inherits: Public},
	})
	if got := set.Chain("LEADS"); !slices.Equal(got, []Role{"LEADS", "SALES", Basic, Public}) {
		t.Errorf("chain = %v", got)
	}
	if got := set.Chain(Admin); !slices.Equal(got, []Role{Admin, Power, Basic, Public, "SALES", "LEADS", "AUDIT"}) {
		t.Errorf("admin chain = %v", got)
	}
	leads := NewActor(set, "LEADS")
	for r, want := range map[Role]bool{"LEADS": true, "SALES": true, Basic: true, Public: true, Power: false, "AUDIT": false, "GHOST": false} {
		if leads.Has(r) != want {
			t.Errorf("LEADS has %s = %v", r, !want)
		}
	}
	if !NewActor(set, Admin).Has("AUDIT") || !(Actor{Builder: true}).Has("GHOST") {
		t.Error("admins and builders hold every role")
	}
	if (Actor{}).Has(Public) {
		t.Error("an empty actor holds nothing")
	}
	if !leads.HasAny(nil) || leads.HasAny([]Role{Power, "AUDIT"}) || !leads.HasAny([]Role{Power, "SALES"}) {
		t.Error("HasAny")
	}
	// Roles without a set fall back to the built-in ladder.
	if !(Actor{Role: Power}).Has(Basic) || (Actor{Role: Basic}).Has(Power) {
		t.Error("built-in ladder")
	}
	if set.CheckInherits("SALES", "LEADS") == nil || set.CheckInherits("SALES", Admin) == nil || set.CheckInherits("X", "GHOST") == nil {
		t.Error("CheckInherits lets bad parents through")
	}
	if err := set.CheckInherits("SALES", Power); err != nil {
		t.Error(err)
	}
	if r, err := ParseRole(" basic "); r != Basic || err != nil {
		t.Errorf("ParseRole = %v, %v", r, err)
	}
	if _, err := ParseRole("no spaces"); err == nil {
		t.Error("ParseRole accepts spaces")
	}
}
