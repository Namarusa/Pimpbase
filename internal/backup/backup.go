// Package backup copies a SQLite data folder into one zip file while the
// server keeps running, and puts such a copy back.
//
// Each database is copied with VACUUM INTO, which reads a consistent
// snapshot without stopping writers. Postgres deployments back up with
// the database's own tools (pg_dump, or the provider's snapshots).
package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// manifestName is a short note inside every backup, which also marks the
// zip as one.
const manifestName = "pimpbase-backup.txt"

// Write copies every database in dataDir (pimpbase.db and apps/*.db) into
// a zip written to w. Files kept in a folder outside the databases are not
// included.
func Write(ctx context.Context, dataDir string, w io.Writer) (n int, err error) {
	paths := []string{filepath.Join(dataDir, "pimpbase.db")}
	apps, _ := filepath.Glob(filepath.Join(dataDir, "apps", "*.db"))
	sort.Strings(apps)
	paths = append(paths, apps...)
	if _, err := os.Stat(paths[0]); err != nil {
		return 0, fmt.Errorf("no Pimpbase data in %s", dataDir)
	}
	tmp, err := os.MkdirTemp("", "pimpbase-backup-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)
	zw := zip.NewWriter(w)
	for _, p := range paths {
		rel, _ := filepath.Rel(dataDir, p)
		snap := filepath.Join(tmp, strings.ReplaceAll(rel, string(filepath.Separator), "_"))
		if err := vacuumInto(ctx, p, snap); err != nil {
			return n, fmt.Errorf("copy %s: %w", rel, err)
		}
		if err := addFile(zw, filepath.ToSlash(rel), snap); err != nil {
			return n, err
		}
		os.Remove(snap)
		n++
	}
	mw, err := zw.Create(manifestName)
	if err != nil {
		return n, err
	}
	fmt.Fprintf(mw, "Pimpbase backup\ncreated: %s\ndatabases: %d\nrestore: pimpbase restore --data <empty folder> <this file>\n", time.Now().UTC().Format(time.RFC3339), n)
	return n, zw.Close()
}

func vacuumInto(ctx context.Context, src, dst string) error {
	db, err := sql.Open("sqlite", "file:"+src+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `VACUUM INTO ?`, dst)
	return err
}

func addFile(zw *zip.Writer, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// ToFile writes a backup to dir/pimpbase-<time>.zip and keeps the newest
// keep backups there (all of them when keep is 0).
func ToFile(ctx context.Context, dataDir, dir string, keep int) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Join(dir, "pimpbase-"+time.Now().UTC().Format("20060102-150405")+".zip")
	f, err := os.Create(name + ".part")
	if err != nil {
		return "", err
	}
	if _, err := Write(ctx, dataDir, f); err != nil {
		f.Close()
		os.Remove(name + ".part")
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(name+".part", name); err != nil {
		return "", err
	}
	if keep > 0 {
		old, _ := filepath.Glob(filepath.Join(dir, "pimpbase-*.zip"))
		sort.Strings(old)
		for len(old) > keep {
			os.Remove(old[0])
			old = old[1:]
		}
	}
	return name, nil
}

// Restore unpacks a backup into dataDir, which must hold no Pimpbase data
// unless force is set. The server must not be running on dataDir.
func Restore(zipPath, dataDir string, force bool) (int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	found := false
	for _, f := range zr.File {
		if f.Name == manifestName {
			found = true
		}
	}
	if !found {
		return 0, errors.New("this is not a Pimpbase backup")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "pimpbase.db")); err == nil && !force {
		return 0, fmt.Errorf("%s already holds Pimpbase data; restore into an empty folder, or pass --force to replace it", dataDir)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "apps"), 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, f := range zr.File {
		if f.Name == manifestName {
			continue
		}
		if f.Name != "pimpbase.db" && !(strings.HasPrefix(f.Name, "apps/") && strings.HasSuffix(f.Name, ".db") && !strings.Contains(f.Name[5:], "/")) {
			return n, fmt.Errorf("unexpected file %q in the backup", f.Name)
		}
		dst := filepath.Join(dataDir, filepath.FromSlash(f.Name))
		for _, suffix := range []string{"-wal", "-shm"} {
			os.Remove(dst + suffix)
		}
		if err := extract(f, dst); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func extract(f *zip.File, dst string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	out, err := os.Create(dst + ".part")
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(dst+".part", dst)
}
