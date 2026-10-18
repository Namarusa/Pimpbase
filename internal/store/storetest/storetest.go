// Package storetest opens throwaway stores for tests: always SQLite, and
// Postgres too when BUBIBASE_TEST_POSTGRES holds a DSN to a server where
// the user may create databases.
package storetest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bubibase/bubibase/internal/store"
)

// Each runs fn once per available database.
func Each(t *testing.T, fn func(t *testing.T, s *store.Store)) {
	t.Run("sqlite", func(t *testing.T) {
		s, err := store.Open(context.Background(), t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		fn(t, s)
	})
	dsn := os.Getenv("BUBIBASE_TEST_POSTGRES")
	if dsn == "" {
		return
	}
	t.Run("postgres", func(t *testing.T) {
		admin, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		name := fmt.Sprintf("bubibase_test_%d", time.Now().UnixNano())
		if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
			t.Fatal(err)
		}
		s, err := store.Open(context.Background(), "", withDB(dsn, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			s.Close()
			admin, _ := sql.Open("pgx", dsn)
			admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
			admin.Close()
		})
		fn(t, s)
	})
}

func withDB(dsn, name string) string {
	if strings.Contains(dsn, "://") {
		i := strings.LastIndex(dsn, "/")
		rest := ""
		if q := strings.Index(dsn[i:], "?"); q >= 0 {
			rest = dsn[i+q:]
		}
		return dsn[:i+1] + name + rest
	}
	var parts []string
	for _, p := range strings.Fields(dsn) {
		if !strings.HasPrefix(p, "dbname=") {
			parts = append(parts, p)
		}
	}
	return strings.Join(append(parts, "dbname="+name), " ")
}
