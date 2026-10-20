package api

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/bubibase/bubibase/internal/backup"
	"github.com/bubibase/bubibase/internal/cluster"
	"github.com/bubibase/bubibase/internal/metrics"
	"github.com/bubibase/bubibase/internal/store"
)

var (
	inFlight  atomic.Int64
	startedAt = time.Now()
)

// Drain makes /readyz fail, so load balancers stop sending requests while
// the server finishes the ones it has.
func (s *Server) Drain() { s.draining.Store(true) }

func (s *Server) describeMetrics(st *store.Store) {
	m := metrics.Default
	m.Describe("bubibase_http_requests_total", "counter", "HTTP requests answered, by route and status class.")
	m.Describe("bubibase_http_request_duration_seconds", "histogram", "Time to answer HTTP requests, by route.")
	m.Describe("bubibase_http_in_flight", "gauge", "HTTP requests being answered.")
	m.Describe("bubibase_rate_limited_total", "counter", "Requests refused by the per-client rate limit.")
	m.Describe("bubibase_quota_refused_total", "counter", "Work refused by app quotas, by quota.")
	m.Describe("bubibase_flow_runs_total", "counter", "Flow runs finished, by status.")
	m.Describe("bubibase_flow_run_duration_seconds", "histogram", "Time flow runs took.")
	m.Describe("bubibase_db_connections", "gauge", "Connections of the metadata database pool, by state.")
	m.Describe("bubibase_db_wait_seconds_total", "counter", "Time spent waiting for a free database connection.")
	m.Describe("bubibase_instances", "gauge", "Servers sharing the database that are alive.")
	m.Describe("bubibase_build_info", "gauge", "Always 1; the labels name the build.")
	m.Describe("go_goroutines", "gauge", "Goroutines.")
	m.Describe("go_memstats_heap_alloc_bytes", "gauge", "Heap bytes in use.")
	m.Describe("process_start_time_seconds", "gauge", "When the process started, in Unix seconds.")
	m.OnCollect(func(m *metrics.Registry) {
		m.Set("bubibase_http_in_flight", float64(inFlight.Load()))
		stats := st.Meta.Stats()
		m.Set("bubibase_db_connections", float64(stats.InUse), "state", "in_use")
		m.Set("bubibase_db_connections", float64(stats.Idle), "state", "idle")
		m.Set("bubibase_db_wait_seconds_total", stats.WaitDuration.Seconds())
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		m.Set("go_goroutines", float64(runtime.NumGoroutine()))
		m.Set("go_memstats_heap_alloc_bytes", float64(ms.HeapAlloc))
		m.Set("process_start_time_seconds", float64(startedAt.Unix()))
		m.Set("bubibase_build_info", 1, "version", s.Version, "storage", string(st.Dialect()))
		if s.Node != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if alive, err := s.Node.Alive(ctx); err == nil {
				m.Set("bubibase_instances", float64(len(alive)))
			}
		}
	})
}

// healthz says the process is up; it never touches the database.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, 200, map[string]any{"ok": true, "version": s.Version})
}

// readyz says whether the server should get traffic: the database answers
// and it is not shutting down.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) error {
	if s.draining.Load() {
		return writeJSON(w, 503, map[string]any{"ok": false, "error": "shutting down"})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Meta.PingContext(ctx); err != nil {
		s.Log.Error("readiness check failed", "err", err)
		return writeJSON(w, 503, map[string]any{"ok": false, "error": "the database does not answer"})
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) error {
	if s.MetricsToken != "" && r.Header.Get("Authorization") != "Bearer "+s.MetricsToken {
		return errStatus(401, "send the metrics token as a Bearer token")
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	return metrics.Default.Write(w)
}

// status tells admins how the server is set up and which servers share
// its database.
func (s *Server) status(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return errStatus(403, "only workspace owners and admins see the server status")
	}
	out := map[string]any{
		"version": s.Version, "storage": s.Files.StorageName(), "database": string(s.store.Dialect()),
		"startedAt": startedAt.UTC(), "rateLimit": s.RateLimit, "rateBurst": s.RateBurst,
		"backups": s.store.Dialect() == store.SQLite, "instances": []cluster.Instance{},
	}
	if s.Node != nil {
		alive, err := s.Node.Alive(r.Context())
		if err != nil {
			return err
		}
		out["instances"] = alive
	}
	return writeJSON(w, 200, out)
}

// downloadBackup sends a zip of every SQLite database, taken while the
// server keeps running.
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return errStatus(403, "only workspace owners and admins download backups")
	}
	if s.store.Dialect() != store.SQLite {
		return errStatus(400, "this server keeps its data in Postgres; back it up with pg_dump or your provider's snapshots")
	}
	name := fmt.Sprintf("bubibase-%s.zip", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if _, err := backup.Write(r.Context(), s.store.DataDir(), w); err != nil {
		// Headers are gone; the broken zip tells the browser it failed.
		s.Log.Error("backup failed", "err", err)
	}
	return nil
}
