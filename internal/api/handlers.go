package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/auth"
	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/tables"
)

// Sessions and setup.

func (s *Server) setCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) error {
	need, err := s.Auth.NeedsSetup(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"needsSetup": need})
}

type credentials struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) error {
	var in credentials
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if _, err := s.Auth.Setup(r.Context(), in.Email, in.Name, in.Password); err != nil {
		return err
	}
	return s.startSession(w, r, in.Email, in.Password)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var in credentials
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ip := s.clientIP(r)
	if s.limiter.blocked(ip) {
		return errStatus(429, "too many failed sign-ins; wait a few minutes")
	}
	err := s.startSession(w, r, in.Email, in.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
		s.limiter.fail(ip)
	}
	return err
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, email, password string) error {
	u, token, err := s.Auth.Login(r.Context(), email, password)
	if err != nil {
		return err
	}
	s.setCookie(w, token, int(auth.SessionTTL/time.Second))
	return writeJSON(w, 200, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Auth.Logout(r.Context(), c.Value); err != nil {
			return err
		}
	}
	s.setCookie(w, "", -1)
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	if u == nil {
		return errLogin
	}
	return writeJSON(w, 200, u)
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	if u == nil {
		return errLogin
	}
	keys, err := s.Auth.APIKeys(r.Context(), u.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, keys)
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	if u == nil {
		return errLogin
	}
	if apiKeyFrom(r).Scoped() {
		return errStatus(403, "an app API key cannot make other keys")
	}
	var in auth.KeyInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.AppID != "" {
		a, err := s.Apps.Get(r.Context(), in.AppID)
		if err != nil {
			return errStatus(400, "there is no such app")
		}
		in.AppID = a.ID
		if in.Role != "" {
			role, err := rbac.ParseRole(in.Role)
			if err != nil {
				return errStatus(400, err.Error())
			}
			act, err := s.userActor(r, a, u)
			if err != nil {
				return err
			}
			set, err := s.Apps.Roles(r.Context(), a.ID)
			if err != nil {
				return err
			}
			if !set.Has(role) {
				return errStatus(400, "this app has no role "+string(role))
			}
			if !act.Has(role) {
				return errStatus(403, "you can only give a key a role you hold yourself")
			}
			in.Role = string(role)
		}
	}
	k, err := s.Auth.CreateAPIKey(r.Context(), u.ID, in)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, k)
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	if u == nil {
		return errLogin
	}
	if err := s.Auth.DeleteAPIKey(r.Context(), u.ID, r.PathValue("id")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

// Users, for workspace admins.

func (s *Server) requireAdmin(r *http.Request) (*auth.User, error) {
	u := userFrom(r)
	if u == nil {
		return nil, errLogin
	}
	if !u.IsAdmin() || apiKeyFrom(r).Scoped() {
		return nil, errStatus(403, "only workspace owners and admins manage users")
	}
	return u, nil
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	if u == nil {
		return errLogin
	}
	if !u.IsBuilder() {
		return errStatus(403, "only builders and admins can list users")
	}
	users, err := s.Auth.List(r.Context())
	if err != nil {
		return err
	}
	rows, err := s.userRows(r, users)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, rows)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	var in struct {
		credentials
		Role string `json:"role"`
		// SSOOnly makes an account that signs in with single sign-on only.
		SSOOnly bool `json:"ssoOnly"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Role == "" {
		in.Role = auth.RoleMember
	}
	if in.Role == auth.RoleOwner && userFrom(r).Role != auth.RoleOwner {
		return errStatus(403, "only owners can make someone an owner")
	}
	create := s.Auth.CreateUser
	if in.SSOOnly {
		create = func(ctx context.Context, email, name, _, role string) (*auth.User, error) {
			return s.Auth.CreateSSOUser(ctx, email, name, role)
		}
	}
	u, err := create(r.Context(), in.Email, in.Name, in.Password, in.Role)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) error {
	me := userFrom(r)
	if me == nil {
		return errLogin
	}
	id := r.PathValue("id")
	var p auth.UserPatch
	if err := readJSON(r, &p); err != nil {
		return err
	}
	// Anyone may change their own name and password; only admins the rest.
	if (!me.IsAdmin() || apiKeyFrom(r).Scoped()) && (id != me.ID || p.Role != nil || p.Disabled != nil) {
		return errStatus(403, "only workspace owners and admins manage users")
	}
	if id == me.ID && p.Disabled != nil && *p.Disabled {
		return errStatus(400, "you cannot switch off your own account")
	}
	if p.Role != nil && *p.Role == auth.RoleOwner && me.Role != auth.RoleOwner {
		return errStatus(403, "only owners can make someone an owner")
	}
	u, err := s.Auth.Update(r.Context(), id, p)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, u)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	if err := s.Auth.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

// Apps.

func (s *Server) requireBuilder(r *http.Request) (*auth.User, error) {
	u := userFrom(r)
	if u == nil {
		return nil, errLogin
	}
	if !u.IsBuilder() || apiKeyFrom(r).Scoped() {
		return nil, errStatus(403, "only builders can change app designs")
	}
	return u, nil
}

// actor resolves who the request runs as inside an app.
func (s *Server) actor(r *http.Request) (*apps.App, rbac.Actor, error) {
	a, err := s.Apps.Get(r.Context(), r.PathValue("app"))
	if err != nil {
		return nil, rbac.Actor{}, err
	}
	u := userFrom(r)
	if u == nil {
		set, err := s.Apps.Roles(r.Context(), a.ID)
		if err != nil {
			return nil, rbac.Actor{}, err
		}
		return a, rbac.NewActor(set, rbac.Public), s.appRequest(a)
	}
	act, err := s.userActor(r, a, u)
	if err == nil && !act.Builder {
		err = s.appRequest(a)
	}
	return a, act, err
}

// previewHeader lets builders preview a published app as one of its roles.
const previewHeader = "X-Bubibase-Preview-Role"

// userActor is what a signed-in user may do in an app: builders hold
// every role, other users the role they were given. An API key limited to
// an app works only there and acts with the key's role.
func (s *Server) userActor(r *http.Request, a *apps.App, u *auth.User) (rbac.Actor, error) {
	ctx := r.Context()
	set, err := s.Apps.Roles(ctx, a.ID)
	if err != nil {
		return rbac.Actor{}, err
	}
	act := rbac.Actor{UserID: u.ID, Email: u.Email, Name: u.Name, Groups: u.Groups}
	key := apiKeyFrom(r)
	if key.Scoped() && key.AppID != a.ID {
		return act, errStatus(403, "this API key only works for another app")
	}
	role := rbac.Public
	if u.IsBuilder() {
		role, act.Builder = rbac.Admin, !key.Scoped()
	} else if given, err := s.Apps.RoleOf(ctx, a.ID, u.ID); err != nil {
		return act, err
	} else if set.Has(given) {
		role = given
	}
	if key.Scoped() && key.Role != "" {
		held := act
		held.Role, held.Roles = role, set.Chain(role)
		if !set.Has(rbac.Role(key.Role)) || !held.Has(rbac.Role(key.Role)) {
			return act, errStatus(403, "this API key's role is no longer yours")
		}
		role = rbac.Role(key.Role)
	}
	if p := r.Header.Get(previewHeader); p != "" && act.Builder {
		if pr, err := rbac.ParseRole(p); err == nil && set.Has(pr) {
			role, act.Builder = pr, false
		}
	}
	act.Role, act.Roles = role, set.Chain(role)
	return act, nil
}

func (s *Server) builderApp(r *http.Request) (*apps.App, error) {
	if _, err := s.requireBuilder(r); err != nil {
		return nil, err
	}
	return s.Apps.Get(r.Context(), r.PathValue("app"))
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	if u == nil {
		return errLogin
	}
	all, err := s.Apps.List(r.Context())
	if err != nil {
		return err
	}
	key := apiKeyFrom(r)
	if u.IsBuilder() && !key.Scoped() {
		return writeJSON(w, 200, all)
	}
	mine := []*apps.App{}
	for _, a := range all {
		if key.Scoped() && key.AppID != a.ID {
			continue
		}
		if act, err := s.userActor(r, a, u); err == nil && act.SignedIn() {
			mine = append(mine, a)
		}
	}
	return writeJSON(w, 200, mine)
}

func (s *Server) createApp(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireBuilder(r); err != nil {
		return err
	}
	var in struct{ Name, Slug string }
	if err := readJSON(r, &in); err != nil {
		return err
	}
	a, err := s.Apps.Create(r.Context(), in.Name, in.Slug)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, a)
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	if !act.SignedIn() {
		return errStatus(404, "app not found")
	}
	return writeJSON(w, 200, map[string]any{"app": a, "role": act.Role, "builder": act.Builder})
}

func (s *Server) updateApp(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Name, Slug *string
		Design     *apps.Design
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	a, err = s.Apps.Update(r.Context(), a.ID, in.Name, in.Slug, in.Design)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, a)
}

func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	// Contents kept outside the database go first, while the app's file
	// list still says where they are.
	if err := s.Files.DeleteApp(r.Context(), a.ID); err != nil {
		return err
	}
	if err := s.Apps.Delete(r.Context(), a.ID); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	m, err := s.Apps.Members(r.Context(), a.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, m)
}

func (s *Server) setMember(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Role rbac.Role `json:"role"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	u, err := s.Auth.Get(r.Context(), r.PathValue("user"))
	if err != nil {
		return err
	}
	if err := s.Apps.SetRole(r.Context(), a.ID, u.ID, in.Role); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

// Tables.

func (s *Server) listTables(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	all, err := s.Tables.List(r.Context(), a.ID)
	if err != nil {
		return err
	}
	visible := []*tables.Table{}
	for _, t := range all {
		if act.Has(t.Permissions.Read) {
			visible = append(visible, t)
		}
	}
	return writeJSON(w, 200, visible)
}

func (s *Server) createTable(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in struct {
		Name   string              `json:"name"`
		Fields []tables.FieldInput `json:"fields"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	t, err := s.Tables.CreateTable(r.Context(), a.ID, in.Name, in.Fields)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, t)
}

func (s *Server) getTable(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	t, err := s.Tables.Get(r.Context(), a.ID, r.PathValue("table"))
	if err != nil {
		return err
	}
	if !act.Builder {
		if err := act.Require(t.Permissions.Read, "reading "+t.Name); err != nil {
			return err
		}
	}
	return writeJSON(w, 200, t)
}

func (s *Server) updateTable(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var p tables.TablePatch
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
	t, err := s.Tables.UpdateTable(r.Context(), a.ID, r.PathValue("table"), p)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, t)
}

func (s *Server) deleteTable(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if err := s.Tables.DeleteTable(r.Context(), a.ID, r.PathValue("table")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) addField(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in tables.FieldInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	t, _, err := s.Tables.AddField(r.Context(), a.ID, r.PathValue("table"), in)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, t)
}

func (s *Server) updateField(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var p tables.FieldPatch
	if err := readJSON(r, &p); err != nil {
		return err
	}
	t, err := s.Tables.UpdateField(r.Context(), a.ID, r.PathValue("table"), r.PathValue("field"), p)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, t)
}

func (s *Server) deleteField(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	t, err := s.Tables.DeleteField(r.Context(), a.ID, r.PathValue("table"), r.PathValue("field"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, t)
}

func (s *Server) checkFormula(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	if !act.SignedIn() {
		return errLogin
	}
	var in struct {
		Formula string `json:"formula"`
		Static  bool   `json:"static"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	typ, errs, err := s.Tables.CheckFormula(r.Context(), a.ID, r.PathValue("table"), in.Formula, in.Static)
	if err != nil {
		return err
	}
	if errs == nil {
		errs = []*formula.Error{}
	}
	return writeJSON(w, 200, map[string]any{"type": typ, "errors": errs})
}

func (s *Server) functions(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, 200, formula.Functions())
}

// Rows.

func rowID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errStatus(404, "row not found")
	}
	return id, nil
}

// rowValues reads a JSON object of values keyed by field name, keeping
// numbers as float64.
func rowValues(r *http.Request) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := readJSON(r, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		var x any
		if err := json.Unmarshal(v, &x); err != nil {
			return nil, errStatus(400, "invalid value for "+k)
		}
		out[k] = x
	}
	return out, nil
}

func (s *Server) listRows(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	page, err := s.Tables.ListRows(r.Context(), act, a.ID, r.PathValue("table"), tables.Query{
		Filter: q.Get("filter"), Sort: q.Get("sort"), Desc: q.Get("desc") == "true" || q.Get("desc") == "1",
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, 200, page)
}

// aggregateRows groups and reduces rows for charts and stat cards:
// ?groupBy=Stage&values=sum:Amount&values=count&filter=…&dateGroup=month
// &sortBy=value&limit=10.
func (s *Server) aggregateRows(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	aq := tables.AggQuery{Filter: q.Get("filter"), GroupBy: q.Get("groupBy"), DateGroup: q.Get("dateGroup"), SortBy: q.Get("sortBy"), Limit: limit}
	for _, v := range q["values"] {
		op, field, _ := strings.Cut(v, ":")
		aq.Values = append(aq.Values, tables.AggValue{Op: op, Field: field})
	}
	res, err := s.Tables.Aggregate(r.Context(), act, a.ID, r.PathValue("table"), aq)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, res)
}

func (s *Server) createRow(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	vals, err := rowValues(r)
	if err != nil {
		return err
	}
	row, err := s.Tables.CreateRow(r.Context(), act, a.ID, r.PathValue("table"), vals)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, row)
}

func (s *Server) getRow(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	id, err := rowID(r)
	if err != nil {
		return err
	}
	row, err := s.Tables.GetRow(r.Context(), act, a.ID, r.PathValue("table"), id)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, row)
}

func (s *Server) updateRow(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	id, err := rowID(r)
	if err != nil {
		return err
	}
	vals, err := rowValues(r)
	if err != nil {
		return err
	}
	row, err := s.Tables.UpdateRow(r.Context(), act, a.ID, r.PathValue("table"), id, vals)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, row)
}

func (s *Server) deleteRow(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	id, err := rowID(r)
	if err != nil {
		return err
	}
	n, err := s.Tables.DeleteRows(r.Context(), act, a.ID, r.PathValue("table"), []int64{id})
	if err != nil {
		return err
	}
	if n == 0 {
		return errStatus(404, "row not found")
	}
	return writeJSON(w, 200, map[string]any{"deleted": n})
}

func (s *Server) deleteRows(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	n, err := s.Tables.DeleteRows(r.Context(), act, a.ID, r.PathValue("table"), in.IDs)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"deleted": n})
}

func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
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
	addFields := r.URL.Query().Get("addFields") == "true"
	res, err := s.Tables.ImportCSV(r.Context(), act, a.ID, r.PathValue("table"), body, addFields)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, res)
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	t, err := s.Tables.Get(r.Context(), a.ID, r.PathValue("table"))
	if err != nil {
		return err
	}
	if err := act.Require(t.Permissions.Read, "reading "+t.Name); err != nil {
		return err
	}
	// Check the filter before streaming, so errors still come back as JSON.
	if _, err := s.Tables.ListRows(r.Context(), act, a.ID, t.ID, tables.Query{Filter: r.URL.Query().Get("filter"), Limit: 1}); err != nil {
		return err
	}
	name := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 32 {
			return '_'
		}
		return r
	}, t.Name)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, name))
	if err := s.Tables.ExportCSV(r.Context(), act, a.ID, t.ID, r.URL.Query().Get("filter"), w); err != nil {
		s.Log.Error("export failed", "err", err)
	}
	return nil
}
