package api

import (
	"net/http"

	"github.com/bubibase/bubibase/internal/screens"
)

// Screens, for builders.

func (s *Server) listScreens(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Screens.List(r.Context(), a.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

func (s *Server) createScreen(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in screens.Input
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if err := s.screenRoles(r, a.ID, in); err != nil {
		return err
	}
	sc, err := s.Screens.Create(r.Context(), a.ID, in)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, sc)
}

func (s *Server) getScreen(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	sc, err := s.Screens.Get(r.Context(), a.ID, r.PathValue("screen"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, sc)
}

func (s *Server) updateScreen(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in screens.Input
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if err := s.screenRoles(r, a.ID, in); err != nil {
		return err
	}
	sc, err := s.Screens.Update(r.Context(), a.ID, r.PathValue("screen"), in)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, sc)
}

func (s *Server) deleteScreen(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if err := s.Screens.Delete(r.Context(), a.ID, r.PathValue("screen")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

// runtime returns what the published app needs: its design, the screens
// and tables the visitor may see, and who they are. Anonymous visitors get
// the PUBLIC screens; with none, they are asked to sign in.
func (s *Server) runtime(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	all, err := s.Screens.List(r.Context(), a.ID)
	if err != nil {
		return err
	}
	visible := []*screens.Screen{}
	for _, sc := range all {
		if act.Has(sc.Role) {
			cp := *sc
			cp.Definition = screens.Prune(sc.Definition, act)
			visible = append(visible, &cp)
		}
	}
	if !act.SignedIn() && len(visible) == 0 {
		if userFrom(r) == nil {
			return errLogin
		}
		return errStatus(403, "you do not have access to this app; ask its builder for a role")
	}
	ts, err := s.Tables.List(r.Context(), a.ID)
	if err != nil {
		return err
	}
	readable := []any{}
	for _, t := range ts {
		if act.Has(t.Permissions.Read) {
			readable = append(readable, t)
		}
	}
	ext, err := s.extViews(r, a.ID, act)
	if err != nil {
		return err
	}
	for _, t := range ext {
		readable = append(readable, t)
	}
	qs, err := s.runtimeQueries(r, a.ID, act)
	if err != nil {
		return err
	}
	fl, err := s.runtimeFlows(r, a.ID, act)
	if err != nil {
		return err
	}
	var user any
	if u := userFrom(r); u != nil {
		user = map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": act.Role, "roles": act.Chain(), "groups": u.Groups}
	}
	out := map[string]any{
		"app": a, "role": act.Role, "builder": act.Builder, "user": user,
		"screens": visible, "tables": readable, "queries": qs, "flows": fl,
	}
	// Builders may preview the app as any of its roles.
	if u := userFrom(r); u != nil && u.IsBuilder() && !apiKeyFrom(r).Scoped() {
		set, err := s.Apps.Roles(r.Context(), a.ID)
		if err != nil {
			return err
		}
		out["roles"] = set.Defs()
		out["preview"] = !act.Builder
	}
	if userFrom(r) == nil {
		ps, err := s.SSO.Enabled(r.Context())
		if err != nil {
			return err
		}
		out["providers"] = ps
	}
	return writeJSON(w, 200, out)
}

// brand is what a published app's sign-in page shows: its name, look and
// the single sign-on providers, for anyone.
func (s *Server) brand(w http.ResponseWriter, r *http.Request) error {
	a, err := s.Apps.Get(r.Context(), r.PathValue("app"))
	if err != nil {
		return err
	}
	ps, err := s.SSO.Enabled(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{
		"name": a.Name, "slug": a.Slug, "design": a.Design, "providers": ps,
	})
}
