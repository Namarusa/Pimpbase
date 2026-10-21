package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/apps"
	"github.com/pimpbase/pimpbase/internal/auth"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/screens"
	"github.com/pimpbase/pimpbase/internal/sso"
)

// requireRoles checks that every named role exists in the app.
func (s *Server) requireRoles(r *http.Request, appID string, roles ...rbac.Role) error {
	set, err := s.Apps.Roles(r.Context(), appID)
	if err != nil {
		return err
	}
	for _, role := range roles {
		if role != "" && role != rbac.None && !set.Has(role) {
			return errStatus(400, "this app has no role "+string(role))
		}
	}
	return nil
}

// bodyRoles checks the role at a path of a JSON body, when it is there.
func (s *Server) bodyRoles(r *http.Request, appID string, body []byte, path ...string) error {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return nil // the service reports bad JSON
	}
	for _, k := range path {
		m, _ := v.(map[string]any)
		v = m[k]
	}
	str, ok := v.(string)
	if !ok {
		return nil
	}
	role, err := rbac.ParseRole(str)
	if err != nil {
		return errStatus(400, err.Error())
	}
	return s.requireRoles(r, appID, role)
}

func (s *Server) screenRoles(r *http.Request, appID string, in screens.Input) error {
	var roles []rbac.Role
	if in.Role != nil {
		roles = append(roles, *in.Role)
	}
	if len(in.Definition) > 0 {
		roles = append(roles, screens.ComponentRoles(in.Definition)...)
	}
	return s.requireRoles(r, appID, roles...)
}

// App roles.

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	set, err := s.Apps.Roles(r.Context(), a.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, set.Defs())
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in apps.RoleInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	d, err := s.Apps.CreateRole(r.Context(), a.ID, in)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, d)
}

func (s *Server) updateRole(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in apps.RoleInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	d, err := s.Apps.UpdateRole(r.Context(), a.ID, rbac.Role(r.PathValue("role")), in)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, d)
}

func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if err := s.Apps.DeleteRole(r.Context(), a.ID, rbac.Role(r.PathValue("role"))); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

// Users, with how they sign in and what apps they use.

type userRow struct {
	*auth.User
	// SignIn lists "password" and the names of the providers the user has
	// signed in with.
	SignIn []string     `json:"signIn"`
	Apps   []apps.Grant `json:"apps"`
}

func (s *Server) userRows(r *http.Request, users []*auth.User) ([]userRow, error) {
	linked, err := s.SSO.LinkedProviders(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]userRow, 0, len(users))
	for _, u := range users {
		row := userRow{User: u, SignIn: []string{}}
		if u.HasPassword {
			row.SignIn = append(row.SignIn, "password")
		}
		row.SignIn = append(row.SignIn, linked[u.ID]...)
		if row.Apps, err = s.Apps.GrantsOf(r.Context(), u.ID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// Single sign-on.

// redirectURL is where providers send people back to.
func (s *Server) redirectURL(r *http.Request) string {
	base := strings.TrimRight(s.PublicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || s.Secure {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/api/sso/callback"
}

func (s *Server) listProvidersPublic(w http.ResponseWriter, r *http.Request) error {
	ps, err := s.SSO.Enabled(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, 200, ps)
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	ps, err := s.SSO.List(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"providers": ps, "redirectUrl": s.redirectURL(r)})
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	var in sso.Input
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := s.SSO.Create(r.Context(), in)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, p)
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	var in sso.Input
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := s.SSO.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, p)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	if err := s.SSO.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

// testMapping shows what a provider's role mappings would give a sign-in
// with the given email and groups.
func (s *Server) testMapping(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return err
	}
	p, err := s.SSO.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	var in struct {
		Email  string         `json:"email"`
		Groups []string       `json:"groups"`
		Claims map[string]any `json:"claims"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	res := p.Evaluate(&sso.Identity{Email: strings.ToLower(in.Email), Groups: in.Groups, Claims: in.Claims})
	if res.Matched == nil {
		res.Matched = []int{}
	}
	return writeJSON(w, 200, res)
}

// ssoStart sends the browser to a provider's sign-in page.
func (s *Server) ssoStart(w http.ResponseWriter, r *http.Request) error {
	to, err := s.SSO.Start(r.Context(), r.PathValue("id"), s.redirectURL(r), r.URL.Query().Get("return"))
	if err != nil {
		s.ssoFail(w, r, sso.SafeReturn(r.URL.Query().Get("return")), err)
		return nil
	}
	http.Redirect(w, r, to, http.StatusFound)
	return nil
}

// ssoCallback finishes a sign-in when the provider sends the browser back.
func (s *Server) ssoCallback(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		msg := q.Get("error_description")
		if msg == "" {
			msg = e
		}
		// The state still says where the person came from.
		s.ssoFail(w, r, "/", &sso.SignInError{Message: "the provider stopped the sign-in: " + msg})
		return nil
	}
	ip := s.clientIP(r)
	if s.limiter.blocked(ip) {
		s.ssoFail(w, r, "/", &sso.SignInError{Message: "too many failed sign-ins; wait a few minutes"})
		return nil
	}
	res, err := s.SSO.Finish(r.Context(), q.Get("state"), q.Get("code"), s.redirectURL(r))
	if err != nil {
		s.limiter.fail(ip)
		s.ssoFail(w, r, "/", err)
		return nil
	}
	u, err := s.SSO.SignIn(r.Context(), res.Provider, res.Identity)
	if err != nil {
		s.ssoFail(w, r, res.ReturnTo, err)
		return nil
	}
	token, err := s.Auth.StartSession(r.Context(), u.ID)
	if err != nil {
		return err
	}
	s.setCookie(w, token, int(auth.SessionTTL/time.Second))
	s.Log.Info("single sign-on", "provider", res.Provider.Name, "user", u.Email)
	http.Redirect(w, r, res.ReturnTo, http.StatusFound)
	return nil
}

// ssoFail sends the browser back with a message the sign-in page shows.
func (s *Server) ssoFail(w http.ResponseWriter, r *http.Request, to string, err error) {
	msg := "the sign-in failed; the server log has the details"
	var se *sso.SignInError
	var ie *sso.InvalidError
	switch {
	case errors.As(err, &se):
		msg = se.Message
	case errors.As(err, &ie):
		msg = ie.Message
	case errors.Is(err, auth.ErrDisabled):
		msg = err.Error()
	case errors.Is(err, sso.ErrNotFound):
		msg = "this sign-in provider no longer exists"
	default:
		s.Log.Error("single sign-on failed", "err", err)
	}
	u, _ := url.Parse(sso.SafeReturn(to))
	q := u.Query()
	q.Set("sso_error", msg)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
