// Package store wraps the SQL databases Bubibase uses and hides the
// differences between SQLite and Postgres behind a small Dialect.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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
	return db.DB.QueryContext(ctx, db.Rebind(query), args...)
}

func (db *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
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
// dataDir: bubibase.db for metadata and apps/<id>.db per app. With a DSN, all
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
		db, err := openSQLite(filepath.Join(dataDir, "bubibase.db"))
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
	return &DB{DB: sqldb, Dialect: SQLite}, nil
}

func (s *Store) Dialect() Dialect { return s.dialect }

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
			return nil, err
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
