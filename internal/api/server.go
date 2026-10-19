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

	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/auth"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/screens"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/internal/tables"
)

const sessionCookie = "bubibase_session"

type Server struct {
	Auth    *auth.Service
	Apps    *apps.Service
	Tables  *tables.Service
	Screens *screens.Service
	Log     *slog.Logger
	// UI is the built builder, served at /. It may be nil.
	UI fs.FS
	// Secure marks cookies as HTTPS only.
	Secure bool

	limiter *limiter
}

func New(s *store.Store, ui fs.FS, log *slog.Logger) *Server {
	return &Server{
		Auth:    auth.NewService(s),
		Apps:    apps.NewService(s),
		Tables:  tables.NewService(s),
		Screens: screens.NewService(s),
		Log:     log,
		UI:      ui,
		limiter: &limiter{hits: map[string][]time.Time{}},
	}
}

type ctxKey int

const userKey ctxKey = 0

func userFrom(r *http.Request) *auth.User {
	u, _ := r.Context().Value(userKey).(*auth.User)
	return u
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

	h("GET /api/apps/{app}/screens", s.listScreens)
	h("POST /api/apps/{app}/screens", s.createScreen)
	h("GET /api/apps/{app}/screens/{screen}", s.getScreen)
	h("PATCH /api/apps/{app}/screens/{screen}", s.updateScreen)
	h("DELETE /api/apps/{app}/screens/{screen}", s.deleteScreen)
	h("GET /api/apps/{app}/runtime", s.runtime)

	h("GET /api/formula/functions", s.functions)
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
			u, err := s.Auth.FromAPIKey(ctx, strings.TrimSpace(key))
			if err != nil {
				writeJSON(w, 401, map[string]any{"error": "invalid API key"})
				return
			}
			ctx = context.WithValue(ctx, userKey, u)
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
	var ai *apps.InvalidError
	var au *auth.InvalidError
	var si *screens.InvalidError
	switch {
	case errors.As(err, &he):
		writeJSON(w, he.status, map[string]any{"error": he.msg})
	case errors.As(err, &tv):
		writeJSON(w, 400, map[string]any{"error": tv.Error(), "field": tv.Field, "formulaErrors": tv.Formula})
	case errors.As(err, &ai):
		writeJSON(w, 400, map[string]any{"error": ai.Message})
	case errors.As(err, &au):
		writeJSON(w, 400, map[string]any{"error": au.Message})
	case errors.As(err, &si):
		writeJSON(w, 400, map[string]any{"error": si.Message})
	case errors.Is(err, rbac.ErrForbidden):
		if userFrom(r) == nil {
			writeJSON(w, 401, map[string]any{"error": "sign in first"})
			return
		}
		writeJSON(w, 403, map[string]any{"error": "you do not have access: " + strings.TrimPrefix(err.Error(), "forbidden: ")})
	case errors.Is(err, tables.ErrNotFound), errors.Is(err, apps.ErrNotFound), errors.Is(err, auth.ErrNotFound), errors.Is(err, screens.ErrNotFound):
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
