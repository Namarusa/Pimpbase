// Package rbac defines app roles and the actor every request runs as.
//
// Every app has four built-in roles, each including the one below it:
// ADMIN > POWER > BASIC > PUBLIC. An app may add custom roles; each custom
// role inherits exactly one other role and so holds everything that role
// holds. ADMIN holds every role of the app, custom ones included.
package rbac

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Role is the id of an app role: one of the built-in ids or a custom id
// such as SALES.
type Role string

const (
	// None is no role: the user cannot use the app.
	None   Role = "NONE"
	Public Role = "PUBLIC"
	Basic  Role = "BASIC"
	Power  Role = "POWER"
	Admin  Role = "ADMIN"
)

// Builtin lists the built-in roles from the least to the most powerful.
var Builtin = []Role{Public, Basic, Power, Admin}

func (r Role) String() string {
	if r == "" {
		return string(None)
	}
	return string(r)
}

// IsBuiltin reports whether r is one of the four built-in roles.
func (r Role) IsBuiltin() bool { return slices.Contains(Builtin, r) }

func (r *Role) UnmarshalText(b []byte) error {
	v, err := ParseRole(string(b))
	*r = v
	return err
}

var idRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,39}$`)

// ParseRole reads a role id. Built-in ids are case-insensitive; custom ids
// are upper case letters, digits and underscores. Whether a custom role
// exists in an app is checked against that app's Set.
func ParseRole(s string) (Role, error) {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "" || u == string(None) {
		return None, nil
	}
	if !idRe.MatchString(u) {
		return None, fmt.Errorf("%q is not a role id (use letters, digits and underscores)", s)
	}
	return Role(u), nil
}

// Def describes one role of an app.
type Def struct {
	ID          Role   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Inherits is the role this one includes. Built-in roles inherit the
	// built-in role below them; PUBLIC inherits nothing.
	Inherits Role `json:"inherits,omitempty"`
	// Color tags the role in the builder, as #rrggbb.
	Color   string `json:"color,omitempty"`
	Builtin bool   `json:"builtin"`
}

// BuiltinDefs returns the built-in roles, most powerful first.
func BuiltinDefs() []Def {
	return []Def{
		{ID: Admin, Name: "Admin", Description: "Everything in the app, including every custom role.", Inherits: Power, Builtin: true, Color: "#dc2626"},
		{ID: Power, Name: "Power", Description: "Basic access plus what tables and screens open to power users.", Inherits: Basic, Builtin: true, Color: "#7c3aed"},
		{ID: Basic, Name: "Basic", Description: "Signed-in users who were given access to the app.", Inherits: Public, Builtin: true, Color: "#2563eb"},
		{ID: Public, Name: "Public", Description: "Anyone, without signing in.", Builtin: true, Color: "#64748b"},
	}
}

// Set is the roles of one app.
type Set struct {
	defs map[Role]Def
	// order keeps custom roles in the order they were made.
	order []Role
}

// NewSet makes the role set of an app from its custom roles.
func NewSet(custom []Def) *Set {
	s := &Set{defs: map[Role]Def{}}
	for _, d := range BuiltinDefs() {
		s.defs[d.ID] = d
		s.order = append(s.order, d.ID)
	}
	for _, d := range custom {
		d.Builtin = false
		s.defs[d.ID] = d
		s.order = append(s.order, d.ID)
	}
	return s
}

// Defs lists the roles: built-in roles first, most powerful first, then
// custom roles.
func (s *Set) Defs() []Def {
	out := make([]Def, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.defs[id])
	}
	return out
}

// Has reports whether the role exists in the app.
func (s *Set) Has(r Role) bool {
	_, ok := s.defs[r]
	return ok
}

// Def returns a role's description.
func (s *Set) Def(r Role) (Def, bool) {
	d, ok := s.defs[r]
	return d, ok
}

// Chain lists r and every role it inherits, nearest first. ADMIN's chain
// also lists every custom role, after the built-in ones. Unknown roles have
// an empty chain.
func (s *Set) Chain(r Role) []Role {
	var out []Role
	for cur := r; cur != "" && cur != None; {
		d, ok := s.defs[cur]
		if !ok || slices.Contains(out, cur) {
			break
		}
		out = append(out, cur)
		cur = d.Inherits
	}
	if r == Admin {
		for _, id := range s.order {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

// CheckInherits reports an error when making role id inherit parent would
// point at a missing role or form a loop.
func (s *Set) CheckInherits(id, parent Role) error {
	if parent == "" || parent == None {
		return &InvalidError{"a custom role inherits another role; pick one"}
	}
	if !s.Has(parent) {
		return &InvalidError{fmt.Sprintf("there is no role %s to inherit", parent)}
	}
	if parent == Admin {
		return &InvalidError{"a role cannot inherit Admin; Admin already holds every role"}
	}
	for cur := parent; cur != "" && cur != None; {
		if cur == id {
			return &InvalidError{fmt.Sprintf("%s cannot inherit %s: that would make a loop", id, parent)}
		}
		d, ok := s.defs[cur]
		if !ok {
			break
		}
		cur = d.Inherits
	}
	return nil
}

// InvalidError messages are safe to show.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

// Actor is who a request runs as, inside one app.
type Actor struct {
	UserID string
	Email  string
	Name   string
	// Role is the actor's role in the app being accessed.
	Role Role
	// Roles is Role and every role it inherits. When nil, Role and the
	// built-in roles below it are used.
	Roles []Role
	// Groups are the user's groups from single sign-on, for row rules.
	Groups []string
	// Builder is true for workspace owners, admins and builders, who may
	// change the app's design.
	Builder bool
}

// NewActor makes an actor for role r in the app whose roles are set.
func NewActor(set *Set, r Role) Actor {
	return Actor{Role: r, Roles: set.Chain(r)}
}

// Chain is the roles the actor holds, nearest first.
func (a Actor) Chain() []Role {
	if a.Roles != nil {
		return a.Roles
	}
	return NewSet(nil).Chain(a.Role)
}

// Has reports whether the actor holds role r. Builders and app admins hold
// every role.
func (a Actor) Has(r Role) bool {
	if a.Builder || a.Role == Admin {
		return true
	}
	if r == "" || r == None {
		return false
	}
	return slices.Contains(a.Chain(), r)
}

// HasAny reports whether the actor holds one of the roles, or whether the
// list is empty.
func (a Actor) HasAny(rs []Role) bool {
	if len(rs) == 0 {
		return true
	}
	for _, r := range rs {
		if a.Has(r) {
			return true
		}
	}
	return false
}

// SignedIn reports whether the actor is a user with access to the app,
// that is, holds more than PUBLIC.
func (a Actor) SignedIn() bool {
	return a.Builder || (a.Role != Public && a.Role != None && a.Role != "")
}

// ErrForbidden is returned when the actor lacks a permission.
var ErrForbidden = errors.New("forbidden")

// Require checks that the actor holds the given role.
func (a Actor) Require(r Role, what string) error {
	if !a.Has(r) {
		return fmt.Errorf("%w: %s needs the %s role", ErrForbidden, what, r)
	}
	return nil
}
