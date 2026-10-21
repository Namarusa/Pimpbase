// Package sso signs users in with OpenID Connect providers (Google,
// Microsoft Entra ID or any standard provider) and maps what the provider
// says about them to workspace and app roles.
package sso

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/auth"
	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/store"
)

// Provider kinds.
const (
	Google    = "google"
	Microsoft = "microsoft"
	OIDC      = "oidc"
)

// Provider is an identity provider users may sign in with.
type Provider struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Config
	// HasSecret says a client secret is stored; the secret itself is never
	// sent back.
	HasSecret bool      `json:"hasSecret"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	secret string
}

// Config is a provider's settings.
type Config struct {
	// Issuer is the OpenID issuer URL of a generic provider. Google and
	// Microsoft providers fill it in themselves.
	Issuer string `json:"issuer"`
	// Tenant is the Microsoft Entra tenant: a tenant id or domain, or
	// "organizations" for any work or school account.
	Tenant   string   `json:"tenant"`
	ClientID string   `json:"clientId"`
	Scopes   []string `json:"scopes"`
	// AllowedDomains, when set, limits sign-in to these email domains.
	AllowedDomains []string `json:"allowedDomains"`
	// AutoCreate makes an account on first sign-in for people who have
	// none; otherwise an admin adds them first.
	AutoCreate bool `json:"autoCreate"`
	// DefaultRole is the workspace role of accounts made on sign-in when
	// no mapping gives one: member or builder.
	DefaultRole string `json:"defaultRole"`
	// TrustEmail links a sign-in to an existing account with the same email
	// even when the provider does not say the email is verified.
	TrustEmail bool `json:"trustEmail"`
	// GroupsClaim names the ID token claim that lists the user's groups.
	GroupsClaim string    `json:"groupsClaim"`
	Mappings    []Mapping `json:"mappings"`
}

// Mapping gives roles to users whose sign-in matches.
type Mapping struct {
	// Claim is "group", "email_domain", "email" or the name of any claim.
	Claim string `json:"claim"`
	Value string `json:"value"`
	// WorkspaceRole, when set, is member, builder or admin.
	WorkspaceRole string    `json:"workspaceRole"`
	Apps          []AppRole `json:"apps"`
}

// AppRole is a role in one app.
type AppRole struct {
	AppID string    `json:"appId"`
	Role  rbac.Role `json:"role"`
}

// Input makes or changes a provider. A nil ClientSecret keeps the stored
// one.
type Input struct {
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	Enabled      *bool   `json:"enabled"`
	ClientSecret *string `json:"clientSecret"`
	Config
}

var (
	ErrNotFound = errors.New("sign-in provider not found")
	domainRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	claimRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_:./-]{0,99}$`)
)

// InvalidError messages are safe to show.
type InvalidError struct{ Field, Message string }

func (e *InvalidError) Error() string { return e.Message }

func invalid(field, format string, args ...any) error {
	return &InvalidError{field, fmt.Sprintf(format, args...)}
}

// Apps is what sso needs to know about apps and their members.
type Apps interface {
	Roles(ctx context.Context, appID string) (*rbac.Set, error)
	SyncSSO(ctx context.Context, userID string, roles map[string]rbac.Role) error
}

type Service struct {
	db   *store.DB
	auth *auth.Service
	apps Apps
	now  func() time.Time
	// discovery caches provider metadata.
	cache *discoveryCache
}

func NewService(s *store.Store, a *auth.Service, apps Apps) *Service {
	return &Service{db: s.Meta, auth: a, apps: apps, now: time.Now, cache: newDiscoveryCache()}
}

// IssuerURL is the issuer a provider signs in with.
func (p *Provider) IssuerURL() string {
	switch p.Kind {
	case Google:
		return "https://accounts.google.com"
	case Microsoft:
		return "https://login.microsoftonline.com/" + url.PathEscape(p.tenant()) + "/v2.0"
	}
	return p.Issuer
}

func (p *Provider) tenant() string {
	if p.Tenant == "" {
		return "organizations"
	}
	return p.Tenant
}

// multiTenant reports whether a Microsoft provider accepts accounts from
// any tenant, whose tokens carry their own tenant's issuer.
func (p *Provider) multiTenant() bool {
	if p.Kind != Microsoft {
		return false
	}
	t := strings.ToLower(p.tenant())
	return t == "organizations" || t == "common" || t == "consumers"
}

func (in *Input) normalize(old *Provider) (*Provider, error) {
	p := &Provider{Enabled: true}
	if old != nil {
		cp := *old
		p = &cp
	}
	if old == nil {
		switch in.Kind {
		case Google, Microsoft, OIDC:
			p.Kind = in.Kind
		default:
			return nil, invalid("kind", "a provider is google, microsoft or oidc")
		}
	}
	p.Name = strings.TrimSpace(in.Name)
	if p.Name == "" {
		p.Name = map[string]string{Google: "Google", Microsoft: "Microsoft", OIDC: "Single sign-on"}[p.Kind]
	}
	if len(p.Name) > 60 {
		return nil, invalid("name", "the button label has at most 60 characters")
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	c := in.Config
	c.ClientID = strings.TrimSpace(c.ClientID)
	if c.ClientID == "" || len(c.ClientID) > 300 {
		return nil, invalid("clientId", "enter the client id the provider gave you")
	}
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	c.Tenant = strings.TrimSpace(c.Tenant)
	switch p.Kind {
	case OIDC:
		u, err := url.Parse(c.Issuer)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, invalid("issuer", "the issuer is a URL such as https://login.example.com")
		}
		c.Tenant = ""
	case Microsoft:
		if c.Tenant != "" && !regexp.MustCompile(`^[A-Za-z0-9.-]{1,100}$`).MatchString(c.Tenant) {
			return nil, invalid("tenant", "the tenant is a tenant id, a domain, or organizations")
		}
		c.Issuer = ""
	default:
		c.Issuer, c.Tenant = "", ""
	}
	scopes := []string{}
	for _, sc := range c.Scopes {
		sc = strings.TrimSpace(sc)
		if sc == "" || slices.Contains(scopes, sc) {
			continue
		}
		if strings.ContainsAny(sc, " \t\"") || len(sc) > 200 {
			return nil, invalid("scopes", "scopes are single words such as profile or groups")
		}
		scopes = append(scopes, sc)
	}
	for _, need := range []string{"openid", "email", "profile"} {
		if !slices.Contains(scopes, need) {
			scopes = append([]string{need}, scopes...)
		}
	}
	slices.SortStableFunc(scopes, func(a, b string) int {
		rank := func(s string) int {
			if i := slices.Index([]string{"openid", "email", "profile"}, s); i >= 0 {
				return i
			}
			return 3
		}
		return rank(a) - rank(b)
	})
	c.Scopes = scopes
	domains := []string{}
	for _, d := range c.AllowedDomains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d == "" || slices.Contains(domains, d) {
			continue
		}
		if !domainRe.MatchString(d) {
			return nil, invalid("allowedDomains", "%q is not an email domain", d)
		}
		domains = append(domains, d)
	}
	c.AllowedDomains = domains
	switch c.DefaultRole {
	case "":
		c.DefaultRole = auth.RoleMember
	case auth.RoleMember, auth.RoleBuilder:
	default:
		return nil, invalid("defaultRole", "accounts made on sign-in are members or builders")
	}
	c.GroupsClaim = strings.TrimSpace(c.GroupsClaim)
	if c.GroupsClaim == "" {
		c.GroupsClaim = "groups"
	}
	if !claimRe.MatchString(c.GroupsClaim) {
		return nil, invalid("groupsClaim", "the groups claim is a claim name such as groups or roles")
	}
	if len(c.Mappings) > 100 {
		return nil, invalid("mappings", "a provider has at most 100 role mappings")
	}
	for i := range c.Mappings {
		m := &c.Mappings[i]
		m.Claim = strings.TrimSpace(m.Claim)
		m.Value = strings.TrimSpace(m.Value)
		if m.Claim == "" {
			m.Claim = "group"
		}
		if !claimRe.MatchString(m.Claim) {
			return nil, invalid("mappings", "rule %d: the claim is group, email_domain, email or a claim name", i+1)
		}
		if m.Value == "" {
			return nil, invalid("mappings", "rule %d: enter the value to match", i+1)
		}
		switch m.WorkspaceRole {
		case "", auth.RoleMember, auth.RoleBuilder, auth.RoleAdmin:
		default:
			return nil, invalid("mappings", "rule %d: single sign-on gives the member, builder or admin workspace role", i+1)
		}
		if m.Apps == nil {
			m.Apps = []AppRole{}
		}
		if m.WorkspaceRole == "" && len(m.Apps) == 0 {
			return nil, invalid("mappings", "rule %d: give a workspace role or an app role", i+1)
		}
	}
	if c.Mappings == nil {
		c.Mappings = []Mapping{}
	}
	p.Config = c
	if in.ClientSecret != nil {
		p.secret = strings.TrimSpace(*in.ClientSecret)
	}
	p.HasSecret = p.secret != ""
	if !p.HasSecret {
		return nil, invalid("clientSecret", "enter the client secret the provider gave you")
	}
	return p, nil
}

// checkApps makes sure every app role a provider maps to exists.
func (s *Service) checkApps(ctx context.Context, p *Provider) error {
	for i, m := range p.Mappings {
		for _, ar := range m.Apps {
			set, err := s.apps.Roles(ctx, ar.AppID)
			if err != nil {
				return err
			}
			if !set.Has(ar.Role) || ar.Role == rbac.Public {
				return invalid("mappings", "rule %d: the app has no role %s", i+1, ar.Role)
			}
		}
	}
	return nil
}

const cols = `id, kind, name, enabled, config, client_secret, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (*Provider, error) {
	var p Provider
	var enabled int
	var cfg, created, updated string
	if err := row.Scan(&p.ID, &p.Kind, &p.Name, &enabled, &cfg, &p.secret, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Enabled = enabled != 0
	p.HasSecret = p.secret != ""
	json.Unmarshal([]byte(cfg), &p.Config)
	if p.Mappings == nil {
		p.Mappings = []Mapping{}
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &p, nil
}

func (s *Service) List(ctx context.Context) ([]*Provider, error) {
	rows, err := s.db.Query(ctx, `SELECT `+cols+` FROM sso_providers ORDER BY position, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Provider{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (*Provider, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+cols+` FROM sso_providers WHERE id = ?`, id))
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Service) Create(ctx context.Context, in Input) (*Provider, error) {
	p, err := in.normalize(nil)
	if err != nil {
		return nil, err
	}
	if err := s.checkApps(ctx, p); err != nil {
		return nil, err
	}
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM sso_providers`).Scan(&n); err != nil {
		return nil, err
	}
	if n >= 20 {
		return nil, invalid("kind", "a workspace has at most 20 sign-in providers")
	}
	now := s.now().UTC().Truncate(time.Second)
	p.ID, p.CreatedAt, p.UpdatedAt = ids.New("sso"), now, now
	cfg, _ := json.Marshal(p.Config)
	_, err = s.db.Exec(ctx, `INSERT INTO sso_providers (`+cols+`, position) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Kind, p.Name, b2i(p.Enabled), string(cfg), p.secret, now.Format(time.RFC3339), now.Format(time.RFC3339), n)
	return p, err
}

func (s *Service) Update(ctx context.Context, id string, in Input) (*Provider, error) {
	old, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := in.normalize(old)
	if err != nil {
		return nil, err
	}
	if err := s.checkApps(ctx, p); err != nil {
		return nil, err
	}
	p.UpdatedAt = s.now().UTC().Truncate(time.Second)
	cfg, _ := json.Marshal(p.Config)
	_, err = s.db.Exec(ctx, `UPDATE sso_providers SET name = ?, enabled = ?, config = ?, client_secret = ?, updated_at = ? WHERE id = ?`,
		p.Name, b2i(p.Enabled), string(cfg), p.secret, p.UpdatedAt.Format(time.RFC3339), id)
	s.cache.forget(old.IssuerURL())
	return p, err
}

// Delete removes a provider. Users who signed in with it keep their
// accounts but must use another way to sign in.
func (s *Service) Delete(ctx context.Context, id string) error {
	p, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM user_identities WHERE provider_id = ?`, `DELETE FROM sso_states WHERE provider_id = ?`, `DELETE FROM sso_providers WHERE id = ?`} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return err
		}
	}
	s.cache.forget(p.IssuerURL())
	return tx.Commit()
}

// Public is what the sign-in page shows about a provider.
type Public struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Enabled lists the providers people may sign in with.
func (s *Service) Enabled(ctx context.Context) ([]Public, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []Public{}
	for _, p := range all {
		if p.Enabled {
			out = append(out, Public{p.ID, p.Kind, p.Name})
		}
	}
	return out, nil
}

// Identity is who a provider says signed in.
type Identity struct {
	ProviderID    string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Groups        []string
	// Claims are the ID token's claims, for mappings on other claims.
	Claims map[string]any
}

func (id *Identity) domain() string {
	_, d, _ := strings.Cut(id.Email, "@")
	return d
}

// claimValues lists the values of a claim as text.
func (id *Identity) claimValues(name string) []string {
	switch name {
	case "group":
		return id.Groups
	case "email_domain":
		return []string{id.domain()}
	case "email":
		return []string{id.Email}
	}
	return textValues(id.Claims[name])
}

func textValues(v any) []string {
	switch v := v.(type) {
	case nil:
		return nil
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, x := range v {
			out = append(out, textValues(x)...)
		}
		return out
	case bool, float64, json.Number:
		return []string{fmt.Sprint(v)}
	}
	return nil
}

// Roles is what a provider's mappings give one sign-in.
type Roles struct {
	// Workspace is the most powerful workspace role any matching rule
	// gives, or "".
	Workspace string `json:"workspace"`
	// Apps holds, per app, the role of the first matching rule that names
	// the app.
	Apps map[string]rbac.Role `json:"apps"`
	// Matched lists the numbers of the rules that matched, from 1.
	Matched []int `json:"matched"`
}

var workspaceRank = map[string]int{auth.RoleMember: 1, auth.RoleBuilder: 2, auth.RoleAdmin: 3}

// Evaluate runs a provider's mappings against a sign-in.
func (p *Provider) Evaluate(id *Identity) Roles {
	out := Roles{Apps: map[string]rbac.Role{}}
	for i, m := range p.Mappings {
		hit := false
		for _, v := range id.claimValues(m.Claim) {
			if strings.EqualFold(v, m.Value) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		out.Matched = append(out.Matched, i+1)
		if workspaceRank[m.WorkspaceRole] > workspaceRank[out.Workspace] {
			out.Workspace = m.WorkspaceRole
		}
		for _, ar := range m.Apps {
			if _, taken := out.Apps[ar.AppID]; !taken {
				out.Apps[ar.AppID] = ar.Role
			}
		}
	}
	return out
}

// SignInError is a reason a sign-in was refused, safe to show.
type SignInError struct{ Message string }

func (e *SignInError) Error() string { return e.Message }

// SignIn finds or makes the account of a provider's sign-in and applies the
// provider's role mappings to it.
func (s *Service) SignIn(ctx context.Context, p *Provider, id *Identity) (*auth.User, error) {
	id.Email = strings.ToLower(strings.TrimSpace(id.Email))
	if id.Email == "" {
		return nil, &SignInError{"the provider did not share your email address; ask an admin to add the email scope"}
	}
	if len(p.AllowedDomains) > 0 && !slices.Contains(p.AllowedDomains, id.domain()) {
		return nil, &SignInError{fmt.Sprintf("accounts from %s cannot sign in here", id.domain())}
	}
	roles := p.Evaluate(id)
	now := s.now().UTC().Format(time.RFC3339)

	var userID string
	err := s.db.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider_id = ? AND subject = ?`, p.ID, id.Subject).Scan(&userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var u *auth.User
	if userID != "" {
		if u, err = s.auth.Get(ctx, userID); err != nil {
			return nil, err
		}
	} else {
		u, err = s.auth.ByEmail(ctx, id.Email)
		switch {
		case err == nil:
			if !id.EmailVerified && !p.TrustEmail {
				return nil, &SignInError{"an account with this email already exists, and the provider did not confirm the email is yours; sign in with your password"}
			}
		case errors.Is(err, auth.ErrNotFound):
			if !p.AutoCreate {
				return nil, &SignInError{fmt.Sprintf("there is no account for %s; ask a workspace admin to add you", id.Email)}
			}
			role := p.DefaultRole
			if roles.Workspace != "" {
				role = roles.Workspace
			}
			if u, err = s.auth.CreateSSOUser(ctx, id.Email, id.Name, role); err != nil {
				return nil, err
			}
		default:
			return nil, err
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO user_identities (provider_id, subject, user_id, email, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?)`,
			p.ID, id.Subject, u.ID, id.Email, now, now); err != nil {
			return nil, err
		}
	}
	if u.Disabled {
		return nil, auth.ErrDisabled
	}
	if _, err := s.db.Exec(ctx, `UPDATE user_identities SET email = ?, last_login_at = ? WHERE provider_id = ? AND subject = ?`, id.Email, now, p.ID, id.Subject); err != nil {
		return nil, err
	}
	if err := s.auth.SetGroups(ctx, u.ID, id.Groups); err != nil {
		return nil, err
	}
	u.Groups = id.Groups
	// Owners keep their role; anyone else gets what the mapping gives.
	if roles.Workspace != "" && roles.Workspace != u.Role && u.Role != auth.RoleOwner {
		if err := s.auth.SetWorkspaceRole(ctx, u.ID, roles.Workspace); err != nil {
			return nil, err
		}
		u.Role = roles.Workspace
	}
	if err := s.apps.SyncSSO(ctx, u.ID, roles.Apps); err != nil {
		return nil, err
	}
	return u, nil
}

// LinkedProviders lists, per user, the names of the providers they have
// signed in with.
func (s *Service) LinkedProviders(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.Query(ctx, `SELECT ui.user_id, p.name FROM user_identities ui JOIN sso_providers p ON p.id = ui.provider_id ORDER BY p.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var u, name string
		if err := rows.Scan(&u, &name); err != nil {
			return nil, err
		}
		if !slices.Contains(out[u], name) {
			out[u] = append(out[u], name)
		}
	}
	return out, rows.Err()
}
