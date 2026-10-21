//go:build !nomysql

package extdb

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

func init() { register(sqlDriver{mysqlDialect}) }

// mysqlDialect serves MySQL and MariaDB.
var mysqlDialect = &dialect{
	kind: "mysql", label: "MySQL / MariaDB", port: "3306", driver: "mysql",
	dsn: func(s Settings) (string, error) {
		if s.URI != "" {
			return strings.TrimPrefix(s.URI, "mysql://"), nil
		}
		if s.Host == "" {
			return "", errorf("enter the database server's host name")
		}
		port := s.Port
		if port == "" {
			port = "3306"
		}
		c := mysql.NewConfig()
		c.User, c.Passwd = s.User, s.Password
		c.Net, c.Addr, c.DBName = "tcp", net.JoinHostPort(s.Host, port), s.Database
		c.ParseTime = true
		c.Loc = time.UTC
		c.Timeout = 10 * time.Second
		c.Params = map[string]string{"charset": "utf8mb4"}
		switch s.SSL {
		case "require":
			c.TLSConfig = "skip-verify"
		case "verify":
			c.TLSConfig = "true"
		case "disable":
			c.TLSConfig = "false"
		default:
			c.TLSConfig = "preferred"
		}
		for _, o := range s.Options {
			if o.Key != "" {
				c.Params[o.Key] = o.Value
			}
		}
		return c.FormatDSN(), nil
	},
	quote:    quoteWith("`", "`"),
	textType: "CHAR",
	boolTrue: func(col string) string { return "(COALESCE(" + col + ", 0) <> 0)" },
	contains: func(a, b string) string { return "(LOCATE(" + b + ", " + a + ") > 0)" },
	nullsLast: func(e string, desc bool) string {
		if desc {
			return "(" + e + " IS NULL), " + e + " DESC"
		}
		return "(" + e + " IS NULL), " + e + " ASC"
	},
	page:      limitOffset,
	charTypes: []string{"varchar", "char", "text", "tinytext", "mediumtext", "longtext", "enum", "set"},
	tables: func(ctx context.Context, db *sql.DB) ([]TableInfo, error) {
		return queryTables(ctx, db, `SELECT table_schema, table_name, table_type FROM information_schema.tables
			WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')
			ORDER BY table_schema = DATABASE() DESC, table_schema, table_name`)
	},
	describe: func(ctx context.Context, db *sql.DB, schema, name string) (*TableInfo, error) {
		t := &TableInfo{Schema: schema, Name: name, Kind: "table"}
		var kind string
		if err := db.QueryRowContext(ctx, `SELECT table_type FROM information_schema.tables WHERE table_schema = ? AND table_name = ?`, schema, name).Scan(&kind); err != nil {
			if err == sql.ErrNoRows {
				return t, nil
			}
			return nil, err
		}
		if strings.Contains(kind, "VIEW") {
			t.Kind = "view"
		}
		rows, err := db.QueryContext(ctx, `SELECT column_name, data_type, column_type, is_nullable, column_default IS NOT NULL, extra, column_key, COALESCE(character_maximum_length, 0)
			FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position`, schema, name)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c Column
			var dataType, columnType, nullable, extra, key string
			var hasDefault bool
			var size int64
			if err := rows.Scan(&c.Name, &dataType, &columnType, &nullable, &hasDefault, &extra, &key, &size); err != nil {
				return nil, err
			}
			c.Nullable = nullable == "YES"
			c.DBType = columnType
			c.Type, c.Binary, c.Options = mapType("mysql", dataType, columnType, size)
			extra = strings.ToLower(extra)
			c.Auto = hasDefault || strings.Contains(extra, "auto_increment")
			c.Generated = strings.Contains(extra, "generated")
			if strings.Contains(extra, "auto_increment") {
				c.Type = "autonumber"
			}
			if key == "PRI" {
				t.PrimaryKey = append(t.PrimaryKey, c.Name)
			}
			t.Columns = append(t.Columns, c)
		}
		return t, rows.Err()
	},
	foreignKeys: `SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY CONSTRAINT_NAME, ORDINAL_POSITION`,
	insert: func(ctx context.Context, src *sqlSource, t *Target, cols []string, args []any) ([]any, error) {
		res, err := src.db.ExecContext(ctx, src.insertSQL(t, cols, "", ""), args...)
		if err != nil {
			return nil, err
		}
		if len(t.Keys) == 0 {
			return nil, nil
		}
		// MySQL returns no row on insert: key values are the ones given,
		// and the one left out is the AUTO_INCREMENT id.
		key := make([]any, len(t.Keys))
		for i, k := range t.Keys {
			found := false
			for j, c := range cols {
				if c == k {
					key[i], found = args[j], true
				}
			}
			if !found {
				id, err := res.LastInsertId()
				if err != nil {
					return nil, err
				}
				key[i] = id
			}
		}
		return key, nil
	},
	defaultSchema: func(s Settings) string {
		if s.Schema != "" {
			return s.Schema
		}
		return s.Database
	},
}
