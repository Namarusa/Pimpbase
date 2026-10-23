// Package flows stores an app's flows (automations) and runs them. A flow
// is a trigger (a row created, changed or deleted, a schedule, a webhook
// or a button on a screen) followed by steps: row changes, API queries,
// conditions, loops, delays, emails, HTTP requests, variables and PDFs.
// Step settings are {{ binding }} templates filled from the trigger and
// the results of earlier steps.
package flows

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

// Flow is one automation of an app.
type Flow struct {
	ID      string `json:"id"`
	AppID   string `json:"appId"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Definition
	// HookToken is the secret part of a webhook flow's address.
	HookToken string    `json:"hookToken"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Definition is what starts a flow and what it does.
type Definition struct {
	Trigger Trigger `json:"trigger"`
	Steps   []*Step `json:"steps"`
	// RunAs is "flow" (the default) for steps that may do anything in the
	// app, or "user" for steps that keep to the permissions of whoever
	// started the run.
	RunAs string `json:"runAs"`
	// RunRole is the app role that runs without a user act as, under
	// RunAs "user": scheduled and webhook runs. Such flows need one; a run
	// without a user and without a role fails instead of guessing.
	RunRole rbac.Role `json:"runRole,omitempty"`
}

// Unattended reports whether the trigger starts runs without a user.
func (t Trigger) Unattended() bool { return t.Type == OnSchedule || t.Type == OnWebhook }

// Run-as modes.
const (
	RunAsFlow = "flow"
	RunAsUser = "user"
)

// Trigger types.
const (
	RowCreated = "row_created"
	RowUpdated = "row_updated"
	RowDeleted = "row_deleted"
	OnSchedule = "schedule"
	OnWebhook  = "webhook"
	OnManual   = "manual"
)

// Trigger starts a flow.
type Trigger struct {
	Type string `json:"type"`
	// Table is the table whose rows start row triggers.
	Table string `json:"table,omitempty"`
	// Fields limits row_updated to changes of these fields (by name).
	Fields []string `json:"fields,omitempty"`
	// Cron and Timezone time a schedule: "30 8 * * mon-fri" in
	// "Europe/Paris".
	Cron     string `json:"cron,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	// Parameters are the values a screen button or a test passes to a
	// manual flow: {{ trigger.params.name }}.
	Parameters []Param `json:"parameters,omitempty"`
	// Role is who may run a manual flow from a published app.
	Role rbac.Role `json:"role"`
	// Respond makes a webhook wait for the flow and answer with its
	// result, instead of answering at once.
	Respond bool `json:"respond,omitempty"`
}

// Param is a value a manual flow takes.
type Param struct {
	Name string `json:"name"`
	// Type is "text", "number", "boolean" or "json".
	Type    string `json:"type"`
	Default string `json:"default"`
}

// Step is one thing a flow does. Its inputs are templates, or lists of
// {field, value} pairs. Conditions run Then or Else; loops run Do once per
// item.
type Step struct {
	ID string `json:"id"`
	// Name is how later steps read the result: {{ steps.Name.row._id }}.
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Inputs   map[string]any `json:"inputs"`
	Then     []*Step        `json:"then,omitempty"`
	Else     []*Step        `json:"else,omitempty"`
	Do       []*Step        `json:"do,omitempty"`
	Disabled bool           `json:"disabled,omitempty"`
	// ContinueOnError lets the flow go on when the step fails; later steps
	// read the message as {{ steps.Name.error }}.
	ContinueOnError bool `json:"continueOnError,omitempty"`
}

// StepTypes are the kinds of step a flow can use.
var StepTypes = []string{
	"create_row", "update_row", "delete_row", "find_rows", "run_query",
	"condition", "loop", "delay", "send_email", "http_request",
	"set_variable", "generate_pdf", "ai", "return",
}

const (
	maxSteps    = 200
	maxNesting  = 6
	maxDelay    = time.Hour
	maxLoop     = 5000
	keepRuns    = 200
	maxLogSteps = 500
)

var (
	ErrNotFound = errors.New("not found")
	nameRe      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _-]{0,62}$`)
	identRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
)

// InvalidError messages are safe to show.
type InvalidError struct {
	Message string
	// Step is the id of the step at fault, if any.
	Step string
}

func (e *InvalidError) Error() string { return e.Message }

func invalid(step, format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...), Step: step}
}

// Check validates a flow and fills in defaults.
func (f *Flow) Check() error {
	n := strings.TrimSpace(f.Name)
	if !nameRe.MatchString(n) {
		return invalid("", "a flow name starts with a letter and uses letters, digits, spaces, dashes and underscores (up to 63)")
	}
	f.Name = n
	if err := f.Trigger.check(); err != nil {
		return err
	}
	switch f.RunAs {
	case "":
		f.RunAs = RunAsFlow
	case RunAsFlow, RunAsUser:
	default:
		return invalid("runAs", "a flow runs as the flow or as the user")
	}
	if f.RunRole == rbac.None {
		f.RunRole = ""
	}
	if f.RunAs != RunAsUser {
		f.RunRole = ""
	} else if f.RunRole == "" && f.Trigger.Unattended() {
		return invalid("runAs", "a %s has no user to act as; choose the role its runs act as", map[string]string{OnSchedule: "schedule", OnWebhook: "webhook"}[f.Trigger.Type])
	}
	if f.Steps == nil {
		f.Steps = []*Step{}
	}
	c := &checker{names: map[string]bool{}, ids: map[string]bool{}}
	if err := c.steps(f.Steps, 0); err != nil {
		return err
	}
	if c.count > maxSteps {
		return invalid("", "a flow has at most %d steps", maxSteps)
	}
	return nil
}

func (t *Trigger) check() error {
	switch t.Type {
	case RowCreated, RowUpdated, RowDeleted:
		if t.Table == "" {
			return invalid("trigger", "choose the table whose rows start the flow")
		}
	case OnSchedule:
		if _, err := ParseCron(t.Cron); err != nil {
			return invalid("trigger", "the schedule: %s", err)
		}
		if t.Timezone == "" {
			t.Timezone = "UTC"
		}
		if _, err := time.LoadLocation(t.Timezone); err != nil {
			return invalid("trigger", "the time zone %q is unknown", t.Timezone)
		}
	case OnWebhook, OnManual:
	case "":
		t.Type = OnManual
	default:
		return invalid("trigger", "the trigger %q is unknown", t.Type)
	}
	if t.Type != RowUpdated {
		t.Fields = nil
	}
	if t.Type != OnSchedule {
		t.Cron, t.Timezone = "", ""
	}
	if t.Type != OnWebhook {
		t.Respond = false
	}
	if t.Parameters == nil {
		t.Parameters = []Param{}
	}
	seen := map[string]bool{}
	for i := range t.Parameters {
		p := &t.Parameters[i]
		p.Name = strings.TrimSpace(p.Name)
		if !identRe.MatchString(p.Name) {
			return invalid("trigger", "parameter names use letters, digits and underscores, and start with a letter")
		}
		if seen[strings.ToLower(p.Name)] {
			return invalid("trigger", "the parameter %s is listed twice", p.Name)
		}
		seen[strings.ToLower(p.Name)] = true
		switch p.Type {
		case "":
			p.Type = "text"
		case "text", "number", "boolean", "json":
		default:
			return invalid("trigger", "a parameter is text, number, boolean or json")
		}
	}
	if t.Role == "" {
		t.Role = rbac.Basic
	}
	if t.Role == rbac.None {
		return invalid("trigger", "pick the role that may run this flow")
	}
	return nil
}

type checker struct {
	names map[string]bool
	ids   map[string]bool
	count int
}

func (c *checker) steps(list []*Step, depth int) error {
	if depth > maxNesting {
		return invalid("", "conditions and loops nest at most %d deep", maxNesting)
	}
	for _, s := range list {
		if s == nil {
			return invalid("", "a step is empty")
		}
		c.count++
		if s.ID == "" {
			s.ID = strings.TrimPrefix(ids.New("s"), "s_")[:8]
		}
		if c.ids[s.ID] {
			return invalid(s.ID, "two steps share the id %s", s.ID)
		}
		c.ids[s.ID] = true
		known := false
		for _, t := range StepTypes {
			known = known || t == s.Type
		}
		if !known {
			return invalid(s.ID, "the step type %q is unknown", s.Type)
		}
		s.Name = strings.TrimSpace(s.Name)
		if !identRe.MatchString(s.Name) {
			return invalid(s.ID, "a step name uses letters, digits and underscores and starts with a letter, such as NewInvoice")
		}
		if c.names[strings.ToLower(s.Name)] {
			return invalid(s.ID, "two steps are named %s; later steps read results by name", s.Name)
		}
		c.names[strings.ToLower(s.Name)] = true
		if s.Inputs == nil {
			s.Inputs = map[string]any{}
		}
		if err := checkInputs(s.Inputs); err != nil {
			return invalid(s.ID, "%s: %s", s.Name, err)
		}
		if s.Type == "find_rows" || s.Type == "update_row" || s.Type == "delete_row" || s.Type == "create_row" {
			if str(s.Inputs, "table") == "" {
				return invalid(s.ID, "%s: choose a table", s.Name)
			}
		}
		if s.Type == "condition" {
			if err := c.steps(s.Then, depth+1); err != nil {
				return err
			}
			if err := c.steps(s.Else, depth+1); err != nil {
				return err
			}
		} else {
			s.Then, s.Else = nil, nil
		}
		if s.Type == "loop" {
			if err := c.steps(s.Do, depth+1); err != nil {
				return err
			}
		} else {
			s.Do = nil
		}
	}
	return nil
}

// checkInputs parses every text in a step's inputs.
func checkInputs(v any) error {
	switch x := v.(type) {
	case string:
		return checkText(x)
	case map[string]any:
		for _, y := range x {
			if err := checkInputs(y); err != nil {
				return err
			}
		}
	case []any:
		for _, y := range x {
			if err := checkInputs(y); err != nil {
				return err
			}
		}
	}
	return nil
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// Storage.

type Service struct {
	store *store.Store
	now   func() time.Time
	// changed, when set, hears that an app's flows changed, so the
	// scheduler and row triggers reload them.
	changed func(appID string)
}

func NewService(s *store.Store) *Service { return &Service{store: s, now: time.Now} }

const flowCols = `id, app_id, name, enabled, definition, hook_token, created_at, updated_at`

func stamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func scanFlow(row interface{ Scan(...any) error }) (*Flow, error) {
	f := &Flow{}
	var enabled int
	var def, created, updated string
	if err := row.Scan(&f.ID, &f.AppID, &f.Name, &enabled, &def, &f.HookToken, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("flow %w", ErrNotFound)
		}
		return nil, err
	}
	f.Enabled = enabled != 0
	f.Definition = Definition{Trigger: Trigger{Role: rbac.Basic}}
	json.Unmarshal([]byte(def), &f.Definition)
	f.Check()
	f.CreatedAt, f.UpdatedAt = stamp(created), stamp(updated)
	return f, nil
}

func (s *Service) list(ctx context.Context, where string, args ...any) ([]*Flow, error) {
	rows, err := s.store.Meta.Query(ctx, `SELECT `+flowCols+` FROM app_flows WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Flow{}
	for rows.Next() {
		f, err := scanFlow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Flows lists an app's flows.
func (s *Service) Flows(ctx context.Context, appID string) ([]*Flow, error) {
	return s.list(ctx, `app_id = ?`, appID)
}

// Enabled lists the switched-on flows of every app.
func (s *Service) Enabled(ctx context.Context) ([]*Flow, error) {
	return s.list(ctx, `enabled = 1`)
}

// Flow finds a flow by id, or by name ignoring case.
func (s *Service) Flow(ctx context.Context, appID, idOrName string) (*Flow, error) {
	return scanFlow(s.store.Meta.QueryRow(ctx, `SELECT `+flowCols+` FROM app_flows WHERE app_id = ? AND (id = ? OR LOWER(name) = LOWER(?)) ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`, appID, idOrName, idOrName, idOrName))
}

// ByHook finds the flow a webhook address names.
func (s *Service) ByHook(ctx context.Context, token string) (*Flow, error) {
	return scanFlow(s.store.Meta.QueryRow(ctx, `SELECT `+flowCols+` FROM app_flows WHERE hook_token = ?`, token))
}

func decodeOnto(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return invalid("", "invalid JSON: %s", err)
	}
	return nil
}

// Changed tells the engine an app's flows were written outside this
// service, as an import does.
func (s *Service) Changed(appID string) { s.notify(appID) }

func (s *Service) notify(appID string) {
	if s.changed != nil {
		s.changed(appID)
	}
}

func (s *Service) checkUnique(ctx context.Context, f *Flow) error {
	var n int
	if err := s.store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM app_flows WHERE app_id = ? AND LOWER(name) = LOWER(?) AND id <> ?`, f.AppID, f.Name, f.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return invalid("", "another flow is already named %s", f.Name)
	}
	return nil
}

// CreateFlow adds a flow from its JSON. New flows start switched off.
func (s *Service) CreateFlow(ctx context.Context, appID string, data []byte) (*Flow, error) {
	f := &Flow{Definition: Definition{Trigger: Trigger{Type: OnManual, Role: rbac.Basic}}}
	if err := decodeOnto(data, f); err != nil {
		return nil, err
	}
	f.ID, f.AppID, f.HookToken = ids.New("flw"), appID, ids.Secret("hook")
	if err := f.Check(); err != nil {
		return nil, err
	}
	if err := s.checkUnique(ctx, f); err != nil {
		return nil, err
	}
	def, _ := json.Marshal(f.Definition)
	now := s.now().UTC().Format(time.RFC3339Nano)
	if _, err := s.store.Meta.Exec(ctx, `INSERT INTO app_flows (`+flowCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, appID, f.Name, boolInt(f.Enabled), string(def), f.HookToken, now, now); err != nil {
		return nil, err
	}
	s.notify(appID)
	return s.Flow(ctx, appID, f.ID)
}

// UpdateFlow changes what the JSON names; "trigger" and "steps" replace
// the old ones whole.
func (s *Service) UpdateFlow(ctx context.Context, appID, id string, data []byte) (*Flow, error) {
	f, err := s.Flow(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, invalid("", "invalid JSON: %s", err)
	}
	if _, ok := keys["trigger"]; ok {
		f.Trigger = Trigger{Role: rbac.Basic}
	}
	if _, ok := keys["steps"]; ok {
		f.Steps = nil
	}
	token, created := f.HookToken, f.CreatedAt
	if err := decodeOnto(data, f); err != nil {
		return nil, err
	}
	f.ID, f.AppID, f.HookToken, f.CreatedAt = id, appID, token, created
	if err := f.Check(); err != nil {
		return nil, err
	}
	if err := s.checkUnique(ctx, f); err != nil {
		return nil, err
	}
	def, _ := json.Marshal(f.Definition)
	if _, err := s.store.Meta.Exec(ctx, `UPDATE app_flows SET name = ?, enabled = ?, definition = ?, updated_at = ? WHERE app_id = ? AND id = ?`,
		f.Name, boolInt(f.Enabled), string(def), s.now().UTC().Format(time.RFC3339Nano), appID, f.ID); err != nil {
		return nil, err
	}
	s.notify(appID)
	return s.Flow(ctx, appID, f.ID)
}

// NewHookToken gives a webhook flow a new address; the old one stops
// working.
func (s *Service) NewHookToken(ctx context.Context, appID, id string) (*Flow, error) {
	res, err := s.store.Meta.Exec(ctx, `UPDATE app_flows SET hook_token = ? WHERE app_id = ? AND id = ?`, ids.Secret("hook"), appID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("flow %w", ErrNotFound)
	}
	return s.Flow(ctx, appID, id)
}

// DeleteFlow removes a flow and its runs.
func (s *Service) DeleteFlow(ctx context.Context, appID, id string) error {
	tx, err := s.store.Meta.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `DELETE FROM app_flow_runs WHERE app_id = ? AND flow_id = ?`, appID, id); err != nil {
		return err
	}
	res, err := tx.Exec(ctx, `DELETE FROM app_flows WHERE app_id = ? AND id = ?`, appID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("flow %w", ErrNotFound)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(appID)
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Runs.

// Run statuses.
const (
	Running   = "running"
	Succeeded = "succeeded"
	Failed    = "failed"
)

// Run is one execution of a flow, with a log line per step.
type Run struct {
	ID     string `json:"id"`
	AppID  string `json:"appId"`
	FlowID string `json:"flowId"`
	Status string `json:"status"`
	// Test runs were started from the builder, with development values.
	Test        bool      `json:"test"`
	TriggerKind string    `json:"triggerKind"`
	Trigger     any       `json:"trigger"`
	Steps       []StepLog `json:"steps"`
	// Output is what a "return" step gave back.
	Output     any       `json:"output"`
	Error      string    `json:"error"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	DurationMs int64     `json:"durationMs"`
}

// StepLog is what one step did. Steps inside loops log once per item.
type StepLog struct {
	StepID string `json:"stepId"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	// Depth is how deep in conditions and loops the step sits.
	Depth int `json:"depth"`
	// Item numbers the loop item, from 1, for steps inside a loop.
	Item int `json:"item,omitempty"`
	// Status is "ok", "failed" or "skipped".
	Status string `json:"status"`
	Inputs any    `json:"inputs,omitempty"`
	Output any    `json:"output,omitempty"`
	// Trimmed is true when long lists and texts were cut for the log.
	Trimmed    bool      `json:"trimmed,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
}

const runCols = `id, app_id, flow_id, status, test, trigger_kind, trigger_data, steps, output, error, started_at, finished_at`

func scanRun(row interface{ Scan(...any) error }, full bool) (*Run, error) {
	r := &Run{}
	var test int
	var trig, steps, output, started, finished string
	if err := row.Scan(&r.ID, &r.AppID, &r.FlowID, &r.Status, &test, &r.TriggerKind, &trig, &steps, &output, &r.Error, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("run %w", ErrNotFound)
		}
		return nil, err
	}
	r.Test = test != 0
	r.StartedAt = stamp(started)
	if finished != "" {
		r.FinishedAt = stamp(finished)
		r.DurationMs = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
	}
	if full {
		json.Unmarshal([]byte(trig), &r.Trigger)
		json.Unmarshal([]byte(steps), &r.Steps)
		json.Unmarshal([]byte(output), &r.Output)
	}
	if r.Steps == nil {
		r.Steps = []StepLog{}
	}
	return r, nil
}

// Runs lists a flow's latest runs, newest first, without their logs.
func (s *Service) Runs(ctx context.Context, appID, flowID string, limit int) ([]*Run, error) {
	if limit <= 0 || limit > keepRuns {
		limit = 50
	}
	rows, err := s.store.Meta.Query(ctx, `SELECT `+runCols+` FROM app_flow_runs WHERE app_id = ? AND flow_id = ? ORDER BY started_at DESC, id DESC LIMIT ?`, appID, flowID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Run{}
	for rows.Next() {
		r, err := scanRun(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Run returns one run with its log.
func (s *Service) Run(ctx context.Context, appID, id string) (*Run, error) {
	return scanRun(s.store.Meta.QueryRow(ctx, `SELECT `+runCols+` FROM app_flow_runs WHERE app_id = ? AND id = ?`, appID, id), true)
}

func (s *Service) insertRun(ctx context.Context, r *Run, trig, instance string) error {
	_, err := s.store.Meta.Exec(ctx, `INSERT INTO app_flow_runs (`+runCols+`, instance_id) VALUES (?, ?, ?, ?, ?, ?, ?, '[]', 'null', '', ?, '', ?)`,
		r.ID, r.AppID, r.FlowID, r.Status, boolInt(r.Test), r.TriggerKind, trig, r.StartedAt.UTC().Format(time.RFC3339Nano), instance)
	return err
}

func (s *Service) saveRun(ctx context.Context, r *Run, steps, output string) error {
	finished := ""
	if !r.FinishedAt.IsZero() {
		finished = r.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.store.Meta.Exec(ctx, `UPDATE app_flow_runs SET status = ?, steps = ?, output = ?, error = ?, finished_at = ? WHERE id = ?`,
		r.Status, steps, output, r.Error, finished, r.ID)
	return err
}

// RunsSince counts an app's runs, tests excepted, started at or after t.
func (s *Service) RunsSince(ctx context.Context, appID string, t time.Time) (int, error) {
	var n int
	err := s.store.Meta.QueryRow(ctx, `SELECT COUNT(*) FROM app_flow_runs WHERE app_id = ? AND test = 0 AND started_at >= ?`, appID, t.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n, err
}

// prune keeps the newest runs of a flow.
func (s *Service) prune(ctx context.Context, flowID string) error {
	_, err := s.store.Meta.Exec(ctx, `DELETE FROM app_flow_runs WHERE flow_id = ? AND id NOT IN (SELECT id FROM app_flow_runs WHERE flow_id = ? ORDER BY started_at DESC, id DESC LIMIT ?)`, flowID, flowID, keepRuns)
	return err
}

// abandon marks runs as failed that a server was doing when it stopped.
// With an empty self, the server is alone and every running run is one
// it left behind. Otherwise runs of servers not seen alive since
// aliveSince are, but never self's own.
func (s *Service) abandon(ctx context.Context, self, aliveSince string) error {
	q := `UPDATE app_flow_runs SET status = ?, error = ?, finished_at = ? WHERE status = ?`
	args := []any{Failed, "the server stopped during the run", s.now().UTC().Format(time.RFC3339Nano), Running}
	if self != "" {
		q += ` AND instance_id <> ? AND instance_id NOT IN (SELECT id FROM instances WHERE seen_at >= ?)`
		args = append(args, self, aliveSince)
	}
	_, err := s.store.Meta.Exec(ctx, q, args...)
	return err
}

// Params gives every parameter of a manual flow a value: the given one or
// else the default, converted to its type.
func (t *Trigger) Params(given map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range given {
		out[k] = v
	}
	for _, p := range t.Parameters {
		v, ok := given[p.Name]
		if !ok {
			for k, x := range given {
				if strings.EqualFold(k, p.Name) {
					v, ok = x, true
					delete(out, k)
				}
			}
		}
		if !ok || v == nil || v == "" {
			v = p.Default
		}
		cv, err := convertParam(p, v)
		if err != nil {
			return nil, invalid("", "the parameter %s %s", p.Name, err)
		}
		out[p.Name] = cv
	}
	return out, nil
}

func convertParam(p Param, v any) (any, error) {
	s, isText := v.(string)
	switch p.Type {
	case "number":
		if !isText {
			if n, ok := number(v); ok {
				return n, nil
			}
			return nil, errors.New("is not a number")
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		n, ok := number(s)
		if !ok {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		return n, nil
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return truthy(s), nil
	case "json":
		if !isText {
			return plain(v), nil
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		var out any
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("is not valid JSON: %s", err)
		}
		return out, nil
	}
	if isText {
		return s, nil
	}
	return plain(v), nil
}
