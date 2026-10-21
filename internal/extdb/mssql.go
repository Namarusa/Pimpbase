//go:build !nomssql

package extdb

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
)

func init() { register(sqlDriver{mssql}) }

var mssql = &dialect{
	kind: "mssql", label: "SQL Server", port: "1433", driver: "sqlserver",
	dsn: func(s Settings) (string, error) {
		if s.URI != "" {
			return s.URI, nil
		}
		if s.Host == "" {
			return "", errorf("enter the database server's host name")
		}
		port := s.Port
		if port == "" {
			port = "1433"
		}
		u := url.URL{Scheme: "sqlserver", Host: net.JoinHostPort(s.Host, port)}
		if s.User != "" {
			u.User = url.UserPassword(s.User, s.Password)
		}
		q := url.Values{}
		if s.Database != "" {
			q.Set("database", s.Database)
		}
		switch s.SSL {
		case "disable":
			q.Set("encrypt", "disable")
		case "require":
			q.Set("encrypt", "true")
			q.Set("TrustServerCertificate", "true")
		case "verify":
			q.Set("encrypt", "true")
		default:
			q.Set("encrypt", "false")
			q.Set("TrustServerCertificate", "true")
		}
		q.Set("app name", "bubibase")
		q.Set("dial timeout", "10")
		for _, o := range s.Options {
			if o.Key != "" {
				q.Set(o.Key, o.Value)
			}
		}
		u.RawQuery = q.Encode()
		return u.String(), nil
	},
	quote:    quoteWith("[", "]"),
	textType: "NVARCHAR(4000)",
	boolTrue: func(col string) string { return "(COALESCE(" + col + ", 0) = 1)" },
	contains: func(a, b string) string { return "(CHARINDEX(" + b + ", " + a + ") > 0)" },
	nullsLast: func(e string, desc bool) string {
		if desc {
			return "CASE WHEN " + e + " IS NULL THEN 1 ELSE 0 END, " + e + " DESC"
		}
		return "CASE WHEN " + e + " IS NULL THEN 1 ELSE 0 END, " + e + " ASC"
	},
	page: func(limit, offset int) string {
		return fmt.Sprintf("OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, limit)
	},
	charTypes: []string{"varchar", "nvarchar", "char", "nchar"},
	tables: func(ctx context.Context, db *sql.DB) ([]TableInfo, error) {
		return queryTables(ctx, db, `SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES
			WHERE TABLE_SCHEMA NOT IN ('sys', 'INFORMATION_SCHEMA')
			ORDER BY CASE WHEN TABLE_SCHEMA = SCHEMA_NAME() THEN 0 ELSE 1 END, TABLE_SCHEMA, TABLE_NAME`)
	},
	describe: func(ctx context.Context, db *sql.DB, schema, name string) (*TableInfo, error) {
		t := &TableInfo{Schema: schema, Name: name, Kind: "table"}
		var kind string
		if err := db.QueryRowContext(ctx, `SELECT TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2`, schema, name).Scan(&kind); err != nil {
			if err == sql.ErrNoRows {
				return t, nil
			}
			return nil, err
		}
		if strings.Contains(kind, "VIEW") {
			t.Kind = "view"
		}
		rows, err := db.QueryContext(ctx, `SELECT c.COLUMN_NAME, c.DATA_TYPE, c.IS_NULLABLE, CASE WHEN c.COLUMN_DEFAULT IS NULL THEN 0 ELSE 1 END,
				COALESCE(COLUMNPROPERTY(OBJECT_ID(QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME)), c.COLUMN_NAME, 'IsIdentity'), 0),
				COALESCE(COLUMNPROPERTY(OBJECT_ID(QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME)), c.COLUMN_NAME, 'IsComputed'), 0),
				COALESCE(c.CHARACTER_MAXIMUM_LENGTH, 0)
			FROM INFORMATION_SCHEMA.COLUMNS c WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2 ORDER BY c.ORDINAL_POSITION`, schema, name)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c Column
			var dataType, nullable string
			var hasDefault, identity, computed int
			var size int64
			if err := rows.Scan(&c.Name, &dataType, &nullable, &hasDefault, &identity, &computed, &size); err != nil {
				return nil, err
			}
			c.Nullable = nullable == "YES"
			c.DBType = dataType
			c.Type, c.Binary, _ = mapType("mssql", dataType, "", size)
			c.Auto = hasDefault == 1 || identity == 1
			// Identity and computed columns are never written; neither is
			// a rowversion.
			c.Generated = identity == 1 || computed == 1 || dataType == "timestamp" || dataType == "rowversion"
			if dataType == "timestamp" {
				c.Type, c.Binary = "text", true
			}
			if identity == 1 {
				c.Type = "autonumber"
			}
			t.Columns = append(t.Columns, c)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		rows.Close()
		pk, err := queryStrings(ctx, db, `SELECT kcu.COLUMN_NAME FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA AND tc.TABLE_NAME = kcu.TABLE_NAME
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY' AND tc.TABLE_SCHEMA = @p1 AND tc.TABLE_NAME = @p2 ORDER BY kcu.ORDINAL_POSITION`, schema, name)
		if err != nil {
			return nil, err
		}
		t.PrimaryKey = pk
		return t, nil
	},
	foreignKeys: `SELECT fk.name, pc.name, SCHEMA_NAME(rt.schema_id), rt.name, rc.name
		FROM sys.foreign_keys fk
		JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
		JOIN sys.tables pt ON pt.object_id = fkc.parent_object_id
		JOIN sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
		JOIN sys.tables rt ON rt.object_id = fkc.referenced_object_id
		JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
		WHERE SCHEMA_NAME(pt.schema_id) = ? AND pt.name = ?
		ORDER BY fk.name, fkc.constraint_column_id`,
	insert: func(ctx context.Context, src *sqlSource, t *Target, cols []string, args []any) ([]any, error) {
		mid := ""
		if len(t.Keys) > 0 {
			mid = " OUTPUT " + src.returning(t, "INSERTED.")
		}
		q := src.insertSQL(t, cols, mid, "")
		if len(t.Keys) == 0 {
			_, err := src.db.ExecContext(ctx, q, args...)
			return nil, err
		}
		return scanKey(src.db.QueryRowContext(ctx, q, args...), len(t.Keys))
	},
	defaultSchema: func(s Settings) string {
		if s.Schema != "" {
			return s.Schema
		}
		return "dbo"
	},
}
