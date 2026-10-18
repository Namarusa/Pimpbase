// Package rbac defines app roles and the actor every request runs as.
package rbac

import (
	"errors"
	"fmt"
	"strings"
)

// Role is an app role. Each role includes every permission of the roles
// below it.
type Role int

const (
	None Role = iota - 1
	Public
	Basic
	Power
	Admin
)

var names = []string{"PUBLIC", "BASIC", "POWER", "ADMIN"}

func (r Role) String() string {
	if r < Public || r > Admin {
		return "NONE"
	}
	return names[r]
}

func (r Role) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

func (r *Role) UnmarshalText(b []byte) error {
	v, err := ParseRole(string(b))
	*r = v
	return err
}

func ParseRole(s string) (Role, error) {
	for i, n := range names {
		if strings.EqualFold(s, n) {
			return Role(i), nil
		}
	}
	if strings.EqualFold(s, "NONE") {
		return None, nil
	}
	return None, fmt.Errorf("unknown role %q (use PUBLIC, BASIC, POWER or ADMIN)", s)
}

// Actor is who a request runs as, inside one app.
type Actor struct {
	UserID string
	Email  string
	Name   string
	// Role is the actor's role in the app being accessed.
	Role Role
	// Builder is true for workspace owners, admins and builders, who may
	// change the app's design.
	Builder bool
}

// ErrForbidden is returned when the actor lacks a permission.
var ErrForbidden = errors.New("forbidden")

// Require checks that the actor holds at least the given role.
func (a Actor) Require(min Role, what string) error {
	if a.Role < min || a.Role == None {
		return fmt.Errorf("%w: %s needs the %s role", ErrForbidden, what, min)
	}
	return nil
}
