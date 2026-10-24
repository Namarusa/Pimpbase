// Package bundle moves an app between Pimpbase servers as one file: its
// tables, screens, roles, API data sources and queries, variables and
// flows, and optionally its rows and files. Importing either creates a new
// app or updates an existing one in place, keeping that app's rows, as a
// development server hands a new version to production.
//
// The file is a zip:
//
//	manifest.json           what the file holds
//	app.json                the app's settings as stored
//	data/<table id>.jsonl   rows, one JSON object per line (with data only)
//	links/<table id>/<field id>.jsonl   many-link pairs
//	files/index.jsonl       file details; files/<file id> their contents
package bundle

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/apps"
	"github.com/pimpbase/pimpbase/internal/files"
	"github.com/pimpbase/pimpbase/internal/flows"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/tables"
)

const (
	Format  = "pimpbase-app"
	Version = 1
	// Extension is the file name ending the builder offers.
	Extension = ".bubiapp"
)

// Manifest says what a file holds.
type Manifest struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	// Server is the version of the Pimpbase that wrote the file.
	Server string `json:"server"`
	App    struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"app"`
	Data    bool `json:"data"`
	Secrets bool `json:"secrets"`
	Tables  int  `json:"tables"`
	Rows    int  `json:"rows"`
	Files   int  `json:"files"`
}

// Row is a row of a settings table, by column.
type Row map[string]any

// Package is app.json: the app's settings rows, as stored.
type Package struct {
	App         Row   `json:"app"`
	Roles       []Row `json:"roles"`
	Tables      []Row `json:"tables"`
	Screens     []Row `json:"screens"`
	Datasources []Row `json:"datasources"`
	Queries     []Row `json:"queries"`
	Variables   []Row `json:"variables"`
	Flows       []Row `json:"flows"`
	// ExtTables are tables read from a database data source; their rows
	// stay in that database.
	ExtTables []Row `json:"extTables,omitempty"`
	// EmailTemplates are the templates Send email steps use; only their
	// newest version travels.
	EmailTemplates []Row `json:"emailTemplates,omitempty"`
}

// metaTable names a settings table and the columns a bundle carries.
type metaTable struct {
	table string
	cols  []string
}

var (
	appCols     = metaTable{"apps", []string{"id", "slug", "name", "design"}}
	roleCols    = metaTable{"app_roles", []string{"id", "name", "description", "inherits", "color", "position"}}
	tableCols   = metaTable{"app_tables", []string{"id", "name", "definition", "created_at", "updated_at"}}
	screenCols  = metaTable{"app_screens", []string{"id", "name", "route", "role", "position", "definition", "created_at", "updated_at"}}
	dsCols      = metaTable{"app_datasources", []string{"id", "name", "kind", "config", "created_at", "updated_at"}}
	queryCols   = metaTable{"app_queries", []string{"id", "datasource_id", "name", "definition", "created_at", "updated_at"}}
	varCols     = metaTable{"app_variables", []string{"name", "secret", "dev_value", "prod_value"}}
	flowCols    = metaTable{"app_flows", []string{"id", "name", "enabled", "definition", "created_at", "updated_at"}}
	extCols     = metaTable{"app_ext_tables", []string{"id", "datasource_id", "name", "definition", "created_at", "updated_at"}}
	emailCols   = metaTable{"email_templates", []string{"id", "name", "definition", "version", "created_at", "updated_at", "updated_by"}}
	orderBy     = map[string]string{"app_roles": "position, id", "app_screens": "position, id", "app_variables": "name"}
	errNotAZip  = &Error{"this is not a Pimpbase app file"}
	maxAppJSON  = int64(64 << 20)
	rowsInBatch = 2000
)

// Error messages are safe to show.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// Service exports and imports apps.
type Service struct {
	Store  *store.Store
	Apps   *apps.Service
	Tables *tables.Service
	Files  *files.Service
	Flows  *flows.Service
	// Server is this server's version, written into exports.
	Server string
	now    func() time.Time
}

func NewService(st *store.Store, as *apps.Service, ts *tables.Service, fs *files.Service, fl *flows.Service) *Service {
	return &Service{Store: st, Apps: as, Tables: ts, Files: fs, Flows: fl, now: time.Now}
}

func (s *Service) readRows(ctx context.Context, m metaTable, where string, args ...any) ([]Row, error) {
	q := "SELECT " + strings.Join(m.cols, ", ") + " FROM " + m.table + " WHERE " + where
	if o := orderBy[m.table]; o != "" {
		q += " ORDER BY " + o
	} else if m.cols[0] == "id" {
		q += " ORDER BY id"
	}
	rows, err := s.Store.Meta.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		raw := make([]any, len(m.cols))
		ptrs := make([]any, len(m.cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		r := Row{}
		for i, c := range m.cols {
			if b, ok := raw[i].([]byte); ok {
				raw[i] = string(b)
			}
			r[c] = raw[i]
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExportOptions choose what goes into a file.
type ExportOptions struct {
	// Data adds every row and file.
	Data bool
	// Secrets keeps the values of secret variables and the passwords and
	// tokens written into data sources; otherwise they are left blank.
	Secrets bool
}

// Export writes an app as one file.
func (s *Service) Export(ctx context.Context, appID string, opts ExportOptions, w io.Writer) (*Manifest, error) {
	a, err := s.Apps.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	var p Package
	list, err := s.readRows(ctx, appCols, "id = ?", a.ID)
	if err != nil {
		return nil, err
	}
	p.App = list[0]
	for _, part := range []struct {
		dst *[]Row
		m   metaTable
	}{{&p.Roles, roleCols}, {&p.Tables, tableCols}, {&p.Screens, screenCols}, {&p.Datasources, dsCols}, {&p.Queries, queryCols}, {&p.Variables, varCols}, {&p.Flows, flowCols}, {&p.ExtTables, extCols}, {&p.EmailTemplates, emailCols}} {
		if *part.dst, err = s.readRows(ctx, part.m, "app_id = ?", a.ID); err != nil {
			return nil, err
		}
	}
	if !opts.Secrets {
		for _, v := range p.Variables {
			if toInt(v["secret"]) != 0 {
				v["dev_value"], v["prod_value"] = "", ""
			}
		}
		for _, d := range p.Datasources {
			d["config"] = blankAuth(str(d["config"]))
		}
	}
	m := &Manifest{Format: Format, Version: Version, ExportedAt: s.now().UTC().Truncate(time.Second), Server: s.Server, Data: opts.Data, Secrets: opts.Secrets, Tables: len(p.Tables)}
	m.App.ID, m.App.Name, m.App.Slug = a.ID, a.Name, a.Slug

	zw := zip.NewWriter(w)
	if err := writeJSON(zw, "app.json", p); err != nil {
		return nil, err
	}
	if opts.Data {
		ts, err := s.Tables.List(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			n, err := s.exportRows(ctx, zw, a.ID, t)
			if err != nil {
				return nil, err
			}
			m.Rows += n
		}
		if m.Files, err = s.exportFiles(ctx, zw, a.ID); err != nil {
			return nil, err
		}
	}
	if err := writeJSON(zw, "manifest.json", m); err != nil {
		return nil, err
	}
	return m, zw.Close()
}

func (s *Service) exportRows(ctx context.Context, zw *zip.Writer, appID string, t *tables.Table) (int, error) {
	w, err := zw.Create("data/" + t.ID + ".jsonl")
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	n := 0
	err = s.Tables.DumpRows(ctx, appID, t, func(r tables.RawRow) error {
		n++
		return enc.Encode(r)
	})
	if err != nil {
		return 0, err
	}
	if err := bw.Flush(); err != nil {
		return 0, err
	}
	for _, f := range t.Fields {
		if f.Type != tables.TypeLink || !f.LinkMany {
			continue
		}
		w, err := zw.Create("links/" + t.ID + "/" + f.ID + ".jsonl")
		if err != nil {
			return 0, err
		}
		bw := bufio.NewWriter(w)
		enc := json.NewEncoder(bw)
		if err := s.Tables.DumpLinks(ctx, appID, f, func(j tables.JoinRow) error { return enc.Encode(j) }); err != nil {
			return 0, err
		}
		if err := bw.Flush(); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (s *Service) exportFiles(ctx context.Context, zw *zip.Writer, appID string) (int, error) {
	list, err := s.Files.List(ctx, appID)
	if err != nil {
		return 0, err
	}
	idx, err := zw.Create("files/index.jsonl")
	if err != nil {
		return 0, err
	}
	enc := json.NewEncoder(idx)
	type entry struct {
		files.File
		TableID    string `json:"tableId"`
		UploadedBy string `json:"uploadedBy"`
	}
	for _, f := range list {
		if err := enc.Encode(entry{*f, f.TableID, f.UploadedBy}); err != nil {
			return 0, err
		}
	}
	for _, f := range list {
		_, data, err := s.Files.Get(ctx, appID, f.ID)
		if err != nil {
			return 0, err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "files/" + f.ID, Method: zip.Store})
		if err != nil {
			return 0, err
		}
		if _, err := w.Write(data); err != nil {
			return 0, err
		}
	}
	return len(list), nil
}

func writeJSON(zw *zip.Writer, name string, v any) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// secretPaths are the data source settings that may hold a password:
// a REST API's sign-in and a database's password and connection string.
var secretPaths = [][2]string{{"auth", "password"}, {"auth", "token"}, {"auth", "value"}, {"db", "password"}, {"db", "uri"}}

// blankAuth clears passwords and tokens written straight into a data
// source; bindings such as {{ env.TOKEN }} stay, as they hold no secret.
func blankAuth(config string) string {
	var c map[string]any
	if json.Unmarshal([]byte(config), &c) != nil {
		return config
	}
	for _, p := range secretPaths {
		part, _ := c[p[0]].(map[string]any)
		if v, ok := part[p[1]].(string); ok && v != "" && !strings.Contains(v, "{{") {
			part[p[1]] = ""
		}
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func toInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case bool:
		if x {
			return 1
		}
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}
