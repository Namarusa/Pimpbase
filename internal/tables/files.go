package tables

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/store"
)

// FileRef is one file held by an attachment field. The content lives in the
// app's file store (package files); the row keeps what lists and links need.
type FileRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// MaxFilesPerField caps the files one attachment field holds.
const MaxFilesPerField = 20

func decodeFiles(s string) any {
	var files []FileRef
	if json.Unmarshal([]byte(s), &files) != nil || len(files) == 0 {
		return nil
	}
	return files
}

// encodeFiles accepts the files as the API shows them (objects with an id,
// extra keys such as url ignored), or a single such object.
func encodeFiles(v any) (any, error) {
	list, ok := v.([]any)
	if !ok {
		if m, isMap := v.(map[string]any); isMap {
			list = []any{m}
		} else {
			return nil, fmt.Errorf("expected a list of uploaded files")
		}
	}
	files := make([]FileRef, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected a list of uploaded files")
		}
		id, _ := m["id"].(string)
		if !strings.HasPrefix(id, "fil_") || len(id) > 64 {
			return nil, fmt.Errorf("a file has no valid id; upload it first")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		f := FileRef{ID: id}
		f.Name, _ = m["name"].(string)
		f.Type, _ = m["type"].(string)
		switch n := m["size"].(type) {
		case float64:
			f.Size = int64(n)
		case json.Number:
			f.Size, _ = n.Int64()
		}
		files = append(files, f)
	}
	if len(files) > MaxFilesPerField {
		return nil, fmt.Errorf("a field holds at most %d files", MaxFilesPerField)
	}
	if len(files) == 0 {
		return nil, nil
	}
	b, _ := json.Marshal(files)
	return string(b), nil
}

// FileURL is where the API serves a stored file.
func FileURL(appID, fileID string) string {
	return "/api/apps/" + appID + "/files/" + fileID
}

func filesJSON(appID string, files []FileRef) []map[string]any {
	out := make([]map[string]any, len(files))
	for i, f := range files {
		out[i] = map[string]any{"id": f.ID, "name": f.Name, "type": f.Type, "size": f.Size, "url": FileURL(appID, f.ID)}
	}
	return out
}

var fileIDRe = regexp.MustCompile(`^fil_[a-z2-7]{1,60}$`)

// FileAccess says whether an actor may download a file uploaded for a
// table. Readable needs read access to the table and, when a row rule
// applies to the actor, a row they may see that holds the file. Held
// reports whether any row holds the file at all, so that a file just
// uploaded and not yet saved to a row can be shown to its uploader.
type FileAccess struct {
	Readable bool
	Held     bool
}

// FileAccess checks a download of a file of a table.
func (s *Service) FileAccess(ctx context.Context, actor rbac.Actor, appID, tableID, fileID string) (FileAccess, error) {
	var out FileAccess
	c, err := s.loadCompiled(ctx, appID, tableID)
	if err != nil {
		return out, err
	}
	if err := actor.Require(c.Permissions.Read, "reading "+c.Name); err != nil {
		return out, err
	}
	rule, err := c.rule(actor)
	if err != nil {
		return out, err
	}
	if rule == nil {
		return FileAccess{Readable: true, Held: true}, nil
	}
	var conds []string
	var args []any
	var fields []*Field
	for _, f := range c.Fields {
		if f.Type == TypeAttachment {
			conds = append(conds, store.Q(f.Column)+" LIKE ?")
			args = append(args, `%"`+fileID+`"%`)
			fields = append(fields, f)
		}
	}
	if len(conds) == 0 || strings.ContainsAny(fileID, `%_"\`) && !strings.HasPrefix(fileID, "fil_") {
		return out, nil
	}
	db, err := s.store.AppDB(ctx, appID)
	if err != nil {
		return out, err
	}
	rows, err := selectRows(ctx, db, db, c, strings.Join(conds, " OR "), args, "ORDER BY _id LIMIT 200")
	if err != nil {
		return out, err
	}
	if err := s.hydrate(ctx, db, db, c, rows, 0); err != nil {
		return out, err
	}
	for _, r := range rows {
		holds := false
		for _, f := range fields {
			list, _ := r.Values[f.ID].([]FileRef)
			if slices.ContainsFunc(list, func(x FileRef) bool { return x.ID == fileID }) {
				holds = true
			}
		}
		if !holds {
			continue
		}
		out.Held = true
		if s.allowed(c, rule, actor, r) {
			out.Readable = true
			return out, nil
		}
	}
	return out, nil
}
