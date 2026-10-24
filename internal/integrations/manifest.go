// Package integrations connects apps to outside services such as Stripe,
// Slack or GitHub. Each service is described by a manifest: the
// credentials a connection needs, the actions flows can run, the webhook
// events that start flows (with how their signatures are checked), and
// the lists that can be synced into tables. The built-in manifests are
// embedded; more can be loaded from a folder of JSON files.
package integrations

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Manifest describes one service.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	// Color and Icon draw the catalog card; Icon is a lucide icon name.
	Color string `json:"color"`
	Icon  string `json:"icon"`
	Docs  string `json:"docs,omitempty"`
	// Official manifests ship with Pimpbase; others were loaded from the
	// integrations folder.
	Official bool `json:"official"`
	// BaseURL is where action paths start. {key} parts are filled from
	// the connection's settings.
	BaseURL     string            `json:"baseUrl"`
	Credentials []Credential      `json:"credentials"`
	Auth        Auth              `json:"auth"`
	Headers     map[string]string `json:"headers,omitempty"`
	// Test is the request that checks a connection works.
	Test *Request `json:"test,omitempty"`
	// TestShows names answer fields to show after a successful test.
	TestShows []string `json:"testShows,omitempty"`
	Errors    Errors   `json:"errors"`
	// Idempotency names the header that makes repeated writes safe; such
	// writes are retried after errors and time-outs.
	Idempotency string    `json:"idempotency,omitempty"`
	Actions     []*Action `json:"actions"`
	Webhook     *Webhook  `json:"webhook,omitempty"`
	Syncs       []*Sync   `json:"syncs,omitempty"`
	// Guide is a short how-to for the connection page (plain text, one
	// paragraph per line).
	Guide string `json:"guide,omitempty"`
}

// Credential is one setting of a connection. Secret ones are never sent
// back to the browser and are hidden from run logs.
type Credential struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Long        bool   `json:"long,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	Default     string `json:"default,omitempty"`
	// Advanced settings are folded away, such as a base address for a
	// self-hosted server.
	Advanced bool `json:"advanced,omitempty"`
	// Pattern, when set, is a regular expression the value must match.
	Pattern string `json:"pattern,omitempty"`
}

// Auth is how requests prove who they are.
type Auth struct {
	// Type is none, bearer, basic, header, query, aws_sigv4 or
	// google_service_account.
	Type string `json:"type"`
	// Credential holds the token (bearer, header, query) or the
	// service account key; Username and Password name the basic ones.
	Credential string `json:"credential,omitempty"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	// Name is the header or query parameter (header, query).
	Name string `json:"name,omitempty"`
	// Prefix goes before the token in a header, such as "Bot ".
	Prefix string `json:"prefix,omitempty"`
	// Scopes are the OAuth scopes a service account asks for.
	Scopes []string `json:"scopes,omitempty"`
}

// Errors says where an answer explains what went wrong.
type Errors struct {
	// Message is the path of the error text in a failed answer.
	Message string `json:"message,omitempty"`
	// OK, when set, is a path that must be true even in a 200 answer
	// (Slack answers {"ok": false, "error": "…"}).
	OK string `json:"ok,omitempty"`
}

// Request is an HTTP call. Path may hold {name} parts, filled from the
// action's inputs or else the connection's settings; URL replaces
// BaseURL+Path when an action calls a full address from a setting.
type Request struct {
	Method string `json:"method"`
	Path   string `json:"path,omitempty"`
	URL    string `json:"url,omitempty"`
	// Body is form, json or none. Stripe and Twilio take form bodies.
	Body string `json:"body,omitempty"`
	// Fixed values are always sent, under their wire names.
	Fixed map[string]any    `json:"fixed,omitempty"`
	Query map[string]string `json:"query,omitempty"`
}

// Action is a flow step a connection offers.
type Action struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Request
	Inputs  []*Input `json:"inputs"`
	Outputs []Output `json:"outputs"`
	// Items is the path of the list in a list answer; later steps read
	// it as steps.Name.rows too.
	Items string `json:"items,omitempty"`
	// Handler names Go code that runs the action instead of one request,
	// for actions that take several calls or special signing.
	Handler string `json:"handler,omitempty"`
	// Writes marks actions that change things, so the catalog can say so.
	Writes bool `json:"writes,omitempty"`
	// SkipOK leaves out the manifest's Errors.OK check, for an action whose
	// answer is not the service's usual JSON.
	SkipOK bool `json:"skipOk,omitempty"`
}

// Input is one setting of an action.
type Input struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Kind is text, long, number, boolean, select, json, pairs (key and
	// value, sent as an object), list (comma separated, sent as a list)
	// or items (rows of Fields, sent as a list of objects).
	Kind        string   `json:"kind"`
	Required    bool     `json:"required,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []Option `json:"options,omitempty"`
	// In is where the value goes: path, query, body or header. The
	// default is query for GET and DELETE, body otherwise.
	In string `json:"in,omitempty"`
	// To is the wire name, a dotted path such as
	// price_data.product_data.name; the default is Key.
	To string `json:"to,omitempty"`
	// Transform changes the value on the way: cents (12.5 → 1250),
	// row (a list sent as one row: [[…]]), string or unix (a date as
	// seconds).
	Transform string   `json:"transform,omitempty"`
	Fields    []*Input `json:"fields,omitempty"`
	// Setting is the connection setting an empty value falls back to,
	// such as a default sender number.
	Setting string `json:"setting,omitempty"`
	// Advanced inputs are folded away in the step settings.
	Advanced bool `json:"advanced,omitempty"`
}

// Option is one choice of a select input.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Output is a value later steps can read, for the binding picker.
type Output struct {
	Path string `json:"path"`
	Hint string `json:"hint,omitempty"`
}

// Webhook is how a service tells Pimpbase that something happened.
type Webhook struct {
	Verify Verify `json:"verify"`
	// Event, ID and Object are where the event type, its id and the
	// object it is about sit in the body. EventHeader and IDHeader read
	// them from headers instead (GitHub). Event may be a fixed name in
	// FixedEvent for services that only send one kind.
	Event       string `json:"event,omitempty"`
	EventHeader string `json:"eventHeader,omitempty"`
	FixedEvent  string `json:"fixedEvent,omitempty"`
	ID          string `json:"id,omitempty"`
	IDHeader    string `json:"idHeader,omitempty"`
	Object      string `json:"object,omitempty"`
	// Challenge answers a service's address check: when the body's
	// ChallengeWhen path equals ChallengeValue, the answer is the body's
	// Challenge value (Slack's url_verification).
	Challenge      string `json:"challenge,omitempty"`
	ChallengeWhen  string `json:"challengeWhen,omitempty"`
	ChallengeValue string `json:"challengeValue,omitempty"`
	// Respond, when set, is the answer body and its content type.
	Respond     string   `json:"respond,omitempty"`
	RespondType string   `json:"respondType,omitempty"`
	Events      []Option `json:"events"`
	Setup       string   `json:"setup,omitempty"`
	// Sample is a typical event body, for test runs before a real event
	// arrived.
	Sample map[string]any `json:"sample,omitempty"`
}

// Verify says how a webhook's signature is checked. See Check.
type Verify struct {
	// Scheme is stripe, github, slack, twilio, standard (Standard
	// Webhooks), hmac or none.
	Scheme string `json:"scheme"`
	// Secret is the credential holding the signing secret.
	Secret string `json:"secret,omitempty"`
	// Header, Algorithm, Encoding and Prefix configure the hmac scheme.
	Header    string `json:"header,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	// Tolerance is how old a signed timestamp may be, in seconds.
	Tolerance int `json:"tolerance,omitempty"`
}

// Sync copies a list from the service into a table, and keeps it up to
// date from webhook events.
type Sync struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Table is the name of the table a first sync makes.
	Table string `json:"table"`
	Request
	Inputs []*Input `json:"inputs,omitempty"`
	// Items is the path of the list in each page.
	Items string `json:"items"`
	Page  Page   `json:"page"`
	// Fields map item values to table fields; the Key one finds the row
	// again. With Detect set, the fields under that path of each item
	// are added as they are found (Airtable's records).
	Fields []SyncField `json:"fields"`
	Detect string      `json:"detect,omitempty"`
	// Events are the webhook events whose object is an item of this list,
	// to add or change its row; Deleted are those that remove it.
	Events  []string `json:"events,omitempty"`
	Deleted []string `json:"deleted,omitempty"`
	// EventItem is where the item sits in an event's body, when it is not
	// the webhook's Object (GitHub's issue events carry it as issue).
	EventItem string `json:"eventItem,omitempty"`
}

// Page says how a list is read page by page.
type Page struct {
	// Type is none, cursor (the next page starts after the last item, or
	// at a value the answer gives) or page (numbered pages).
	Type string `json:"type"`
	// Param is the query parameter that asks for the next page.
	Param string `json:"param,omitempty"`
	// Next is the answer path of the next cursor; LastID, when set, is
	// the item path whose value on the last item is the cursor.
	Next   string `json:"next,omitempty"`
	LastID string `json:"lastId,omitempty"`
	// More is the answer path that is true while more pages follow.
	More string `json:"more,omitempty"`
}

// SyncField is one column of a synced table.
type SyncField struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Type is text, longtext, number, boolean or date.
	Type      string `json:"type,omitempty"`
	Transform string `json:"transform,omitempty"`
	Key       bool   `json:"key,omitempty"`
}

//go:embed manifests/*.json
var builtin embed.FS

// Catalog is the manifests the server knows, by id.
type Catalog struct {
	byID  map[string]*Manifest
	order []*Manifest
}

// LoadCatalog reads the built-in manifests and, when dir is set, every
// *.json file in it. A file in dir with the id of a built-in replaces it.
func LoadCatalog(dir string) (*Catalog, error) {
	c := &Catalog{byID: map[string]*Manifest{}}
	files, _ := fs.Glob(builtin, "manifests/*.json")
	for _, f := range files {
		data, _ := builtin.ReadFile(f)
		m, err := ParseManifest(data)
		if err != nil {
			return nil, fmt.Errorf("built-in integration %s: %w", f, err)
		}
		m.Official = true
		c.add(m)
	}
	if dir != "" {
		list, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return nil, err
		}
		for _, f := range list {
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			m, err := ParseManifest(data)
			if err != nil {
				return nil, fmt.Errorf("integration %s: %w", filepath.Base(f), err)
			}
			if m.Official {
				m.Official = false
			}
			for _, a := range m.Actions {
				if a.Handler != "" {
					return nil, fmt.Errorf("integration %s: action %s names Go code (handler), which only built-in integrations may", filepath.Base(f), a.Key)
				}
			}
			if t := m.Auth.Type; t == "aws_sigv4" {
				return nil, fmt.Errorf("integration %s: %s signing is only for built-in integrations", filepath.Base(f), t)
			}
			c.add(m)
		}
	}
	sort.SliceStable(c.order, func(i, j int) bool {
		if c.order[i].Official != c.order[j].Official {
			return c.order[i].Official
		}
		return c.order[i].Name < c.order[j].Name
	})
	return c, nil
}

func (c *Catalog) add(m *Manifest) {
	if old := c.byID[m.ID]; old != nil {
		for i, x := range c.order {
			if x == old {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
	}
	c.byID[m.ID] = m
	c.order = append(c.order, m)
}

// Get returns a manifest by id, or nil.
func (c *Catalog) Get(id string) *Manifest { return c.byID[id] }

// List returns every manifest, official ones first.
func (c *Catalog) List() []*Manifest { return c.order }

var (
	idRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
	keyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)
)

// ParseManifest reads and checks a manifest.
func ParseManifest(data []byte) (*Manifest, error) {
	m := &Manifest{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(m); err != nil {
		return nil, err
	}
	return m, m.check()
}

func (m *Manifest) check() error {
	if !idRe.MatchString(m.ID) {
		return fmt.Errorf("the id %q uses lower-case letters, digits and underscores", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("%s has no name", m.ID)
	}
	creds := map[string]bool{}
	for _, c := range m.Credentials {
		if !keyRe.MatchString(c.Key) {
			return fmt.Errorf("the setting key %q is not a plain name", c.Key)
		}
		if creds[c.Key] {
			return fmt.Errorf("the setting %s is listed twice", c.Key)
		}
		if c.Pattern != "" {
			if _, err := regexp.Compile(c.Pattern); err != nil {
				return fmt.Errorf("the setting %s: %w", c.Key, err)
			}
		}
		creds[c.Key] = true
	}
	need := func(what, key string) error {
		if key != "" && !creds[key] {
			return fmt.Errorf("%s names the setting %q, which is not listed", what, key)
		}
		return nil
	}
	switch m.Auth.Type {
	case "", "none":
		m.Auth.Type = "none"
	case "bearer", "header", "query", "google_service_account":
		if m.Auth.Credential == "" {
			return fmt.Errorf("%s auth needs a credential", m.Auth.Type)
		}
		if (m.Auth.Type == "header" || m.Auth.Type == "query") && m.Auth.Name == "" {
			return fmt.Errorf("%s auth needs a name", m.Auth.Type)
		}
	case "basic":
		if m.Auth.Username == "" {
			return fmt.Errorf("basic auth needs a username setting")
		}
	case "aws_sigv4":
	default:
		return fmt.Errorf("the auth type %q is unknown", m.Auth.Type)
	}
	for _, k := range []string{m.Auth.Credential, m.Auth.Username, m.Auth.Password} {
		if err := need("auth", k); err != nil {
			return err
		}
	}
	keys := map[string]bool{}
	for _, a := range m.Actions {
		if !keyRe.MatchString(a.Key) || keys[a.Key] {
			return fmt.Errorf("the action key %q is not a plain, unique name", a.Key)
		}
		keys[a.Key] = true
		if a.Handler == "" {
			if err := a.Request.check(); err != nil {
				return fmt.Errorf("action %s: %w", a.Key, err)
			}
		}
		if err := checkInputs(a.Inputs); err != nil {
			return fmt.Errorf("action %s: %w", a.Key, err)
		}
	}
	if w := m.Webhook; w != nil {
		switch w.Verify.Scheme {
		case "stripe", "github", "slack", "twilio", "standard", "hmac":
			if w.Verify.Secret == "" {
				return fmt.Errorf("the webhook's %s check needs the setting holding its secret", w.Verify.Scheme)
			}
		case "", "none":
			w.Verify.Scheme = "none"
		default:
			return fmt.Errorf("the webhook check %q is unknown", w.Verify.Scheme)
		}
		if err := need("the webhook", w.Verify.Secret); err != nil {
			return err
		}
	}
	syncs := map[string]bool{}
	for _, s := range m.Syncs {
		if !keyRe.MatchString(s.Key) || syncs[s.Key] {
			return fmt.Errorf("the sync key %q is not a plain, unique name", s.Key)
		}
		syncs[s.Key] = true
		if err := s.Request.check(); err != nil {
			return fmt.Errorf("sync %s: %w", s.Key, err)
		}
		if err := checkInputs(s.Inputs); err != nil {
			return fmt.Errorf("sync %s: %w", s.Key, err)
		}
		hasKey := false
		for _, f := range s.Fields {
			hasKey = hasKey || f.Key
		}
		if !hasKey {
			return fmt.Errorf("sync %s needs a key field, to find rows again", s.Key)
		}
		switch s.Page.Type {
		case "", "none":
			s.Page.Type = "none"
		case "cursor", "page":
			if s.Page.Param == "" {
				return fmt.Errorf("sync %s: paging needs the parameter that asks for the next page", s.Key)
			}
		default:
			return fmt.Errorf("sync %s: the paging %q is unknown", s.Key, s.Page.Type)
		}
	}
	if m.Test != nil {
		if err := m.Test.check(); err != nil {
			return fmt.Errorf("the test request: %w", err)
		}
	}
	return nil
}

func (r *Request) check() error {
	switch strings.ToUpper(r.Method) {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		r.Method = strings.ToUpper(r.Method)
	case "":
		r.Method = "GET"
	default:
		return fmt.Errorf("the method %q is unknown", r.Method)
	}
	if r.Path == "" && r.URL == "" {
		return fmt.Errorf("a request needs a path or an address")
	}
	switch r.Body {
	case "":
		r.Body = "json"
	case "json", "form", "none":
	default:
		return fmt.Errorf("the body %q is form, json or none", r.Body)
	}
	return nil
}

func checkInputs(list []*Input) error {
	seen := map[string]bool{}
	for _, in := range list {
		if !keyRe.MatchString(in.Key) || seen[in.Key] {
			return fmt.Errorf("the input key %q is not a plain, unique name", in.Key)
		}
		seen[in.Key] = true
		switch in.Kind {
		case "":
			in.Kind = "text"
		case "text", "long", "number", "boolean", "select", "json", "pairs", "list":
		case "items":
			if len(in.Fields) == 0 {
				return fmt.Errorf("the input %s lists items without fields", in.Key)
			}
			if err := checkInputs(in.Fields); err != nil {
				return err
			}
		default:
			return fmt.Errorf("the input kind %q is unknown", in.Kind)
		}
		switch in.In {
		case "", "path", "query", "body", "header":
		default:
			return fmt.Errorf("the input %s goes in %q, which is not path, query, body or header", in.Key, in.In)
		}
		switch in.Transform {
		case "", "cents", "row", "string", "unix":
		default:
			return fmt.Errorf("the input %s has the unknown transform %q", in.Key, in.Transform)
		}
	}
	return nil
}

// Action finds an action by key.
func (m *Manifest) Action(key string) *Action {
	for _, a := range m.Actions {
		if a.Key == key {
			return a
		}
	}
	return nil
}

// Sync finds a sync by key.
func (m *Manifest) Sync(key string) *Sync {
	for _, s := range m.Syncs {
		if s.Key == key {
			return s
		}
	}
	return nil
}

// Credential finds a setting by key.
func (m *Manifest) Credential(key string) *Credential {
	for i := range m.Credentials {
		if m.Credentials[i].Key == key {
			return &m.Credentials[i]
		}
	}
	return nil
}
