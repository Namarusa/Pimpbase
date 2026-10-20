package api

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/bundle"
)

// MaxImport is the largest app file an import accepts.
const MaxImport = 2 << 30

// exportApp sends an app as one .bubiapp file. ?data=1 adds rows and
// files; ?secrets=1, for admins only, keeps secret values.
func (s *Server) exportApp(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	opts := bundle.ExportOptions{Data: truthy(q.Get("data")), Secrets: truthy(q.Get("secrets"))}
	if opts.Secrets {
		if _, err := s.requireAdmin(r); err != nil {
			return errStatus(403, "only workspace owners and admins export secrets")
		}
	}
	// Write to a temporary file first, so a failure still answers in JSON.
	tmp, err := os.CreateTemp("", "bubibase-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := s.Bundles.Export(r.Context(), a.ID, opts, tmp); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+a.Slug+bundle.Extension+`"`)
	http.ServeContent(w, r, "", time.Time{}, tmp)
	return nil
}

// importApp reads a .bubiapp file sent as the request body. Without
// {app} it makes a new app (?name=, ?slug=); with it, it updates that
// app in place (?prune=1). ?dryRun=1 only answers the plan, and
// ?enableFlows=1 keeps flows switched on.
func (s *Server) importApp(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireBuilder(r); err != nil {
		return err
	}
	q := r.URL.Query()
	opts := bundle.ImportOptions{
		Name: q.Get("name"), Slug: q.Get("slug"),
		DryRun: truthy(q.Get("dryRun")), Prune: truthy(q.Get("prune")), EnableFlows: truthy(q.Get("enableFlows")),
	}
	if id := r.PathValue("app"); id != "" {
		a, err := s.Apps.Get(r.Context(), id)
		if err != nil {
			return err
		}
		opts.Target = a.ID
	}
	tmp, err := os.CreateTemp("", "bubibase-import-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	r.Body = http.MaxBytesReader(w, r.Body, MaxImport)
	var tooBig *http.MaxBytesError
	src := io.Reader(r.Body)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		f, _, err := r.FormFile("file")
		if errors.As(err, &tooBig) {
			return errStatus(413, "the app file is larger than 2 GB")
		} else if err != nil {
			return errStatus(400, "send the app file as the field \"file\"")
		}
		defer f.Close()
		src = f
	}
	n, err := io.Copy(tmp, src)
	if errors.As(err, &tooBig) {
		return errStatus(413, "the app file is larger than 2 GB")
	} else if err != nil {
		return err
	}
	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		return errStatus(400, "this is not a Bubibase app file")
	}
	plan, err := s.Bundles.Import(r.Context(), zr, opts)
	if err != nil {
		return err
	}
	status := 200
	if plan.Applied && plan.Mode == "new" {
		status = 201
	}
	return writeJSON(w, status, plan)
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
