// Package extdb connects to databases an app's builder already runs
// (PostgreSQL, MySQL and MariaDB, SQL Server, MongoDB): it opens and
// pools connections, reads their schema, runs queries with every binding
// sent as a parameter, and reads and writes the rows of one table.
//
// Drivers are pure Go and compiled in; each one but PostgreSQL can be
// left out of the binary with a build tag (nomysql, nomssql, nomongo).
package extdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Settings say how to reach a database. Text values may hold bindings,
// usually {{ env.NAME }}; they are filled before a driver sees them.
type Settings struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	// Schema is where tables are looked for first, such as public or dbo.
	Schema string `json:"schema,omitempty"`
	// SSL is "", "disable", "require" or "verify" (check the certificate).
	SSL string `json:"ssl,omitempty"`
	// URI is a whole connection string; it wins over the fields above.
	URI string `json:"uri,omitempty"`
	// Options are extra driver parameters, added to the connection string.
	Options []Option `json:"options"`
	// MaxConnections caps the pool; 0 means 5.
	MaxConnections int `json:"maxConnections,omitempty"`
	// ReadOnly refuses writes through tables and queries.
	ReadOnly bool `json:"readOnly,omitempty"`
}

// Option is one extra connection parameter.
type Option struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Text lists the settings that may hold bindings, for filling them.
func (s *Settings) Text() []*string {
	out := []*string{&s.Host, &s.Port, &s.Database, &s.User, &s.Password, &s.Schema, &s.URI}
	for i := range s.Options {
		out = append(out, &s.Options[i].Value)
	}
	return out
}

// Error messages are safe to show to builders.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func errorf(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

var ErrReadOnly = errors.New("this data source is read only")

// Driver opens connections of one kind.
type Driver interface {
	Kind() string
	// Label names the kind for people, such as "PostgreSQL".
	Label() string
	DefaultPort() string
	Open(s Settings) (Source, error)
}

var drivers = map[string]Driver{}

func register(d Driver) { drivers[d.Kind()] = d }

// Kinds lists the compiled-in drivers.
func Kinds() []Driver {
	out := make([]Driver, 0, len(drivers))
	for _, d := range drivers {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind() < out[j].Kind() })
	return out
}

// Known reports whether kind is a database kind, compiled in or not.
func Known(kind string) bool {
	switch kind {
	case "postgres", "mysql", "mssql", "mongodb":
		return true
	}
	return false
}

// Lookup finds a compiled-in driver.
func Lookup(kind string) (Driver, error) {
	d, ok := drivers[kind]
	if !ok {
		if Known(kind) {
			return nil, errorf("this Bubibase was built without the %s connector", kind)
		}
		return nil, errorf("there is no %q connector", kind)
	}
	return d, nil
}

// Source is an open, pooled connection to one database.
type Source interface {
	Ping(ctx context.Context) error
	// Tables lists the tables, views and collections there are.
	Tables(ctx context.Context) ([]TableInfo, error)
	// Describe reads one table's columns and primary key.
	Describe(ctx context.Context, schema, name string) (*TableInfo, error)
	// Run sends a query the builder wrote.
	Run(ctx context.Context, q Statement) (*Result, error)
	// Rows reads, counts and writes the rows of one table.
	Rows() Rows
	Close() error
}

// TableInfo describes a table, view or collection.
type TableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	// Kind is "table", "view" or "collection".
	Kind       string   `json:"kind"`
	Columns    []Column `json:"columns,omitempty"`
	PrimaryKey []string `json:"primaryKey,omitempty"`
}

// Column is one column of a table, with the field type Bubibase gives it.
type Column struct {
	Name string `json:"name"`
	// Type is a Bubibase field type: text, longtext, number, boolean,
	// date, single_select or autonumber.
	Type string `json:"type"`
	// DBType is the database's own type, such as varchar(80).
	DBType   string   `json:"dbType"`
	Nullable bool     `json:"nullable"`
	Options  []string `json:"options,omitempty"`
	// Auto columns are filled by the database (identity, serial, a
	// default) and may be left out on insert.
	Auto bool `json:"auto,omitempty"`
	// Generated columns are computed by the database and never written.
	Generated bool `json:"generated,omitempty"`
	// Binary columns hold bytes and are not shown.
	Binary bool `json:"binary,omitempty"`
}

// Statement is a query with its parameters.
type Statement struct {
	SQL  string
	Args []any
	// Mongo queries.
	Collection string
	Operation  string
	Filter     string // JSON with bindings filled
	Document   string
	Options    string
	// MaxRows caps the rows read.
	MaxRows int
}

// Result is what a query gave back.
type Result struct {
	Rows []map[string]any
	// Columns are the column names in order.
	Columns      []string
	RowsAffected int64
	// Truncated is true when more rows than MaxRows came back.
	Truncated bool
	// Query says whether rows were read (true) or a statement executed.
	Query bool
}

// Pool keeps one Source per data source and setting, so connections are
// reused across requests. A data source whose settings change gets a new
// pool and the old one is closed.
type Pool struct {
	mu      sync.Mutex
	sources map[string]*pooled
}

type pooled struct {
	key  string
	src  Source
	used time.Time
}

func NewPool() *Pool { return &Pool{sources: map[string]*pooled{}} }

// Get returns the Source for a data source id with the given (filled)
// settings, opening it on first use.
func (p *Pool) Get(id, kind string, s Settings) (Source, error) {
	d, err := Lookup(kind)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d", kind, s.Host, s.Port, s.Database, s.User, s.Password, s.Schema, s.SSL, s.URI, s.MaxConnections)
	for _, o := range s.Options {
		fmt.Fprintf(h, "\x00%s=%s", o.Key, o.Value)
	}
	key := hex.EncodeToString(h.Sum(nil))
	slot := id + "\x00" + kind
	p.mu.Lock()
	defer p.mu.Unlock()
	if cur, ok := p.sources[slot]; ok {
		if cur.key == key {
			cur.used = time.Now()
			return cur.src, nil
		}
		cur.src.Close()
		delete(p.sources, slot)
	}
	src, err := d.Open(s)
	if err != nil {
		return nil, err
	}
	p.sources[slot] = &pooled{key: key, src: src, used: time.Now()}
	return src, nil
}

// Forget closes the pools of a data source, such as when it is deleted.
func (p *Pool) Forget(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for slot, cur := range p.sources {
		if strings.HasPrefix(slot, id+"\x00") {
			cur.src.Close()
			delete(p.sources, slot)
		}
	}
}

// Close closes every pool.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for slot, cur := range p.sources {
		cur.src.Close()
		delete(p.sources, slot)
	}
}

// Test opens a fresh connection with the settings, pings it and closes it.
func Test(ctx context.Context, kind string, s Settings) (*TestResult, error) {
	d, err := Lookup(kind)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	src, err := d.Open(s)
	if err != nil {
		return &TestResult{OK: false, Message: err.Error()}, nil
	}
	defer src.Close()
	if err := src.Ping(ctx); err != nil {
		return &TestResult{OK: false, Message: Clean(err), DurationMs: time.Since(start).Milliseconds()}, nil
	}
	n := -1
	if ts, err := src.Tables(ctx); err == nil {
		n = len(ts)
	}
	return &TestResult{OK: true, Message: "connected", Tables: n, DurationMs: time.Since(start).Milliseconds()}, nil
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	Tables     int    `json:"tables"`
	DurationMs int64  `json:"durationMs"`
}

// Clean makes a driver error readable: one line, without the driver's
// package prefix.
func Clean(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for _, p := range []string{"mssql: ", "Error 0: ", "pq: "} {
		msg = strings.TrimPrefix(msg, p)
	}
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 400 {
		msg = msg[:400] + "…"
	}
	return msg
}
