package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteURI(t *testing.T) {
	for in, want := range map[string]string{
		"data/pimpbase.db":    "file:data/pimpbase.db",
		"/srv/a b/x.db":       "file:/srv/a b/x.db",
		"/srv/100%#1?/x.db":   "file:/srv/100%25%231%3f/x.db",
		"//server/share/x.db": "file:////server/share/x.db",
	} {
		if got := SQLiteURI(in); got != want {
			t.Errorf("SQLiteURI(%q) = %q, want %q", in, got, want)
		}
	}
}

// A data folder whose name needs escaping in a URI still opens, and the
// databases land in it rather than somewhere else.
func TestOpenOddFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Data #1 100%")
	s, err := Open(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Stat(filepath.Join(dir, "pimpbase.db")); err != nil {
		t.Fatal(err)
	}
}
