// Package auth manages workspace users, password sign-in, sessions and API
// keys.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/bubibase/bubibase/internal/ids"
	"github.com/bubibase/bubibase/internal/store"
)

// Workspace roles. Owners and admins manage users; builders design apps;
// members only use the apps they are given access to.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleBuilder = "builder"
	RoleMember  = "member"
)

var roles = []string{RoleOwner, RoleAdmin, RoleBuilder, RoleMember}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	// Disabled users cannot sign in, and their API keys stop working.
	Disabled bool `json:"disabled"`
	// HasPassword is false for users who only sign in with single sign-on.
	HasPassword bool `json:"hasPassword"`
	// Groups come from the user's last single sign-on, for row rules.
	Groups      []string   `json:"groups"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
}

// IsBuilder reports whether the user may design apps.
func (u *User) IsBuilder() bool {
	return u != nil && (u.Role == RoleOwner || u.Role == RoleAdmin || u.Role == RoleBuilder)
}

// IsAdmin reports whether the user may manage users.
func (u *User) IsAdmin() bool { return u != nil && (u.Role == RoleOwner || u.Role == RoleAdmin) }

var (
	ErrBadCredentials = errors.New("wrong email or password")
	ErrNotFound       = errors.New("user not found")
	ErrSetupDone      = errors.New("this workspace is already set up")
	ErrDisabled       = errors.New("this account is switched off; ask a workspace admin")
)

type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

const SessionTTL = 30 * 24 * time.Hour

type Service struct {
	db  *store.DB
	now func() time.Time
}

func NewService(s *store.Store) *Service { return &Service{db: s.Meta, now: time.Now} }

// Password hashing with Argon2id, at the OWASP minimum cost.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash keeps sign-in timing the same for unknown emails.
var dummyHash = hashPassword("bubibase-timing")

func normEmail(e string) (string, error) {
	e = strings.ToLower(strings.TrimSpace(e))
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e {
		return "", &InvalidError{"enter a valid email address"}
	}
	return e, nil
}

func validPassword(pw string) error {
	if len(pw) < 10 {
		return &InvalidError{"the password needs at least 10 characters"}
	}
	if len(pw) > 200 {
		return &InvalidError{"the password is too long"}
	}
	return nil
}

func scanUser(row interface{ Scan(...any) error }) (*User, string, error) {
	var u User
	var hash, created, groups, last string
	var disabled int
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &hash, &created, &disabled, &groups, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if t, err := time.Parse(time.RFC3339, last); err == nil {
		u.LastLoginAt = &t
	}
	u.Disabled = disabled != 0
	u.HasPassword = hash != ""
	json.Unmarshal([]byte(groups), &u.Groups)
	if u.Groups == nil {
		u.Groups = []string{}
	}
	return &u, hash, nil
}

const userCols = `id, email, name, role, password_hash, created_at, disabled, sso_groups, last_login_at`

// NeedsSetup reports whether no user exists yet.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n == 0, err
}

// Setup creates the first user, the workspace owner.
func (s *Service) Setup(ctx context.Context, email, name, password string) (*User, error) {
	need, err := s.NeedsSetup(ctx)
	if err != nil {
		return nil, err
	}
	if !need {
		return nil, ErrSetupDone
	}
	return s.CreateUser(ctx, email, name, password, RoleOwner)
}

// CreateUser adds a user who signs in with a password.
func (s *Service) CreateUser(ctx context.Context, email, name, password, role string) (*User, error) {
	if err := validPassword(password); err != nil {
		return nil, err
	}
	return s.create(ctx, email, name, hashPassword(password), role)
}

// CreateSSOUser adds a user who signs in with single sign-on only.
func (s *Service) CreateSSOUser(ctx context.Context, email, name, role string) (*User, error) {
	return s.create(ctx, email, name, "", role)
}

func (s *Service) create(ctx context.Context, email, name, hash, role string) (*User, error) {
	email, err := normEmail(email)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	if !contains(roles, role) {
		return nil, &InvalidError{"unknown workspace role " + role}
	}
	u := &User{ID: ids.New("usr"), Email: email, Name: name, Role: role, HasPassword: hash != "", Groups: []string{}, CreatedAt: s.now().UTC().Truncate(time.Second)}
	_, err = s.db.Exec(ctx, `INSERT INTO users (`+userCols+`) VALUES (?, ?, ?, ?, ?, ?, 0, '[]', '')`,
		u.ID, u.Email, u.Name, u.Role, hash, u.CreatedAt.Format(time.RFC3339))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return nil, &InvalidError{"a user with this email already exists"}
	}
	return u, err
}

// ByEmail finds a user by email address.
func (s *Service) ByEmail(ctx context.Context, email string) (*User, error) {
	u, _, err := scanUser(s.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, strings.ToLower(strings.TrimSpace(email))))
	return u, err
}

// SetGroups records the groups single sign-on reported for a user.
func (s *Service) SetGroups(ctx context.Context, userID string, groups []string) error {
	if groups == nil {
		groups = []string{}
	}
	b, _ := json.Marshal(groups)
	_, err := s.db.Exec(ctx, `UPDATE users SET sso_groups = ? WHERE id = ?`, string(b), userID)
	return err
}

// SetWorkspaceRole changes a user's workspace role, keeping an owner.
func (s *Service) SetWorkspaceRole(ctx context.Context, userID, role string) error {
	_, err := s.Update(ctx, userID, UserPatch{Role: &role})
	return err
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (s *Service) Get(ctx context.Context, id string) (*User, error) {
	u, _, err := scanUser(s.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	return u, err
}

func (s *Service) List(ctx context.Context) ([]*User, error) {
	rows, err := s.db.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*User{}
	for rows.Next() {
		u, _, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserPatch changes a user. Nil members are left alone.
type UserPatch struct {
	Name     *string `json:"name"`
	Role     *string `json:"role"`
	Password *string `json:"password"`
	Disabled *bool   `json:"disabled"`
}

func (s *Service) Update(ctx context.Context, id string, p UserPatch) (*User, error) {
	u, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) != "" {
		u.Name = strings.TrimSpace(*p.Name)
	}
	if p.Role != nil && *p.Role != u.Role {
		if !contains(roles, *p.Role) {
			return nil, &InvalidError{"unknown workspace role " + *p.Role}
		}
		if u.Role == RoleOwner {
			if err := s.keepAnOwner(ctx); err != nil {
				return nil, err
			}
		}
		u.Role = *p.Role
	}
	if p.Disabled != nil && *p.Disabled != u.Disabled {
		if *p.Disabled && u.Role == RoleOwner {
			return nil, &InvalidError{"an owner cannot be switched off; make them an admin first"}
		}
		u.Disabled = *p.Disabled
		if u.Disabled {
			if _, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = ?`, u.ID); err != nil {
				return nil, err
			}
		}
	}
	disabled := 0
	if u.Disabled {
		disabled = 1
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET name = ?, role = ?, disabled = ? WHERE id = ?`, u.Name, u.Role, disabled, u.ID); err != nil {
		return nil, err
	}
	if p.Password != nil {
		if err := validPassword(*p.Password); err != nil {
			return nil, err
		}
		if _, err := s.db.Exec(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hashPassword(*p.Password), u.ID); err != nil {
			return nil, err
		}
		// A new password signs out every session.
		if _, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = ?`, u.ID); err != nil {
			return nil, err
		}
		u.HasPassword = true
	}
	return u, nil
}

func (s *Service) keepAnOwner(ctx context.Context) error {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = ?`, RoleOwner).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return &InvalidError{"the workspace needs at least one owner"}
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	u, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if u.Role == RoleOwner {
		if err := s.keepAnOwner(ctx); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM sessions WHERE user_id = ?`, `DELETE FROM api_keys WHERE user_id = ?`, `DELETE FROM app_users WHERE user_id = ?`, `DELETE FROM user_identities WHERE user_id = ?`, `DELETE FROM users WHERE id = ?`} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Login checks a password and starts a session, returning its token.
func (s *Service) Login(ctx context.Context, email, password string) (*User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, hash, err := scanUser(s.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
	if errors.Is(err, ErrNotFound) {
		checkPassword(dummyHash, password)
		return nil, "", ErrBadCredentials
	}
	if err != nil {
		return nil, "", err
	}
	if hash == "" {
		// Single sign-on users have no password.
		checkPassword(dummyHash, password)
		return nil, "", ErrBadCredentials
	}
	if !checkPassword(hash, password) {
		return nil, "", ErrBadCredentials
	}
	if u.Disabled {
		return nil, "", ErrDisabled
	}
	token, err := s.StartSession(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	return u, token, nil
}

// StartSession signs a user in, returning the session token.
func (s *Service) StartSession(ctx context.Context, userID string) (string, error) {
	token := ids.Secret("bbs")
	now := s.now().UTC()
	exp := now.Add(SessionTTL).Format(time.RFC3339)
	if _, err := s.db.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, ids.Hash(token), userID, exp); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now.Format(time.RFC3339), userID); err != nil {
		return "", err
	}
	// Drop expired sessions now and then.
	s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now.Format(time.RFC3339))
	return token, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = ?`, ids.Hash(token))
	return err
}

// FromSession returns the user of a session token.
func (s *Service) FromSession(ctx context.Context, token string) (*User, error) {
	var userID, exp string
	err := s.db.QueryRow(ctx, `SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`, ids.Hash(token)).Scan(&userID, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if t, _ := time.Parse(time.RFC3339, exp); s.now().After(t) {
		return nil, ErrNotFound
	}
	u, err := s.Get(ctx, userID)
	if err == nil && u.Disabled {
		return nil, ErrNotFound
	}
	return u, err
}

// APIKey is a token for machine access; it acts as its user, limited to
// one app and one role of that app when AppID is set.
type APIKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// AppID limits the key to one app's data; builder and admin endpoints
	// refuse it.
	AppID string `json:"appId"`
	// Role, with AppID, is the app role the key acts with. The user must
	// still hold it when the key is used.
	Role       string     `json:"role"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	// Key is only set when the key is created.
	Key string `json:"key,omitempty"`
}

// Scoped reports whether the key is limited to one app.
func (k *APIKey) Scoped() bool { return k != nil && k.AppID != "" }

// KeyInput makes an API key.
type KeyInput struct {
	Name  string `json:"name"`
	AppID string `json:"appId"`
	Role  string `json:"role"`
	// ExpiresInDays is 0 for a key that never expires.
	ExpiresInDays int `json:"expiresInDays"`
}

func (s *Service) CreateAPIKey(ctx context.Context, userID string, in KeyInput) (*APIKey, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		return nil, &InvalidError{"give the key a name of up to 100 characters"}
	}
	if in.ExpiresInDays < 0 || in.ExpiresInDays > 3650 {
		return nil, &InvalidError{"a key expires in 1 to 3650 days, or never"}
	}
	if in.Role != "" && in.AppID == "" {
		return nil, &InvalidError{"pick the app the role belongs to"}
	}
	now := s.now().UTC().Truncate(time.Second)
	k := &APIKey{ID: ids.New("key"), Name: name, AppID: in.AppID, Role: in.Role, CreatedAt: now, Key: ids.Secret("bbk")}
	exp := ""
	if in.ExpiresInDays > 0 {
		t := now.Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)
		k.ExpiresAt = &t
		exp = t.Format(time.RFC3339)
	}
	_, err := s.db.Exec(ctx, `INSERT INTO api_keys (id, user_id, name, key_hash, created_at, app_id, role, expires_at, last_used_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')`,
		k.ID, userID, k.Name, ids.Hash(k.Key), k.CreatedAt.Format(time.RFC3339), k.AppID, k.Role, exp)
	return k, err
}

const keyCols = `id, name, app_id, role, created_at, expires_at, last_used_at`

func scanKey(row interface{ Scan(...any) error }) (*APIKey, error) {
	var k APIKey
	var created, exp, used string
	if err := row.Scan(&k.ID, &k.Name, &k.AppID, &k.Role, &created, &exp, &used); err != nil {
		return nil, err
	}
	k.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if t, err := time.Parse(time.RFC3339, exp); err == nil {
		k.ExpiresAt = &t
	}
	if t, err := time.Parse(time.RFC3339, used); err == nil {
		k.LastUsedAt = &t
	}
	return &k, nil
}

func (s *Service) APIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.db.Query(ctx, `SELECT `+keyCols+` FROM api_keys WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func (s *Service) DeleteAPIKey(ctx context.Context, userID, keyID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, keyID, userID)
	return err
}

// FromAPIKey returns the user an API key belongs to, and the key.
func (s *Service) FromAPIKey(ctx context.Context, key string) (*User, *APIKey, error) {
	var userID string
	row := s.db.QueryRow(ctx, `SELECT user_id, `+keyCols+` FROM api_keys WHERE key_hash = ?`, ids.Hash(key))
	k, err := scanKey(scanFirst{row, &userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	now := s.now().UTC()
	if k.ExpiresAt != nil && now.After(*k.ExpiresAt) {
		return nil, nil, ErrNotFound
	}
	u, err := s.Get(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if u.Disabled {
		return nil, nil, ErrNotFound
	}
	// Note the use at most once a minute, to keep writes down.
	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > time.Minute {
		s.db.Exec(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now.Format(time.RFC3339), k.ID)
	}
	return u, k, nil
}

// scanFirst scans one leading column into dst, then the rest as usual.
type scanFirst struct {
	row interface{ Scan(...any) error }
	dst any
}

func (s scanFirst) Scan(dest ...any) error { return s.row.Scan(append([]any{s.dst}, dest...)...) }
