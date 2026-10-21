// Package store wraps the SQL databases Pimpbase uses and hides the
// differences between SQLite and Postgres behind a small Dialect.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Dialect names the SQL flavour of a database.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

// DB is a database handle that knows its dialect. Queries are written with
// "?" placeholders and rebound for Postgres.
type DB struct {
	*sql.DB
	Dialect Dialect
	// schema qualifies table names on Postgres, where every app's data
	// lives in its own schema. It is empty for SQLite.
	schema string
	// read, on SQLite, is a pool of read-only connections. The one write
	// connection serializes writers; in WAL mode readers need not wait.
	read *sql.DB
}

// readOnly reports whether a statement only reads, so it may use the
// read pool.
func readOnly(query string) bool {
	q := strings.TrimLeft(query, " \t\r\n(")
	if len(q) < 6 {
		return false
	}
	head := strings.ToUpper(q[:6])
	return (head == "SELECT" || strings.HasPrefix(head, "WITH ")) && !strings.Contains(strings.ToUpper(query), "RETURNING")
}

// Close closes the handle and its read pool.
func (db *DB) Close() error {
	if db.read != nil {
		db.read.Close()
	}
	return db.DB.Close()
}

// Q quotes an identifier.
func Q(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Table returns the quoted, schema-qualified name of a table.
func (db *DB) Table(name string) string {
	if db.schema != "" {
		return Q(db.schema) + "." + Q(name)
	}
	return Q(name)
}

// Rebind converts "?" placeholders to "$n" for Postgres. Question marks
// inside single-quoted string literals are left alone.
func (db *DB) Rebind(query string) string {
	if db.Dialect != Postgres {
		return query
	}
	var b strings.Builder
	n, inStr := 0, false
	for _, r := range query {
		switch {
		case r == '\'':
			inStr = !inStr
			b.WriteRune(r)
		case r == '?' && !inStr:
			n++
			b.WriteString("$" + strconv.Itoa(n))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (db *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.DB.ExecContext(ctx, db.Rebind(query), args...)
}

func (db *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if db.read != nil && readOnly(query) {
		return db.read.QueryContext(ctx, query, args...)
	}
	return db.DB.QueryContext(ctx, db.Rebind(query), args...)
}

func (db *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	if db.read != nil && readOnly(query) {
		return db.read.QueryRowContext(ctx, query, args...)
	}
	return db.DB.QueryRowContext(ctx, db.Rebind(query), args...)
}

// Tx is a transaction on a DB, with the same helpers.
type Tx struct {
	*sql.Tx
	db *DB
}

func (db *DB) Begin(ctx context.Context) (*Tx, error) {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, db: db}, nil
}

func (tx *Tx) DB() *DB { return tx.db }

func (tx *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, tx.db.Rebind(query), args...)
}

func (tx *Tx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.Tx.QueryContext(ctx, tx.db.Rebind(query), args...)
}

func (tx *Tx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.Tx.QueryRowContext(ctx, tx.db.Rebind(query), args...)
}

// Lock takes a lock that servers sharing the database hold one at a time,
// until unlock is called. SQLite databases have one server, so there it
// does nothing.
func (db *DB) Lock(ctx context.Context, key string) (unlock func(), err error) {
	if db.Dialect != Postgres {
		return func() {}, nil
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1))`, key); err != nil {
		conn.Close()
		return nil, err
	}
	return func() {
		conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, key)
		conn.Close()
	}, nil
}

// Lock holds a lock like DB.Lock until the transaction ends.
func (tx *Tx) Lock(ctx context.Context, key string) error {
	if tx.db.Dialect != Postgres {
		return nil
	}
	_, err := tx.Tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key)
	return err
}

// Execer is satisfied by both *DB and *Tx.
type Execer interface {
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) *sql.Row
}

// Types used in DDL, per dialect.
func (db *DB) TypeText() string { return "TEXT" }
func (db *DB) TypeReal() string {
	if db.Dialect == Postgres {
		return "DOUBLE PRECISION"
	}
	return "REAL"
}
func (db *DB) TypeInt() string {
	if db.Dialect == Postgres {
		return "BIGINT"
	}
	return "INTEGER"
}
func (db *DB) TypeBlob() string {
	if db.Dialect == Postgres {
		return "BYTEA"
	}
	return "BLOB"
}
func (db *DB) TypeSerialPK() string {
	if db.Dialect == Postgres {
		return "BIGSERIAL PRIMARY KEY"
	}
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

// Store owns the metadata database and opens per-app data databases.
type Store struct {
	Meta    *DB
	dialect Dialect
	dataDir string
	dsn     string

	mu   sync.Mutex
	apps map[string]*DB
}

// Open opens the store. With an empty postgresDSN it uses SQLite files under
// dataDir: pimpbase.db for metadata and apps/<id>.db per app. With a DSN, all
// data goes to that Postgres database, one schema per app.
func Open(ctx context.Context, dataDir, postgresDSN string) (*Store, error) {
	s := &Store{dataDir: dataDir, dsn: postgresDSN, apps: map[string]*DB{}}
	if postgresDSN != "" {
		s.dialect = Postgres
		sqldb, err := sql.Open("pgx", postgresDSN)
		if err != nil {
			return nil, err
		}
		if err := sqldb.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("connect to postgres: %w", err)
		}
		s.Meta = &DB{DB: sqldb, Dialect: Postgres}
	} else {
		s.dialect = SQLite
		if err := os.MkdirAll(filepath.Join(dataDir, "apps"), 0o755); err != nil {
			return nil, err
		}
		db, err := openSQLite(filepath.Join(dataDir, "pimpbase.db"))
		if err != nil {
			return nil, err
		}
		s.Meta = db
	}
	if err := migrateMeta(ctx, s.Meta); err != nil {
		return nil, fmt.Errorf("migrate metadata: %w", err)
	}
	return s, nil
}

func openSQLite(path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids lock contention.
	sqldb.SetMaxOpenConns(1)
	// Make the file and switch it to WAL before readers open it.
	if err := sqldb.Ping(); err != nil {
		sqldb.Close()
		return nil, err
	}
	read, err := sql.Open("sqlite", "file:"+path+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		sqldb.Close()
		return nil, err
	}
	n := min(runtime.NumCPU(), 8)
	read.SetMaxOpenConns(n)
	read.SetMaxIdleConns(n)
	return &DB{DB: sqldb, Dialect: SQLite, read: read}, nil
}

func (s *Store) Dialect() Dialect { return s.dialect }

// DataDir is the folder of the SQLite databases; empty on Postgres.
func (s *Store) DataDir() string {
	if s.dialect == Postgres {
		return ""
	}
	return s.dataDir
}

// AppDB returns the data database of an app, creating it if needed.
func (s *Store) AppDB(ctx context.Context, appID string) (*DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if db, ok := s.apps[appID]; ok {
		return db, nil
	}
	var db *DB
	if s.dialect == Postgres {
		schema := "app_" + appID
		if _, err := s.Meta.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+Q(schema)); err != nil {
			// Another server may have created it at the same moment.
			if _, err := s.Meta.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+Q(schema)); err != nil {
				return nil, err
			}
		}
		db = &DB{DB: s.Meta.DB, Dialect: Postgres, schema: schema}
	} else {
		var err error
		db, err = openSQLite(filepath.Join(s.dataDir, "apps", appID+".db"))
		if err != nil {
			return nil, err
		}
	}
	s.apps[appID] = db
	return db, nil
}

// DropApp deletes all data of an app.
func (s *Store) DropApp(ctx context.Context, appID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	db := s.apps[appID]
	delete(s.apps, appID)
	if s.dialect == Postgres {
		_, err := s.Meta.Exec(ctx, "DROP SCHEMA IF EXISTS "+Q("app_"+appID)+" CASCADE")
		return err
	}
	if db != nil {
		db.Close()
	}
	base := filepath.Join(s.dataDir, "apps", appID+".db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(base + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialect == SQLite {
		for _, db := range s.apps {
			db.Close()
		}
	}
	return s.Meta.Close()
}
