package tables

import (
	"context"

	"github.com/bubibase/bubibase/internal/rbac"
)

// Labels gives the labels of rows of a table that something outside it
// links to, such as a column of an external database table. Rows the
// actor may not read, under the table's permissions or their row rule,
// come back hidden with no label; ids without a row are left out.
func (s *Service) Labels(ctx context.Context, actor rbac.Actor, appID, tableID string, ids []int64) (map[int64]LinkRef, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	readable := actor.Builder || actor.Has(c.Permissions.Read)
	rule, err := c.rule(actor)
	if err != nil {
		return nil, err
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := map[int64]LinkRef{}
	for _, chunk := range chunks(ids, 500) {
		rows, err := selectRows(ctx, db, db, c, "_id IN "+inList(len(chunk)), toArgs(chunk), "")
		if err != nil {
			return nil, err
		}
		if err := s.hydrate(ctx, db, db, c, rows, 0); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if !readable || !s.allowed(c, rule, actor, r) {
				out[r.ID] = LinkRef{ID: r.ID, Hidden: true}
				continue
			}
			out[r.ID] = LinkRef{ID: r.ID, Label: s.label(c, r)}
		}
	}
	return out, nil
}

// FindByLabel gives the ids of rows whose label is the text given, at
// most limit of them, for reading links written as labels.
func (s *Service) FindByLabel(ctx context.Context, appID, tableID, label string, limit int) ([]int64, error) {
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return nil, err
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return nil, err
	}
	var out []int64
	for after := int64(0); len(out) < limit; {
		rows, err := selectRows(ctx, db, db, c, "_id > ?", []any{after}, "ORDER BY _id LIMIT 1000")
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		if err := s.hydrate(ctx, db, db, c, rows, 1); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if s.label(c, r) == label && len(out) < limit {
				out = append(out, r.ID)
			}
			after = r.ID
		}
	}
	return out, nil
}
