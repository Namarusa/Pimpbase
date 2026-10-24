package integrations

import "testing"

func TestBuiltinManifests(t *testing.T) {
	c, err := LoadCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"stripe", "slack", "github", "twilio", "airtable", "google_sheets", "s3"}
	for _, id := range want {
		m := c.Get(id)
		if m == nil {
			t.Fatalf("%s is missing", id)
		}
		for _, a := range m.Actions {
			if a.Handler != "" && handlers[a.Handler] == nil {
				t.Errorf("%s.%s names the missing handler %s", id, a.Key, a.Handler)
			}
		}
	}
	if len(c.List()) != len(want) {
		t.Errorf("%d manifests", len(c.List()))
	}
}
