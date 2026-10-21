package exttables

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/formula"
	"github.com/pimpbase/pimpbase/internal/queries"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/tables"
)

// ImportCSV adds the rows of a CSV file whose first line names the
// columns. Each row is added on its own, with the checks of CreateRow:
// rows the database or a row rule refuses are skipped and reported, the
// rest stay added. Link columns take the linked row's key or its label.
func (s *Service) ImportCSV(ctx context.Context, actor rbac.Actor, env, appID, id string, r io.Reader) (*tables.ImportResult, error) {
	rc, err := s.open(ctx, appID, id, env)
	if err != nil {
		return nil, err
	}
	if err := actor.Require(rc.t.Permissions.Write, "adding rows to "+rc.t.Name); err != nil {
		return nil, err
	}
	if rc.readOnly() {
		return nil, invalid("", "%s is read only", rc.t.Name)
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
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	res := &tables.ImportResult{Errors: []string{}, NewFields: []string{}, IgnoredFields: []string{}}
	cols := make([]string, len(header))
	for i, name := range header {
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "_") {
			continue
		}
		c := rc.t.column(name)
		if c == nil || c.Hidden || c.Binary || c.Generated {
			res.IgnoredFields = append(res.IgnoredFields, name)
			continue
		}
		cols[i] = c.Name
	}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, invalid("file", "cannot read the CSV: %v", err)
		}
		if res.Imported+res.Skipped >= tables.MaxImportRows {
			return res, invalid("file", "a file can hold at most %d rows; the first %d were read", tables.MaxImportRows, tables.MaxImportRows)
		}
		vals := map[string]any{}
		for i, name := range cols {
			if name == "" || i >= len(rec) {
				continue
			}
			// An empty cell leaves a column to the database's default.
			if strings.TrimSpace(rec[i]) != "" {
				vals[name] = rec[i]
			}
		}
		if _, err := s.createRow(ctx, rc, actor, vals); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var ve *tables.ValidationError
			var re *queries.RunError
			if !errors.As(err, &ve) && !errors.As(err, &re) && !errors.Is(err, rbac.ErrForbidden) {
				return nil, err
			}
			res.Skipped++
			if len(res.Errors) < 100 {
				res.Errors = append(res.Errors, fmt.Sprintf("line %d: %s", line, err))
			}
			continue
		}
		res.Imported++
	}
	return res, nil
}

// ExportCSV writes the rows matching a filter as CSV, with links as their
// labels.
func (s *Service) ExportCSV(ctx context.Context, actor rbac.Actor, env, appID, id, filter string, w io.Writer) error {
	t, err := s.Get(ctx, appID, id)
	if err != nil {
		return err
	}
	v, err := s.View(ctx, t)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	header := []string{"_id"}
	for _, f := range v.Fields {
		header = append(header, f.Name)
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for offset := 0; ; offset += 1000 {
		page, err := s.ListRows(ctx, actor, env, appID, id, tables.Query{Filter: filter, Limit: 1000, Offset: offset})
		if err != nil {
			return err
		}
		for _, row := range page.Rows {
			rec := []string{keyText(row["_id"])}
			for _, f := range v.Fields {
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
	case nil:
		return ""
	case LinkRef:
		return x.Label
	case []LinkRef:
		labels := make([]string, len(x))
		for i, l := range x {
			labels[i] = l.Label
		}
		return strings.Join(labels, ", ")
	case time.Time:
		return x.UTC().Format(formula.DateLayout)
	case float64:
		return keyText(x)
	}
	return formula.ToText(v)
}
