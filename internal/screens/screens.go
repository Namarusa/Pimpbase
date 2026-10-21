// Package screens stores the screens of an app: a route, the role needed to
// open it and a tree of components the builder and the app runtime render.
package screens

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/store"
)

// Screen is one page of an app.
type Screen struct {
	ID    string    `json:"id"`
	AppID string    `json:"appId"`
	Name  string    `json:"name"`
	Route string    `json:"route"`
	Role  rbac.Role `json:"role"`
	// Position orders screens in the builder and the app's navigation.
	Position int `json:"position"`
	// Definition is the root component: {"id", "type", "props", "children"}.
	Definition json.RawMessage `json:"definition"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// Input changes a screen; nil fields keep their value.
type Input struct {
	Name       *string         `json:"name"`
	Route      *string         `json:"route"`
	Role       *rbac.Role      `json:"role"`
	Position   *int            `json:"position"`
	Definition json.RawMessage `json:"definition"`
}

var (
	ErrNotFound = errors.New("screen not found")
	segmentRe   = regexp.MustCompile(`^([a-z0-9][a-z0-9_-]*|:[A-Za-z_][A-Za-z0-9_]*)$`)
	typeRe      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)
)

// InvalidError messages are safe to show.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

const (
	maxDefinition = 1 << 20
	maxNodes      = 2000
	maxDepth      = 40
)

type Service struct {
	store *store.Store
	now   func() time.Time
}

func NewService(s *store.Store) *Service { return &Service{store: s, now: time.Now} }

// NormalizeRoute checks a route such as /customers/:id and returns it
// without a trailing slash.
func NormalizeRoute(route string) (string, error) {
	route = strings.TrimSpace(route)
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	route = strings.TrimRight(route, "/")
	if route == "" {
		return "/", nil
	}
	params := map[string]bool{}
	for _, seg := range strings.Split(route[1:], "/") {
		if !segmentRe.MatchString(seg) {
			return "", &InvalidError{fmt.Sprintf("the route part %q may use lowercase letters, digits, dashes and underscores, or be a :parameter", seg)}
		}
		if strings.HasPrefix(seg, ":") {
			if params[seg] {
				return "", &InvalidError{fmt.Sprintf("the route uses %s twice", seg)}
			}
			params[seg] = true
		}
	}
	return route, nil
}

// node is the shape every component in a definition has.
type node struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Props    map[string]json.RawMessage `json:"props"`
	Children []node                     `json:"children"`
}

// CheckDefinition validates a component tree and returns it compacted.
func CheckDefinition(def json.RawMessage) (json.RawMessage, error) {
	if len(def) > maxDefinition {
		return nil, &InvalidError{"the screen is too large"}
	}
	var root node
	dec := json.NewDecoder(bytes.NewReader(def))
	if err := dec.Decode(&root); err != nil {
		return nil, &InvalidError{"the screen definition is not a component: " + err.Error()}
	}
	seen := map[string]bool{}
	count := 0
	var walk func(n *node, depth int) error
	walk = func(n *node, depth int) error {
		count++
		if count > maxNodes {
			return &InvalidError{fmt.Sprintf("a screen holds at most %d components", maxNodes)}
		}
		if depth > maxDepth {
			return &InvalidError{fmt.Sprintf("components nest at most %d deep", maxDepth)}
		}
		if n.ID == "" || len(n.ID) > 64 {
			return &InvalidError{"every component needs an id of up to 64 characters"}
		}
		if seen[n.ID] {
			return &InvalidError{fmt.Sprintf("the component id %q is used twice", n.ID)}
		}
		seen[n.ID] = true
		if !typeRe.MatchString(n.Type) {
			return &InvalidError{fmt.Sprintf("the component type %q is not valid", n.Type)}
		}
		for i := range n.Children {
			if err := walk(&n.Children[i], depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(&root, 1); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, def); err != nil {
		return nil, &InvalidError{"the screen definition is not valid JSON"}
	}
	return buf.Bytes(), nil
}

const cols = `id, app_id, name, route, role, position, definition, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (*Screen, error) {
	var sc Screen
	var role, def, created, updated string
	if err := row.Scan(&sc.ID, &sc.AppID, &sc.Name, &sc.Route, &role, &sc.Position, &def, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sc.Role, _ = rbac.ParseRole(role)
	sc.Definition = json.RawMessage(def)
	sc.CreatedAt, _ = time.Parse(time.RFC3339, created)
	sc.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &sc, nil
}

// List returns an app's screens in navigation order.
func (s *Service) List(ctx context.Context, appID string) ([]*Screen, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT `+cols+` FROM app_screens WHERE app_id = ? ORDER BY position, name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Screen{}
	for rows.Next() {
		sc, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, appID, id string) (*Screen, error) {
	return scan(s.store.Meta.QueryRow(ctx, `SELECT `+cols+` FROM app_screens WHERE app_id = ? AND id = ?`, appID, id))
}

func (s *Service) apply(sc *Screen, in Input) error {
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 100 {
			return &InvalidError{"a screen needs a name of up to 100 characters"}
		}
		sc.Name = n
	}
	if in.Route != nil {
		r, err := NormalizeRoute(*in.Route)
		if err != nil {
			return err
		}
		sc.Route = r
	}
	if in.Role != nil {
		if *in.Role == rbac.None || *in.Role == "" {
			return &InvalidError{"pick the role that may see the screen"}
		}
		sc.Role = *in.Role
	}
	if in.Position != nil {
		sc.Position = *in.Position
	}
	if len(in.Definition) > 0 && string(in.Definition) != "null" {
		def, err := CheckDefinition(in.Definition)
		if err != nil {
			return err
		}
		sc.Definition = def
	}
	return nil
}

func uniqueErr(err error, route string) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return &InvalidError{fmt.Sprintf("another screen already uses the route %s", route)}
	}
	return err
}

// Create adds a screen. Name and route are required; the role defaults to
// BASIC and the definition to an empty container.
func (s *Service) Create(ctx context.Context, appID string, in Input) (*Screen, error) {
	sc := &Screen{ID: ids.New("scr"), AppID: appID, Role: rbac.Basic}
	if in.Name == nil || in.Route == nil {
		return nil, &InvalidError{"a screen needs a name and a route"}
	}
	if in.Position == nil {
		var max sql.NullInt64
		if err := s.store.Meta.QueryRow(ctx, `SELECT MAX(position) FROM app_screens WHERE app_id = ?`, appID).Scan(&max); err != nil {
			return nil, err
		}
		sc.Position = int(max.Int64) + 1
	}
	sc.Definition = json.RawMessage(`{"id":"root","type":"container","props":{"direction":"column","gap":"md","padding":"lg"},"children":[]}`)
	if err := s.apply(sc, in); err != nil {
		return nil, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	_, err := s.store.Meta.Exec(ctx, `INSERT INTO app_screens (`+cols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ID, appID, sc.Name, sc.Route, sc.Role.String(), sc.Position, string(sc.Definition), now, now)
	if err := uniqueErr(err, sc.Route); err != nil {
		return nil, err
	}
	return s.Get(ctx, appID, sc.ID)
}

func (s *Service) Update(ctx context.Context, appID, id string, in Input) (*Screen, error) {
	sc, err := s.Get(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if err := s.apply(sc, in); err != nil {
		return nil, err
	}
	_, err = s.store.Meta.Exec(ctx, `UPDATE app_screens SET name = ?, route = ?, role = ?, position = ?, definition = ?, updated_at = ? WHERE app_id = ? AND id = ?`,
		sc.Name, sc.Route, sc.Role.String(), sc.Position, string(sc.Definition), s.now().UTC().Format(time.RFC3339), appID, id)
	if err := uniqueErr(err, sc.Route); err != nil {
		return nil, err
	}
	return s.Get(ctx, appID, id)
}

func (s *Service) Delete(ctx context.Context, appID, id string) error {
	res, err := s.store.Meta.Exec(ctx, `DELETE FROM app_screens WHERE app_id = ? AND id = ?`, appID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// nodeRoles reads a component's "Visible to" roles from its props.
func nodeRoles(n map[string]any) []rbac.Role {
	props, _ := n["props"].(map[string]any)
	list, _ := props["roles"].([]any)
	var out []rbac.Role
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, rbac.Role(s))
		}
	}
	return out
}

// ComponentRoles lists the roles components of a tree are limited to.
func ComponentRoles(def json.RawMessage) []rbac.Role {
	var root map[string]any
	if json.Unmarshal(def, &root) != nil {
		return nil
	}
	var out []rbac.Role
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		out = append(out, nodeRoles(n)...)
		kids, _ := n["children"].([]any)
		for _, k := range kids {
			if m, ok := k.(map[string]any); ok {
				walk(m)
			}
		}
	}
	walk(root)
	return out
}

// Prune removes the components an actor may not see, with everything inside
// them, so a published app never receives them.
func Prune(def json.RawMessage, actor rbac.Actor) json.RawMessage {
	if actor.Builder {
		return def
	}
	dec := json.NewDecoder(bytes.NewReader(def))
	dec.UseNumber()
	var root map[string]any
	if dec.Decode(&root) != nil {
		return def
	}
	changed := false
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		kids, _ := n["children"].([]any)
		keep := kids[:0]
		for _, k := range kids {
			m, ok := k.(map[string]any)
			if !ok {
				continue
			}
			if !actor.HasAny(nodeRoles(m)) {
				changed = true
				continue
			}
			walk(m)
			keep = append(keep, m)
		}
		if kids != nil {
			n["children"] = keep
		}
	}
	walk(root)
	if !changed {
		return def
	}
	out, err := json.Marshal(root)
	if err != nil {
		return def
	}
	return out
}
