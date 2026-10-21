// Package files stores the files uploaded to an app's attachment fields.
// By default they live in the app's own database, next to its tables, so
// they work the same on SQLite and Postgres, are backed up with the data
// and go away with the app. A server may keep the contents in a folder or
// a bucket instead (see Blobs); the database then holds the file's details
// and where its content is.
package files

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/store"
)

// MaxSize caps one uploaded file.
const MaxSize = 25 << 20

var ErrNotFound = errors.New("file not found")

// InvalidError messages are safe to show.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

// File describes a stored file. TableID is the table it was uploaded for:
// reading the file needs read access to that table.
type File struct {
	ID      string `json:"id"`
	TableID string `json:"-"`
	// UploadedBy is the id of the user who uploaded the file, if signed in.
	UploadedBy string    `json:"-"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Service struct {
	store *store.Store
	now   func() time.Time
	// Blobs, when set, receives the contents of new files. Files already
	// stored stay where they are and remain readable.
	Blobs Blobs
	// BeforeSave, when set, may refuse a new file of size bytes, such as
	// one over the app's quota.
	BeforeSave func(ctx context.Context, appID string, size int64) error
	// ready remembers the apps whose file table exists.
	ready sync.Map
}

func NewService(s *store.Store) *Service {
	return &Service{store: s, now: time.Now}
}

const tableName = "bb_files"

func (s *Service) db(ctx context.Context, appID string) (*store.DB, error) {
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	if _, ok := s.ready.Load(appID); ok {
		return db, nil
	}
	_, err = db.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+db.Table(tableName)+` (
		id TEXT PRIMARY KEY,
		table_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		size `+db.TypeInt()+` NOT NULL,
		content `+db.TypeBlob()+` NOT NULL,
		created_at TEXT NOT NULL,
		uploaded_by TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return nil, err
	}
	// Apps made before uploads were tied to their uploader, or before
	// contents could live elsewhere, lack these columns.
	for _, col := range []string{"uploaded_by", "storage"} {
		if _, err := db.Exec(ctx, `SELECT `+col+` FROM `+db.Table(tableName)+` WHERE 1 = 0`); err != nil {
			if _, err := db.Exec(ctx, `ALTER TABLE `+db.Table(tableName)+` ADD COLUMN `+col+` TEXT NOT NULL DEFAULT ''`); err != nil {
				return nil, err
			}
		}
	}
	s.ready.Store(appID, true)
	return db, nil
}

// Forget drops what the service remembers about a deleted app.
func (s *Service) Forget(appID string) { s.ready.Delete(appID) }

// Save stores a file uploaded for a table of an app.
func (s *Service) Save(ctx context.Context, appID, tableID, uploadedBy, name, declaredType string, r io.Reader) (*File, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, &InvalidError{fmt.Sprintf("a file can be at most %d MB", MaxSize>>20)}
	}
	if len(data) == 0 {
		return nil, &InvalidError{"the file is empty"}
	}
	if s.BeforeSave != nil {
		if err := s.BeforeSave(ctx, appID, int64(len(data))); err != nil {
			return nil, err
		}
	}
	f := &File{ID: ids.New("fil"), TableID: tableID, UploadedBy: uploadedBy, Name: cleanName(name), Size: int64(len(data)), CreatedAt: s.now().UTC().Truncate(time.Second)}
	f.Type = contentType(f.Name, declaredType, data)
	db, err := s.db(ctx, appID)
	if err != nil {
		return nil, err
	}
	storage, content := "", data
	if s.Blobs != nil {
		if err := s.Blobs.Put(ctx, blobKey(appID, f.ID), f.Type, data); err != nil {
			return nil, err
		}
		storage, content = s.Blobs.Name(), []byte{}
	}
	_, err = db.Exec(ctx, `INSERT INTO `+db.Table(tableName)+` (id, table_id, name, type, size, content, created_at, uploaded_by, storage) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.TableID, f.Name, f.Type, f.Size, content, f.CreatedAt.Format(time.RFC3339), f.UploadedBy, storage)
	if err != nil {
		if s.Blobs != nil {
			s.Blobs.Delete(context.WithoutCancel(ctx), blobKey(appID, f.ID))
		}
		return nil, err
	}
	return f, nil
}

// StorageName says where new file contents go: "database", "dir" or "s3".
func (s *Service) StorageName() string {
	if s.Blobs == nil {
		return "database"
	}
	return s.Blobs.Name()
}

func blobKey(appID, fileID string) string { return appID + "/" + fileID }

// List returns the details of every file of an app.
func (s *Service) List(ctx context.Context, appID string) ([]*File, error) {
	db, err := s.db(ctx, appID)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT id, table_id, name, type, size, created_at, uploaded_by FROM `+db.Table(tableName)+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f := &File{}
		var created string
		if err := rows.Scan(&f.ID, &f.TableID, &f.Name, &f.Type, &f.Size, &created, &f.UploadedBy); err != nil {
			return nil, err
		}
		f.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, f)
	}
	return out, rows.Err()
}

// Restore stores a file with the id and details it had elsewhere.
func (s *Service) Restore(ctx context.Context, appID string, f *File, data []byte) error {
	if !strings.HasPrefix(f.ID, "fil_") || len(data) > MaxSize {
		return &InvalidError{fmt.Sprintf("bad file %q", f.ID)}
	}
	db, err := s.db(ctx, appID)
	if err != nil {
		return err
	}
	storage, content := "", data
	if s.Blobs != nil {
		if err := s.Blobs.Put(ctx, blobKey(appID, f.ID), f.Type, data); err != nil {
			return err
		}
		storage, content = s.Blobs.Name(), []byte{}
	}
	_, err = db.Exec(ctx, `INSERT INTO `+db.Table(tableName)+` (id, table_id, name, type, size, content, created_at, uploaded_by, storage) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.TableID, cleanName(f.Name), f.Type, int64(len(data)), content, f.CreatedAt.UTC().Format(time.RFC3339), f.UploadedBy, storage)
	return err
}

// Usage is the number and total size of an app's files.
func (s *Service) Usage(ctx context.Context, appID string) (count int, bytes int64, err error) {
	db, err := s.db(ctx, appID)
	if err != nil {
		return 0, 0, err
	}
	err = db.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM `+db.Table(tableName)).Scan(&count, &bytes)
	return count, bytes, err
}

// DeleteApp removes the contents an app keeps outside its database; the
// database goes with the app itself.
func (s *Service) DeleteApp(ctx context.Context, appID string) error {
	defer s.Forget(appID)
	if s.Blobs == nil {
		return nil
	}
	db, err := s.db(ctx, appID)
	if err != nil {
		return err
	}
	rows, err := db.Query(ctx, `SELECT id FROM `+db.Table(tableName)+` WHERE storage = ?`, s.Blobs.Name())
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.Blobs.Delete(ctx, blobKey(appID, id)); err != nil {
			return err
		}
	}
	return nil
}

// Get returns a file and its content.
func (s *Service) Get(ctx context.Context, appID, id string) (*File, []byte, error) {
	db, err := s.db(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	f := &File{ID: id}
	var created, storage string
	var data []byte
	err = db.QueryRow(ctx, `SELECT table_id, name, type, size, content, created_at, uploaded_by, storage FROM `+db.Table(tableName)+` WHERE id = ?`, id).
		Scan(&f.TableID, &f.Name, &f.Type, &f.Size, &data, &created, &f.UploadedBy, &storage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if storage != "" {
		if s.Blobs == nil || s.Blobs.Name() != storage {
			return nil, nil, fmt.Errorf("file %s is kept in %s storage, which this server is not set up for", id, storage)
		}
		if data, err = s.Blobs.Get(ctx, blobKey(appID, id)); err != nil {
			return nil, nil, err
		}
	}
	return f, data, nil
}

// cleanName keeps the last path element of a file name, without control
// characters, and at most 200 bytes long.
func cleanName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == utf8.RuneError {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	for len(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	return name
}

// contentType trusts what the content looks like over what the browser
// said, and the file extension when the content says nothing.
func contentType(name, declared string, data []byte) string {
	sniffed := http.DetectContentType(data)
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return "application/pdf"
	}
	base, _, _ := mime.ParseMediaType(sniffed)
	if base != "application/octet-stream" && base != "text/plain" {
		return base
	}
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		if b, _, err := mime.ParseMediaType(t); err == nil {
			return b
		}
	}
	if d, _, err := mime.ParseMediaType(declared); err == nil && d != "" {
		return d
	}
	return base
}

// Inline reports whether browsers may show a file of this type in the page
// instead of downloading it. Types that can run scripts never are.
func Inline(typ string) bool {
	switch typ {
	case "application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "text/plain", "audio/mpeg", "audio/wav", "video/mp4", "video/webm":
		return true
	}
	return false
}
