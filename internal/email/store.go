package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/ids"
)

// ErrNotFound is a template or version that does not exist.
var ErrNotFound = errors.New("not found")

// keepVersions is how many saved versions each template keeps.
const keepVersions = 50

// definition is what a version holds: the template without its identity.
type definition struct {
	Key         string  `json:"key,omitempty"`
	Description string  `json:"description"`
	Subject     string  `json:"subject"`
	Preheader   string  `json:"preheader"`
	Mode        string  `json:"mode"`
	Blocks      []Block `json:"blocks"`
	Style       Style   `json:"style"`
	HTML        string  `json:"html"`
	Text        string  `json:"text"`
	Params      []Param `json:"params"`
}

func defOf(t *Template) definition {
	return definition{t.Key, t.Description, t.Subject, t.Preheader, t.Mode, t.Blocks, t.Style, t.HTML, t.Text, t.Params}
}

func (d definition) apply(t *Template) {
	t.Key, t.Description, t.Subject, t.Preheader, t.Mode = d.Key, d.Description, d.Subject, d.Preheader, d.Mode
	t.Blocks, t.Style, t.HTML, t.Text, t.Params = d.Blocks, d.Style, d.HTML, d.Text, d.Params
	if t.Blocks == nil {
		t.Blocks = []Block{}
	}
	if t.Params == nil {
		t.Params = []Param{}
	}
}

const tplCols = `id, app_id, name, definition, version, created_at, updated_at, updated_by`

func scanTemplate(row interface{ Scan(...any) error }) (*Template, error) {
	t := &Template{}
	var def, created, updated string
	if err := row.Scan(&t.ID, &t.AppID, &t.Name, &def, &t.Version, &created, &updated, &t.UpdatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var d definition
	if err := json.Unmarshal([]byte(def), &d); err != nil {
		return nil, err
	}
	d.apply(t)
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return t, nil
}

// Templates lists an app's templates by name. An empty appID lists the
// workspace's saved system emails.
func (s *Service) Templates(ctx context.Context, appID string) ([]*Template, error) {
	rows, err := s.db.Query(ctx, `SELECT `+tplCols+` FROM email_templates WHERE app_id = ? ORDER BY name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Template finds a template by id or by name.
func (s *Service) Template(ctx context.Context, appID, idOrName string) (*Template, error) {
	t, err := scanTemplate(s.db.QueryRow(ctx, `SELECT `+tplCols+` FROM email_templates WHERE app_id = ? AND id = ?`, appID, idOrName))
	if errors.Is(err, ErrNotFound) {
		t, err = scanTemplate(s.db.QueryRow(ctx, `SELECT `+tplCols+` FROM email_templates WHERE app_id = ? AND LOWER(name) = ?`, appID, strings.ToLower(strings.TrimSpace(idOrName))))
	}
	return t, err
}

func (s *Service) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

// CreateTemplate adds a template as its version 1.
func (s *Service) CreateTemplate(ctx context.Context, appID string, t *Template, by string) (*Template, error) {
	if err := t.Check(); err != nil {
		return nil, err
	}
	if len(t.Name) > 0 {
		if _, err := s.Template(ctx, appID, t.Name); err == nil {
			return nil, invalid("a template named %s exists already", t.Name)
		}
	}
	def, err := json.Marshal(defOf(t))
	if err != nil {
		return nil, err
	}
	if len(def) > maxSize {
		return nil, invalid("the template is too large (over %d KB)", maxSize>>10)
	}
	t.ID, t.AppID, t.Version, t.UpdatedBy = ids.New("eml"), appID, 1, by
	now := s.stamp()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `INSERT INTO email_templates (`+tplCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, t.ID, appID, t.Name, string(def), 1, now, now, by); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_template_versions (template_id, version, definition, saved_at, saved_by) VALUES (?, ?, ?, ?, ?)`, t.ID, 1, string(def), now, by); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Template(ctx, appID, t.ID)
}

// UpdateTemplate saves changes. A change to the content makes a new
// version; a rename alone does not.
func (s *Service) UpdateTemplate(ctx context.Context, appID, id string, in *Template, by string) (*Template, error) {
	cur, err := s.Template(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	in.Key = cur.Key
	if err := in.Check(); err != nil {
		return nil, err
	}
	if !strings.EqualFold(in.Name, cur.Name) {
		if other, err := s.Template(ctx, appID, in.Name); err == nil && other.ID != cur.ID {
			return nil, invalid("a template named %s exists already", in.Name)
		}
	}
	def, err := json.Marshal(defOf(in))
	if err != nil {
		return nil, err
	}
	if len(def) > maxSize {
		return nil, invalid("the template is too large (over %d KB)", maxSize>>10)
	}
	old, _ := json.Marshal(defOf(cur))
	version := cur.Version
	now := s.stamp()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if string(old) != string(def) {
		version++
		if _, err := tx.Exec(ctx, `INSERT INTO email_template_versions (template_id, version, definition, saved_at, saved_by) VALUES (?, ?, ?, ?, ?)`, cur.ID, version, string(def), now, by); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM email_template_versions WHERE template_id = ? AND version <= ?`, cur.ID, version-keepVersions); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE email_templates SET name = ?, definition = ?, version = ?, updated_at = ?, updated_by = ? WHERE id = ?`, in.Name, string(def), version, now, by, cur.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Template(ctx, appID, cur.ID)
}

// DeleteTemplate removes a template and its versions.
func (s *Service) DeleteTemplate(ctx context.Context, appID, id string) error {
	t, err := s.Template(ctx, appID, id)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `DELETE FROM email_template_versions WHERE template_id = ?`, t.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM email_templates WHERE id = ?`, t.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteApp removes an app's templates, when the app goes.
func (s *Service) DeleteApp(ctx context.Context, appID string) error {
	if appID == "" {
		return nil
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM email_template_versions WHERE template_id IN (SELECT id FROM email_templates WHERE app_id = ?)`, appID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM email_templates WHERE app_id = ?`, appID)
	return err
}

// VersionInfo describes a saved version.
type VersionInfo struct {
	Version int       `json:"version"`
	Subject string    `json:"subject"`
	SavedAt time.Time `json:"savedAt"`
	SavedBy string    `json:"savedBy"`
}

// Versions lists a template's saved versions, newest first.
func (s *Service) Versions(ctx context.Context, appID, id string) ([]VersionInfo, error) {
	t, err := s.Template(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT version, definition, saved_at, saved_by FROM email_template_versions WHERE template_id = ? ORDER BY version DESC`, t.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VersionInfo{}
	for rows.Next() {
		var v VersionInfo
		var def, at string
		if err := rows.Scan(&v.Version, &def, &at, &v.SavedBy); err != nil {
			return nil, err
		}
		var d definition
		json.Unmarshal([]byte(def), &d)
		v.Subject = d.Subject
		v.SavedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Version is a template as it was saved in a version.
func (s *Service) Version(ctx context.Context, appID, id string, version int) (*Template, error) {
	t, err := s.Template(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	var def, at, by string
	err = s.db.QueryRow(ctx, `SELECT definition, saved_at, saved_by FROM email_template_versions WHERE template_id = ? AND version = ?`, t.ID, version).Scan(&def, &at, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var d definition
	if err := json.Unmarshal([]byte(def), &d); err != nil {
		return nil, err
	}
	d.apply(t)
	t.Version = version
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, at)
	t.UpdatedBy = by
	return t, nil
}

// Restore saves an old version's content as the newest version.
func (s *Service) Restore(ctx context.Context, appID, id string, version int, by string) (*Template, error) {
	old, err := s.Version(ctx, appID, id, version)
	if err != nil {
		return nil, err
	}
	cur, err := s.Template(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	old.Name = cur.Name
	return s.UpdateTemplate(ctx, appID, cur.ID, old, by)
}
