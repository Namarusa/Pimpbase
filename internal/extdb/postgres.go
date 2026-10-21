package extdb

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func init() { register(sqlDriver{postgres}) }

var postgres = &dialect{
	kind: "postgres", label: "PostgreSQL", port: "5432", driver: "pgx",
	dsn: func(s Settings) (string, error) {
		if s.URI != "" {
			return s.URI, nil
		}
		if s.Host == "" {
			return "", errorf("enter the database server's host name")
		}
		port := s.Port
		if port == "" {
			port = "5432"
		}
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(s.Host, port), Path: "/" + s.Database}
		if s.User != "" {
			u.User = url.UserPassword(s.User, s.Password)
		}
		q := url.Values{}
		switch s.SSL {
		case "disable":
			q.Set("sslmode", "disable")
		case "require":
			q.Set("sslmode", "require")
		case "verify":
			q.Set("sslmode", "verify-full")
		default:
			q.Set("sslmode", "prefer")
		}
		q.Set("application_name", "bubibase")
		q.Set("connect_timeout", "10")
		for _, o := range s.Options {
			if o.Key != "" {
				q.Set(o.Key, o.Value)
			}
		}
		u.RawQuery = q.Encode()
		return u.String(), nil
	},
	quote:    quoteWith(`"`, `"`),
	textType: "TEXT",
	boolTrue: func(col string) string { return "COALESCE(" + col + ", FALSE)" },
	contains: func(a, b string) string { return "(STRPOS(" + a + ", " + b + ") > 0)" },
	nullsLast: func(e string, desc bool) string {
		if desc {
			return e + " DESC NULLS LAST"
		}
		return e + " ASC NULLS LAST"
	},
	page:      limitOffset,
	charTypes: []string{"text", "varchar", "character varying", "char", "character", "bpchar", "citext", "name"},
	tables: func(ctx context.Context, db *sql.DB) ([]TableInfo, error) {
		return queryTables(ctx, db, `SELECT table_schema, table_name, table_type FROM information_schema.tables
			WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema NOT LIKE 'pg_toast%'
			ORDER BY table_schema = 'public' DESC, table_schema, table_name`)
	},
	describe: func(ctx context.Context, db *sql.DB, schema, name string) (*TableInfo, error) {
		t := &TableInfo{Schema: schema, Name: name, Kind: "table"}
		var kind string
		if err := db.QueryRowContext(ctx, `SELECT table_type FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2`, schema, name).Scan(&kind); err != nil {
			if err == sql.ErrNoRows {
				return t, nil
			}
			return nil, err
		}
		if strings.Contains(kind, "VIEW") {
			t.Kind = "view"
		}
		rows, err := db.QueryContext(ctx, `SELECT column_name, data_type, udt_name, is_nullable, COALESCE(column_default, ''),
				is_identity, is_generated, COALESCE(character_maximum_length, 0), is_updatable
			FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`, schema, name)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var enums []int
		for rows.Next() {
			var c Column
			var dataType, udt, nullable, def, identity, generated, updatable string
			var size int64
			if err := rows.Scan(&c.Name, &dataType, &udt, &nullable, &def, &identity, &generated, &size, &updatable); err != nil {
				return nil, err
			}
			c.Nullable = nullable == "YES"
			c.DBType = udt
			if dataType == "character varying" && size > 0 {
				c.DBType = "varchar"
			}
			c.Type, c.Binary, _ = mapType("postgres", udt, "", size)
			if dataType == "USER-DEFINED" {
				enums = append(enums, len(t.Columns))
			}
			if dataType == "ARRAY" {
				c.Type = "text"
			}
			c.Auto = def != "" || identity == "YES"
			c.Generated = generated == "ALWAYS" || updatable == "NO"
			if identity == "YES" || strings.HasPrefix(def, "nextval(") {
				c.Type = "autonumber"
			}
			t.Columns = append(t.Columns, c)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		rows.Close()
		for _, i := range enums {
			opts, err := queryStrings(ctx, db, `SELECT e.enumlabel FROM pg_type t JOIN pg_enum e ON e.enumtypid = t.oid WHERE t.typname = $1 ORDER BY e.enumsortorder`, t.Columns[i].DBType)
			if err != nil {
				return nil, err
			}
			if len(opts) > 0 {
				t.Columns[i].Type, t.Columns[i].Options = "single_select", opts
			}
		}
		pk, err := queryStrings(ctx, db, `SELECT kcu.column_name FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema AND tc.table_name = kcu.table_name
			WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = $1 AND tc.table_name = $2 ORDER BY kcu.ordinal_position`, schema, name)
		if err != nil {
			return nil, err
		}
		t.PrimaryKey = pk
		return t, nil
	},
	insert: func(ctx context.Context, src *sqlSource, t *Target, cols []string, args []any) (any, error) {
		tail := ""
		if t.Key != "" {
			tail = " RETURNING " + src.d.quote(t.Key)
		}
		q := src.insertSQL(t, cols, "", tail)
		if t.Key == "" {
			_, err := src.db.ExecContext(ctx, q, args...)
			return nil, err
		}
		var key any
		err := src.db.QueryRowContext(ctx, q, args...).Scan(&key)
		return key, err
	},
	defaultSchema: func(s Settings) string {
		if s.Schema != "" {
			return s.Schema
		}
		return "public"
	},
}
