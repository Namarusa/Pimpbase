package integrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pimpbase/pimpbase/internal/cluster"
	"github.com/pimpbase/pimpbase/internal/files"
	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/tables"
)

// Connection is an app's account with a service: its settings, the
// secrets kept for it, and its syncs. Test runs of flows use the test
// values where they are set (a Stripe test-mode key), and the others
// otherwise.
type Connection struct {
	ID    string `json:"id"`
	AppID string `json:"appId"`
	// Kind is the manifest id, such as "stripe".
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Settings are the values that are not secret; TestSettings replace
	// them in test runs.
	Settings     map[string]string `json:"settings"`
	TestSettings map[string]string `json:"testSettings"`
	// Secrets says which secret values are set, never what they are.
	Secrets map[string]SecretState `json:"secrets"`
	// Hints shows the start and end of secret values that identify an
	// account without revealing it, such as "sk_test_…4242".
	Hints     map[string]SecretHint `json:"hints"`
	Syncs     []*SyncConfig         `json:"syncs"`
	HookToken string                `json:"hookToken"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`

	secrets     map[string]string
	testSecrets map[string]string
}

// SecretState says whether a secret is set for live and test runs.
type SecretState struct {
	Live bool `json:"live"`
	Test bool `json:"test"`
}

// SecretHint is a shortened secret, safe to show.
type SecretHint struct {
	Live string `json:"live,omitempty"`
	Test string `json:"test,omitempty"`
}

// SyncConfig is one sync a connection runs.
type SyncConfig struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	// Table is the table rows go to, made by the first sync.
	Table string `json:"table"`
	// Every is how often it runs by itself, in minutes; 0 runs it only
	// when asked (and from webhook events, with Live).
	Every  int               `json:"every"`
	Live   bool              `json:"live"`
	Params map[string]string `json:"params"`
	// Fields are the fields found by detection, beyond the manifest's.
	Fields    []SyncField `json:"fields,omitempty"`
	LastRun   string      `json:"lastRun,omitempty"`
	LastOK    bool        `json:"lastOk"`
	LastError string      `json:"lastError,omitempty"`
	// LastCount is how many items the last run read; Created, Updated
	// and Deleted what it did with them.
	LastCount int `json:"lastCount"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
}

// Environments.
const (
	Live = "production"
	Test = "development"
)

// Service keeps connections and runs their actions, webhooks and syncs.
type Service struct {
	store   *store.Store
	Catalog *Catalog
	Tables  *tables.Service
	Files   *files.Service
	Client  *http.Client
	Log     *slog.Logger
	// Node, when set, makes sure one server runs each scheduled sync.
	Node *cluster.Node
	now  func() time.Time
	// sleep waits between retries; tests make it instant.
	sleep func(ctx context.Context, d time.Duration) error

	mu     sync.Mutex
	tokens map[string]*cachedToken
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewService(s *store.Store, cat *Catalog, ts *tables.Service, fs *files.Service, log *slog.Logger) *Service {
	return &Service{
		store: s, Catalog: cat, Tables: ts, Files: fs, Log: log, now: time.Now,
		Client: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		}},
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		tokens: map[string]*cachedToken{},
	}
}

var (
	ErrNotFound = errors.New("not found")
	nameRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.()-]{0,62}$`)
)

// InvalidError messages are safe to show.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{fmt.Sprintf(format, args...)}
}

type storedConfig struct {
	Settings     map[string]string `json:"settings"`
	TestSettings map[string]string `json:"testSettings"`
	Syncs        []*SyncConfig     `json:"syncs"`
}

type storedSecrets struct {
	Live map[string]string `json:"live"`
	Test map[string]string `json:"test"`
}

const connCols = `id, app_id, kind, name, config, secrets, hook_token, created_at, updated_at`

func stamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func scanConn(row interface{ Scan(...any) error }) (*Connection, error) {
	c := &Connection{}
	var cfg, sec, created, updated string
	if err := row.Scan(&c.ID, &c.AppID, &c.Kind, &c.Name, &cfg, &sec, &c.HookToken, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("connection %w", ErrNotFound)
		}
		return nil, err
	}
	var sc storedConfig
	json.Unmarshal([]byte(cfg), &sc)
	var ss storedSecrets
	json.Unmarshal([]byte(sec), &ss)
	c.Settings, c.TestSettings, c.Syncs = orEmpty(sc.Settings), orEmpty(sc.TestSettings), sc.Syncs
	if c.Syncs == nil {
		c.Syncs = []*SyncConfig{}
	}
	c.secrets, c.testSecrets = orEmpty(ss.Live), orEmpty(ss.Test)
	c.fillStates()
	c.CreatedAt, c.UpdatedAt = stamp(created), stamp(updated)
	return c, nil
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (c *Connection) fillStates() {
	c.Secrets, c.Hints = map[string]SecretState{}, map[string]SecretHint{}
	for k, v := range c.secrets {
		st := c.Secrets[k]
		st.Live = v != ""
		c.Secrets[k] = st
		h := c.Hints[k]
		h.Live = hint(v)
		c.Hints[k] = h
	}
	for k, v := range c.testSecrets {
		st := c.Secrets[k]
		st.Test = v != ""
		c.Secrets[k] = st
		h := c.Hints[k]
		h.Test = hint(v)
		c.Hints[k] = h
	}
}

// hint keeps a secret's recognisable prefix (such as sk_test_) and its
// last four characters.
func hint(v string) string {
	if len(v) < 16 {
		return ""
	}
	pre := ""
	if i := strings.LastIndex(v[:min(len(v)/2, 12)], "_"); i > 0 {
		pre = v[:i+1]
	}
	return pre + "…" + v[len(v)-4:]
}

// Values are the settings and secrets a run in env uses.
func (c *Connection) Values(env string) map[string]string {
	out := map[string]string{}
	for k, v := range c.Settings {
		out[k] = v
	}
	for k, v := range c.secrets {
		out[k] = v
	}
	if env == Test {
		for k, v := range c.TestSettings {
			if v != "" {
				out[k] = v
			}
		}
		for k, v := range c.testSecrets {
			if v != "" {
				out[k] = v
			}
		}
	}
	return out
}

// SecretValues lists the connection's secrets, to hide from logs.
func (c *Connection) SecretValues() []string {
	var out []string
	for _, m := range []map[string]string{c.secrets, c.testSecrets} {
		for _, v := range m {
			if len(v) >= 3 {
				out = append(out, v)
			}
		}
	}
	return out
}

// Missing lists the required settings with no value for env.
func (c *Connection) Missing(m *Manifest, env string) []string {
	vals := c.Values(env)
	var out []string
	for _, cr := range m.Credentials {
		if cr.Required && strings.TrimSpace(vals[cr.Key]) == "" && cr.Default == "" {
			out = append(out, cr.Label)
		}
	}
	return out
}

// Sync returns the connection's settings for a sync, or nil.
func (c *Connection) Sync(key string) *SyncConfig {
	for _, s := range c.Syncs {
		if s.Key == key {
			return s
		}
	}
	return nil
}

func (s *Service) list(ctx context.Context, where string, args ...any) ([]*Connection, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT `+connCols+` FROM app_integrations WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Connection{}
	for rows.Next() {
		c, err := scanConn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Connections lists an app's connections.
func (s *Service) Connections(ctx context.Context, appID string) ([]*Connection, error) {
	return s.list(ctx, `app_id = ?`, appID)
}

// Connection finds one by id, or by name ignoring case.
func (s *Service) Connection(ctx context.Context, appID, idOrName string) (*Connection, error) {
	return scanConn(s.store.Meta.QueryRow(ctx, `SELECT `+connCols+` FROM app_integrations WHERE app_id = ? AND (id = ? OR LOWER(name) = LOWER(?)) ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`, appID, idOrName, idOrName, idOrName))
}

// ByHook finds the connection a webhook address names.
func (s *Service) ByHook(ctx context.Context, token string) (*Connection, error) {
	return scanConn(s.store.Meta.QueryRow(ctx, `SELECT `+connCols+` FROM app_integrations WHERE hook_token = ?`, token))
}

// ConnInput is what a builder sends to make or change a connection. For
// Secrets and TestSecrets, a key left out keeps its value and an empty
// value removes it.
type ConnInput struct {
	Kind         string            `json:"kind"`
	Name         *string           `json:"name"`
	Settings     map[string]string `json:"settings"`
	TestSettings map[string]string `json:"testSettings"`
	Secrets      map[string]string `json:"secrets"`
	TestSecrets  map[string]string `json:"testSecrets"`
	Syncs        []*SyncConfig     `json:"syncs"`
}

func (s *Service) apply(c *Connection, m *Manifest, in ConnInput) error {
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if !nameRe.MatchString(c.Name) {
		return invalid("a connection name starts with a letter or digit and uses letters, digits, spaces and . _ ( ) - (up to 63)")
	}
	put := func(dst map[string]string, src map[string]string, secret bool) error {
		for k, v := range src {
			cr := m.Credential(k)
			if cr == nil {
				return invalid("%s has no setting %q", m.Name, k)
			}
			if cr.Secret != secret {
				if secret {
					return invalid("%s is not a secret setting", cr.Label)
				}
				return invalid("%s is secret; send it with the secrets", cr.Label)
			}
			v = strings.TrimSpace(v)
			if v == "" {
				delete(dst, k)
				continue
			}
			if cr.Pattern != "" && !regexp.MustCompile(cr.Pattern).MatchString(v) {
				return invalid("%s does not look right%s", cr.Label, map[bool]string{true: " (" + cr.Placeholder + ")", false: ""}[cr.Placeholder != ""])
			}
			if len(v) > 64<<10 {
				return invalid("%s is too long", cr.Label)
			}
			dst[k] = v
		}
		return nil
	}
	if err := put(c.Settings, in.Settings, false); err != nil {
		return err
	}
	if err := put(c.TestSettings, in.TestSettings, false); err != nil {
		return err
	}
	if err := put(c.secrets, in.Secrets, true); err != nil {
		return err
	}
	if err := put(c.testSecrets, in.TestSecrets, true); err != nil {
		return err
	}
	if in.Syncs != nil {
		keep := map[string]*SyncConfig{}
		for _, sc := range c.Syncs {
			keep[sc.Key] = sc
		}
		out := []*SyncConfig{}
		seen := map[string]bool{}
		for _, sc := range in.Syncs {
			if sc == nil || m.Sync(sc.Key) == nil {
				return invalid("%s has no sync %q", m.Name, sc.Key)
			}
			if seen[sc.Key] {
				return invalid("the sync %s is listed twice", sc.Key)
			}
			seen[sc.Key] = true
			if sc.Every < 0 || (sc.Every > 0 && sc.Every < 5) || sc.Every > 7*24*60 {
				return invalid("a sync runs at most every 5 minutes, and at least once a week")
			}
			if sc.Params == nil {
				sc.Params = map[string]string{}
			}
			// What a sync did is the server's to say.
			if old := keep[sc.Key]; old != nil {
				sc.LastRun, sc.LastOK, sc.LastError, sc.LastCount = old.LastRun, old.LastOK, old.LastError, old.LastCount
				sc.Created, sc.Updated, sc.Deleted = old.Created, old.Updated, old.Deleted
				if sc.Table == "" {
					sc.Table = old.Table
				}
				if sc.Fields == nil {
					sc.Fields = old.Fields
				}
			}
			out = append(out, sc)
		}
		c.Syncs = out
	}
	return nil
}

func (s *Service) save(ctx context.Context, c *Connection, create bool) error {
	cfg, _ := json.Marshal(storedConfig{Settings: c.Settings, TestSettings: c.TestSettings, Syncs: c.Syncs})
	sec, _ := json.Marshal(storedSecrets{Live: c.secrets, Test: c.testSecrets})
	now := s.now().UTC().Format(time.RFC3339Nano)
	var n int
	if err := s.store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM app_integrations WHERE app_id = ? AND LOWER(name) = LOWER(?) AND id <> ?`, c.AppID, c.Name, c.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return invalid("another connection is already named %s", c.Name)
	}
	if create {
		_, err := s.store.Meta.Exec(ctx, `INSERT INTO app_integrations (`+connCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.AppID, c.Kind, c.Name, string(cfg), string(sec), c.HookToken, now, now)
		return err
	}
	_, err := s.store.Meta.Exec(ctx, `UPDATE app_integrations SET name = ?, config = ?, secrets = ?, updated_at = ? WHERE app_id = ? AND id = ?`,
		c.Name, string(cfg), string(sec), now, c.AppID, c.ID)
	return err
}

// Create adds a connection.
func (s *Service) Create(ctx context.Context, appID string, in ConnInput) (*Connection, error) {
	m := s.Catalog.Get(in.Kind)
	if m == nil {
		return nil, invalid("there is no integration %q", in.Kind)
	}
	c := &Connection{
		ID: ids.New("int"), AppID: appID, Kind: m.ID, Name: m.Name, HookToken: ids.Secret("ihook"),
		Settings: map[string]string{}, TestSettings: map[string]string{}, Syncs: []*SyncConfig{},
		secrets: map[string]string{}, testSecrets: map[string]string{},
	}
	if in.Name == nil {
		// A second Stripe account becomes "Stripe 2".
		list, err := s.Connections(ctx, appID)
		if err != nil {
			return nil, err
		}
		taken := map[string]bool{}
		for _, x := range list {
			taken[strings.ToLower(x.Name)] = true
		}
		for i := 2; taken[strings.ToLower(c.Name)]; i++ {
			c.Name = fmt.Sprintf("%s %d", m.Name, i)
		}
	}
	for _, cr := range m.Credentials {
		if cr.Default != "" && !cr.Secret {
			c.Settings[cr.Key] = cr.Default
		}
	}
	if err := s.apply(c, m, in); err != nil {
		return nil, err
	}
	if err := s.save(ctx, c, true); err != nil {
		return nil, err
	}
	return s.Connection(ctx, appID, c.ID)
}

// Update changes a connection.
func (s *Service) Update(ctx context.Context, appID, id string, in ConnInput) (*Connection, error) {
	c, err := s.Connection(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	m := s.Catalog.Get(c.Kind)
	if m == nil {
		return nil, invalid("the integration %q is no longer installed", c.Kind)
	}
	if err := s.apply(c, m, in); err != nil {
		return nil, err
	}
	if err := s.save(ctx, c, false); err != nil {
		return nil, err
	}
	s.forgetToken(c.ID)
	return s.Connection(ctx, appID, c.ID)
}

// saveSyncs writes back what syncs did, keeping other changes made
// meanwhile.
func (s *Service) saveSync(ctx context.Context, appID, id string, done *SyncConfig) error {
	c, err := s.Connection(ctx, appID, id)
	if err != nil {
		return err
	}
	for i, sc := range c.Syncs {
		if sc.Key == done.Key {
			cp := *done
			cp.Enabled, cp.Every, cp.Live, cp.Params = sc.Enabled, sc.Every, sc.Live, sc.Params
			c.Syncs[i] = &cp
		}
	}
	return s.save(ctx, c, false)
}

// NewHookToken gives a connection a new webhook address.
func (s *Service) NewHookToken(ctx context.Context, appID, id string) (*Connection, error) {
	res, err := s.store.Meta.Exec(ctx, `UPDATE app_integrations SET hook_token = ? WHERE app_id = ? AND id = ?`, ids.Secret("ihook"), appID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("connection %w", ErrNotFound)
	}
	return s.Connection(ctx, appID, id)
}

// Delete removes a connection and its event log.
func (s *Service) Delete(ctx context.Context, appID, id string) error {
	if _, err := s.store.Meta.Exec(ctx, `DELETE FROM integration_events WHERE connection_id = ?`, id); err != nil {
		return err
	}
	res, err := s.store.Meta.Exec(ctx, `DELETE FROM app_integrations WHERE app_id = ? AND id = ?`, appID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("connection %w", ErrNotFound)
	}
	s.forgetToken(id)
	return nil
}

// Secrets lists the secret values of an app's connections, longest
// first, so flow logs can hide them.
func (s *Service) Secrets(ctx context.Context, appID string) ([]string, error) {
	list, err := s.Connections(ctx, appID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range list {
		out = append(out, c.SecretValues()...)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out, nil
}
