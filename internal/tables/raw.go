package tables

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/store"
)

// The raw functions copy tables and rows as the database holds them, for
// moving an app between servers. They check no permissions: callers must
// be builders.

// RawRow is one row as stored: _id, _created_at, _updated_at and the
// table's columns, keyed by column name.
type RawRow map[string]any

// JoinRow links row Src to row Dst through a many-link field.
type JoinRow struct {
	Src int64 `json:"src"`
	Dst int64 `json:"dst"`
}

// storedColumns lists the columns rows of t carry.
func storedColumns(t *Table) []string {
	cols := []string{"_id", "_created_at", "_updated_at"}
	for _, f := range t.Fields {
		if f.stored() {
			cols = append(cols, f.Column)
		}
	}
	return cols
}

// DumpRows calls fn with every row of a table, in _id order.
func (s *Service) DumpRows(ctx context.Context, appID string, t *Table, fn func(RawRow) error) error {
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return err
	}
	cols := storedColumns(t)
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = store.Q(c)
	}
	rows, err := db.Query(ctx, "SELECT "+strings.Join(quoted, ", ")+" FROM "+db.Table(t.sqlName())+" ORDER BY _id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		r := RawRow{}
		for i, c := range cols {
			if b, ok := raw[i].([]byte); ok {
				raw[i] = string(b)
			}
			r[c] = raw[i]
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

// DumpLinks calls fn with every pair of a many-link field.
func (s *Service) DumpLinks(ctx context.Context, appID string, f *Field, fn func(JoinRow) error) error {
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return err
	}
	rows, err := db.Query(ctx, "SELECT src, dst FROM "+db.Table(joinName(f))+" ORDER BY src, dst")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var j JoinRow
		if err := rows.Scan(&j.Src, &j.Dst); err != nil {
			return err
		}
		if err := fn(j); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Restore creates a table exactly as defined, ids and columns included,
// with no rows. Its link fields must point at tables of the app or at
// tables in with, which are being restored alongside it.
func (s *Service) Restore(ctx context.Context, appID string, t *Table, with ...*Table) error {
	if t.ID == "" || !strings.HasPrefix(t.ID, "tbl_") || strings.ContainsAny(t.ID, `"' `) {
		return invalid("id", "bad table id %q", t.ID)
	}
	if err := validName("table", t.Name); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		if !strings.HasPrefix(f.Column, "c_") || strings.ContainsAny(f.Column, `"' `) || seen[f.Column] {
			return invalid("column", "bad column %q in table %s", f.Column, t.Name)
		}
		seen[f.Column] = true
		if !slices.Contains(allTypes, f.Type) {
			return invalid("type", "unknown field type %q", f.Type)
		}
	}
	t.AppID = appID
	t.Permissions.normalize()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = s.now().UTC()
	}
	lookup := s.lookup(ctx, appID)
	if _, err := compile(t, func(id string) (*Table, error) {
		if id == t.ID {
			return t, nil
		}
		for _, w := range with {
			if w.ID == id {
				return w, nil
			}
		}
		return lookup(id)
	}); err != nil {
		return err
	}
	_, err := s.change(ctx, appID, true, func(tx *store.Tx) (*Table, error) {
		db := tx.DB()
		ddl := fmt.Sprintf(`CREATE TABLE %s (_id %s, _created_at TEXT NOT NULL, _updated_at TEXT NOT NULL)`, db.Table(t.sqlName()), db.TypeSerialPK())
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return nil, err
		}
		for _, f := range t.Fields {
			if err := createStorage(ctx, tx, t, f); err != nil {
				return nil, err
			}
		}
		return t, nil
	})
	return err
}

// LoadRows inserts rows as stored, keeping their ids, in one transaction.
// Columns the table does not have are refused.
func (s *Service) LoadRows(ctx context.Context, appID string, t *Table, rows []RawRow) error {
	if len(rows) == 0 {
		return nil
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return err
	}
	cols := storedColumns(t)
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = store.Q(c)
	}
	known := map[string]bool{}
	for _, c := range cols {
		known[c] = true
	}
	// JSON gives every number as a float; integer columns want integers.
	ints := map[string]bool{"_id": true}
	for _, f := range t.Fields {
		if f.stored() && columnType(db, f) == db.TypeInt() {
			ints[f.Column] = true
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const batch = 200
	for start := 0; start < len(rows); start += batch {
		chunk := rows[start:min(start+batch, len(rows))]
		ph := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
		q := "INSERT INTO " + db.Table(t.sqlName()) + " (" + strings.Join(quoted, ", ") + ") VALUES " + strings.TrimSuffix(strings.Repeat(ph+", ", len(chunk)), ", ")
		args := make([]any, 0, len(chunk)*len(cols))
		for _, r := range chunk {
			for k := range r {
				if !known[k] {
					return invalid("rows", "table %s has no column %q", t.Name, k)
				}
			}
			for _, c := range cols {
				v := r[c]
				if ints[c] && v != nil {
					v = int64(toFloat(v))
				}
				if c == "_created_at" || c == "_updated_at" {
					if v == nil {
						v = s.now().UTC().Format(time.RFC3339Nano)
					}
				}
				args = append(args, v)
			}
		}
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			return err
		}
	}
	if db.Dialect == store.Postgres {
		// Rows inserted with their ids leave the id sequence behind.
		seq := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%s', '_id'), (SELECT COALESCE(MAX(_id), 1) FROM %s))`, strings.ReplaceAll(db.Table(t.sqlName()), "'", "''"), db.Table(t.sqlName()))
		if _, err := tx.Exec(ctx, seq); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.touch(appID, t.ID)
	return nil
}

// LoadLinks inserts the pairs of a many-link field.
func (s *Service) LoadLinks(ctx context.Context, appID string, f *Field, pairs []JoinRow) error {
	if len(pairs) == 0 {
		return nil
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for start := 0; start < len(pairs); start += 400 {
		chunk := pairs[start:min(start+400, len(pairs))]
		q := "INSERT INTO " + db.Table(joinName(f)) + " (src, dst) VALUES " + strings.TrimSuffix(strings.Repeat("(?, ?), ", len(chunk)), ", ")
		args := make([]any, 0, 2*len(chunk))
		for _, p := range chunk {
			args = append(args, p.Src, p.Dst)
		}
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}
