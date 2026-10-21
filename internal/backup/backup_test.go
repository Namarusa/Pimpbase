package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pimpbase/pimpbase/internal/apps"
	"github.com/pimpbase/pimpbase/internal/store"
)

// TestBackupRestore backs up a live data folder and opens the copy.
func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := apps.NewService(s).Create(ctx, "Kept", "")
	if err != nil {
		t.Fatal(err)
	}
	db, _ := s.AppDB(ctx, a.ID)
	db.Exec(ctx, `CREATE TABLE notes (body TEXT)`)
	db.Exec(ctx, `INSERT INTO notes VALUES ('written before the backup')`)

	var buf bytes.Buffer
	n, err := Write(ctx, dir, &buf) // the store is still open, as on a running server
	if err != nil || n != 2 {
		t.Fatalf("write = %d, %v", n, err)
	}
	s.Close()
	zipPath := filepath.Join(t.TempDir(), "b.zip")
	os.WriteFile(zipPath, buf.Bytes(), 0o644)

	if _, err := Restore(zipPath, dir, false); err == nil {
		t.Error("restored over existing data without force")
	}
	target := t.TempDir()
	if n, err := Restore(zipPath, target, false); err != nil || n != 2 {
		t.Fatalf("restore = %d, %v", n, err)
	}
	s2, err := store.Open(ctx, target, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got, err := apps.NewService(s2).Get(ctx, a.ID); err != nil || got.Name != "Kept" {
		t.Fatalf("app after restore = %v, %v", got, err)
	}
	db2, _ := s2.AppDB(ctx, a.ID)
	var body string
	if err := db2.QueryRow(ctx, `SELECT body FROM notes`).Scan(&body); err != nil || body != "written before the backup" {
		t.Errorf("note = %q, %v", body, err)
	}
	if _, err := Restore(filepath.Join(target, "pimpbase.db"), t.TempDir(), false); err == nil {
		t.Error("restored a non-backup")
	}
	name, err := ToFile(ctx, target, filepath.Join(t.TempDir(), "backups"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); err != nil {
		t.Error(err)
	}
}
