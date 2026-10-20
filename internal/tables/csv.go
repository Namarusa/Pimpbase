package tables

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/rbac"
)

// MaxImportRows caps one CSV import.
const MaxImportRows = 100_000

// ImportResult reports what an import did.
type ImportResult struct {
	Imported      int      `json:"imported"`
	Skipped       int      `json:"skipped"`
	Errors        []string `json:"errors"`
	NewFields     []string `json:"newFields"`
	IgnoredFields []string `json:"ignoredFields"`
}

// ImportCSV adds the rows of a CSV file whose first line names the fields.
// Builders may let it add missing text fields and new select options.
// Rows with invalid values are skipped and reported; the rest are added in
// one transaction.
func (s *Service) ImportCSV(ctx context.Context, actor rbac.Actor, appID, tableID string, r io.Reader, addFields bool) (*ImportResult, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(c.Permissions.Write, "adding rows to "+c.Name); err != nil {
		return nil, err
	}
	if _, err := c.rule(actor); err != nil {
		return nil, err
	}
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, invalid("file", "the file is empty")
	}
	if err != nil {
		return nil, invalid("file", "cannot read the CSV: %v", err)
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff") // Excel's byte order mark
	}
	var records [][]string
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, invalid("file", "cannot read the CSV: %v", err)
		}
		if len(records) >= MaxImportRows {
			return nil, invalid("file", "a file can hold at most %d rows", MaxImportRows)
		}
		records = append(records, rec)
	}

	res := &ImportResult{Errors: []string{}, NewFields: []string{}, IgnoredFields: []string{}}
	// Match columns to fields, adding text fields when allowed.
	for _, name := range header {
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "_") {
			continue
		}
		f := c.FieldByName(name)
		switch {
		case f != nil && f.writable() && f.Type != TypeAttachment:
		case f == nil && addFields && actor.Builder:
			if _, _, err := s.AddField(ctx, appID, tableID, FieldInput{Name: name, Type: TypeText}); err != nil {
				return nil, err
			}
			res.NewFields = append(res.NewFields, name)
		default:
			res.IgnoredFields = append(res.IgnoredFields, name)
		}
	}
	// Builders get new select options added for them.
	if actor.Builder {
		for i, name := range header {
			f := c.FieldByName(strings.TrimSpace(name))
			if f == nil || (f.Type != TypeSingleSelect && f.Type != TypeMultiSelect) || len(f.Options) == 0 {
				continue
			}
			opts := append([]string{}, f.Options...)
			for _, rec := range records {
				if i >= len(rec) {
					continue
				}
				vals := []string{rec[i]}
				if f.Type == TypeMultiSelect {
					vals = strings.Split(rec[i], ",")
				}
				for _, v := range vals {
					if v = strings.TrimSpace(v); v != "" && !slices.Contains(opts, v) {
						opts = append(opts, v)
					}
				}
			}
			if len(opts) > len(f.Options) {
				if _, err := s.UpdateField(ctx, appID, tableID, f.ID, FieldPatch{Options: opts}); err != nil {
					return nil, err
				}
			}
		}
	}
	if c, err = s.loadCompiled(ctx, appID, tableID); err != nil {
		return nil, err
	}

	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rule, err := c.rule(actor)
	if err != nil {
		return nil, err
	}
	for n, rec := range records {
		values := map[string]any{}
		for i, name := range header {
			if i >= len(rec) {
				break
			}
			// Files cannot come from a CSV, only their names go out in one.
			if f := c.FieldByName(strings.TrimSpace(name)); f != nil && f.writable() && f.Type != TypeAttachment {
				values[f.Name] = rec[i]
			}
		}
		if rule != nil {
			if _, err := tx.Exec(ctx, "SAVEPOINT bb_import_row"); err != nil {
				return nil, err
			}
		}
		id, err := s.insert(ctx, tx, c, values, true)
		if err != nil {
			var v *ValidationError
			if !errors.As(err, &v) {
				return nil, err
			}
			if rule != nil {
				if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT bb_import_row"); err != nil {
					return nil, err
				}
			}
			res.Skipped++
			if len(res.Errors) < 100 {
				res.Errors = append(res.Errors, fmt.Sprintf("line %d: %s", n+2, v.Error()))
			}
			continue
		}
		if rule != nil {
			r, err := s.loadRow(ctx, tx, db, c, id)
			if err != nil {
				return nil, err
			}
			if !s.allowed(c, rule, actor, r) {
				if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT bb_import_row"); err != nil {
					return nil, err
				}
				res.Skipped++
				if len(res.Errors) < 100 {
					res.Errors = append(res.Errors, fmt.Sprintf("line %d: the row does not match your row rule", n+2))
				}
				continue
			}
			if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT bb_import_row"); err != nil {
				return nil, err
			}
		}
		res.Imported++
	}
	defer s.touch(appID, tableID)
	return res, tx.Commit()
}

// ExportCSV writes the rows matching a filter as CSV, with links as their
// labels and multi selects joined by commas.
func (s *Service) ExportCSV(ctx context.Context, actor rbac.Actor, appID, tableID, filter string, w io.Writer) error {
	t, err := s.Get(ctx, appID, tableID)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	header := []string{"_id"}
	for _, f := range t.Fields {
		header = append(header, f.Name)
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for offset := 0; ; offset += 1000 {
		page, err := s.ListRows(ctx, actor, appID, tableID, Query{Filter: filter, Limit: 1000, Offset: offset})
		if err != nil {
			return err
		}
		for _, row := range page.Rows {
			rec := []string{fmt.Sprint(row["_id"])}
			for _, f := range t.Fields {
				rec = append(rec, csvValue(row[f.Name]))
			}
			if err := cw.Write(rec); err != nil {
				return err
			}
		}
		if len(page.Rows) < 1000 {
			break
		}
	}
	cw.Flush()
	return cw.Error()
}

func csvValue(v any) string {
	switch x := v.(type) {
	case LinkRef:
		return x.Label
	case []LinkRef:
		labels := make([]string, len(x))
		for i, l := range x {
			labels[i] = l.Label
		}
		return strings.Join(labels, ", ")
	case []map[string]any: // attachment files
		names := make([]string, len(x))
		for i, f := range x {
			names[i], _ = f["name"].(string)
		}
		return strings.Join(names, ", ")
	case time.Time:
		return x.UTC().Format(formula.DateLayout)
	}
	return formula.ToText(v)
}
