package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/pimpbase/pimpbase/internal/extdb"
	"github.com/pimpbase/pimpbase/internal/exttables"
	"github.com/pimpbase/pimpbase/internal/queries"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/tables"
)

// External databases: connector kinds, connection tests, schema browsing,
// imported tables and their rows.

// envHeader asks for the development connection settings of external
// tables; only builders get them.
const envHeader = "X-Pimpbase-Env"

func (s *Server) dataEnv(r *http.Request, act rbac.Actor) string {
	if r.Header.Get(envHeader) == queries.Development && act.Builder {
		return queries.Development
	}
	return queries.Production
}

func (s *Server) connectors(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireBuilder(r); err != nil {
		return err
	}
	out := []map[string]any{}
	for _, d := range extdb.Kinds() {
		out = append(out, map[string]any{"kind": d.Kind(), "label": d.Label(), "port": d.DefaultPort()})
	}
	return writeJSON(w, 200, out)
}

// testConnection tries a database data source's settings, saved or not.
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Kind   string         `json:"kind"`
		Config queries.Config `json:"config"`
		Env    string         `json:"env"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ds := &queries.Datasource{Name: "Test", Kind: in.Kind, Config: in.Config}
	res, err := s.Runner.TestConnection(r.Context(), a.ID, ds, in.Env)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, res)
}

// datasourceSchema lists the tables of a database, marking those imported.
func (s *Server) datasourceSchema(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	ts, err := s.Ext.Schema(r.Context(), a.ID, r.PathValue("ds"))
	if err != nil {
		return err
	}
	imported, err := s.Ext.List(r.Context(), a.ID)
	if err != nil {
		return err
	}
	type entry struct {
		extdb.TableInfo
		Imported string `json:"imported,omitempty"`
	}
	out := make([]entry, 0, len(ts))
	for _, t := range ts {
		e := entry{TableInfo: t}
		for _, x := range imported {
			if x.DatasourceID == r.PathValue("ds") && x.Source == t.Name && (x.Schema == t.Schema || x.Schema == "") {
				e.Imported = x.ID
			}
		}
		out = append(out, e)
	}
	return writeJSON(w, 200, out)
}

func (s *Server) describeTable(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	info, err := s.Ext.Describe(r.Context(), a.ID, r.PathValue("ds"), q.Get("schema"), q.Get("table"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, info)
}

func (s *Server) importTable(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Schema string `json:"schema"`
		Source string `json:"source"`
		Name   string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	t, err := s.Ext.Import(r.Context(), a.ID, r.PathValue("ds"), in.Schema, in.Source, in.Name)
	if err != nil {
		return err
	}
	v, err := s.Ext.View(r.Context(), t)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, v)
}

// extViews lists the external tables an actor may read.
func (s *Server) extViews(r *http.Request, appID string, act rbac.Actor) ([]*exttables.View, error) {
	ts, err := s.Ext.List(r.Context(), appID)
	if err != nil {
		return nil, err
	}
	readable := []*exttables.Table{}
	for _, t := range ts {
		if act.Has(t.Permissions.Read) {
			readable = append(readable, t)
		}
	}
	return s.Ext.Views(r.Context(), appID, readable)
}

// extTable handles the table and row endpoints for an external table.
func (s *Server) extTable(w http.ResponseWriter, r *http.Request, op string) error {
	ctx := r.Context()
	id := r.PathValue("table")
	switch op {
	case "get":
		a, act, err := s.actor(r)
		if err != nil {
			return err
		}
		t, err := s.Ext.Get(ctx, a.ID, id)
		if err != nil {
			return err
		}
		if err := act.Require(t.Permissions.Read, "reading "+t.Name); err != nil {
			return err
		}
		v, err := s.Ext.View(ctx, t)
		if err != nil {
			return err
		}
		return writeJSON(w, 200, v)
	case "update", "delete", "refresh":
		a, err := s.builderApp(r)
		if err != nil {
			return err
		}
		var t *exttables.Table
		switch op {
		case "delete":
			if err := s.Ext.Delete(ctx, a.ID, id); err != nil {
				return err
			}
			return writeJSON(w, 200, map[string]any{"ok": true})
		case "refresh":
			if t, err = s.Ext.Refresh(ctx, a.ID, id); err != nil {
				return err
			}
		default:
			var p exttables.Patch
			if err := readJSON(r, &p); err != nil {
				return err
			}
			if perm := p.Permissions; perm != nil {
				roles := []rbac.Role{perm.Read, perm.Write, perm.Delete}
				for _, rule := range perm.Rows {
					roles = append(roles, rule.Role)
				}
				if err := s.requireRoles(r, a.ID, roles...); err != nil {
					return err
				}
			}
			if t, err = s.Ext.Update(ctx, a.ID, id, p); err != nil {
				return err
			}
		}
		v, err := s.Ext.View(ctx, t)
		if err != nil {
			return err
		}
		return writeJSON(w, 200, v)
	}

	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	env := s.dataEnv(r, act)
	switch op {
	case "list":
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		page, err := s.Ext.ListRows(ctx, act, env, a.ID, id, tables.Query{
			Filter: q.Get("filter"), Sort: q.Get("sort"), Desc: q.Get("desc") == "true" || q.Get("desc") == "1",
			Limit: limit, Offset: offset,
		})
		if err != nil {
			return err
		}
		return writeJSON(w, 200, page)
	case "row":
		row, err := s.Ext.GetRow(ctx, act, env, a.ID, id, r.PathValue("id"))
		if err != nil {
			return err
		}
		return writeJSON(w, 200, row)
	case "create", "updateRow":
		var vals map[string]any
		if err := readJSON(r, &vals); err != nil {
			return err
		}
		if op == "create" {
			row, err := s.Ext.CreateRow(ctx, act, env, a.ID, id, vals)
			if err != nil {
				return err
			}
			return writeJSON(w, 201, row)
		}
		row, err := s.Ext.UpdateRow(ctx, act, env, a.ID, id, r.PathValue("id"), vals)
		if err != nil {
			return err
		}
		return writeJSON(w, 200, row)
	case "import":
		r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
		var body io.Reader = r.Body
		if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "multipart/form-data" {
			f, _, err := r.FormFile("file")
			if err != nil {
				return errStatus(400, "attach the CSV as the form field named file")
			}
			defer f.Close()
			body = f
		}
		res, err := s.Ext.ImportCSV(ctx, act, env, a.ID, id, body)
		if err != nil {
			return err
		}
		return writeJSON(w, 200, res)
	case "export":
		t, err := s.Ext.Get(ctx, a.ID, id)
		if err != nil {
			return err
		}
		filter := r.URL.Query().Get("filter")
		// Check the filter and access before streaming, so errors still
		// come back as JSON.
		if _, err := s.Ext.ListRows(ctx, act, env, a.ID, id, tables.Query{Filter: filter, Limit: 1}); err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, fileName(t.Name)))
		if err := s.Ext.ExportCSV(ctx, act, env, a.ID, id, filter, w); err != nil {
			s.Log.Error("export failed", "err", err)
		}
		return nil
	case "deleteRow", "deleteRows":
		var keys []string
		if op == "deleteRow" {
			keys = []string{r.PathValue("id")}
		} else {
			var in struct {
				IDs []any `json:"ids"`
			}
			if err := readJSON(r, &in); err != nil {
				return err
			}
			for _, k := range in.IDs {
				if n, ok := k.(json.Number); ok {
					keys = append(keys, n.String())
				} else {
					keys = append(keys, fmt.Sprint(k))
				}
			}
		}
		n, err := s.Ext.DeleteRows(ctx, act, env, a.ID, id, keys)
		if err != nil {
			return err
		}
		if op == "deleteRow" && n == 0 {
			return errStatus(404, "row not found")
		}
		return writeJSON(w, 200, map[string]any{"deleted": n})
	}
	return errStatus(400, "an external table's columns come from its database; change them there and refresh the table")
}

// ext routes a table endpoint to extTable when the table is external.
func (s *Server) ext(op string, fn func(w http.ResponseWriter, r *http.Request) error) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		if exttables.Is(r.PathValue("table")) {
			return s.extTable(w, r, op)
		}
		return fn(w, r)
	}
}

// fileName makes a table name safe in a Content-Disposition header.
func fileName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 32 {
			return '_'
		}
		return r
	}, name)
}
