// Package files stores the files uploaded to an app's attachment fields.
// They live in the app's own database, next to its tables, so they work the
// same on SQLite and Postgres, are backed up with the data and go away with
// the app.
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

	"github.com/bubibase/bubibase/internal/ids"
	"github.com/bubibase/bubibase/internal/store"
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
	ID        string    `json:"id"`
	TableID   string    `json:"-"`
	// UploadedBy is the id of the user who uploaded the file, if signed in.
	UploadedBy string    `json:"-"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

type Service struct {
	store *store.Store
	now   func() time.Time
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
	// Apps made before uploads were tied to their uploader lack the column.
	if _, err := db.Exec(ctx, `SELECT uploaded_by FROM `+db.Table(tableName)+` WHERE 1 = 0`); err != nil {
		if _, err := db.Exec(ctx, `ALTER TABLE `+db.Table(tableName)+` ADD COLUMN uploaded_by TEXT NOT NULL DEFAULT ''`); err != nil {
			return nil, err
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
	f := &File{ID: ids.New("fil"), TableID: tableID, UploadedBy: uploadedBy, Name: cleanName(name), Size: int64(len(data)), CreatedAt: s.now().UTC().Truncate(time.Second)}
	f.Type = contentType(f.Name, declaredType, data)
	db, err := s.db(ctx, appID)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(ctx, `INSERT INTO `+db.Table(tableName)+` (id, table_id, name, type, size, content, created_at, uploaded_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.TableID, f.Name, f.Type, f.Size, data, f.CreatedAt.Format(time.RFC3339), f.UploadedBy)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Get returns a file and its content.
func (s *Service) Get(ctx context.Context, appID, id string) (*File, []byte, error) {
	db, err := s.db(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	f := &File{ID: id}
	var created string
	var data []byte
	err = db.QueryRow(ctx, `SELECT table_id, name, type, size, content, created_at, uploaded_by FROM `+db.Table(tableName)+` WHERE id = ?`, id).
		Scan(&f.TableID, &f.Name, &f.Type, &f.Size, &data, &created, &f.UploadedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, created)
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
