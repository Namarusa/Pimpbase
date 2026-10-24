package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pimpbase/pimpbase/internal/ids"
)

// One-time links sent by email: an invitation to choose a password, a
// password reset, or a sign-in link. Only the token's hash is stored, a
// new link of a kind replaces the older one, and using a link spends it.

const (
	TokenInvite = "invite"
	TokenReset  = "reset"
	TokenSignIn = "sign_in"
)

// TokenTTL is how long each kind of link works.
var TokenTTL = map[string]time.Duration{
	TokenInvite: 7 * 24 * time.Hour,
	TokenReset:  time.Hour,
	TokenSignIn: 15 * time.Minute,
}

// ErrBadToken is a link that is unknown, spent or expired.
var ErrBadToken = errors.New("this link has expired or was used already; ask for a new one")

// CreateInvited adds a user who has no password yet; an invitation link
// lets them choose one.
func (s *Service) CreateInvited(ctx context.Context, email, name, role string) (*User, error) {
	return s.create(ctx, email, name, "", role)
}

// NewToken makes a link token for a user.
func (s *Service) NewToken(ctx context.Context, userID, kind string) (string, error) {
	ttl, ok := TokenTTL[kind]
	if !ok {
		return "", errors.New("unknown token kind " + kind)
	}
	token := ids.Secret("lnk")
	now := s.now().UTC()
	if _, err := s.db.Exec(ctx, `DELETE FROM user_tokens WHERE (user_id = ? AND kind = ?) OR expires_at < ?`, userID, kind, now.Format(time.RFC3339)); err != nil {
		return "", err
	}
	_, err := s.db.Exec(ctx, `INSERT INTO user_tokens (token_hash, user_id, kind, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		ids.Hash(token), userID, kind, now.Add(ttl).Format(time.RFC3339), now.Format(time.RFC3339))
	return token, err
}

// PeekToken tells who a link is for, without spending it.
func (s *Service) PeekToken(ctx context.Context, token string, kinds ...string) (*User, string, error) {
	var userID, kind, exp string
	err := s.db.QueryRow(ctx, `SELECT user_id, kind, expires_at FROM user_tokens WHERE token_hash = ?`, ids.Hash(token)).Scan(&userID, &kind, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrBadToken
	}
	if err != nil {
		return nil, "", err
	}
	return s.tokenUser(ctx, userID, kind, exp, kinds)
}

func (s *Service) tokenUser(ctx context.Context, userID, kind, exp string, kinds []string) (*User, string, error) {
	if !contains(kinds, kind) {
		return nil, "", ErrBadToken
	}
	if t, err := time.Parse(time.RFC3339, exp); err != nil || s.now().After(t) {
		return nil, "", ErrBadToken
	}
	u, err := s.Get(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return nil, "", ErrBadToken
	}
	if err != nil {
		return nil, "", err
	}
	if u.Disabled {
		return nil, "", ErrDisabled
	}
	return u, kind, nil
}

// useToken spends a link: only one request can.
func (s *Service) useToken(ctx context.Context, token string, kinds ...string) (*User, string, error) {
	h := ids.Hash(token)
	var userID, kind, exp string
	err := s.db.QueryRow(ctx, `SELECT user_id, kind, expires_at FROM user_tokens WHERE token_hash = ?`, h).Scan(&userID, &kind, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrBadToken
	}
	if err != nil {
		return nil, "", err
	}
	if !contains(kinds, kind) {
		return nil, "", ErrBadToken
	}
	res, err := s.db.Exec(ctx, `DELETE FROM user_tokens WHERE token_hash = ?`, h)
	if err != nil {
		return nil, "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, "", ErrBadToken
	}
	return s.tokenUser(ctx, userID, kind, exp, kinds)
}

// SetPasswordWithToken spends an invitation or reset link to set a new
// password, signs out the user's other sessions, and starts a session.
func (s *Service) SetPasswordWithToken(ctx context.Context, token, password string) (*User, string, error) {
	if err := validPassword(password); err != nil {
		return nil, "", err
	}
	u, _, err := s.useToken(ctx, token, TokenInvite, TokenReset)
	if err != nil {
		return nil, "", err
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hashPassword(password), u.ID); err != nil {
		return nil, "", err
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = ?`, u.ID); err != nil {
		return nil, "", err
	}
	u.HasPassword = true
	session, err := s.StartSession(ctx, u.ID)
	return u, session, err
}

// SignInWithToken spends a sign-in link and starts a session.
func (s *Service) SignInWithToken(ctx context.Context, token string) (*User, string, error) {
	u, _, err := s.useToken(ctx, token, TokenSignIn)
	if err != nil {
		return nil, "", err
	}
	session, err := s.StartSession(ctx, u.ID)
	return u, session, err
}

// RecentToken tells whether a link of a kind was made for a user within d,
// so links are not sent over and over.
func (s *Service) RecentToken(ctx context.Context, userID, kind string, d time.Duration) bool {
	var n int
	s.db.QueryRow(ctx, `SELECT COUNT(*) FROM user_tokens WHERE user_id = ? AND kind = ? AND created_at > ?`, userID, kind, s.now().UTC().Add(-d).Format(time.RFC3339)).Scan(&n)
	return n > 0
}
