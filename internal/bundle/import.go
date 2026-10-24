package bundle

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/apps"
	"github.com/pimpbase/pimpbase/internal/files"
	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/tables"
)

// ImportOptions say what an import does.
type ImportOptions struct {
	// Target is the app to update, by id or URL name. Empty makes a new
	// app.
	Target string
	// Name and Slug name a new app; empty keeps the file's.
	Name string
	Slug string
	// DryRun works out the plan and changes nothing.
	DryRun bool
	// Prune, when updating, deletes tables, fields, queries, data sources
	// and flows the file does not have. Without it they are kept.
	Prune bool
	// EnableFlows keeps flows switched on that were on in the file. New
	// flows are imported switched off otherwise, so a copy never sends a
	// schedule's emails twice.
	EnableFlows bool
}

// Plan is what an import does, or did.
type Plan struct {
	// Mode is "new" or "update".
	Mode string `json:"mode"`
	App  struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"app"`
	Manifest *Manifest `json:"manifest"`
	Changes  []Change  `json:"changes"`
	Warnings []string  `json:"warnings"`
	// Applied is false for a dry run.
	Applied bool `json:"applied"`
	// Rows and Files count what was copied with a new app.
	Rows  int `json:"rows"`
	Files int `json:"files"`
}

// Change is one line of a plan.
type Change struct {
	// Kind is table, field, screen, role, datasource, query, variable,
	// flow or design.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Action is add, change, remove or keep.
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

func (p *Plan) add(kind, name, action, detail string) {
	p.Changes = append(p.Changes, Change{kind, name, action, detail})
}

func (p *Plan) warn(format string, args ...any) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...))
}

// file is an opened bundle.
type file struct {
	zr       *zip.Reader
	manifest *Manifest
	appJSON  string
}

func open(zr *zip.Reader) (*file, error) {
	f := &file{zr: zr}
	var m Manifest
	if err := f.decode("manifest.json", &m); err != nil {
		return nil, err
	}
	if m.Format != Format {
		return nil, errNotAZip
	}
	if m.Version > Version {
		return nil, &Error{fmt.Sprintf("this file was made by a newer Pimpbase (format %d); update this server first", m.Version)}
	}
	f.manifest = &m
	raw, err := f.read("app.json", maxAppJSON)
	if err != nil {
		return nil, err
	}
	f.appJSON = string(raw)
	return f, nil
}

func (f *file) entry(name string) *zip.File {
	for _, e := range f.zr.File {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func (f *file) read(name string, limit int64) ([]byte, error) {
	e := f.entry(name)
	if e == nil {
		return nil, errNotAZip
	}
	r, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &Error{name + " is too large"}
	}
	return data, nil
}

func (f *file) decode(name string, v any) error {
	data, err := f.read(name, maxAppJSON)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return &Error{"cannot read " + name + ": " + err.Error()}
	}
	return nil
}

// lines calls fn with each JSON line of an entry, if the entry exists.
func (f *file) lines(name string, fn func([]byte) error) error {
	e := f.entry(name)
	if e == nil {
		return nil
	}
	r, err := e.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		if err := fn(sc.Bytes()); err != nil {
			return err
		}
	}
	return sc.Err()
}

var idRe = regexp.MustCompile(`^(app|tbl|scr|ds|qry|flw|ext|eml)_[a-z2-7]{16}$`)

// packageIDs lists the ids a package brings, from its rows' "id" columns.
func packageIDs(p *Package) []string {
	var out []string
	add := func(rows []Row) {
		for _, r := range rows {
			if id := str(r["id"]); idRe.MatchString(id) {
				out = append(out, id)
			}
		}
	}
	add([]Row{p.App})
	add(p.Tables)
	add(p.Screens)
	add(p.Datasources)
	add(p.Queries)
	add(p.Flows)
	add(p.ExtTables)
	add(p.EmailTemplates)
	return out
}

// taken reports which ids already exist on this server.
func (s *Service) taken(ctx context.Context, list []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, t := range []string{"apps", "app_tables", "app_screens", "app_datasources", "app_queries", "app_flows", "app_ext_tables", "email_templates"} {
		for start := 0; start < len(list); start += 500 {
			chunk := list[start:min(start+500, len(list))]
			args := make([]any, len(chunk))
			for i, id := range chunk {
				args[i] = id
			}
			rows, err := s.Store.Meta.Query(ctx, `SELECT id FROM `+t+` WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?, ", len(chunk)), ", ")+`)`, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var id string
				rows.Scan(&id)
				out[id] = true
			}
			rows.Close()
		}
	}
	return out, nil
}

func fresh(id string) string {
	prefix, _, _ := strings.Cut(id, "_")
	return ids.New(prefix)
}

// remap replaces ids throughout the package's JSON. Ids are random, so
// replacing their text cannot touch anything else.
func remap(text string, m map[string]string) string {
	if len(m) == 0 {
		return text
	}
	pairs := make([]string, 0, 2*len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k != m[k] {
			pairs = append(pairs, k, m[k])
		}
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

func (s *Service) decodePackage(text string) (*Package, error) {
	var p Package
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&p); err != nil {
		return nil, &Error{"cannot read app.json: " + err.Error()}
	}
	if p.App == nil || !idRe.MatchString(str(p.App["id"])) {
		return nil, &Error{"app.json names no app"}
	}
	return &p, nil
}

// Import reads a file and creates or updates an app.
func (s *Service) Import(ctx context.Context, zr *zip.Reader, opts ImportOptions) (*Plan, error) {
	f, err := open(zr)
	if err != nil {
		return nil, err
	}
	p, err := s.decodePackage(f.appJSON)
	if err != nil {
		return nil, err
	}
	if opts.Target != "" {
		return s.update(ctx, f, p, opts)
	}
	return s.create(ctx, f, p, opts)
}

func definition(r Row) (*tables.Table, error) {
	var t tables.Table
	if err := json.Unmarshal([]byte(str(r["definition"])), &t); err != nil {
		return nil, &Error{"cannot read the table " + str(r["name"]) + ": " + err.Error()}
	}
	return &t, nil
}

// create makes a new app from a file. Ids are kept unless one is taken on
// this server, as when copying an app next to itself; then all are new.
func (s *Service) create(ctx context.Context, f *file, p *Package, opts ImportOptions) (*Plan, error) {
	plan := &Plan{Mode: "new", Manifest: f.manifest, Changes: []Change{}, Warnings: []string{}}
	all := packageIDs(p)
	taken, err := s.taken(ctx, all)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if len(taken) > 0 {
		for _, id := range all {
			m[id] = fresh(id)
		}
		plan.warn("This server already has some of these ids, so the copy gets new ones; files exported from it later will not update the original.")
		if p, err = s.decodePackage(remap(f.appJSON, m)); err != nil {
			return nil, err
		}
	}
	appID := str(p.App["id"])
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = str(p.App["name"])
	}
	slug := opts.Slug
	if slug == "" && strings.TrimSpace(opts.Name) != "" {
		slug = apps.Slugify(name)
	} else if slug == "" {
		slug = str(p.App["slug"])
	}
	if slug, err = s.freeSlug(ctx, slug, opts.Slug != ""); err != nil {
		return nil, err
	}
	plan.App.ID, plan.App.Name, plan.App.Slug = appID, name, slug
	for _, r := range p.Roles {
		plan.add("role", str(r["name"]), "add", "")
	}
	for _, r := range p.Tables {
		plan.add("table", str(r["name"]), "add", "")
	}
	for _, r := range p.Screens {
		plan.add("screen", str(r["name"]), "add", str(r["route"]))
	}
	for _, r := range p.Datasources {
		plan.add("datasource", str(r["name"]), "add", "")
	}
	for _, r := range p.Queries {
		plan.add("query", str(r["name"]), "add", "")
	}
	for _, r := range p.Variables {
		detail := ""
		if toInt(r["secret"]) != 0 && str(r["prod_value"]) == "" {
			detail = "secret: set its value after importing"
		}
		plan.add("variable", str(r["name"]), "add", detail)
	}
	for _, r := range p.Flows {
		detail := "switched off"
		if opts.EnableFlows && toInt(r["enabled"]) != 0 {
			detail = "switched on"
		}
		plan.add("flow", str(r["name"]), "add", detail+"; webhooks get a new address")
	}
	for _, r := range p.EmailTemplates {
		plan.add("email template", str(r["name"]), "add", "")
	}
	if f.manifest.Data {
		plan.Rows, plan.Files = f.manifest.Rows, f.manifest.Files
	}
	if opts.DryRun {
		return plan, nil
	}

	now := s.now().UTC().Format(time.RFC3339)
	if _, err := s.Store.Meta.Exec(ctx, `INSERT INTO apps (id, slug, name, design, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		appID, slug, name, str(p.App["design"]), now, now); err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			s.Apps.Delete(context.WithoutCancel(ctx), appID)
		}
	}()
	if _, err := s.Store.AppDB(ctx, appID); err != nil {
		return nil, err
	}
	if err := s.insertMeta(ctx, appID, p, opts, nil); err != nil {
		return nil, err
	}
	defs := map[string]*tables.Table{}
	var restoring []*tables.Table
	for _, r := range p.Tables {
		t, err := definition(r)
		if err != nil {
			return nil, err
		}
		defs[t.ID] = t
		restoring = append(restoring, t)
	}
	for _, t := range restoring {
		if err := s.Tables.Restore(ctx, appID, t, restoring...); err != nil {
			return nil, fmt.Errorf("table %s: %w", t.Name, err)
		}
	}
	if f.manifest.Data {
		if err := s.loadData(ctx, f, appID, defs, m); err != nil {
			return nil, err
		}
	}
	s.Flows.Changed(appID)
	ok = true
	plan.Applied = true
	return plan, nil
}

// freeSlug returns slug, or slug-2, slug-3… when it is taken and was not
// asked for by name.
func (s *Service) freeSlug(ctx context.Context, slug string, explicit bool) (string, error) {
	if slug == "" {
		slug = "app"
	}
	base := slug
	for i := 2; i < 100; i++ {
		var n int
		if err := s.Store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM apps WHERE slug = ?`, slug).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return slug, nil
		}
		if explicit {
			return "", &Error{fmt.Sprintf("the URL name %q is taken", slug)}
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	return "", &Error{"no free URL name for the app"}
}

// insertMeta writes the roles, screens, data sources, queries, variables
// and flows of a new app. keep lists flows (by id) whose enabled state
// and webhook address come from an existing app.
func (s *Service) insertMeta(ctx context.Context, appID string, p *Package, opts ImportOptions, keep map[string]Row) error {
	tx, err := s.Store.Meta.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ins := func(table string, cols []string, r Row, extra map[string]any) error {
		names := append([]string{"app_id"}, cols...)
		args := []any{appID}
		for _, c := range cols {
			args = append(args, plain(r[c]))
		}
		for k, v := range extra {
			names = append(names, k)
			args = append(args, v)
		}
		_, err := tx.Exec(ctx, `INSERT INTO `+table+` (`+strings.Join(names, ", ")+`) VALUES (`+strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ")+`)`, args...)
		return err
	}
	for _, r := range p.Roles {
		if err := ins(roleCols.table, roleCols.cols, r, nil); err != nil {
			return err
		}
	}
	for _, r := range p.Screens {
		if err := ins(screenCols.table, screenCols.cols, r, nil); err != nil {
			return err
		}
	}
	for _, r := range p.Datasources {
		if err := ins(dsCols.table, dsCols.cols, r, nil); err != nil {
			return err
		}
	}
	for _, r := range p.Queries {
		if err := ins(queryCols.table, queryCols.cols, r, nil); err != nil {
			return err
		}
	}
	for _, r := range p.Variables {
		if err := ins(varCols.table, varCols.cols, r, nil); err != nil {
			return err
		}
	}
	for _, r := range p.ExtTables {
		if err := ins(extCols.table, extCols.cols, r, nil); err != nil {
			return err
		}
	}
	for _, r := range p.Flows {
		r = clone(r)
		if !opts.EnableFlows {
			r["enabled"] = int64(0)
		}
		if err := ins(flowCols.table, flowCols.cols, r, map[string]any{"hook_token": ids.Secret("hook")}); err != nil {
			return err
		}
	}
	for _, r := range p.EmailTemplates {
		if err := ins(emailCols.table, emailCols.cols, r, nil); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO email_template_versions (template_id, version, definition, saved_at, saved_by) VALUES (?, ?, ?, ?, ?)`,
			r["id"], plain(r["version"]), r["definition"], r["updated_at"], r["updated_by"]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func clone(r Row) Row {
	out := Row{}
	for k, v := range r {
		out[k] = v
	}
	return out
}

// plain turns JSON numbers back into Go numbers for the database.
func plain(v any) any {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return i
		}
		f, _ := n.Float64()
		return f
	}
	return v
}

// loadData copies the rows, links and files of a file into a new app.
func (s *Service) loadData(ctx context.Context, f *file, appID string, defs map[string]*tables.Table, m map[string]string) error {
	for oldID := range tablesInFile(f) {
		newID := oldID
		if v, ok := m[oldID]; ok {
			newID = v
		}
		t := defs[newID]
		if t == nil {
			return &Error{"the file holds rows of a table it does not describe"}
		}
		var batch []tables.RawRow
		flush := func() error {
			err := s.Tables.LoadRows(ctx, appID, t, batch)
			batch = batch[:0]
			return err
		}
		err := f.lines("data/"+oldID+".jsonl", func(line []byte) error {
			var r tables.RawRow
			if err := json.Unmarshal(line, &r); err != nil {
				return &Error{"cannot read a row of " + t.Name + ": " + err.Error()}
			}
			batch = append(batch, r)
			if len(batch) >= rowsInBatch {
				return flush()
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := flush(); err != nil {
			return err
		}
		for _, fld := range t.Fields {
			if fld.Type != tables.TypeLink || !fld.LinkMany {
				continue
			}
			var pairs []tables.JoinRow
			err := f.lines("links/"+oldID+"/"+fld.ID+".jsonl", func(line []byte) error {
				var j tables.JoinRow
				if err := json.Unmarshal(line, &j); err != nil {
					return err
				}
				pairs = append(pairs, j)
				return nil
			})
			if err != nil {
				return err
			}
			if err := s.Tables.LoadLinks(ctx, appID, fld, pairs); err != nil {
				return err
			}
		}
	}
	type entry struct {
		files.File
		TableID    string `json:"tableId"`
		UploadedBy string `json:"uploadedBy"`
	}
	return f.lines("files/index.jsonl", func(line []byte) error {
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		data, err := f.read("files/"+e.ID, files.MaxSize)
		if err != nil {
			return err
		}
		fl := e.File
		fl.TableID, fl.UploadedBy = remap(e.TableID, m), e.UploadedBy
		return s.Files.Restore(ctx, appID, &fl, data)
	})
}

// tablesInFile lists the table ids of the file's data entries.
func tablesInFile(f *file) map[string]bool {
	out := map[string]bool{}
	for _, e := range f.zr.File {
		if id, ok := strings.CutPrefix(e.Name, "data/"); ok {
			out[strings.TrimSuffix(id, ".jsonl")] = true
		}
	}
	return out
}

// update brings an existing app to the file's version, keeping its rows,
// its members, its variables' values and its flows' on/off state and
// webhook addresses. Things are matched by id, then by name.
func (s *Service) update(ctx context.Context, f *file, p *Package, opts ImportOptions) (*Plan, error) {
	target, err := s.Apps.Get(ctx, opts.Target)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Mode: "update", Manifest: f.manifest, Changes: []Change{}, Warnings: []string{}}
	plan.App.ID, plan.App.Name, plan.App.Slug = target.ID, target.Name, target.Slug
	if f.manifest.Data {
		plan.warn("The file holds rows and files; updating an app never touches its data, so they are left out.")
	}

	cur := map[string][]Row{}
	for _, mt := range []metaTable{tableCols, screenCols, dsCols, queryCols, flowCols, roleCols, varCols, extCols, emailCols} {
		if cur[mt.table], err = s.readRows(ctx, mt, "app_id = ?", target.ID); err != nil {
			return nil, err
		}
	}
	// Match the file's things to the app's, by id then by name.
	m := map[string]string{str(p.App["id"]): target.ID}
	matched := map[string]Row{} // the app's row, by its id
	match := func(pkg []Row, have []Row, key string) {
		for _, r := range pkg {
			id := str(r["id"])
			for _, h := range have {
				if str(h["id"]) == id {
					m[id], matched[id] = id, h
					break
				}
			}
			if _, ok := m[id]; ok {
				continue
			}
			for _, h := range have {
				if strings.EqualFold(str(h[key]), str(r[key])) && matched[str(h["id"])] == nil {
					m[id], matched[str(h["id"])] = str(h["id"]), h
					break
				}
			}
		}
	}
	match(p.Tables, cur["app_tables"], "name")
	match(p.Screens, cur["app_screens"], "route")
	match(p.Datasources, cur["app_datasources"], "name")
	match(p.Queries, cur["app_queries"], "name")
	match(p.Flows, cur["app_flows"], "name")
	match(p.ExtTables, cur["app_ext_tables"], "name")
	match(p.EmailTemplates, cur["email_templates"], "name")
	// Things the app lacks keep their ids unless another app has them.
	var rest []string
	for _, id := range packageIDs(p) {
		if _, ok := m[id]; !ok {
			rest = append(rest, id)
		}
	}
	taken, err := s.taken(ctx, rest)
	if err != nil {
		return nil, err
	}
	for _, id := range rest {
		m[id] = id
		if taken[id] {
			m[id] = fresh(id)
		}
	}
	if p, err = s.decodePackage(remap(f.appJSON, m)); err != nil {
		return nil, err
	}
	byID := func(rows []Row) map[string]Row {
		out := map[string]Row{}
		for _, r := range rows {
			out[str(r["id"])] = r
		}
		return out
	}

	// Roles.
	haveRoles := map[string]Row{}
	for _, r := range cur["app_roles"] {
		haveRoles[str(r["id"])] = r
	}
	for _, r := range p.Roles {
		h, ok := haveRoles[str(r["id"])]
		switch {
		case !ok:
			plan.add("role", str(r["name"]), "add", "")
		case differs(h, r, "name", "description", "inherits", "color", "position"):
			plan.add("role", str(r["name"]), "change", "")
		}
	}
	// Tables and fields.
	haveTables := map[string]*tables.Table{}
	for _, r := range cur["app_tables"] {
		t, err := definition(r)
		if err != nil {
			return nil, err
		}
		haveTables[t.ID] = t
	}
	type tableWork struct {
		pkg, have *tables.Table
	}
	var newTables, changed []tableWork
	inFile := map[string]bool{}
	for _, r := range p.Tables {
		t, err := definition(r)
		if err != nil {
			return nil, err
		}
		inFile[t.ID] = true
		h := haveTables[t.ID]
		if h == nil {
			plan.add("table", t.Name, "add", "")
			newTables = append(newTables, tableWork{t, nil})
			continue
		}
		changed = append(changed, tableWork{t, h})
		planFields(plan, t, h, opts.Prune)
	}
	for id, h := range haveTables {
		if !inFile[id] {
			if opts.Prune {
				plan.add("table", h.Name, "remove", "deletes its rows")
			} else {
				plan.add("table", h.Name, "keep", "not in the file")
			}
		}
	}
	// Screens are replaced whole.
	havePkgScreen := map[string]bool{}
	for _, r := range p.Screens {
		havePkgScreen[str(r["id"])] = true
		action := "add"
		if h := matched[str(r["id"])]; h != nil {
			action = "change"
			if !differs(r, h, "name", "route", "role", "position") && sameJSON(str(r["definition"]), str(h["definition"])) {
				action = "keep"
			}
		}
		plan.add("screen", str(r["name"]), action, str(r["route"]))
	}
	for _, r := range cur["app_screens"] {
		if !havePkgScreen[str(r["id"])] {
			plan.add("screen", str(r["name"]), "remove", str(r["route"]))
		}
	}
	planRows := func(kind string, pkg, have []Row, cols ...string) {
		hb := byID(have)
		in := map[string]bool{}
		for _, r := range pkg {
			in[str(r["id"])] = true
			h, ok := hb[str(r["id"])]
			switch {
			case !ok:
				detail := ""
				if kind == "flow" {
					detail = "switched off"
				}
				plan.add(kind, str(r["name"]), "add", detail)
			case differs(h, r, cols...):
				plan.add(kind, str(r["name"]), "change", "")
			}
		}
		for _, h := range have {
			if !in[str(h["id"])] {
				if opts.Prune {
					plan.add(kind, str(h["name"]), "remove", "")
				} else {
					plan.add(kind, str(h["name"]), "keep", "not in the file")
				}
			}
		}
	}
	planRows("datasource", p.Datasources, cur["app_datasources"], "name", "kind", "config")
	planRows("query", p.Queries, cur["app_queries"], "name", "datasource_id", "definition")
	planRows("flow", p.Flows, cur["app_flows"], "name", "definition")
	planRows("external table", p.ExtTables, cur["app_ext_tables"], "name", "datasource_id", "definition")
	planRows("email template", p.EmailTemplates, cur["email_templates"], "name", "definition")
	haveVars := map[string]bool{}
	for _, r := range cur["app_variables"] {
		haveVars[str(r["name"])] = true
	}
	for _, r := range p.Variables {
		if !haveVars[str(r["name"])] {
			plan.add("variable", str(r["name"]), "add", "")
		}
	}
	if !sameJSON(str(p.App["design"]), designOf(target)) {
		plan.add("design", "Theme and navigation", "change", "")
	}
	if opts.DryRun {
		return plan, nil
	}

	// Apply, in an order where everything a step needs exists.
	if err := s.upsertRoles(ctx, target.ID, p.Roles, haveRoles); err != nil {
		return plan, err
	}
	var adding []*tables.Table
	for _, w := range newTables {
		adding = append(adding, w.pkg)
	}
	for _, w := range newTables {
		if err := s.Tables.Restore(ctx, target.ID, w.pkg, adding...); err != nil {
			return plan, fmt.Errorf("table %s: %w", w.pkg.Name, err)
		}
	}
	for _, w := range changed {
		s.applyFields(ctx, plan, target.ID, w.pkg, w.have, opts.Prune)
	}
	if opts.Prune {
		for id, h := range haveTables {
			if !inFile[id] {
				if err := s.Tables.DeleteTable(ctx, target.ID, id); err != nil {
					plan.warn("table %s was kept: %v", h.Name, err)
				}
			}
		}
	}
	if err := s.applyMeta(ctx, plan, target.ID, p, cur, opts); err != nil {
		return plan, err
	}
	s.Flows.Changed(target.ID)
	plan.Applied = true
	return plan, nil
}

func designOf(a *apps.App) string {
	b, _ := json.Marshal(a.Design)
	return string(b)
}

// sameJSON compares two JSON texts by meaning, not spelling.
func sameJSON(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}

func differs(a, b Row, cols ...string) bool {
	for _, c := range cols {
		if fmt.Sprint(plain(a[c])) != fmt.Sprint(plain(b[c])) {
			return true
		}
	}
	return false
}

// fieldMatch pairs the file's fields with the app's, by id then by name.
func fieldMatch(pkg, have *tables.Table) map[string]*tables.Field {
	out := map[string]*tables.Field{}
	used := map[string]bool{}
	for _, f := range pkg.Fields {
		if h := have.FieldByID(f.ID); h != nil {
			out[f.ID], used[h.ID] = h, true
		}
	}
	for _, f := range pkg.Fields {
		if out[f.ID] != nil {
			continue
		}
		if h := have.FieldByName(f.Name); h != nil && !used[h.ID] {
			out[f.ID], used[h.ID] = h, true
		}
	}
	return out
}

// fieldPatch works out what changes a field; skipped says what cannot.
func fieldPatch(f, h *tables.Field) (p tables.FieldPatch, changes []string, skipped string) {
	if f.Name != h.Name {
		p.Name = &f.Name
		changes = append(changes, "renamed from "+h.Name)
	}
	if f.Type != h.Type {
		if f.LinkTable != "" || h.LinkTable != "" || !scalar(f.Type) || !scalar(h.Type) {
			return tables.FieldPatch{}, nil, fmt.Sprintf("cannot change from %s to %s; add a new field instead", h.Type, f.Type)
		}
		t := f.Type
		p.Type = &t
		changes = append(changes, fmt.Sprintf("type %s → %s", h.Type, f.Type))
	}
	if f.LinkTable != h.LinkTable || f.LinkMany != h.LinkMany {
		return tables.FieldPatch{}, nil, "links to another table or another number of rows; add a new field instead"
	}
	if (f.Type == tables.TypeSingleSelect || f.Type == tables.TypeMultiSelect) && !slices.Equal(f.Options, h.Options) {
		p.Options = append([]string{}, f.Options...)
		if p.Options == nil {
			p.Options = []string{}
		}
		changes = append(changes, "options")
	}
	if f.Type == tables.TypeFormula {
		if f.Formula != h.Formula {
			p.Formula = &f.Formula
			changes = append(changes, "formula")
		}
		if f.FormulaMode != h.FormulaMode {
			p.FormulaMode = &f.FormulaMode
			changes = append(changes, "formula mode")
		}
	}
	if f.Required != h.Required {
		r := f.Required
		p.Required = &r
		changes = append(changes, map[bool]string{true: "now required", false: "now optional"}[r])
	}
	if f.Indexed != h.Indexed {
		i := f.Indexed
		p.Indexed = &i
		changes = append(changes, map[bool]string{true: "indexed", false: "index removed"}[i])
	}
	return p, changes, ""
}

func scalar(t tables.FieldType) bool {
	switch t {
	case tables.TypeText, tables.TypeLongText, tables.TypeNumber, tables.TypeBoolean, tables.TypeDate, tables.TypeSingleSelect:
		return true
	}
	return false
}

func planFields(plan *Plan, pkg, have *tables.Table, prune bool) {
	if pkg.Name != have.Name {
		plan.add("table", pkg.Name, "change", "renamed from "+have.Name)
	}
	pm, _ := json.Marshal(pkg.Permissions)
	hm, _ := json.Marshal(have.Permissions)
	if string(pm) != string(hm) {
		plan.add("table", pkg.Name, "change", "permissions or row rules")
	}
	pairs := fieldMatch(pkg, have)
	used := map[string]bool{}
	for _, f := range pkg.Fields {
		h := pairs[f.ID]
		label := pkg.Name + "." + f.Name
		if h == nil {
			plan.add("field", label, "add", string(f.Type))
			continue
		}
		used[h.ID] = true
		_, changes, skipped := fieldPatch(f, h)
		switch {
		case skipped != "":
			plan.add("field", label, "keep", skipped)
			plan.warn("%s %s", label, skipped)
		case len(changes) > 0:
			plan.add("field", label, "change", strings.Join(changes, ", "))
		}
	}
	for _, h := range have.Fields {
		if !used[h.ID] {
			if prune {
				plan.add("field", pkg.Name+"."+h.Name, "remove", "deletes its values")
			} else {
				plan.add("field", pkg.Name+"."+h.Name, "keep", "not in the file")
			}
		}
	}
}

// applyFields changes one table. Formula fields come last, once the fields
// they read have their new names. Changes that fail become warnings.
func (s *Service) applyFields(ctx context.Context, plan *Plan, appID string, pkg, have *tables.Table, prune bool) {
	pairs := fieldMatch(pkg, have)
	for _, formulas := range []bool{false, true} {
		for _, f := range pkg.Fields {
			if (f.Type == tables.TypeFormula) != formulas {
				continue
			}
			label := pkg.Name + "." + f.Name
			h := pairs[f.ID]
			if h == nil {
				in := tables.FieldInput{Name: f.Name, Type: f.Type, Required: f.Required, Options: f.Options, LinkTable: f.LinkTable, LinkMany: f.LinkMany, Formula: f.Formula, FormulaMode: f.FormulaMode, Indexed: f.Indexed}
				if _, _, err := s.Tables.AddField(ctx, appID, have.ID, in); err != nil {
					if in.Required {
						in.Required = false
						if _, _, err2 := s.Tables.AddField(ctx, appID, have.ID, in); err2 == nil {
							plan.warn("%s was added as optional: %v", label, err)
							continue
						}
					}
					plan.warn("%s was not added: %v", label, err)
				}
				continue
			}
			patch, changes, skipped := fieldPatch(f, h)
			if skipped != "" || len(changes) == 0 {
				continue
			}
			if _, err := s.Tables.UpdateField(ctx, appID, have.ID, h.ID, patch); err != nil {
				plan.warn("%s was left as it was: %v", label, err)
			}
		}
	}
	if prune {
		used := map[string]bool{}
		for _, h := range pairs {
			used[h.ID] = true
		}
		for _, h := range have.Fields {
			if !used[h.ID] {
				if _, err := s.Tables.DeleteField(ctx, appID, have.ID, h.ID); err != nil {
					plan.warn("%s.%s was kept: %v", pkg.Name, h.Name, err)
				}
			}
		}
	}
	// Name, row label and permissions.
	now, err := s.Tables.Get(ctx, appID, have.ID)
	if err != nil {
		plan.warn("table %s: %v", pkg.Name, err)
		return
	}
	var patch tables.TablePatch
	if pkg.Name != now.Name {
		patch.Name = &pkg.Name
	}
	if pf := pkg.FieldByID(pkg.PrimaryField); pf != nil {
		if nf := now.FieldByName(pf.Name); nf != nil && nf.ID != now.PrimaryField {
			patch.PrimaryField = &nf.ID
		}
	}
	perm := pkg.Permissions
	patch.Permissions = &perm
	if _, err := s.Tables.UpdateTable(ctx, appID, have.ID, patch); err != nil {
		plan.warn("table %s keeps its name and permissions: %v", pkg.Name, err)
	}
}

func (s *Service) upsertRoles(ctx context.Context, appID string, roles []Row, have map[string]Row) error {
	for _, r := range roles {
		var err error
		if _, ok := have[str(r["id"])]; ok {
			_, err = s.Store.Meta.Exec(ctx, `UPDATE app_roles SET name = ?, description = ?, inherits = ?, color = ?, position = ? WHERE app_id = ? AND id = ?`,
				r["name"], r["description"], r["inherits"], r["color"], plain(r["position"]), appID, r["id"])
		} else {
			_, err = s.Store.Meta.Exec(ctx, `INSERT INTO app_roles (app_id, id, name, description, inherits, color, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				appID, r["id"], r["name"], r["description"], r["inherits"], r["color"], plain(r["position"]))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// applyMeta replaces the screens and design, and updates data sources,
// queries, variables and flows.
func (s *Service) applyMeta(ctx context.Context, plan *Plan, appID string, p *Package, cur map[string][]Row, opts ImportOptions) error {
	tx, err := s.Store.Meta.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().UTC().Format(time.RFC3339Nano)
	exec := func(q string, args ...any) error {
		_, err := tx.Exec(ctx, q, args...)
		return err
	}
	if err := exec(`DELETE FROM app_screens WHERE app_id = ?`, appID); err != nil {
		return err
	}
	for _, r := range p.Screens {
		if err := exec(`INSERT INTO app_screens (app_id, id, name, route, role, position, definition, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			appID, r["id"], r["name"], r["route"], r["role"], plain(r["position"]), r["definition"], r["created_at"], now); err != nil {
			return err
		}
	}
	haveDS := map[string]Row{}
	for _, r := range cur["app_datasources"] {
		haveDS[str(r["id"])] = r
	}
	for _, r := range p.Datasources {
		if h, ok := haveDS[str(r["id"])]; ok {
			config := keepAuth(str(r["config"]), str(h["config"]))
			if err := exec(`UPDATE app_datasources SET name = ?, kind = ?, config = ?, updated_at = ? WHERE app_id = ? AND id = ?`, r["name"], r["kind"], config, now, appID, r["id"]); err != nil {
				return err
			}
		} else if err := exec(`INSERT INTO app_datasources (app_id, id, name, kind, config, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, appID, r["id"], r["name"], r["kind"], r["config"], now, now); err != nil {
			return err
		}
	}
	haveQ := map[string]bool{}
	for _, r := range cur["app_queries"] {
		haveQ[str(r["id"])] = true
	}
	for _, r := range p.Queries {
		if haveQ[str(r["id"])] {
			if err := exec(`UPDATE app_queries SET datasource_id = ?, name = ?, definition = ?, updated_at = ? WHERE app_id = ? AND id = ?`, r["datasource_id"], r["name"], r["definition"], now, appID, r["id"]); err != nil {
				return err
			}
		} else if err := exec(`INSERT INTO app_queries (app_id, id, datasource_id, name, definition, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, appID, r["id"], r["datasource_id"], r["name"], r["definition"], now, now); err != nil {
			return err
		}
	}
	haveExt := map[string]bool{}
	for _, r := range cur["app_ext_tables"] {
		haveExt[str(r["id"])] = true
	}
	for _, r := range p.ExtTables {
		if haveExt[str(r["id"])] {
			if err := exec(`UPDATE app_ext_tables SET datasource_id = ?, name = ?, definition = ?, updated_at = ? WHERE app_id = ? AND id = ?`, r["datasource_id"], r["name"], r["definition"], now, appID, r["id"]); err != nil {
				return err
			}
		} else if err := exec(`INSERT INTO app_ext_tables (app_id, id, datasource_id, name, definition, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, appID, r["id"], r["datasource_id"], r["name"], r["definition"], now, now); err != nil {
			return err
		}
	}
	haveVars := map[string]bool{}
	for _, r := range cur["app_variables"] {
		haveVars[str(r["name"])] = true
	}
	for _, r := range p.Variables {
		if !haveVars[str(r["name"])] {
			if err := exec(`INSERT INTO app_variables (app_id, name, secret, dev_value, prod_value) VALUES (?, ?, ?, ?, ?)`, appID, r["name"], plain(r["secret"]), r["dev_value"], r["prod_value"]); err != nil {
				return err
			}
		}
	}
	haveF := map[string]bool{}
	for _, r := range cur["app_flows"] {
		haveF[str(r["id"])] = true
	}
	for _, r := range p.Flows {
		if haveF[str(r["id"])] {
			if err := exec(`UPDATE app_flows SET name = ?, definition = ?, updated_at = ? WHERE app_id = ? AND id = ?`, r["name"], r["definition"], now, appID, r["id"]); err != nil {
				return err
			}
			continue
		}
		enabled := int64(0)
		if opts.EnableFlows {
			enabled = toInt(plain(r["enabled"]))
		}
		if err := exec(`INSERT INTO app_flows (app_id, id, name, enabled, definition, hook_token, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, appID, r["id"], r["name"], enabled, r["definition"], ids.Secret("hook"), now, now); err != nil {
			return err
		}
	}
	haveT := map[string]Row{}
	for _, r := range cur["email_templates"] {
		haveT[str(r["id"])] = r
	}
	for _, r := range p.EmailTemplates {
		h, ok := haveT[str(r["id"])]
		if !ok {
			if err := exec(`INSERT INTO email_templates (app_id, id, name, definition, version, created_at, updated_at, updated_by) VALUES (?, ?, ?, ?, 1, ?, ?, ?)`, appID, r["id"], r["name"], r["definition"], now, now, r["updated_by"]); err != nil {
				return err
			}
			if err := exec(`INSERT INTO email_template_versions (template_id, version, definition, saved_at, saved_by) VALUES (?, 1, ?, ?, ?)`, r["id"], r["definition"], now, r["updated_by"]); err != nil {
				return err
			}
			continue
		}
		version := toInt(plain(h["version"]))
		if str(h["definition"]) != str(r["definition"]) {
			version++
			if err := exec(`INSERT INTO email_template_versions (template_id, version, definition, saved_at, saved_by) VALUES (?, ?, ?, ?, ?)`, r["id"], version, r["definition"], now, "app import"); err != nil {
				return err
			}
		}
		if err := exec(`UPDATE email_templates SET name = ?, definition = ?, version = ?, updated_at = ? WHERE app_id = ? AND id = ?`, r["name"], r["definition"], version, now, appID, r["id"]); err != nil {
			return err
		}
	}
	if opts.Prune {
		inFile := map[string]bool{}
		for _, list := range [][]Row{p.Datasources, p.Queries, p.Flows, p.ExtTables, p.EmailTemplates} {
			for _, r := range list {
				inFile[str(r["id"])] = true
			}
		}
		for _, h := range cur["email_templates"] {
			if !inFile[str(h["id"])] {
				if err := exec(`DELETE FROM email_template_versions WHERE template_id = ?`, h["id"]); err != nil {
					return err
				}
				if err := exec(`DELETE FROM email_templates WHERE app_id = ? AND id = ?`, appID, h["id"]); err != nil {
					return err
				}
			}
		}
		for table, rows := range map[string][]Row{"app_queries": cur["app_queries"], "app_flows": cur["app_flows"], "app_ext_tables": cur["app_ext_tables"]} {
			for _, h := range rows {
				if !inFile[str(h["id"])] {
					if err := exec(`DELETE FROM `+table+` WHERE app_id = ? AND id = ?`, appID, h["id"]); err != nil {
						return err
					}
				}
			}
		}
		for _, h := range cur["app_datasources"] {
			if !inFile[str(h["id"])] {
				if err := exec(`DELETE FROM app_queries WHERE app_id = ? AND datasource_id = ?`, appID, h["id"]); err != nil {
					return err
				}
				if err := exec(`DELETE FROM app_ext_tables WHERE app_id = ? AND datasource_id = ?`, appID, h["id"]); err != nil {
					return err
				}
				if err := exec(`DELETE FROM app_datasources WHERE app_id = ? AND id = ?`, appID, h["id"]); err != nil {
					return err
				}
			}
		}
	}
	if err := exec(`UPDATE apps SET design = ?, updated_at = ? WHERE id = ?`, p.App["design"], s.now().UTC().Format(time.RFC3339), appID); err != nil {
		return err
	}
	return tx.Commit()
}

// keepAuth keeps the app's passwords and tokens where the file's are
// blank, as they are in files exported without secrets.
func keepAuth(config, have string) string {
	var c, h map[string]any
	if json.Unmarshal([]byte(config), &c) != nil || json.Unmarshal([]byte(have), &h) != nil {
		return config
	}
	for _, p := range secretPaths {
		cp, _ := c[p[0]].(map[string]any)
		hp, _ := h[p[0]].(map[string]any)
		if cp == nil || hp == nil || (p[0] == "auth" && cp["type"] != hp["type"]) {
			continue
		}
		if v, _ := cp[p[1]].(string); v == "" {
			if old, ok := hp[p[1]].(string); ok {
				cp[p[1]] = old
			}
		}
	}
	b, _ := json.Marshal(c)
	return string(b)
}

var _ = errors.New
