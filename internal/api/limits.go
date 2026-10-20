package api

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/metrics"
)

// rateLimiter gives each client a bucket of Burst requests that refills
// at Rate per second. It is per server: behind a load balancer with n
// servers a client may get up to n times the rate.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// allow takes a token from the client's bucket. When it is empty, wait says
// how long until the next token.
func (l *rateLimiter) allow(key string, rate float64, burst int, now time.Time) (ok bool, wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	if now.Sub(l.swept) > time.Minute {
		// Forget clients whose bucket is full again.
		for k, b := range l.buckets {
			if now.Sub(b.last).Seconds()*rate >= float64(burst) {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(burst), last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(float64(burst), b.tokens+now.Sub(b.last).Seconds()*rate)
	b.last = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / rate * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// windows counts requests per app in the current minute, per server.
type windows struct {
	mu sync.Mutex
	m  map[string]*window
}

type window struct {
	minute int64
	n      int
}

func (w *windows) hit(appID string, now time.Time) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m == nil {
		w.m = map[string]*window{}
	}
	m := now.Unix() / 60
	c := w.m[appID]
	if c == nil || c.minute != m {
		c = &window{minute: m}
		w.m[appID] = c
	}
	c.n++
	return c.n
}

func (w *windows) current(appID string, now time.Time) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c := w.m[appID]; c != nil && c.minute == now.Unix()/60 {
		return c.n
	}
	return 0
}

// usage remembers how many rows each app holds for a while, counting rows
// added since, so that row quotas need not count every table on every
// insert. Rows added through other servers show after usageTTL.
type usage struct {
	mu   sync.Mutex
	rows map[string]rowUsage
}

type rowUsage struct {
	n  int
	at time.Time
}

const usageTTL = 30 * time.Second

// rateKey names who a request counts against: its API key, its user, or
// else its address.
func (s *Server) rateKey(r *http.Request) string {
	if k := apiKeyFrom(r); k != nil {
		return "key:" + k.ID
	}
	if u := userFrom(r); u != nil {
		return "user:" + u.ID
	}
	return "ip:" + s.clientIP(r)
}

// clientIP is the address of the client. Behind a proxy it is the last
// address the proxy appended to X-Forwarded-For.
func (s *Server) clientIP(r *http.Request) string {
	if s.BehindProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimited answers 429 when the client is over the server's rate limit.
func (s *Server) rateLimited(w http.ResponseWriter, r *http.Request) bool {
	if s.RateLimit <= 0 || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
		return false
	}
	burst := s.RateBurst
	if burst <= 0 {
		burst = int(math.Max(1, s.RateLimit*2))
	}
	ok, wait := s.rate.allow(s.rateKey(r), s.RateLimit, burst, time.Now())
	if ok {
		return false
	}
	metrics.Default.Inc("bubibase_rate_limited_total", "limit", "client")
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeJSON(w, 429, map[string]any{"error": "too many requests; slow down and try again shortly", "code": "rate_limited"})
	return true
}

// appRequest counts a request to an app by someone using it and refuses
// it over the app's quota.
func (s *Server) appRequest(a *apps.App) error {
	n := s.requests.hit(a.ID, time.Now())
	if max := a.Quotas.RequestsPerMinute; max > 0 && n > max {
		metrics.Default.Inc("bubibase_quota_refused_total", "quota", "requests")
		return &apps.QuotaError{Message: fmt.Sprintf("this app has reached its limit of %d requests a minute; try again shortly", max), Rate: true}
	}
	return nil
}

// checkRows refuses rows over the app's row quota.
func (s *Server) checkRows(ctx context.Context, appID string, n int) error {
	a, err := s.Apps.Get(ctx, appID)
	if err != nil {
		return err
	}
	max := a.Quotas.MaxRows
	if max <= 0 {
		return nil
	}
	now := time.Now()
	s.usage.mu.Lock()
	u, ok := s.usage.rows[appID]
	s.usage.mu.Unlock()
	if !ok || now.Sub(u.at) > usageTTL {
		count, err := s.Tables.RowCount(ctx, appID)
		if err != nil {
			return err
		}
		u = rowUsage{n: count, at: now}
	}
	if u.n+n > max {
		metrics.Default.Inc("bubibase_quota_refused_total", "quota", "rows")
		return &apps.QuotaError{Message: fmt.Sprintf("this app may hold at most %d rows and holds %d; delete rows or ask an admin to raise the limit", max, u.n)}
	}
	u.n += n
	s.usage.mu.Lock()
	if s.usage.rows == nil {
		s.usage.rows = map[string]rowUsage{}
	}
	s.usage.rows[appID] = u
	s.usage.mu.Unlock()
	return nil
}

// checkFiles refuses files over the app's storage quota.
func (s *Server) checkFiles(ctx context.Context, appID string, size int64) error {
	a, err := s.Apps.Get(ctx, appID)
	if err != nil {
		return err
	}
	if a.Quotas.MaxFileMB <= 0 {
		return nil
	}
	_, used, err := s.Files.Usage(ctx, appID)
	if err != nil {
		return err
	}
	if max := int64(a.Quotas.MaxFileMB) << 20; used+size > max {
		metrics.Default.Inc("bubibase_quota_refused_total", "quota", "files")
		return &apps.QuotaError{Message: fmt.Sprintf("this app may store at most %d MB of files and holds %.1f MB", a.Quotas.MaxFileMB, float64(used)/(1<<20))}
	}
	return nil
}

// checkRuns refuses flow runs over the app's hourly quota.
func (s *Server) checkRuns(ctx context.Context, appID string) error {
	a, err := s.Apps.Get(ctx, appID)
	if err != nil {
		return err
	}
	max := a.Quotas.FlowRunsPerHour
	if max <= 0 {
		return nil
	}
	n, err := s.Flows.RunsSince(ctx, appID, time.Now().Add(-time.Hour))
	if err != nil {
		return err
	}
	if n >= max {
		metrics.Default.Inc("bubibase_quota_refused_total", "quota", "flow_runs")
		return &apps.QuotaError{Message: fmt.Sprintf("this app has started its limit of %d flow runs in the last hour", max), Rate: true}
	}
	return nil
}

// Usage is what an app uses against its quotas.
type Usage struct {
	Quotas             apps.Quotas `json:"quotas"`
	Rows               int         `json:"rows"`
	Files              int         `json:"files"`
	FileBytes          int64       `json:"fileBytes"`
	FlowRunsLastHour   int         `json:"flowRunsLastHour"`
	RequestsThisMinute int         `json:"requestsThisMinute"`
}

func (s *Server) appUsage(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	u := Usage{Quotas: a.Quotas, RequestsThisMinute: s.requests.current(a.ID, time.Now())}
	if u.Rows, err = s.Tables.RowCount(ctx, a.ID); err != nil {
		return err
	}
	if u.Files, u.FileBytes, err = s.Files.Usage(ctx, a.ID); err != nil {
		return err
	}
	if u.FlowRunsLastHour, err = s.Flows.RunsSince(ctx, a.ID, time.Now().Add(-time.Hour)); err != nil {
		return err
	}
	return writeJSON(w, 200, u)
}

func (s *Server) setQuotas(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireAdmin(r); err != nil {
		return errStatus(403, "only workspace owners and admins set app quotas")
	}
	a, err := s.Apps.Get(r.Context(), r.PathValue("app"))
	if err != nil {
		return err
	}
	var q apps.Quotas
	if err := readJSON(r, &q); err != nil {
		return err
	}
	a, err = s.Apps.SetQuotas(r.Context(), a.ID, q)
	if err != nil {
		return err
	}
	s.usage.mu.Lock()
	delete(s.usage.rows, a.ID)
	s.usage.mu.Unlock()
	return writeJSON(w, 200, a)
}
