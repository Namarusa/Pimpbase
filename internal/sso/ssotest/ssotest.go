// Package ssotest is a small OpenID Connect provider for tests and local
// trials of single sign-on: it signs in any of its users with one click.
// It is not secure; never use it for real accounts.
package ssotest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// User is someone the provider can sign in.
type User struct {
	Subject       string         `json:"sub"`
	Email         string         `json:"email"`
	EmailVerified bool           `json:"email_verified"`
	Name          string         `json:"name"`
	Groups        []string       `json:"groups"`
	Extra         map[string]any `json:"extra,omitempty"`
}

// Provider is the fake identity provider. Serve it with Handler at Issuer.
type Provider struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Users        []User
	// Pick, when set, signs in that user (by subject) without showing the
	// chooser page.
	Pick string
	// Title heads the chooser page.
	Title string

	key   *rsa.PrivateKey
	mu    sync.Mutex
	codes map[string]grant
}

type grant struct {
	user      User
	nonce     string
	challenge string
	redirect  string
	at        time.Time
}

// New makes a provider with a fresh signing key.
func New(issuer, clientID, clientSecret string, users ...User) *Provider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return &Provider{Issuer: strings.TrimRight(issuer, "/"), ClientID: clientID, ClientSecret: clientSecret, Users: users, key: key, codes: map[string]grant{}}
}

func (p *Provider) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("GET /userinfo", p.userinfo)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (p *Provider) discovery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"issuer":                                p.Issuer,
		"authorization_endpoint":                p.Issuer + "/authorize",
		"token_endpoint":                        p.Issuer + "/token",
		"userinfo_endpoint":                     p.Issuer + "/userinfo",
		"jwks_uri":                              p.Issuer + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "email", "profile", "groups"},
	})
}

func (p *Provider) keys(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

var chooser = template.Must(template.New("").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title>
<style>
body{font:15px/1.5 system-ui,sans-serif;background:#f4f4f5;margin:0;display:grid;place-items:center;min-height:100vh;color:#18181b}
.card{background:#fff;border:1px solid #e4e4e7;border-radius:14px;padding:28px;width:min(400px,92vw);box-shadow:0 1px 3px rgba(0,0,0,.06)}
h1{font-size:19px;margin:0 0 4px}p{color:#71717a;margin:0 0 18px;font-size:14px}
button{display:flex;gap:12px;align-items:center;width:100%;text-align:left;background:#fff;border:1px solid #e4e4e7;border-radius:10px;padding:10px 12px;margin-top:8px;cursor:pointer;font:inherit}
button:hover{background:#fafafa;border-color:#a1a1aa}
.av{width:34px;height:34px;border-radius:50%;background:#e0e7ff;color:#3730a3;display:grid;place-items:center;font-weight:600;font-size:13px;flex:none}
.em{color:#71717a;font-size:13px}.g{color:#52525b;font-size:12px}
.badge{font-size:11px;background:#fef3c7;color:#92400e;border-radius:999px;padding:2px 8px;display:inline-block;margin-bottom:12px}
</style></head><body><form class="card" method="post">
<span class="badge">Test identity provider</span>
<h1>{{.Title}}</h1><p>Choose an account to continue to <b>{{.Client}}</b>.</p>
{{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}
{{range .Users}}<button name="user" value="{{.Subject}}"><span class="av">{{.Initials}}</span><span><div>{{.Name}}</div><div class="em">{{.Email}}</div>{{if .Groups}}<div class="g">Groups: {{.GroupList}}</div>{{end}}</span></button>{{end}}
</form></body></html>`))

type chooserUser struct {
	User
	Initials, GroupList string
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	q := r.Form
	if q.Get("client_id") != p.ClientID {
		http.Error(w, "unknown client_id", 400)
		return
	}
	redirect := q.Get("redirect_uri")
	ru, err := url.Parse(redirect)
	if err != nil || redirect == "" {
		http.Error(w, "bad redirect_uri", 400)
		return
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "PKCE with S256 is required", 400)
		return
	}
	pick := q.Get("user")
	if pick == "" {
		pick = p.Pick
	}
	if pick == "" {
		hidden := map[string]string{}
		for _, k := range []string{"client_id", "redirect_uri", "state", "nonce", "code_challenge", "code_challenge_method", "scope", "response_type"} {
			hidden[k] = q.Get(k)
		}
		var users []chooserUser
		for _, u := range p.Users {
			ini := ""
			for _, w := range strings.Fields(u.Name) {
				if len(ini) < 2 {
					ini += strings.ToUpper(w[:1])
				}
			}
			users = append(users, chooserUser{u, ini, strings.Join(u.Groups, ", ")})
		}
		title := p.Title
		if title == "" {
			title = "Sign in"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		chooser.Execute(w, map[string]any{"Title": title, "Client": ru.Host, "Hidden": hidden, "Users": users})
		return
	}
	var user *User
	for i := range p.Users {
		if p.Users[i].Subject == pick {
			user = &p.Users[i]
		}
	}
	back := ru.Query()
	back.Set("state", q.Get("state"))
	if user == nil {
		back.Set("error", "access_denied")
		back.Set("error_description", "unknown user")
	} else {
		code := random()
		p.mu.Lock()
		p.codes[code] = grant{*user, q.Get("nonce"), q.Get("code_challenge"), redirect, time.Now()}
		p.mu.Unlock()
		back.Set("code", code)
	}
	ru.RawQuery = back.Encode()
	http.Redirect(w, r, ru.String(), http.StatusFound)
}

func random() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	if id != p.ClientID || secret != p.ClientSecret {
		writeJSON(w, 401, map[string]any{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	g, found := p.codes[r.Form.Get("code")]
	delete(p.codes, r.Form.Get("code"))
	p.mu.Unlock()
	if !found || time.Since(g.at) > 5*time.Minute || r.Form.Get("redirect_uri") != g.redirect {
		writeJSON(w, 400, map[string]any{"error": "invalid_grant"})
		return
	}
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, 400, map[string]any{"error": "invalid_grant", "error_description": "PKCE verifier does not match"})
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.Issuer, "aud": p.ClientID, "sub": g.user.Subject, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"nonce": g.nonce, "email": g.user.Email, "email_verified": g.user.EmailVerified, "name": g.user.Name,
	}
	if g.user.Groups != nil {
		claims["groups"] = g.user.Groups
	}
	for k, v := range g.user.Extra {
		claims[k] = v
	}
	raw, err := p.sign(claims)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": "at-" + g.user.Subject, "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
}

// sign makes a JWT signed with the provider's key.
func (p *Provider) sign(claims map[string]any) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(claims)
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}

func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request) {
	sub, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer at-")
	if !ok {
		writeJSON(w, 401, map[string]any{"error": "invalid_token"})
		return
	}
	for _, u := range p.Users {
		if u.Subject == sub {
			writeJSON(w, 200, map[string]any{"sub": u.Subject, "email": u.Email, "email_verified": u.EmailVerified, "name": u.Name, "groups": u.Groups})
			return
		}
	}
	writeJSON(w, 404, map[string]any{"error": "unknown user"})
}
