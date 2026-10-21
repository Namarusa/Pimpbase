package extdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/formula"
)

// dialect is what differs between the SQL databases.
type dialect struct {
	kind, label, port string
	driver            string
	dsn               func(s Settings) (string, error)
	quote             func(name string) string
	// textType is the type CAST(x AS …) uses to make text.
	textType string
	// boolTrue is a condition true when a boolean column is true.
	boolTrue func(col string) string
	// contains is a condition true when text b is inside text a.
	contains func(a, b string) string
	// nullsLast orders by e with blanks last.
	nullsLast func(e string, desc bool) string
	// page limits rows; it follows ORDER BY.
	page func(limit, offset int) string
	// charTypes are the column types that hold text as text.
	charTypes []string
	tables    func(ctx context.Context, db *sql.DB) ([]TableInfo, error)
	describe  func(ctx context.Context, db *sql.DB, schema, name string) (*TableInfo, error)
	// foreignKeys lists the foreign keys of a table as rows of constraint
	// name, column, and the schema, table and column it references.
	foreignKeys string
	// insert adds a row and returns its key.
	insert func(ctx context.Context, src *sqlSource, t *Target, cols []string, args []any) ([]any, error)
	// defaultSchema is the schema of a table named without one.
	defaultSchema func(s Settings) string
}

type sqlDriver struct{ d *dialect }

func (s sqlDriver) Kind() string        { return s.d.kind }
func (s sqlDriver) Label() string       { return s.d.label }
func (s sqlDriver) DefaultPort() string { return s.d.port }

func (s sqlDriver) Open(set Settings) (Source, error) {
	dsn, err := s.d.dsn(set)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(s.d.driver, dsn)
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	n := set.MaxConnections
	if n <= 0 {
		n = 5
	}
	db.SetMaxOpenConns(n)
	db.SetMaxIdleConns(n)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &sqlSource{d: s.d, db: db, set: set}, nil
}

type sqlSource struct {
	d   *dialect
	db  *sql.DB
	set Settings
}

func (s *sqlSource) Close() error { return s.db.Close() }
func (s *sqlSource) Rows() Rows   { return s }

func (s *sqlSource) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

func (s *sqlSource) Tables(ctx context.Context) ([]TableInfo, error) {
	ts, err := s.d.tables(ctx, s.db)
	if err != nil {
		return nil, errorf("reading the tables failed: %s", Clean(err))
	}
	return ts, nil
}

func (s *sqlSource) Describe(ctx context.Context, schema, name string) (*TableInfo, error) {
	if schema == "" {
		schema = s.d.defaultSchema(s.set)
	}
	t, err := s.d.describe(ctx, s.db, schema, name)
	if err != nil {
		return nil, errorf("reading %s failed: %s", name, Clean(err))
	}
	if len(t.Columns) == 0 {
		return nil, errorf("there is no table %s.%s, or it has no columns you may read", schema, name)
	}
	if err := s.readForeignKeys(ctx, t); err != nil {
		return nil, errorf("reading the foreign keys of %s failed: %s", name, Clean(err))
	}
	return t, nil
}

// readForeignKeys marks the columns that reference another table. Foreign
// keys over several columns are left out: links follow one column.
func (s *sqlSource) readForeignKeys(ctx context.Context, t *TableInfo) error {
	if s.d.foreignKeys == "" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(s.d.foreignKeys), t.Schema, t.Name)
	if err != nil {
		return err
	}
	defer rows.Close()
	type fk struct {
		col string
		ref Reference
	}
	byName := map[string][]fk{}
	var names []string
	for rows.Next() {
		var name string
		var f fk
		if err := rows.Scan(&name, &f.col, &f.ref.Schema, &f.ref.Table, &f.ref.Column); err != nil {
			return err
		}
		if byName[name] == nil {
			names = append(names, name)
		}
		byName[name] = append(byName[name], f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, n := range names {
		if fs := byName[n]; len(fs) == 1 {
			for i := range t.Columns {
				if t.Columns[i].Name == fs[0].col && t.Columns[i].References == nil {
					ref := fs[0].ref
					t.Columns[i].References = &ref
				}
			}
		}
	}
	return nil
}

// rebind numbers "?" placeholders for the dialect, skipping quoted text.
func (s *sqlSource) rebind(q string) string {
	if s.d.kind == "mysql" {
		return q
	}
	var b strings.Builder
	n := 0
	var quote byte
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case quote != 0:
			if c == quote || quote == '[' && c == ']' {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '[' || c == '`':
			quote = c
		case c == '?':
			n++
			b.WriteString(placeholder(s.d.kind, n))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func (s *sqlSource) Run(ctx context.Context, q Statement) (*Result, error) {
	max := q.MaxRows
	if max <= 0 {
		max = 5000
	}
	if !QueryLike(q.SQL) {
		if s.set.ReadOnly {
			return nil, ErrReadOnly
		}
		res, err := s.db.ExecContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		n, _ := res.RowsAffected()
		return &Result{Rows: []map[string]any{}, Columns: []string{}, RowsAffected: n}, nil
	}
	var rows *sql.Rows
	var err error
	if s.set.ReadOnly {
		// The database refuses writes too, where it can.
		tx, terr := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: s.d.kind != "mssql"})
		if terr != nil {
			return nil, errorf("%s", Clean(terr))
		}
		defer tx.Rollback()
		rows, err = tx.QueryContext(ctx, q.SQL, q.Args...)
	} else {
		rows, err = s.db.QueryContext(ctx, q.SQL, q.Args...)
	}
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	defer rows.Close()
	out, err := s.scan(rows, nil, max)
	if err != nil {
		return nil, err
	}
	out.Query = true
	return out, nil
}

// scan reads rows, by the given columns or else by the database's column
// types.
func (s *sqlSource) scan(rows *sql.Rows, cols []Column, max int) (*Result, error) {
	names, err := rows.Columns()
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	if cols == nil {
		types, _ := rows.ColumnTypes()
		cols = make([]Column, len(names))
		for i, n := range names {
			cols[i] = Column{Name: n, Type: "text"}
			if i < len(types) {
				t := types[i].DatabaseTypeName()
				cols[i].DBType = strings.ToLower(t)
				cols[i].Type, cols[i].Binary, _ = mapType(s.d.kind, t, "", 0)
			}
		}
	}
	res := &Result{Rows: []map[string]any{}, Columns: names}
	vals := make([]any, len(names))
	ptrs := make([]any, len(names))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if max > 0 && len(res.Rows) >= max {
			res.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, errorf("%s", Clean(err))
		}
		row := make(map[string]any, len(names))
		for i, n := range names {
			row[n] = Read(vals[i], &cols[i])
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errorf("%s", Clean(err))
	}
	return res, nil
}

func (s *sqlSource) table(t *Target) string {
	schema := t.Schema
	if schema == "" {
		schema = s.d.defaultSchema(s.set)
	}
	if schema == "" {
		return s.d.quote(t.Name)
	}
	return s.d.quote(schema) + "." + s.d.quote(t.Name)
}

func (s *sqlSource) Count(ctx context.Context, t *Target, where *Cond) (int, error) {
	q := "SELECT COUNT(*) FROM " + s.table(t)
	var args []any
	if where != nil {
		q += " WHERE " + where.SQL
		args = where.Args
	}
	var n int
	if err := s.db.QueryRowContext(ctx, s.rebind(q), args...).Scan(&n); err != nil {
		return 0, errorf("%s", Clean(err))
	}
	return n, nil
}

func (s *sqlSource) Select(ctx context.Context, t *Target, sel Select) ([]map[string]any, error) {
	var cols []Column
	var names []string
	for _, c := range t.Columns {
		if c.Binary {
			continue
		}
		cols = append(cols, c)
		names = append(names, s.d.quote(c.Name))
	}
	if len(names) == 0 {
		return []map[string]any{}, nil
	}
	q := "SELECT " + strings.Join(names, ", ") + " FROM " + s.table(t)
	var args []any
	if sel.Where != nil {
		q += " WHERE " + sel.Where.SQL
		args = sel.Where.Args
	}
	var order []string
	for _, o := range sel.Order {
		order = append(order, s.d.nullsLast(s.d.quote(o.Column), o.Desc))
	}
	// The key breaks ties, so pages do not overlap.
	dir := ""
	if len(sel.Order) > 0 && sel.Order[0].Desc {
		dir = " DESC"
	}
	for _, k := range t.Keys {
		ordered := false
		for _, o := range sel.Order {
			ordered = ordered || o.Column == k
		}
		if !ordered {
			order = append(order, s.d.quote(k)+dir)
		}
	}
	if len(order) == 0 && s.d.kind == "mssql" && sel.Limit > 0 {
		order = append(order, "(SELECT NULL)")
	}
	if len(order) > 0 {
		q += " ORDER BY " + strings.Join(order, ", ")
	}
	if sel.Limit > 0 {
		q += " " + s.d.page(sel.Limit, sel.Offset)
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	defer rows.Close()
	res, err := s.scan(rows, cols, 0)
	if err != nil {
		return nil, err
	}
	return res.Rows, nil
}

func (s *sqlSource) Insert(ctx context.Context, t *Target, values map[string]any) ([]any, error) {
	if s.set.ReadOnly {
		return nil, ErrReadOnly
	}
	var cols []string
	var args []any
	for _, c := range t.Columns {
		if v, ok := values[c.Name]; ok && !c.Generated {
			cols = append(cols, c.Name)
			args = append(args, v)
		}
	}
	key, err := s.d.insert(ctx, s, t, cols, args)
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	for i := range key {
		if kc := t.Column(t.Keys[i]); kc != nil {
			key[i] = Read(key[i], kc)
		}
	}
	return key, nil
}

// keyWhere is k1 = ? AND k2 = ? for the key columns.
func (s *sqlSource) keyWhere(t *Target) string {
	parts := make([]string, len(t.Keys))
	for i, k := range t.Keys {
		parts[i] = s.d.quote(k) + " = ?"
	}
	return strings.Join(parts, " AND ")
}

// returning lists the key columns for RETURNING or OUTPUT, with a prefix
// before each.
func (s *sqlSource) returning(t *Target, prefix string) string {
	parts := make([]string, len(t.Keys))
	for i, k := range t.Keys {
		parts[i] = prefix + s.d.quote(k)
	}
	return strings.Join(parts, ", ")
}

// scanKey reads one row of key values.
func scanKey(row interface{ Scan(...any) error }, n int) ([]any, error) {
	key := make([]any, n)
	ptrs := make([]any, n)
	for i := range key {
		ptrs[i] = &key[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		return nil, err
	}
	return key, nil
}

// insertSQL is INSERT INTO t (a, b) VALUES (?, ?), with extra text between
// the columns and VALUES for SQL Server's OUTPUT.
func (s *sqlSource) insertSQL(t *Target, cols []string, mid, tail string) string {
	q := "INSERT INTO " + s.table(t)
	if len(cols) == 0 {
		if s.d.kind == "mysql" {
			return q + " () VALUES ()" + tail
		}
		return q + mid + " DEFAULT VALUES" + tail
	}
	quoted := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = s.d.quote(c)
		marks[i] = "?"
	}
	return s.rebind(q + " (" + strings.Join(quoted, ", ") + ")" + mid + " VALUES (" + strings.Join(marks, ", ") + ")" + tail)
}

func (s *sqlSource) Update(ctx context.Context, t *Target, key []any, values map[string]any) error {
	if s.set.ReadOnly {
		return ErrReadOnly
	}
	var sets []string
	var args []any
	for _, c := range t.Columns {
		if v, ok := values[c.Name]; ok && !c.Generated && !t.IsKey(c.Name) {
			sets = append(sets, s.d.quote(c.Name)+" = ?")
			args = append(args, v)
		}
	}
	if len(sets) == 0 {
		return nil
	}
	if len(key) != len(t.Keys) || len(key) == 0 {
		return ErrNoRow
	}
	args = append(args, key...)
	res, err := s.db.ExecContext(ctx, s.rebind("UPDATE "+s.table(t)+" SET "+strings.Join(sets, ", ")+" WHERE "+s.keyWhere(t)), args...)
	if err != nil {
		return errorf("%s", Clean(err))
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 && s.d.kind != "mysql" {
		return ErrNoRow
	}
	return nil
}

// ErrNoRow is a key that matches no row.
var ErrNoRow = fmt.Errorf("row not found")

func (s *sqlSource) Delete(ctx context.Context, t *Target, keys [][]any) (int, error) {
	if s.set.ReadOnly {
		return 0, ErrReadOnly
	}
	if len(t.Keys) == 0 {
		return 0, nil
	}
	// A single key is matched with IN; several with (a = ? AND b = ?) OR …,
	// in chunks that stay under every driver's parameter limit.
	chunk := 500 / len(t.Keys)
	total := 0
	for start := 0; start < len(keys); start += chunk {
		end := min(start+chunk, len(keys))
		var where string
		var args []any
		if len(t.Keys) == 1 {
			where = s.d.quote(t.Keys[0]) + " IN (" + strings.TrimSuffix(strings.Repeat("?, ", end-start), ", ") + ")"
			for _, k := range keys[start:end] {
				args = append(args, k[0])
			}
		} else {
			one := "(" + s.keyWhere(t) + ")"
			parts := make([]string, 0, end-start)
			for _, k := range keys[start:end] {
				if len(k) != len(t.Keys) {
					continue
				}
				parts = append(parts, one)
				args = append(args, k...)
			}
			if len(parts) == 0 {
				continue
			}
			where = strings.Join(parts, " OR ")
		}
		res, err := s.db.ExecContext(ctx, s.rebind("DELETE FROM "+s.table(t)+" WHERE "+where), args...)
		if err != nil {
			return total, errorf("%s", Clean(err))
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, nil
}

func (s *sqlSource) In(t *Target, column string, values []any) *Cond {
	if len(values) == 0 {
		return &Cond{SQL: "1 = 0"}
	}
	args := make([]any, 0, len(values))
	if c := t.Column(column); c != nil {
		for _, v := range values {
			if x, err := Write(v, c); err == nil {
				args = append(args, x)
			}
		}
	} else {
		args = append(args, values...)
	}
	if len(args) == 0 {
		return &Cond{SQL: "1 = 0"}
	}
	return &Cond{SQL: s.d.quote(column) + " IN (" + strings.TrimSuffix(strings.Repeat("?, ", len(args)), ", ") + ")", Args: args}
}

func (s *sqlSource) Compile(t *Target, c *formula.Checked, env formula.Env) (*Cond, bool) {
	comp := &sqlCompiler{d: s.d, t: t, c: c, env: env}
	e, ok := comp.pred(c.Root)
	if !ok {
		return nil, false
	}
	return &Cond{SQL: e.sql, Args: e.args}, true
}

// mapType gives the field type of a database column type. udt is
// PostgreSQL's type name, size a character limit (-1 for MAX).
func mapType(kind, dataType, columnType string, size int64) (typ string, binary bool, options []string) {
	t := strings.ToLower(strings.TrimSpace(dataType))
	full := strings.ToLower(columnType)
	if i := strings.Index(t, "("); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimPrefix(t, "unsigned ")
	t = strings.TrimSuffix(t, " unsigned")
	switch {
	case kind == "mysql" && t == "tinyint" && strings.HasPrefix(full, "tinyint(1)"):
		return "boolean", false, nil
	case kind == "mysql" && t == "bit" && (full == "bit(1)" || full == ""):
		return "boolean", false, nil
	case kind == "mysql" && t == "enum":
		return "single_select", false, enumOptions(full)
	}
	switch t {
	case "smallint", "integer", "int", "bigint", "tinyint", "mediumint", "int2", "int4", "int8",
		"decimal", "numeric", "real", "double", "double precision", "float", "float4", "float8", "money", "smallmoney", "year":
		return "number", false, nil
	case "boolean", "bool", "bit":
		return "boolean", false, nil
	case "date", "datetime", "datetime2", "smalldatetime", "datetimeoffset", "timestamp", "timestamptz",
		"timestamp without time zone", "timestamp with time zone":
		return "date", false, nil
	case "text", "mediumtext", "longtext", "ntext", "json", "jsonb", "xml", "tinytext":
		return "longtext", false, nil
	case "bytea", "blob", "mediumblob", "longblob", "tinyblob", "binary", "varbinary", "image", "rowversion", "geometry", "geography", "hierarchyid", "sql_variant":
		return "text", true, nil
	case "varchar", "nvarchar", "character varying", "char", "nchar", "character", "bpchar":
		if size < 0 || size > 255 {
			return "longtext", false, nil
		}
	}
	return "text", false, nil
}

// enumOptions reads the choices of a MySQL enum('a','b').
func enumOptions(columnType string) []string {
	i, j := strings.Index(columnType, "("), strings.LastIndex(columnType, ")")
	if i < 0 || j < i {
		return nil
	}
	var out []string
	inner := columnType[i+1 : j]
	for len(inner) > 0 {
		inner = strings.TrimLeft(inner, ", ")
		if !strings.HasPrefix(inner, "'") {
			break
		}
		var b strings.Builder
		k := 1
		for ; k < len(inner); k++ {
			if inner[k] == '\'' {
				if k+1 < len(inner) && inner[k+1] == '\'' {
					b.WriteByte('\'')
					k++
					continue
				}
				break
			}
			b.WriteByte(inner[k])
		}
		out = append(out, b.String())
		if k+1 > len(inner) {
			break
		}
		inner = inner[k+1:]
	}
	return out
}

// queryTables runs a query giving schema, name and type of each table.
func queryTables(ctx context.Context, db *sql.DB, q string, args ...any) ([]TableInfo, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TableInfo{}
	for rows.Next() {
		var t TableInfo
		var kind string
		if err := rows.Scan(&t.Schema, &t.Name, &kind); err != nil {
			return nil, err
		}
		t.Kind = "table"
		if strings.Contains(strings.ToUpper(kind), "VIEW") {
			t.Kind = "view"
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// queryStrings reads one text column.
func queryStrings(ctx context.Context, db *sql.DB, q string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func quoteWith(open, close string) func(string) string {
	return func(name string) string {
		return open + strings.ReplaceAll(name, close, close+close) + close
	}
}

func limitOffset(limit, offset int) string {
	return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
}
