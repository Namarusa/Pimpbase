// Package api serves Bubibase's HTTP API and the embedded builder.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bubibase/bubibase/internal/ai"
	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/auth"
	"github.com/bubibase/bubibase/internal/files"
	"github.com/bubibase/bubibase/internal/flows"
	"github.com/bubibase/bubibase/internal/queries"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/screens"
	"github.com/bubibase/bubibase/internal/sso"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/tables"
)

const sessionCookie = "bubibase_session"

type Server struct {
	Auth    *auth.Service
	Apps    *apps.Service
	Tables  *tables.Service
	Screens *screens.Service
	Queries *queries.Service
	Files   *files.Service
	Runner  *queries.Runner
	Flows   *flows.Service
	SSO     *sso.Service
	AI      *ai.Service
	// Engine runs flows; call Engine.Start to begin schedules.
	Engine *flows.Engine
	Log    *slog.Logger
	// UI is the built builder, served at /. It may be nil.
	UI fs.FS
	// Secure marks cookies as HTTPS only.
	Secure bool
	// PublicURL is the address people reach Bubibase at, such as
	// https://apps.example.com; single sign-on providers send people back
	// there. Empty uses the address of each request.
	PublicURL string

	limiter *limiter
}

func New(s *store.Store, ui fs.FS, log *slog.Logger) *Server {
	qs := queries.NewService(s)
	srv := &Server{
		Queries: qs,
		Runner:  queries.NewRunner(qs),
		Auth:    auth.NewService(s),
		Apps:    apps.NewService(s),
		Tables:  tables.NewService(s),
		Screens: screens.NewService(s),
		Files:   files.NewService(s),
		Flows:   flows.NewService(s),
		Log:     log,
		UI:      ui,
		limiter: &limiter{hits: map[string][]time.Time{}},
	}
	srv.SSO = sso.NewService(s, srv.Auth, srv.Apps)
	srv.AI = ai.NewService(s)
	srv.Engine = flows.NewEngine(srv.Flows, srv.Tables, srv.Queries, srv.Runner, srv.Files, srv.Apps, log)
	return srv
}

type ctxKey int

const (
	userKey ctxKey = iota
	apiKeyKey
)

func userFrom(r *http.Request) *auth.User {
	u, _ := r.Context().Value(userKey).(*auth.User)
	return u
}

// apiKeyFrom returns the API key the request was made with, if any.
func apiKeyFrom(r *http.Request) *auth.APIKey {
	k, _ := r.Context().Value(apiKeyKey).(*auth.APIKey)
	return k
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	h := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if err := fn(w, r); err != nil {
				s.writeError(w, r, err)
			}
		})
	}
	h("GET /api/health", func(w http.ResponseWriter, r *http.Request) error {
		return writeJSON(w, 200, map[string]any{"ok": true})
	})
	h("GET /api/setup", s.getSetup)
	h("POST /api/setup", s.postSetup)
	h("POST /api/auth/login", s.login)
	h("POST /api/auth/logout", s.logout)
	h("GET /api/auth/me", s.me)
	h("GET /api/auth/keys", s.listKeys)
	h("POST /api/auth/keys", s.createKey)
	h("DELETE /api/auth/keys/{id}", s.deleteKey)
	h("GET /api/auth/providers", s.listProvidersPublic)
	h("GET /api/sso/{id}/start", s.ssoStart)
	h("GET /api/sso/callback", s.ssoCallback)
	h("GET /api/sso/providers", s.listProviders)
	h("POST /api/sso/providers", s.createProvider)
	h("PATCH /api/sso/providers/{id}", s.updateProvider)
	h("DELETE /api/sso/providers/{id}", s.deleteProvider)
	h("POST /api/sso/providers/{id}/test", s.testMapping)

	h("GET /api/users", s.listUsers)
	h("POST /api/users", s.createUser)
	h("PATCH /api/users/{id}", s.updateUser)
	h("DELETE /api/users/{id}", s.deleteUser)

	h("GET /api/apps", s.listApps)
	h("POST /api/apps", s.createApp)
	h("GET /api/apps/{app}", s.getApp)
	h("PATCH /api/apps/{app}", s.updateApp)
	h("DELETE /api/apps/{app}", s.deleteApp)
	h("GET /api/apps/{app}/members", s.listMembers)
	h("PUT /api/apps/{app}/members/{user}", s.setMember)
	h("GET /api/apps/{app}/roles", s.listRoles)
	h("POST /api/apps/{app}/roles", s.createRole)
	h("PATCH /api/apps/{app}/roles/{role}", s.updateRole)
	h("DELETE /api/apps/{app}/roles/{role}", s.deleteRole)

	h("GET /api/apps/{app}/tables", s.listTables)
	h("POST /api/apps/{app}/tables", s.createTable)
	h("GET /api/apps/{app}/tables/{table}", s.getTable)
	h("PATCH /api/apps/{app}/tables/{table}", s.updateTable)
	h("DELETE /api/apps/{app}/tables/{table}", s.deleteTable)
	h("POST /api/apps/{app}/tables/{table}/fields", s.addField)
	h("PATCH /api/apps/{app}/tables/{table}/fields/{field}", s.updateField)
	h("DELETE /api/apps/{app}/tables/{table}/fields/{field}", s.deleteField)
	h("POST /api/apps/{app}/tables/{table}/formula/check", s.checkFormula)

	h("GET /api/apps/{app}/tables/{table}/rows", s.listRows)
	h("POST /api/apps/{app}/tables/{table}/rows", s.createRow)
	h("GET /api/apps/{app}/tables/{table}/rows/{id}", s.getRow)
	h("PATCH /api/apps/{app}/tables/{table}/rows/{id}", s.updateRow)
	h("DELETE /api/apps/{app}/tables/{table}/rows/{id}", s.deleteRow)
	h("POST /api/apps/{app}/tables/{table}/rows/delete", s.deleteRows)
	h("POST /api/apps/{app}/tables/{table}/import", s.importCSV)
	h("GET /api/apps/{app}/tables/{table}/export", s.exportCSV)
	h("POST /api/apps/{app}/tables/{table}/files", s.uploadFile)
	h("GET /api/apps/{app}/files/{file}", s.getFile)

	h("GET /api/apps/{app}/screens", s.listScreens)
	h("POST /api/apps/{app}/screens", s.createScreen)
	h("GET /api/apps/{app}/screens/{screen}", s.getScreen)
	h("PATCH /api/apps/{app}/screens/{screen}", s.updateScreen)
	h("DELETE /api/apps/{app}/screens/{screen}", s.deleteScreen)
	h("GET /api/apps/{app}/runtime", s.runtime)
	h("GET /api/apps/{app}/brand", s.brand)

	h("GET /api/apps/{app}/datasources", s.listDatasources)
	h("POST /api/apps/{app}/datasources", s.createDatasource)
	h("PATCH /api/apps/{app}/datasources/{ds}", s.updateDatasource)
	h("DELETE /api/apps/{app}/datasources/{ds}", s.deleteDatasource)
	h("GET /api/apps/{app}/queries", s.listQueries)
	h("POST /api/apps/{app}/queries", s.createQuery)
	h("POST /api/apps/{app}/queries/test", s.testQuery)
	h("GET /api/apps/{app}/queries/{query}", s.getQuery)
	h("PATCH /api/apps/{app}/queries/{query}", s.updateQuery)
	h("DELETE /api/apps/{app}/queries/{query}", s.deleteQuery)
	h("POST /api/apps/{app}/queries/{query}/run", s.runQuery)
	h("GET /api/apps/{app}/variables", s.listVariables)
	h("PUT /api/apps/{app}/variables/{name}", s.setVariable)
	h("DELETE /api/apps/{app}/variables/{name}", s.deleteVariable)

	h("GET /api/apps/{app}/flows", s.listFlows)
	h("POST /api/apps/{app}/flows", s.createFlow)
	h("GET /api/apps/{app}/flows/{flow}", s.getFlow)
	h("PATCH /api/apps/{app}/flows/{flow}", s.updateFlow)
	h("DELETE /api/apps/{app}/flows/{flow}", s.deleteFlow)
	h("POST /api/apps/{app}/flows/{flow}/hook", s.newHook)
	h("POST /api/apps/{app}/flows/{flow}/test", s.testFlow)
	h("POST /api/apps/{app}/flows/{flow}/run", s.runFlow)
	h("GET /api/apps/{app}/flows/{flow}/runs", s.flowRuns)
	h("GET /api/apps/{app}/runs/{run}", s.flowRun)
	h("GET /api/flows/schedule", s.nextRuns)
	h("POST /api/hooks/{token}", s.webhook)
	h("PUT /api/hooks/{token}", s.webhook)
	h("GET /api/hooks/{token}", s.webhook)

	h("GET /api/formula/functions", s.functions)

	h("GET /api/ai", s.aiStatus)
	h("PATCH /api/ai", s.aiUpdate)
	h("POST /api/apps/{app}/ai/draft", s.aiDraft)
	h("POST /api/apps/{app}/ai/formula", s.aiFormula)
	h("POST /api/apps/{app}/ai/ask", s.aiAsk)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"error": "no such API endpoint"})
	})
	if s.UI != nil {
		mux.Handle("/", spa(s.UI))
	}
	return s.middleware(mux)
}

// middleware identifies the user, blocks cross-site writes and logs.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		ctx := r.Context()
		viaCookie := false
		if key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			u, k, err := s.Auth.FromAPIKey(ctx, strings.TrimSpace(key))
			if err != nil {
				writeJSON(w, 401, map[string]any{"error": "invalid or expired API key"})
				return
			}
			ctx = context.WithValue(ctx, userKey, u)
			ctx = context.WithValue(ctx, apiKeyKey, k)
		} else if c, err := r.Cookie(sessionCookie); err == nil {
			if u, err := s.Auth.FromSession(ctx, c.Value); err == nil {
				ctx = context.WithValue(ctx, userKey, u)
				viaCookie = true
			}
		}
		if viaCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			writeJSON(w, 403, map[string]any{"error": "cross-site request blocked"})
			return
		}
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r.WithContext(ctx))
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.Log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

// sameOrigin rejects writes sent by other sites with the user's cookie.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Browsers send Origin on cross-site POSTs; fall back to Referer.
		origin = r.Header.Get("Referer")
		if origin == "" {
			return r.Header.Get("Sec-Fetch-Site") != "cross-site"
		}
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// httpError is an error with a status and a message safe to show.
type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errStatus(status int, msg string) error { return &httpError{status, msg} }

var errLogin = errStatus(401, "sign in first")

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var he *httpError
	var tv *tables.ValidationError
	var appErr *apps.InvalidError
	var au *auth.InvalidError
	var si *screens.InvalidError
	var qi *queries.InvalidError
	var qr *queries.RunError
	var fi *files.InvalidError
	var fl *flows.InvalidError
	var ri *rbac.InvalidError
	var ssoErr *sso.InvalidError
	var aiErr *ai.Error
	switch {
	case errors.Is(err, ai.ErrNoKey):
		writeJSON(w, 409, map[string]any{"error": err.Error(), "code": "ai_not_configured"})
	case errors.As(err, &aiErr):
		writeJSON(w, 400, map[string]any{"error": aiErr.Message})
	case errors.As(err, &ri):
		writeJSON(w, 400, map[string]any{"error": ri.Message})
	case errors.As(err, &ssoErr):
		writeJSON(w, 400, map[string]any{"error": ssoErr.Message, "field": ssoErr.Field})
	case errors.Is(err, auth.ErrDisabled):
		writeJSON(w, 403, map[string]any{"error": err.Error()})
	case errors.As(err, &he):
		writeJSON(w, he.status, map[string]any{"error": he.msg})
	case errors.As(err, &tv):
		writeJSON(w, 400, map[string]any{"error": tv.Error(), "field": tv.Field, "formulaErrors": tv.Formula})
	case errors.As(err, &appErr):
		writeJSON(w, 400, map[string]any{"error": appErr.Message})
	case errors.As(err, &au):
		writeJSON(w, 400, map[string]any{"error": au.Message})
	case errors.As(err, &si):
		writeJSON(w, 400, map[string]any{"error": si.Message})
	case errors.As(err, &fi):
		writeJSON(w, 400, map[string]any{"error": fi.Message})
	case errors.As(err, &fl):
		writeJSON(w, 400, map[string]any{"error": fl.Message, "step": fl.Step})
	case errors.As(err, &qi):
		writeJSON(w, 400, map[string]any{"error": qi.Message, "field": qi.Field})
	case errors.As(err, &qr):
		status := 502
		if qr.Binding {
			status = 400
		}
		writeJSON(w, status, map[string]any{"error": qr.Message})
	case errors.Is(err, rbac.ErrForbidden):
		if userFrom(r) == nil {
			writeJSON(w, 401, map[string]any{"error": "sign in first"})
			return
		}
		writeJSON(w, 403, map[string]any{"error": "you do not have access: " + strings.TrimPrefix(err.Error(), "forbidden: ")})
	case errors.Is(err, tables.ErrNotFound), errors.Is(err, apps.ErrNotFound), errors.Is(err, auth.ErrNotFound), errors.Is(err, screens.ErrNotFound), errors.Is(err, queries.ErrNotFound), errors.Is(err, files.ErrNotFound), errors.Is(err, flows.ErrNotFound), errors.Is(err, apps.ErrRoleNotFound), errors.Is(err, sso.ErrNotFound):
		writeJSON(w, 404, map[string]any{"error": err.Error()})
	case errors.Is(err, auth.ErrBadCredentials):
		writeJSON(w, 401, map[string]any{"error": err.Error()})
	case errors.Is(err, auth.ErrSetupDone):
		writeJSON(w, 409, map[string]any{"error": err.Error()})
	default:
		s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeJSON(w, 500, map[string]any{"error": "something went wrong; the server log has the details"})
	}
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return errStatus(400, "invalid JSON: "+err.Error())
	}
	return nil
}

// limiter slows down password guessing: 10 failures per IP per 10 minutes.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *limiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-10 * time.Minute)
	recent := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if t.After(cut) {
			recent = append(recent, t)
		}
	}
	l.hits[ip] = recent
	return len(recent) >= 10
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[ip] = append(l.hits[ip], time.Now())
}

// spa serves the builder's files, and index.html for client-side routes.
func spa(ui fs.FS) http.Handler {
	files := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := ui.Open(path); err == nil {
				f.Close()
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(ui, "index.html")
		if err != nil {
			http.Error(w, "the builder is not built", 404)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
