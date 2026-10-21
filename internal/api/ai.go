package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/bubibase/bubibase/internal/ai"
	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/exttables"
	"github.com/bubibase/bubibase/internal/flows"
	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/queries"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/screens"
	"github.com/bubibase/bubibase/internal/tables"
)

// The AI assistant. Every route needs a builder, and its settings an
// admin; API keys never leave the server, and published apps cannot reach
// these routes.

func (s *Server) aiStatus(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireBuilder(r); err != nil {
		return err
	}
	st, err := s.AI.Status(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, 200, st)
}

func (s *Server) aiUpdate(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	var in ai.Update
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if err := s.AI.Update(r.Context(), in); err != nil {
		return err
	}
	return s.aiStatus(w, r)
}

// aiTest checks a provider's settings with a tiny request.
func (s *Server) aiTest(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	var in struct {
		Provider string `json:"provider"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := s.AI.Test(r.Context(), in.Provider)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, c)
}

// aiApp describes an app to the model: its structure, never its data or
// the values of its variables.
func (s *Server) aiApp(ctx context.Context, a *apps.App) (map[string]any, error) {
	ts, err := s.Tables.List(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	xs, err := s.allExtViews(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	names := tableNames(ts, xs)
	var tlist []map[string]any
	for _, t := range ts {
		tlist = append(tlist, aiTable(t, names))
	}
	for _, v := range xs {
		tlist = append(tlist, aiExtTable(v, names))
	}
	ds, err := s.Queries.Datasources(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	var dlist []map[string]any
	for _, d := range ds {
		dlist = append(dlist, map[string]any{"id": d.ID, "name": d.Name, "kind": d.Kind, "baseUrl": d.Config.BaseURL})
	}
	qs, err := s.Queries.Queries(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	var qlist []map[string]any
	for _, q := range qs {
		var params []string
		for _, p := range q.Parameters {
			params = append(params, p.Name)
		}
		var fields []string
		for _, f := range q.Fields {
			fields = append(fields, f.Name)
		}
		qlist = append(qlist, map[string]any{"id": q.ID, "name": q.Name, "datasourceId": q.DatasourceID, "method": q.Method, "path": q.Path, "parameters": params, "resultFields": fields})
	}
	vars, err := s.Queries.Variables(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	var vlist []map[string]any
	for _, v := range vars {
		vlist = append(vlist, map[string]any{"name": v.Name, "secret": v.Secret})
	}
	fs, err := s.Flows.Flows(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	var flist []map[string]any
	for _, f := range fs {
		var params []string
		for _, p := range f.Trigger.Parameters {
			params = append(params, p.Name)
		}
		flist = append(flist, map[string]any{"id": f.ID, "name": f.Name, "trigger": f.Trigger.Type, "parameters": params})
	}
	sc, err := s.Screens.List(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	var slist []map[string]any
	for _, x := range sc {
		slist = append(slist, map[string]any{"id": x.ID, "name": x.Name, "route": x.Route, "role": x.Role})
	}
	set, err := s.Apps.Roles(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	var rlist []map[string]any
	for _, d := range set.Defs() {
		rlist = append(rlist, map[string]any{"id": d.ID, "name": d.Name, "inherits": d.Inherits})
	}
	return map[string]any{
		"name": a.Name, "tables": tlist, "datasources": dlist, "queries": qlist, "variables": vlist,
		"flows": flist, "screens": slist, "roles": rlist,
	}, nil
}

func aiTable(t *tables.Table, names map[string]string) map[string]any {
	var fields []map[string]any
	for _, f := range t.Fields {
		m := map[string]any{"name": f.Name, "type": f.Type}
		if len(f.Options) > 0 {
			m["options"] = f.Options
		}
		if f.Type == tables.TypeLink {
			m["linkTable"] = names[f.LinkTable]
			m["linkMany"] = f.LinkMany
		}
		if f.Type == tables.TypeFormula {
			m["formula"] = f.Formula
		}
		if f.Required {
			m["required"] = true
		}
		if f.ID == t.PrimaryField {
			m["rowLabel"] = true
		}
		fields = append(fields, m)
	}
	return map[string]any{"id": t.ID, "name": t.Name, "fields": fields}
}

// allExtViews lists an app's external tables as the API shows them.
func (s *Server) allExtViews(ctx context.Context, appID string) ([]*exttables.View, error) {
	ts, err := s.Ext.List(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.Ext.Views(ctx, appID, ts)
}

func tableNames(ts []*tables.Table, xs []*exttables.View) map[string]string {
	names := map[string]string{}
	for _, t := range ts {
		names[t.ID] = t.Name
	}
	for _, v := range xs {
		names[v.ID] = v.Name
	}
	return names
}

// aiExtTable describes an external table like aiTable, saying where its
// rows live: its columns come from the database and cannot be drafted.
func aiExtTable(v *exttables.View, names map[string]string) map[string]any {
	var fields []map[string]any
	for _, f := range v.Fields {
		m := map[string]any{"name": f.Name, "type": f.Type}
		if len(f.Options) > 0 {
			m["options"] = f.Options
		}
		if f.Type == tables.TypeLink {
			m["linkTable"] = names[f.LinkTable]
			m["linkMany"] = f.LinkMany
		}
		if f.Required {
			m["required"] = true
		}
		if f.ReadOnly {
			m["readOnly"] = true
		}
		if f.Name == v.PrimaryField {
			m["rowLabel"] = true
		}
		fields = append(fields, m)
	}
	return map[string]any{"id": v.ID, "name": v.Name, "fields": fields, "external": map[string]any{
		"database": v.External.Kind, "source": strings.TrimPrefix(v.External.Schema+"."+v.External.Source, "."),
		"readOnly": v.External.ReadOnly, "note": "rows live in an external database; its fields come from there and cannot be added or changed",
	}}
}

func (s *Server) aiDraft(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Kind    string          `json:"kind"`
		Prompt  string          `json:"prompt"`
		Catalog json.RawMessage `json:"catalog"`
		Target  string          `json:"target"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if len(in.Catalog) > 400<<10 {
		return errStatus(400, "the catalog is too large")
	}
	ctx := r.Context()
	app, err := s.aiApp(ctx, a)
	if err != nil {
		return err
	}
	req := ai.DraftRequest{Kind: in.Kind, Prompt: in.Prompt, App: app, Catalog: in.Catalog}
	var current any
	switch in.Kind {
	case ai.KindTable:
		req.Check = func(ctx context.Context, d json.RawMessage) (json.RawMessage, error) {
			return s.checkTableDraft(ctx, a.ID, d)
		}
	case ai.KindScreen:
		if in.Target != "" {
			if current, err = s.Screens.Get(ctx, a.ID, in.Target); err != nil {
				return err
			}
		}
		req.Check = func(ctx context.Context, d json.RawMessage) (json.RawMessage, error) {
			return s.checkScreenDraft(ctx, a.ID, in.Catalog, d)
		}
	case ai.KindQuery:
		if in.Target != "" {
			if current, err = s.Queries.Query(ctx, a.ID, in.Target); err != nil {
				return err
			}
		}
		req.Check = func(ctx context.Context, d json.RawMessage) (json.RawMessage, error) {
			return s.checkQueryDraft(ctx, a.ID, in.Target, d)
		}
	case ai.KindFlow:
		if in.Target != "" {
			if current, err = s.Flows.Flow(ctx, a.ID, in.Target); err != nil {
				return err
			}
		}
		req.Check = func(ctx context.Context, d json.RawMessage) (json.RawMessage, error) {
			return s.checkFlowDraft(ctx, a.ID, in.Target, d)
		}
	default:
		return errStatus(400, "drafts are tables, screens, queries or flows")
	}
	if current != nil {
		req.Current, _ = json.Marshal(current)
	}
	d, err := s.AI.MakeDraft(ctx, req)
	if err != nil {
		return err
	}
	out := map[string]any{"kind": d.Kind, "summary": d.Summary, "changes": d.Changes, "notes": d.Notes, "draft": d.Draft, "target": in.Target}
	if current != nil {
		out["current"] = current
	}
	return writeJSON(w, 200, out)
}

// tableDraft is the shape of a table draft.
type tableDraft struct {
	Tables []struct {
		Name     string              `json:"name"`
		Existing bool                `json:"existing"`
		Fields   []tables.FieldInput `json:"fields"`
		ID       string              `json:"id,omitempty"`
	} `json:"tables"`
}

// checkTableDraft checks every table and field of a draft against the app
// and resolves link targets: existing tables to their ids, drafted ones
// stay as names, for the builder to resolve when it creates them.
func (s *Server) checkTableDraft(ctx context.Context, appID string, raw json.RawMessage) (json.RawMessage, error) {
	var d tableDraft
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("the draft is not in the table shape: %v", err)
	}
	if len(d.Tables) == 0 {
		return nil, errors.New("the draft has no tables")
	}
	ts, err := s.Tables.List(ctx, appID)
	if err != nil {
		return nil, err
	}
	byName := map[string]*tables.Table{}
	for _, t := range ts {
		byName[strings.ToLower(t.Name)] = t
	}
	xs, err := s.allExtViews(ctx, appID)
	if err != nil {
		return nil, err
	}
	extByName := map[string]*exttables.View{}
	for _, v := range xs {
		extByName[strings.ToLower(v.Name)] = v
		extByName[strings.ToLower(v.ID)] = v
	}
	drafted := map[string]bool{}
	for _, t := range d.Tables {
		if v := extByName[strings.ToLower(t.Name)]; v != nil {
			return nil, fmt.Errorf("%s is a table of an external database; its fields come from the database and cannot be drafted", v.Name)
		}
		if !t.Existing {
			if byName[strings.ToLower(t.Name)] != nil {
				return nil, fmt.Errorf("a table named %s already exists; mark it existing to add fields to it", t.Name)
			}
			drafted["draft:"+strings.ToLower(t.Name)] = true
		}
	}
	for i := range d.Tables {
		t := &d.Tables[i]
		var base *tables.Table
		if t.Existing {
			if base = byName[strings.ToLower(t.Name)]; base == nil {
				return nil, fmt.Errorf("there is no table named %s", t.Name)
			}
			t.Name, t.ID = base.Name, base.ID
		}
		if len(t.Fields) == 0 {
			return nil, fmt.Errorf("the table %s has no fields", t.Name)
		}
		check := make([]tables.FieldInput, len(t.Fields))
		for j, f := range t.Fields {
			if f.Type == tables.TypeLink {
				target := strings.ToLower(strings.TrimSpace(f.LinkTable))
				if v := extByName[target]; v != nil {
					return nil, fmt.Errorf("field %q links to %s, a table of an external database; built-in tables cannot link to those yet, but %s can link to a built-in table from its settings", f.Name, v.Name, v.Name)
				}
				if ex := byName[target]; ex != nil {
					f.LinkTable = ex.ID
				} else if ex := findByID(ts, f.LinkTable); ex != nil {
					f.LinkTable = ex.ID
				} else if drafted["draft:"+target] {
					f.LinkTable = "draft:" + target
				} else {
					return nil, fmt.Errorf("field %q links to %q, which is neither a table of the app nor of the draft", f.Name, f.LinkTable)
				}
				t.Fields[j].LinkTable = f.LinkTable
			}
			check[j] = f
		}
		if err := s.Tables.CheckFields(ctx, appID, base, t.Name, check, drafted); err != nil {
			return nil, err
		}
	}
	return json.Marshal(d)
}

func findByID(ts []*tables.Table, id string) *tables.Table {
	for _, t := range ts {
		if t.ID == id {
			return t
		}
	}
	return nil
}

var nodeIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// checkScreenDraft checks a screen draft: its route and role, that every
// component is one the builder offers, ids are unique, and tables and
// queries exist. The builder fills in component defaults.
func (s *Server) checkScreenDraft(ctx context.Context, appID string, catalog, raw json.RawMessage) (json.RawMessage, error) {
	var d struct {
		Name       string          `json:"name"`
		Route      string          `json:"route"`
		Role       string          `json:"role"`
		Definition json.RawMessage `json:"definition"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("the draft is not in the screen shape: %v", err)
	}
	if strings.TrimSpace(d.Name) == "" {
		return nil, errors.New("the screen needs a name")
	}
	route, err := screens.NormalizeRoute(d.Route)
	if err != nil {
		return nil, err
	}
	d.Route = route
	role, err := rbac.ParseRole(d.Role)
	if err != nil {
		return nil, err
	}
	if role == rbac.None {
		role = rbac.Basic
	}
	set, err := s.Apps.Roles(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !set.Has(role) {
		return nil, fmt.Errorf("the app has no role %s", role)
	}
	d.Role = string(role)
	var cat struct {
		Components []struct {
			Type      string   `json:"type"`
			Container bool     `json:"container"`
			Parents   []string `json:"parents"`
		} `json:"components"`
	}
	json.Unmarshal(catalog, &cat)
	known := map[string]bool{}
	containers := map[string]bool{}
	parents := map[string][]string{}
	for _, c := range cat.Components {
		known[c.Type] = true
		containers[c.Type] = c.Container
		parents[c.Type] = c.Parents
	}
	ts, err := s.Tables.List(ctx, appID)
	if err != nil {
		return nil, err
	}
	xs, err := s.Ext.List(ctx, appID)
	if err != nil {
		return nil, err
	}
	qs, err := s.Queries.Queries(ctx, appID)
	if err != nil {
		return nil, err
	}
	type node struct {
		ID       string         `json:"id"`
		Type     string         `json:"type"`
		Props    map[string]any `json:"props"`
		Children []*node        `json:"children"`
	}
	var root node
	if err := json.Unmarshal(d.Definition, &root); err != nil {
		return nil, fmt.Errorf("the definition is not a component tree: %v", err)
	}
	seen := map[string]bool{}
	count := 0
	var walk func(n *node, parent string) error
	walk = func(n *node, parent string) error {
		count++
		if len(known) > 0 && !known[n.Type] {
			return fmt.Errorf("there is no component type %q", n.Type)
		}
		if ps := parents[n.Type]; len(ps) > 0 && !contains(ps, parent) {
			return fmt.Errorf("a %s goes only inside %s", n.Type, strings.Join(ps, " or "))
		}
		if !nodeIDRe.MatchString(n.ID) || seen[n.ID] {
			return fmt.Errorf("component ids must be unique words; %q is not", n.ID)
		}
		seen[n.ID] = true
		if t, _ := n.Props["table"].(string); t != "" {
			if q, ok := strings.CutPrefix(t, "query:"); ok {
				if !slicesContainsFunc(qs, func(x *queries.Query) bool { return x.ID == q }) {
					return fmt.Errorf("component %s uses the query %s, which does not exist", n.ID, q)
				}
			} else if findByID(ts, t) == nil && !hasExt(xs, t) {
				return fmt.Errorf("component %s uses the table %s, which does not exist; use a table id", n.ID, t)
			}
		}
		if len(n.Children) > 0 && len(known) > 0 && !containers[n.Type] {
			return fmt.Errorf("a %s cannot hold other components", n.Type)
		}
		for _, c := range n.Children {
			if err := walk(c, n.Type); err != nil {
				return err
			}
		}
		return nil
	}
	if root.Type != "container" {
		return nil, errors.New("the root component is a container")
	}
	if err := walk(&root, ""); err != nil {
		return nil, err
	}
	if count > 400 {
		return nil, errors.New("the screen has too many components; keep it under 400")
	}
	def, err := screens.CheckDefinition(d.Definition)
	if err != nil {
		return nil, err
	}
	d.Definition = def
	return json.Marshal(d)
}

func hasExt(xs []*exttables.Table, id string) bool {
	for _, x := range xs {
		if x.ID == id {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func slicesContainsFunc[T any](list []T, fn func(T) bool) bool {
	for _, x := range list {
		if fn(x) {
			return true
		}
	}
	return false
}

func (s *Server) checkQueryDraft(ctx context.Context, appID, target string, raw json.RawMessage) (json.RawMessage, error) {
	var head struct {
		DatasourceID string `json:"datasourceId"`
		Role         string `json:"role"`
	}
	json.Unmarshal(raw, &head)
	if _, err := s.Queries.Datasource(ctx, appID, head.DatasourceID); err != nil {
		return nil, fmt.Errorf("the data source %q does not exist; use one from the app", head.DatasourceID)
	}
	var m map[string]any
	json.Unmarshal(raw, &m)
	if m == nil {
		return nil, errors.New("the draft is not a query")
	}
	m["id"] = target
	data, _ := json.Marshal(m)
	q, err := s.Queries.CheckQuery(ctx, appID, data)
	if err != nil {
		return nil, err
	}
	if q.Role != "" && q.Role != rbac.None {
		set, err := s.Apps.Roles(ctx, appID)
		if err != nil {
			return nil, err
		}
		if !set.Has(q.Role) {
			return nil, fmt.Errorf("the app has no role %s", q.Role)
		}
	}
	delete(m, "id")
	return json.Marshal(m)
}

func (s *Server) checkFlowDraft(ctx context.Context, appID, target string, raw json.RawMessage) (json.RawMessage, error) {
	f := &flows.Flow{Definition: flows.Definition{Trigger: flows.Trigger{Type: flows.OnManual, Role: rbac.Basic}}}
	if err := json.Unmarshal(raw, f); err != nil {
		return nil, fmt.Errorf("the draft is not in the flow shape: %v", err)
	}
	if err := f.Check(); err != nil {
		return nil, err
	}
	ts, err := s.Tables.List(ctx, appID)
	if err != nil {
		return nil, err
	}
	xs, err := s.Ext.List(ctx, appID)
	if err != nil {
		return nil, err
	}
	if f.Trigger.Table != "" && findByID(ts, f.Trigger.Table) == nil && !hasExt(xs, f.Trigger.Table) {
		return nil, fmt.Errorf("the trigger's table %s does not exist; use a table id", f.Trigger.Table)
	}
	qs, err := s.Queries.Queries(ctx, appID)
	if err != nil {
		return nil, err
	}
	var walk func(list []*flows.Step) error
	walk = func(list []*flows.Step) error {
		for _, st := range list {
			if t, _ := st.Inputs["table"].(string); t != "" && findByID(ts, t) == nil && !hasExt(xs, t) {
				return fmt.Errorf("step %s uses the table %s, which does not exist; use a table id", st.Name, t)
			}
			if t, _ := st.Inputs["table"].(string); st.Type == "generate_pdf" && hasExt(xs, t) {
				return fmt.Errorf("step %s stores a PDF with an external table; PDFs are stored with built-in tables", st.Name)
			}
			if q, _ := st.Inputs["query"].(string); q != "" && !slicesContainsFunc(qs, func(x *queries.Query) bool { return x.ID == q || x.Name == q }) {
				return fmt.Errorf("step %s runs the query %s, which does not exist", st.Name, q)
			}
			for _, sub := range [][]*flows.Step{st.Then, st.Else, st.Do} {
				if err := walk(sub); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(f.Steps); err != nil {
		return nil, err
	}
	set, err := s.Apps.Roles(ctx, appID)
	if err != nil {
		return nil, err
	}
	for _, role := range []rbac.Role{f.Trigger.Role, f.RunRole} {
		if role != "" && role != rbac.None && !set.Has(role) {
			return nil, fmt.Errorf("the app has no role %s", role)
		}
	}
	if target == "" {
		f.Enabled = false // new flows are tested before they run
	}
	out, _ := json.Marshal(map[string]any{"name": f.Name, "enabled": f.Enabled, "trigger": f.Trigger, "steps": f.Steps, "runAs": f.RunAs, "runRole": f.RunRole})
	return out, nil
}

func (s *Server) aiFormula(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Table   string `json:"table"`
		Prompt  string `json:"prompt"`
		Purpose string `json:"purpose"`
		Current string `json:"current"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	ts, err := s.Tables.List(ctx, a.ID)
	if err != nil {
		return err
	}
	xs, err := s.allExtViews(ctx, a.ID)
	if err != nil {
		return err
	}
	names := tableNames(ts, xs)
	var desc map[string]any
	var check func(ctx context.Context, src string) (formula.Type, []*formula.Error, error)
	if exttables.Is(in.Table) {
		t, err := s.Ext.Get(ctx, a.ID, in.Table)
		if err != nil {
			return err
		}
		for _, v := range xs {
			if v.ID == t.ID {
				desc = aiExtTable(v, names)
			}
		}
		check = func(ctx context.Context, src string) (formula.Type, []*formula.Error, error) {
			return s.Ext.CheckFormula(ctx, a.ID, t.ID, src)
		}
	} else {
		t, err := s.Tables.Get(ctx, a.ID, in.Table)
		if err != nil {
			return err
		}
		desc = aiTable(t, names)
		check = func(ctx context.Context, src string) (formula.Type, []*formula.Error, error) {
			return s.Tables.CheckFormula(ctx, a.ID, t.ID, src, false)
		}
	}
	f, err := s.AI.MakeFormula(ctx, ai.FormulaRequest{
		Prompt: in.Prompt, Purpose: in.Purpose, Current: in.Current,
		Table: desc, Functions: formula.Functions(),
		Check: func(ctx context.Context, src string) (string, error) {
			typ, errs, err := check(ctx, src)
			if err != nil {
				return "", err
			}
			if len(errs) > 0 {
				return "", fmt.Errorf("the formula does not compile: %s", errs[0].Message)
			}
			return strings.ToLower(typ.String()), nil
		},
	})
	if err != nil {
		return err
	}
	return writeJSON(w, 200, f)
}

// aiAsk answers a question about the app's data with the access of the
// builder, of a role, or of one member ("user:<id>"): tables, row rules
// and hidden links apply as they would to that person.
func (s *Server) aiAsk(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Question string `json:"question"`
		As       string `json:"as"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	_, act, err := s.actor(r)
	if err != nil {
		return err
	}
	env := s.dataEnv(r, act)
	who := "a builder of the app, who sees all data"
	if in.As != "" {
		set, err := s.Apps.Roles(ctx, a.ID)
		if err != nil {
			return err
		}
		if id, ok := strings.CutPrefix(in.As, "user:"); ok {
			// A member of the app, with their own role, email and groups,
			// so row rules about them apply.
			u, err := s.Auth.Get(ctx, id)
			if err != nil {
				return err
			}
			role, err := s.Apps.RoleOf(ctx, a.ID, u.ID)
			if err != nil {
				return err
			}
			if u.IsBuilder() || !set.Has(role) {
				return errStatus(400, u.Name+" is a builder or has no role in this app; pick a member")
			}
			act = rbac.NewActor(set, role)
			act.UserID, act.Email, act.Name, act.Groups = u.ID, u.Email, u.Name, u.Groups
			who = fmt.Sprintf("%s (%s), whose app role is %s", u.Name, u.Email, role)
		} else {
			role, err := rbac.ParseRole(in.As)
			if err != nil {
				return errStatus(400, err.Error())
			}
			if !set.Has(role) {
				return errStatus(400, "the app has no role "+string(role))
			}
			u := userFrom(r)
			act = rbac.NewActor(set, role)
			act.UserID, act.Email, act.Name = u.ID, u.Email, u.Name
			who = "someone with the app role " + string(role)
		}
	}
	ts, err := s.Tables.List(ctx, a.ID)
	if err != nil {
		return err
	}
	xs, err := s.allExtViews(ctx, a.ID)
	if err != nil {
		return err
	}
	names := tableNames(ts, xs)
	var readable []*dataTable
	described := []map[string]any{}
	for _, t := range ts {
		if act.Has(t.Permissions.Read) {
			t := t
			dt := &dataTable{id: t.ID, name: t.Name, list: func(ctx context.Context, q tables.Query) (*tables.Page, error) {
				return s.Tables.ListRows(ctx, act, a.ID, t.ID, q)
			}}
			for _, f := range t.Fields {
				dt.fields = append(dt.fields, f.Name)
			}
			readable = append(readable, dt)
			described = append(described, aiTable(t, names))
		}
	}
	for _, v := range xs {
		if act.Has(v.Permissions.Read) {
			v := v
			dt := &dataTable{id: v.ID, name: v.Name, list: func(ctx context.Context, q tables.Query) (*tables.Page, error) {
				return s.Ext.ListRows(ctx, act, env, a.ID, v.ID, q)
			}}
			for _, f := range v.Fields {
				dt.fields = append(dt.fields, f.Name)
			}
			readable = append(readable, dt)
			described = append(described, aiExtTable(v, names))
		}
	}
	tools := s.dataTools(act, readable)
	ans, err := s.AI.Ask(ctx, ai.AskRequest{Question: in.Question, Tables: described, Who: who, Tools: tools})
	if err != nil {
		return err
	}
	return writeJSON(w, 200, ans)
}

// dataTable is a table, built-in or external, that data questions read
// with the asker's access.
type dataTable struct {
	id, name string
	fields   []string
	list     func(ctx context.Context, q tables.Query) (*tables.Page, error)
}

// field gives a field's name as the table spells it, or "".
func (t *dataTable) field(name string) string {
	for _, f := range t.fields {
		if strings.EqualFold(f, name) {
			return f
		}
	}
	return ""
}

// dataTools read rows with the actor's access, for data questions.
func (s *Server) dataTools(act rbac.Actor, readable []*dataTable) []ai.Tool {
	find := func(name string) (*dataTable, error) {
		for _, t := range readable {
			if strings.EqualFold(t.name, name) || t.id == name {
				return t, nil
			}
		}
		return nil, fmt.Errorf("there is no table %q you may read", name)
	}
	shown := func(t *dataTable, row map[string]any, only []string) map[string]any {
		out := map[string]any{"_id": row["_id"]}
		for _, f := range t.fields {
			if len(only) > 0 && !containsFold(only, f) {
				continue
			}
			out[f] = plainValue(row[f])
		}
		return out
	}
	queryRows := ai.Tool{
		Name:        "query_rows",
		Description: "List rows of a table, with an optional filter formula, sort and fields. Returns the total that match and up to limit rows.",
		Schema: map[string]any{
			"table":  map[string]any{"type": "string", "description": "Table name"},
			"filter": map[string]any{"type": "string", "description": "Formula that is true for the rows to list, e.g. [Status] = \"Open\""},
			"sort":   map[string]any{"type": "string", "description": "Field name to sort by"},
			"desc":   map[string]any{"type": "boolean"},
			"limit":  map[string]any{"type": "integer", "description": "At most 50"},
			"fields": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Only these fields"},
		},
		Required: []string{"table"},
		Run: func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			var in struct {
				Table, Filter, Sort string
				Desc                bool
				Limit               int
				Fields              []string
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return "", false, err
			}
			t, err := find(in.Table)
			if err != nil {
				return "", false, err
			}
			if in.Limit <= 0 || in.Limit > 50 {
				in.Limit = 50
			}
			page, err := t.list(ctx, tables.Query{Filter: in.Filter, Sort: in.Sort, Desc: in.Desc, Limit: in.Limit})
			if err != nil {
				return "", false, err
			}
			rows := make([]map[string]any, len(page.Rows))
			for i, row := range page.Rows {
				rows[i] = shown(t, row, in.Fields)
			}
			b, _ := json.Marshal(map[string]any{"total": page.Total, "rows": rows})
			return string(b), false, nil
		},
	}
	summarize := ai.Tool{
		Name:        "summarize",
		Description: "Count rows, or sum, average, min or max a number field, over the rows that match a filter, optionally per value of a group field. Covers up to 20000 rows.",
		Schema: map[string]any{
			"table":   map[string]any{"type": "string"},
			"filter":  map[string]any{"type": "string"},
			"op":      map[string]any{"type": "string", "enum": []string{"count", "sum", "avg", "min", "max"}},
			"field":   map[string]any{"type": "string", "description": "The number or date field, for every op but count"},
			"groupBy": map[string]any{"type": "string", "description": "Field whose values make the groups"},
		},
		Required: []string{"table", "op"},
		Run: func(ctx context.Context, input json.RawMessage) (string, bool, error) {
			var in struct{ Table, Filter, Op, Field, GroupBy string }
			if err := json.Unmarshal(input, &in); err != nil {
				return "", false, err
			}
			t, err := find(in.Table)
			if err != nil {
				return "", false, err
			}
			field, group := t.field(in.Field), t.field(in.GroupBy)
			if in.Op != "count" && field == "" {
				return "", false, fmt.Errorf("the table %s has no field %q", t.name, in.Field)
			}
			if in.GroupBy != "" && group == "" {
				return "", false, fmt.Errorf("the table %s has no field %q", t.name, in.GroupBy)
			}
			type acc struct {
				n, count   int
				sum        float64
				min, max   float64
				minS, maxS string
			}
			groups := map[string]*acc{}
			var order []string
			scanned := 0
			for offset := 0; offset < 20000; offset += 1000 {
				page, err := t.list(ctx, tables.Query{Filter: in.Filter, Limit: 1000, Offset: offset})
				if err != nil {
					return "", false, err
				}
				for _, row := range page.Rows {
					scanned++
					key := ""
					if in.GroupBy != "" {
						key = fmt.Sprint(plainValue(row[group]))
					}
					g := groups[key]
					if g == nil {
						g = &acc{min: math.Inf(1), max: math.Inf(-1)}
						groups[key] = g
						order = append(order, key)
					}
					g.count++
					if in.Op == "count" {
						continue
					}
					switch v := row[field].(type) {
					case float64:
						g.n++
						g.sum += v
						g.min, g.max = math.Min(g.min, v), math.Max(g.max, v)
					case string: // dates compare as text
						g.n++
						if g.minS == "" || v < g.minS {
							g.minS = v
						}
						if v > g.maxS {
							g.maxS = v
						}
					}
				}
				if len(page.Rows) < 1000 {
					break
				}
			}
			sort.Strings(order)
			var result []map[string]any
			for _, k := range order {
				g := groups[k]
				m := map[string]any{"rows": g.count}
				if in.GroupBy != "" {
					m[in.GroupBy] = k
				}
				switch in.Op {
				case "sum":
					m["sum"] = g.sum
				case "avg":
					if g.n > 0 {
						m["avg"] = g.sum / float64(g.n)
					}
				case "min":
					if g.minS != "" {
						m["min"] = g.minS
					} else if g.n > 0 {
						m["min"] = g.min
					}
				case "max":
					if g.maxS != "" {
						m["max"] = g.maxS
					} else if g.n > 0 {
						m["max"] = g.max
					}
				}
				result = append(result, m)
			}
			if result == nil {
				result = []map[string]any{}
			}
			b, _ := json.Marshal(map[string]any{"scanned": scanned, "groups": result})
			return string(b), false, nil
		},
	}
	return []ai.Tool{queryRows, summarize}
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// plainValue shows link and file values by their names; hidden linked
// rows stay unnamed.
func plainValue(v any) any {
	switch x := v.(type) {
	case tables.LinkRef:
		if x.Hidden {
			return "(hidden row)"
		}
		return x.Label
	case []tables.LinkRef:
		out := make([]any, len(x))
		for i, l := range x {
			out[i] = plainValue(l)
		}
		return out
	case exttables.LinkRef:
		if x.Hidden {
			return "(hidden row)"
		}
		return x.Label
	case []exttables.LinkRef:
		out := make([]any, len(x))
		for i, l := range x {
			out[i] = plainValue(l)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, f := range x {
			out[i] = f["name"]
		}
		return out
	}
	return v
}
