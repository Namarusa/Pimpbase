package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bubibase/bubibase/internal/files"
	"github.com/bubibase/bubibase/internal/tables"
)

// uploadFile stores a file for a table's attachment fields. The row keeps
// the answer's id, name, type and size once it is saved.
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	t, err := s.Tables.Get(r.Context(), a.ID, r.PathValue("table"))
	if err != nil {
		return err
	}
	if err := act.Require(t.Permissions.Write, "adding files to "+t.Name); err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, files.MaxSize+1<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		if strings.Contains(err.Error(), "too large") {
			return errStatus(413, fmt.Sprintf("a file can be at most %d MB", files.MaxSize>>20))
		}
		return errStatus(400, "attach the file as the form field named file")
	}
	defer f.Close()
	saved, err := s.Files.Save(r.Context(), a.ID, t.ID, act.UserID, hdr.Filename, hdr.Header.Get("Content-Type"), f)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, map[string]any{"id": saved.ID, "name": saved.Name, "type": saved.Type, "size": saved.Size, "url": tables.FileURL(a.ID, saved.ID)})
}

// getFile sends a stored file to whoever may read the table it belongs to
// and, when a row rule applies to them, a row of it that holds the file. A
// file not yet saved to any row is open to its uploader only. PDFs and
// pictures open in the browser; ?download=1 saves them instead.
func (s *Server) getFile(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	f, data, err := s.Files.Get(r.Context(), a.ID, r.PathValue("file"))
	if err != nil {
		return err
	}
	t, err := s.Tables.Get(r.Context(), a.ID, f.TableID)
	if err != nil {
		return files.ErrNotFound // the table went away with its files
	}
	access, err := s.Tables.FileAccess(r.Context(), act, a.ID, t.ID, f.ID)
	if err != nil {
		return err
	}
	if !access.Readable && (access.Held || act.UserID == "" || act.UserID != f.UploadedBy) {
		return files.ErrNotFound // like rows outside a row rule, the file does not exist for them
	}
	disposition := "inline"
	typ := f.Type
	if !files.Inline(typ) || r.URL.Query().Get("download") != "" {
		disposition = "attachment"
	}
	if !files.Inline(typ) {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, asciiName(f.Name), url.PathEscape(f.Name)))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	http.ServeContent(w, r, "", f.CreatedAt, bytes.NewReader(data))
	return nil
}

func asciiName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 32 || r > 126 {
			return '_'
		}
		return r
	}, name)
}
