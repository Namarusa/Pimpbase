package tables

import (
	"encoding/json"
	"fmt"
	"strings"
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
