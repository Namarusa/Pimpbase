// Package apps manages the apps hosted by a Bubibase instance and who may
// use each one.
package apps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/ids"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/store"
)

type App struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Design    Design    `json:"design"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Design holds the look of a published app.
type Design struct {
	// Theme is "light", "dark" or "system".
	Theme string `json:"theme"`
	// Accent is the primary colour, as #rrggbb; empty uses the default.
	Accent string `json:"accent"`
	// Navigation is "sidebar", "top" or "none".
	Navigation string `json:"navigation"`
	// Title replaces the app name in the published app's header.
	Title string `json:"title"`
}

var accentRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (d *Design) normalize() error {
	switch d.Theme {
	case "":
		d.Theme = "system"
	case "light", "dark", "system":
	default:
		return &InvalidError{"the theme is light, dark or system"}
	}
	switch d.Navigation {
	case "":
		d.Navigation = "sidebar"
	case "sidebar", "top", "none":
	default:
		return &InvalidError{"navigation is sidebar, top or none"}
	}
	if d.Accent != "" && !accentRe.MatchString(d.Accent) {
		return &InvalidError{"the accent colour is written #rrggbb"}
	}
	d.Title = strings.TrimSpace(d.Title)
	if len(d.Title) > 100 {
		return &InvalidError{"the title has at most 100 characters"}
	}
	return nil
}

var (
	ErrNotFound = errors.New("app not found")
	slugRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// InvalidError messages are safe to show.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

type Service struct {
	store *store.Store
	now   func() time.Time
}

func NewService(s *store.Store) *Service { return &Service{store: s, now: time.Now} }

// Slugify makes a URL slug from a name.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		s = "app"
	}
	return s
}

func scanApp(row interface{ Scan(...any) error }) (*App, error) {
	var a App
	var created, updated, design string
	if err := row.Scan(&a.ID, &a.Slug, &a.Name, &design, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	json.Unmarshal([]byte(design), &a.Design)
	a.Design.normalize()
	return &a, nil
}

const cols = `id, slug, name, design, created_at, updated_at`

func (s *Service) List(ctx context.Context) ([]*App, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT `+cols+` FROM apps ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Get finds an app by id or slug.
func (s *Service) Get(ctx context.Context, idOrSlug string) (*App, error) {
	return scanApp(s.store.Meta.QueryRow(ctx, `SELECT `+cols+` FROM apps WHERE id = ? OR slug = ?`, idOrSlug, idOrSlug))
}

// Create makes an app. An empty slug is derived from the name and made
// unique.
func (s *Service) Create(ctx context.Context, name, slug string) (*App, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, &InvalidError{"an app needs a name of up to 100 characters"}
	}
	explicit := slug != ""
	if !explicit {
		slug = Slugify(name)
	}
	if !slugRe.MatchString(slug) {
		return nil, &InvalidError{"the URL name may use lowercase letters, digits and dashes"}
	}
	base := slug
	now := s.now().UTC().Format(time.RFC3339)
	id := ids.New("app")
	for i := 2; ; i++ {
		_, err := s.store.Meta.Exec(ctx, `INSERT INTO apps (`+cols+`) VALUES (?, ?, ?, '{}', ?, ?)`, id, slug, name, now, now)
		if err == nil {
			break
		}
		if !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, err
		}
		if explicit || i > 50 {
			return nil, &InvalidError{fmt.Sprintf("the URL name %q is taken", slug)}
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	if _, err := s.store.AppDB(ctx, id); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, id string, name, slug *string, design *Design) (*App, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len(n) > 100 {
			return nil, &InvalidError{"an app needs a name of up to 100 characters"}
		}
		a.Name = n
	}
	if slug != nil {
		if !slugRe.MatchString(*slug) {
			return nil, &InvalidError{"the URL name may use lowercase letters, digits and dashes"}
		}
		a.Slug = *slug
	}
	if design != nil {
		if err := design.normalize(); err != nil {
			return nil, err
		}
		a.Design = *design
	}
	dj, _ := json.Marshal(a.Design)
	_, err = s.store.Meta.Exec(ctx, `UPDATE apps SET name = ?, slug = ?, design = ?, updated_at = ? WHERE id = ?`, a.Name, a.Slug, string(dj), s.now().UTC().Format(time.RFC3339), a.ID)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return nil, &InvalidError{fmt.Sprintf("the URL name %q is taken", a.Slug)}
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, a.ID)
}

// Delete removes an app, its tables and all its data.
func (s *Service) Delete(ctx context.Context, id string) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DropApp(ctx, a.ID); err != nil {
		return err
	}
	tx, err := s.store.Meta.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM app_screens WHERE app_id = ?`, `DELETE FROM app_tables WHERE app_id = ?`, `DELETE FROM app_users WHERE app_id = ?`, `DELETE FROM apps WHERE id = ?`} {
		if _, err := tx.Exec(ctx, q, a.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Member is a user's role in an app.
type Member struct {
	UserID string    `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Role   rbac.Role `json:"role"`
}

func (s *Service) Members(ctx context.Context, appID string) ([]Member, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT u.id, u.email, u.name, au.role FROM app_users au JOIN users u ON u.id = au.user_id WHERE au.app_id = ? ORDER BY u.email`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		var role string
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &role); err != nil {
			return nil, err
		}
		m.Role, _ = rbac.ParseRole(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetRole gives a user a role in an app; rbac.None removes their access.
func (s *Service) SetRole(ctx context.Context, appID, userID string, role rbac.Role) error {
	if _, err := s.store.Meta.Exec(ctx, `DELETE FROM app_users WHERE app_id = ? AND user_id = ?`, appID, userID); err != nil {
		return err
	}
	if role < rbac.Basic {
		return nil
	}
	_, err := s.store.Meta.Exec(ctx, `INSERT INTO app_users (app_id, user_id, role) VALUES (?, ?, ?)`, appID, userID, role.String())
	return err
}

// RoleOf returns a user's role in an app, or rbac.None.
func (s *Service) RoleOf(ctx context.Context, appID, userID string) (rbac.Role, error) {
	var role string
	err := s.store.Meta.QueryRow(ctx, `SELECT role FROM app_users WHERE app_id = ? AND user_id = ?`, appID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return rbac.None, nil
	}
	if err != nil {
		return rbac.None, err
	}
	return rbac.ParseRole(role)
}
