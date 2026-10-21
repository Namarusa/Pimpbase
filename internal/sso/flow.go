package sso

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/pimpbase/pimpbase/internal/ids"
)

// StateTTL is how long a sign-in may take at the provider.
const StateTTL = 10 * time.Minute

// HTTPClient talks to providers; tests may replace it.
var HTTPClient = &http.Client{Timeout: 15 * time.Second}

type discoveryCache struct {
	mu sync.Mutex
	m  map[string]cachedProvider
}

type cachedProvider struct {
	p  *oidc.Provider
	at time.Time
}

func newDiscoveryCache() *discoveryCache { return &discoveryCache{m: map[string]cachedProvider{}} }

func (c *discoveryCache) forget(issuer string) {
	c.mu.Lock()
	delete(c.m, issuer)
	c.mu.Unlock()
}

func clientCtx(ctx context.Context) context.Context { return oidc.ClientContext(ctx, HTTPClient) }

// discover fetches a provider's OpenID metadata, keeping it for an hour.
func (s *Service) discover(ctx context.Context, p *Provider) (*oidc.Provider, error) {
	issuer := p.IssuerURL()
	s.cache.mu.Lock()
	if c, ok := s.cache.m[issuer]; ok && time.Since(c.at) < time.Hour {
		s.cache.mu.Unlock()
		return c.p, nil
	}
	s.cache.mu.Unlock()
	ctx = clientCtx(ctx)
	if p.multiTenant() {
		// Multi-tenant Microsoft metadata names a placeholder issuer; the
		// real one is checked per token in verify.
		ctx = oidc.InsecureIssuerURLContext(ctx, "https://login.microsoftonline.com/{tenantid}/v2.0")
	}
	op, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, &SignInError{fmt.Sprintf("could not reach %s: %v", p.Name, err)}
	}
	s.cache.mu.Lock()
	s.cache.m[issuer] = cachedProvider{op, time.Now()}
	s.cache.mu.Unlock()
	return op, nil
}

func (s *Service) oauth(op *oidc.Provider, p *Provider, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: p.ClientID, ClientSecret: p.secret, RedirectURL: redirectURL,
		Endpoint: op.Endpoint(), Scopes: p.Scopes,
	}
}

func random() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SafeReturn keeps a post-sign-in destination on this site.
func SafeReturn(to string) string {
	if !strings.HasPrefix(to, "/") || strings.HasPrefix(to, "//") || strings.HasPrefix(to, "/\\") || strings.ContainsAny(to, "\r\n") {
		return "/"
	}
	return to
}

// Start begins a sign-in: it remembers the state and returns the URL of
// the provider's sign-in page.
func (s *Service) Start(ctx context.Context, providerID, redirectURL, returnTo string) (string, error) {
	p, err := s.Get(ctx, providerID)
	if err != nil {
		return "", err
	}
	if !p.Enabled {
		return "", &SignInError{p.Name + " sign-in is switched off"}
	}
	op, err := s.discover(ctx, p)
	if err != nil {
		return "", err
	}
	state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
	now := s.now().UTC()
	s.db.Exec(ctx, `DELETE FROM sso_states WHERE expires_at < ?`, now.Format(time.RFC3339))
	if _, err := s.db.Exec(ctx, `INSERT INTO sso_states (state_hash, provider_id, nonce, verifier, return_to, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		ids.Hash(state), p.ID, nonce, verifier, SafeReturn(returnTo), now.Add(StateTTL).Format(time.RFC3339)); err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if p.Kind == Google || p.Kind == Microsoft {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}
	return s.oauth(op, p, redirectURL).AuthCodeURL(state, opts...), nil
}

// Result is a finished sign-in at a provider.
type Result struct {
	Provider *Provider
	Identity *Identity
	ReturnTo string
}

var msIssuerRe = regexp.MustCompile(`^https://login\.microsoftonline\.com/([0-9a-fA-F-]{36})/v2\.0$`)

// Finish completes a sign-in from the provider's redirect back: it checks
// the state, swaps the code for tokens and verifies the ID token.
func (s *Service) Finish(ctx context.Context, state, code, redirectURL string) (*Result, error) {
	var providerID, nonce, verifier, returnTo, exp string
	err := s.db.QueryRow(ctx, `SELECT provider_id, nonce, verifier, return_to, expires_at FROM sso_states WHERE state_hash = ?`, ids.Hash(state)).
		Scan(&providerID, &nonce, &verifier, &returnTo, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &SignInError{"this sign-in link was already used or is unknown; start again"}
	}
	if err != nil {
		return nil, err
	}
	// A state works once.
	if _, err := s.db.Exec(ctx, `DELETE FROM sso_states WHERE state_hash = ?`, ids.Hash(state)); err != nil {
		return nil, err
	}
	if t, _ := time.Parse(time.RFC3339, exp); s.now().After(t) {
		return nil, &SignInError{"the sign-in took too long; start again"}
	}
	p, err := s.Get(ctx, providerID)
	if err != nil {
		return nil, &SignInError{"this sign-in provider was removed"}
	}
	if !p.Enabled {
		return nil, &SignInError{p.Name + " sign-in is switched off"}
	}
	op, err := s.discover(ctx, p)
	if err != nil {
		return nil, err
	}
	cctx := clientCtx(ctx)
	tok, err := s.oauth(op, p, redirectURL).Exchange(cctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, &SignInError{fmt.Sprintf("%s refused the sign-in: %v", p.Name, err)}
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, &SignInError{p.Name + " sent no ID token; check that the openid scope is allowed"}
	}
	vcfg := &oidc.Config{ClientID: p.ClientID, SkipIssuerCheck: p.multiTenant(), Now: s.now}
	idt, err := op.Verifier(vcfg).Verify(cctx, raw)
	if err != nil {
		return nil, &SignInError{fmt.Sprintf("the ID token from %s is not valid: %v", p.Name, err)}
	}
	if idt.Nonce != nonce {
		return nil, &SignInError{"the ID token was not made for this sign-in; start again"}
	}
	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		return nil, err
	}
	if p.multiTenant() {
		m := msIssuerRe.FindStringSubmatch(idt.Issuer)
		if m == nil || !strings.EqualFold(m[1], fmt.Sprint(claims["tid"])) {
			return nil, &SignInError{"the ID token's issuer does not match its Microsoft tenant"}
		}
	}
	id := &Identity{ProviderID: p.ID, Subject: idt.Subject, Claims: claims}
	id.Email, _ = claims["email"].(string)
	if v, ok := claims["email_verified"].(bool); ok {
		id.EmailVerified = v
	} else if v, ok := claims["email_verified"].(string); ok {
		id.EmailVerified = v == "true"
	}
	if id.Email == "" && p.Kind == Microsoft {
		// Work accounts without a mailbox carry their sign-in name instead.
		id.Email, _ = claims["preferred_username"].(string)
	}
	id.Name, _ = claims["name"].(string)
	id.Groups = textValues(claims[p.GroupsClaim])
	if id.Email == "" || id.Name == "" {
		// Some providers only put these in the userinfo response.
		if ui, err := op.UserInfo(cctx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == idt.Subject {
			extra := map[string]any{}
			ui.Claims(&extra)
			if id.Email == "" {
				id.Email, id.EmailVerified = ui.Email, ui.EmailVerified
			}
			if id.Name == "" {
				id.Name, _ = extra["name"].(string)
			}
			if len(id.Groups) == 0 {
				id.Groups = textValues(extra[p.GroupsClaim])
			}
		}
	}
	if id.Groups == nil {
		id.Groups = []string{}
	}
	return &Result{Provider: p, Identity: id, ReturnTo: returnTo}, nil
}
