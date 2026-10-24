package flows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pimpbase/pimpbase/internal/apps"
	"github.com/pimpbase/pimpbase/internal/cluster"
	"github.com/pimpbase/pimpbase/internal/email"
	"github.com/pimpbase/pimpbase/internal/exttables"
	"github.com/pimpbase/pimpbase/internal/files"
	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/interp"
	"github.com/pimpbase/pimpbase/internal/metrics"
	"github.com/pimpbase/pimpbase/internal/queries"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/tables"
)

const (
	// maxChain stops flows that change rows from starting each other
	// forever: a row change made by a flow started by a row change made by
	// a flow… is ignored past this depth.
	maxChain = 3
	// maxExecuted caps the steps one run may execute, loops included.
	maxExecuted = 20000
	runTimeout  = 2 * time.Hour
	maxRunning  = 8
	maxHTTPBody = 5 << 20
	maxLogValue = 16 << 10
)

// Engine runs flows: it hears row changes, keeps the schedule and starts
// runs. It is safe for concurrent use.
type Engine struct {
	Flows  *Service
	Tables *tables.Service
	// Ext serves tables imported from external databases; nil leaves
	// them out.
	Ext     *exttables.Service
	Queries *queries.Service
	Runner  *queries.Runner
	Files   *files.Service
	Apps    *apps.Service
	// Mailer delivers Send email steps; Emails holds the templates they
	// use. Both may be the same email.Service.
	Mailer email.Mailer
	Emails *email.Service
	Log    *slog.Logger
	// Client sends HTTP request steps.
	Client *http.Client
	// AI answers AI steps; nil makes them fail.
	AI Generator
	// Node, when set, shares the work with other servers on the same
	// database: each scheduled run is claimed by one server, and runs of
	// servers that died are marked failed. Without it the engine assumes
	// it is alone.
	Node *cluster.Node
	// BeforeRun, when set, may refuse to start a run that is not a test,
	// such as one over the app's quota.
	BeforeRun func(ctx context.Context, appID string) error

	base   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	slots  chan struct{}
	now    func() time.Time

	mu    sync.Mutex
	byApp map[string]rowList   // enabled row-trigger flows, loaded on demand
	last  map[string]time.Time // the minute each schedule flow last ran
}

// rowList is an app's row-trigger flows as loaded at a time. Other servers
// may change flows, so the list is reloaded after rowListTTL.
type rowList struct {
	flows []*Flow
	at    time.Time
}

const rowListTTL = 5 * time.Second

func NewEngine(svc *Service, ts *tables.Service, qs *queries.Service, runner *queries.Runner, fs *files.Service, as *apps.Service, log *slog.Logger) *Engine {
	base, cancel := context.WithCancel(context.Background())
	e := &Engine{
		Flows: svc, Tables: ts, Queries: qs, Runner: runner, Files: fs, Apps: as, Log: log,
		Mailer: noMailer{},
		Client: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		}},
		base: base, cancel: cancel, slots: make(chan struct{}, maxRunning), now: time.Now,
		byApp: map[string]rowList{}, last: map[string]time.Time{},
	}
	svc.changed = e.forget
	ts.OnRow = e.rowChanged
	return e
}

// UseExternal lets flows read and write external tables, and start on
// changes made to their rows through Pimpbase.
func (e *Engine) UseExternal(ext *exttables.Service) {
	e.Ext = ext
	ext.OnRow = e.rowChanged
}

// Start begins the schedule and marks runs left over from a previous
// server as failed.
func (e *Engine) Start(ctx context.Context) error {
	if err := e.abandon(ctx); err != nil {
		return err
	}
	e.wg.Add(1)
	go e.schedule()
	return nil
}

// Stop cancels running flows and waits for them, up to the deadline.
func (e *Engine) Stop(ctx context.Context) {
	e.cancel()
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// abandon marks runs as failed that no live server is doing.
func (e *Engine) abandon(ctx context.Context) error {
	if e.Node == nil {
		return e.Flows.abandon(ctx, "", "")
	}
	return e.Flows.abandon(ctx, e.Node.ID, e.Node.AliveSince())
}

// instanceID names this server on runs it starts.
func (e *Engine) instanceID() string {
	if e.Node == nil {
		return ""
	}
	return e.Node.ID
}

func (e *Engine) forget(appID string) {
	e.mu.Lock()
	delete(e.byApp, appID)
	e.mu.Unlock()
}

func (e *Engine) rowFlows(ctx context.Context, appID string) ([]*Flow, error) {
	e.mu.Lock()
	cached, ok := e.byApp[appID]
	e.mu.Unlock()
	if ok && e.now().Sub(cached.at) < rowListTTL {
		return cached.flows, nil
	}
	all, err := e.Flows.list(ctx, `app_id = ? AND enabled = 1`, appID)
	if err != nil {
		return nil, err
	}
	list := []*Flow{}
	for _, f := range all {
		switch f.Trigger.Type {
		case RowCreated, RowUpdated, RowDeleted:
			list = append(list, f)
		}
	}
	e.mu.Lock()
	e.byApp[appID] = rowList{flows: list, at: e.now()}
	e.mu.Unlock()
	return list, nil
}

type chainKey struct{}

func chainOf(ctx context.Context) int {
	n, _ := ctx.Value(chainKey{}).(int)
	return n
}

// rowChanged starts the flows a row change triggers. It returns at once;
// the flows run in the background.
func (e *Engine) rowChanged(ctx context.Context, ev tables.RowEvent) {
	chain := chainOf(ctx)
	ctx = context.WithoutCancel(ctx)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		list, err := e.rowFlows(ctx, ev.AppID)
		if err != nil {
			e.Log.Error("loading flows failed", "app", ev.AppID, "err", err)
			return
		}
		kind := "row_" + ev.Kind
		var table *stepTable
		for _, f := range list {
			if f.Trigger.Type != kind || f.Trigger.Table != ev.TableID {
				continue
			}
			if chain >= maxChain {
				e.Log.Warn("flow not started: row changes made by flows started each other too deep", "flow", f.Name, "app", f.AppID)
				continue
			}
			changed := changedFields(ev.Old, ev.Row)
			if kind == RowUpdated && len(f.Trigger.Fields) > 0 && !overlaps(changed, f.Trigger.Fields) {
				continue
			}
			if table == nil {
				if table, err = e.lookup(ctx, ev.AppID, ev.TableID); err != nil {
					return
				}
			}
			data := map[string]any{"table": map[string]any{"id": table.id, "name": table.name}, "row": ev.Row}
			if kind == RowUpdated {
				data["old"], data["changed"] = ev.Old, changed
			}
			act := ev.Actor
			in := StartInput{Kind: kind, Data: data, Chain: chain + 1, Actor: &act}
			if ev.Actor.UserID != "" {
				in.User = map[string]any{"id": ev.Actor.UserID, "email": ev.Actor.Email, "name": ev.Actor.Name, "role": ev.Actor.Role.String()}
			}
			if _, _, err := e.Begin(ctx, f, in); err != nil {
				e.Log.Error("starting a flow failed", "flow", f.Name, "err", err)
			}
		}
	}()
}

func changedFields(old, row map[string]any) []string {
	out := []string{}
	for k, v := range row {
		if strings.HasPrefix(k, "_") {
			continue
		}
		a, _ := json.Marshal(v)
		b, _ := json.Marshal(old[k])
		if !bytes.Equal(a, b) {
			out = append(out, k)
		}
	}
	return out
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}

// schedule starts schedule flows at the start of each minute they name.
func (e *Engine) schedule() {
	defer e.wg.Done()
	for {
		now := e.now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-e.base.Done():
			return
		case <-time.After(next.Sub(now) + 50*time.Millisecond):
		}
		e.RunDue(next)
		if e.Node != nil {
			// Runs of servers that stopped answering will never finish.
			if err := e.abandon(e.base); err != nil {
				e.Log.Error("marking abandoned runs failed", "err", err)
			}
		}
	}
}

// RunDue starts the schedule flows due in the minute of t. Each flow runs
// at most once per minute.
func (e *Engine) RunDue(t time.Time) {
	ctx := e.base
	all, err := e.Flows.Enabled(ctx)
	if err != nil {
		e.Log.Error("loading scheduled flows failed", "err", err)
		return
	}
	minute := t.Truncate(time.Minute)
	for _, f := range all {
		if f.Trigger.Type != OnSchedule {
			continue
		}
		s, err := ParseCron(f.Trigger.Cron)
		if err != nil {
			continue
		}
		loc, err := time.LoadLocation(f.Trigger.Timezone)
		if err != nil {
			loc = time.UTC
		}
		local := minute.In(loc)
		if !s.Match(local) {
			continue
		}
		e.mu.Lock()
		due := e.last[f.ID].Before(minute)
		if due {
			e.last[f.ID] = minute
		}
		e.mu.Unlock()
		if !due {
			continue
		}
		if e.Node != nil {
			// Every server sees the minute; one of them runs the flow.
			won, err := e.Node.Claim(ctx, "schedule:"+f.ID+":"+minute.UTC().Format("2006-01-02T15:04"))
			if err != nil {
				e.Log.Error("claiming a scheduled flow failed", "flow", f.Name, "err", err)
				continue
			}
			if !won {
				continue
			}
		}
		data := map[string]any{"time": local.Format(time.RFC3339), "date": local.Format("2006-01-02"), "timezone": loc.String()}
		if _, _, err := e.Begin(ctx, f, StartInput{Kind: OnSchedule, Data: data}); err != nil {
			e.Log.Error("starting a scheduled flow failed", "flow", f.Name, "err", err)
		}
	}
}

// NextRuns lists the next times a schedule fires, for the editor.
func NextRuns(cron, tz string, from time.Time, n int) ([]time.Time, error) {
	s, err := ParseCron(cron)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("the time zone %q is unknown", tz)
	}
	var out []time.Time
	t := from.In(loc)
	for len(out) < n {
		next, ok := s.Next(t)
		if !ok {
			break
		}
		out = append(out, next)
		t = next
	}
	return out, nil
}

// StartInput is what starts a run.
type StartInput struct {
	// Kind is the trigger type that fired, or "test".
	Kind string
	// Data is what steps read as {{ trigger.… }}.
	Data map[string]any
	// User is who caused the run, if anyone: id, email, name, role.
	User map[string]any
	// Test runs use development variable values and run switched-off
	// flows.
	Test bool
	// Chain counts flows started by row changes of other flows.
	Chain int
	// Actor is who started the run, for flows that run as the user. Nil
	// means nobody: such runs act as the flow's RunRole.
	Actor *rbac.Actor
	// DryRun renders Send email steps into the run log without sending.
	DryRun bool
}

// Begin starts a run in the background. The channel closes when it ends.
func (e *Engine) Begin(ctx context.Context, f *Flow, in StartInput) (*Run, <-chan struct{}, error) {
	if e.base.Err() != nil {
		return nil, nil, errors.New("the server is shutting down")
	}
	if e.BeforeRun != nil && !in.Test {
		if err := e.BeforeRun(ctx, f.AppID); err != nil {
			return nil, nil, err
		}
	}
	app, err := e.Apps.Get(ctx, f.AppID)
	if err != nil {
		return nil, nil, err
	}
	env := queries.Production
	if in.Test {
		env = queries.Development
	}
	vars, secrets, err := e.Queries.Values(ctx, f.AppID, env)
	if err != nil {
		return nil, nil, err
	}
	if in.Data == nil {
		in.Data = map[string]any{}
	}
	trig := plain(in.Data)
	r := &Run{ID: ids.New("run"), AppID: f.AppID, FlowID: f.ID, Status: Running, Test: in.Test, TriggerKind: in.Kind, Trigger: trig, Steps: []StepLog{}, StartedAt: e.now().UTC()}
	x := &exec{
		e: e, flow: f, run: r, env: env, secrets: secrets, chain: in.Chain,
		steps: map[string]any{}, vars: map[string]any{}, starter: in.Actor, dryRun: in.DryRun,
	}
	user := in.User
	if f.RunAs == RunAsUser && in.Actor == nil {
		if x.unattended, err = e.unattended(ctx, f); err != nil {
			x.refused = err.Error()
		} else if user == nil {
			user = map[string]any{"name": x.unattended.Name, "role": string(x.unattended.Role)}
		}
	}
	if user == nil {
		user = map[string]any{}
	}
	x.scope = interp.Map{
		"trigger": trig, "steps": x.steps, "vars": x.vars, "env": vars, "user": user,
		"app": map[string]any{"id": app.ID, "name": app.Name, "slug": app.Slug},
	}
	x.accent = app.Design.Accent
	tj, _ := json.Marshal(trig)
	if err := e.Flows.insertRun(ctx, r, x.hide(string(tj)), e.instanceID()); err != nil {
		return nil, nil, err
	}
	copy := *r
	done := make(chan struct{})
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer close(done)
		select {
		case e.slots <- struct{}{}:
		case <-e.base.Done():
			r.Status, r.Error, r.FinishedAt = Failed, "the server stopped before the run began", e.now().UTC()
			x.save(context.Background())
			return
		}
		defer func() { <-e.slots }()
		x.execute()
	}()
	return &copy, done, nil
}

// RunNow starts a run and waits for it up to wait. It returns the run as
// it stands then: finished, or still running.
func (e *Engine) RunNow(ctx context.Context, f *Flow, in StartInput, wait time.Duration) (*Run, error) {
	r, done, err := e.Begin(ctx, f, in)
	if err != nil {
		return nil, err
	}
	select {
	case <-done:
	case <-time.After(wait):
	case <-ctx.Done():
	}
	return e.Flows.Run(context.WithoutCancel(ctx), f.AppID, r.ID)
}

// exec is one run in progress.
type exec struct {
	e       *Engine
	flow    *Flow
	run     *Run
	env     string
	secrets []string
	chain   int
	accent  string
	// starter is who started the run.
	starter *rbac.Actor
	// unattended is who a run without a starter acts as, for flows that
	// run as the user; refused says why there is no such actor.
	unattended *rbac.Actor
	refused    string

	scope interp.Map
	steps map[string]any
	vars  map[string]any

	// dryRun keeps emails in the log; mail is the email the current step
	// made, for its log line, and mails counts emails logged.
	dryRun bool
	mail   *EmailLog
	mails  int

	ctx      context.Context
	executed int
	dropped  int
	returned bool
	lastSave time.Time
}

// unattended is who a run without a user acts as, for a flow that runs as
// the user: its run-as role, which must still exist in the app.
func (e *Engine) unattended(ctx context.Context, f *Flow) (*rbac.Actor, error) {
	if f.RunRole == "" {
		return nil, errors.New("this flow runs with the permissions of who started it, but nobody did (a schedule or webhook) and it has no run-as role; choose one in the trigger settings")
	}
	set, err := e.Apps.Roles(ctx, f.AppID)
	if err != nil {
		return nil, err
	}
	if !set.Has(f.RunRole) {
		return nil, fmt.Errorf("this flow's run-as role %s no longer exists; choose another in the trigger settings", f.RunRole)
	}
	a := rbac.NewActor(set, f.RunRole)
	a.Name = "Flow " + f.Name
	return &a, nil
}

// actor is who row steps act as: an app admin named after the flow, or,
// for flows that run as the user, whoever started the run, or the flow's
// run-as role when nobody did. A run with none of these never starts its
// steps, so the last case is only a safe default.
func (x *exec) actor() rbac.Actor {
	if x.flow.RunAs == RunAsUser {
		if x.starter != nil {
			return *x.starter
		}
		if x.unattended != nil {
			return *x.unattended
		}
		return rbac.Actor{Name: "Flow " + x.flow.Name, Role: rbac.None, Roles: []rbac.Role{}}
	}
	return rbac.Actor{Name: "Flow " + x.flow.Name, Role: rbac.Admin, Builder: true}
}

func (x *exec) execute() {
	ctx, cancel := context.WithTimeout(x.e.base, runTimeout)
	defer cancel()
	x.ctx = context.WithValue(ctx, chainKey{}, x.chain)
	var err error
	if x.refused != "" {
		err = errors.New(x.refused)
	} else {
		err = x.list(x.flow.Steps, x.scope, 0, 0)
	}
	r := x.run
	r.FinishedAt = x.e.now().UTC()
	r.Status = Succeeded
	if err != nil {
		r.Status = Failed
		r.Error = x.hide(err.Error())
		if errors.Is(err, context.DeadlineExceeded) {
			r.Error = fmt.Sprintf("the run took longer than %s and was stopped", runTimeout)
		} else if errors.Is(err, context.Canceled) {
			r.Error = "the server stopped during the run"
		}
	}
	if x.dropped > 0 {
		r.Steps = append(r.Steps, StepLog{Name: fmt.Sprintf("%d more steps ran but were not logged", x.dropped), Type: "note", Status: "ok", StartedAt: r.FinishedAt})
	}
	x.save(context.Background())
	metrics.Default.Inc("pimpbase_flow_runs_total", "status", r.Status)
	metrics.Default.Since("pimpbase_flow_run_duration_seconds", r.StartedAt)
	if err := x.e.Flows.prune(context.Background(), x.flow.ID); err != nil {
		x.e.Log.Error("pruning flow runs failed", "err", err)
	}
}

func (x *exec) save(ctx context.Context) {
	steps, _ := json.Marshal(x.run.Steps)
	out, _ := json.Marshal(x.run.Output)
	if err := x.e.Flows.saveRun(ctx, x.run, x.hide(string(steps)), x.hide(string(out))); err != nil {
		x.e.Log.Error("saving a flow run failed", "run", x.run.ID, "err", err)
	}
	x.lastSave = time.Now()
}

func (x *exec) hide(s string) string {
	for _, sec := range x.secrets {
		s = strings.ReplaceAll(s, sec, "••••••")
		if js, _ := json.Marshal(sec); len(js) > 2 {
			s = strings.ReplaceAll(s, string(js[1:len(js)-1]), "••••••")
		}
	}
	return s
}

// log adds a step's line, or counts it once the log is full. It returns
// the line's index, or -1.
func (x *exec) log(l StepLog) int {
	if len(x.run.Steps) >= maxLogSteps {
		x.dropped++
		return -1
	}
	x.run.Steps = append(x.run.Steps, l)
	return len(x.run.Steps) - 1
}

// StepError is a step that failed; its message is safe to show.
type StepError struct {
	Step    string
	Message string
}

func (e *StepError) Error() string { return e.Step + ": " + e.Message }

// list runs steps in order. item numbers the loop item they run for.
func (x *exec) list(steps []*Step, sc interp.Scope, depth, item int) error {
	for _, s := range steps {
		if x.returned {
			return nil
		}
		if err := x.ctx.Err(); err != nil {
			return err
		}
		l := StepLog{StepID: s.ID, Name: s.Name, Type: s.Type, Depth: depth, Item: item, StartedAt: x.e.now().UTC()}
		if s.Disabled {
			l.Status = "skipped"
			l.Error = "the step is switched off"
			x.log(l)
			continue
		}
		x.executed++
		if x.executed > maxExecuted {
			return &StepError{s.Name, fmt.Sprintf("the run executed more than %d steps; check the loops", maxExecuted)}
		}
		at := x.log(l)
		in := &inputs{x: x, sc: sc, values: map[string]any{}}
		start := time.Now()
		out, err := x.step(s, in, depth, item)
		l.DurationMs = time.Since(start).Milliseconds()
		l.Inputs = trimLog(in.values, &l.Trimmed)
		l.Email, x.mail = x.mail, nil
		if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			l.Status, l.Error = "failed", "stopped"
			x.put(at, l)
			return err
		}
		if err != nil {
			var se *StepError
			if errors.As(err, &se) && se.Step != s.Name {
				// A step inside a condition or loop failed; this one stops too.
				l.Status, l.Error = "failed", "a step inside failed"
				x.put(at, l)
				return err
			}
			l.Status, l.Error = "failed", err.Error()
			x.put(at, l)
			if s.ContinueOnError {
				x.steps[s.Name] = map[string]any{"error": err.Error()}
				continue
			}
			if se == nil {
				err = &StepError{s.Name, err.Error()}
			}
			return err
		}
		out = plain(out)
		x.steps[s.Name] = out
		l.Status, l.Output = "ok", trimLog(out, &l.Trimmed)
		x.put(at, l)
	}
	return nil
}

func (x *exec) put(at int, l StepLog) {
	if at >= 0 {
		// Inner steps were logged after this one; keep their lines.
		x.run.Steps[at] = l
	}
}

// inputs fills a step's settings and keeps what they became for the log.
type inputs struct {
	x      *exec
	sc     interp.Scope
	values map[string]any
	raw    map[string]any
}

func (in *inputs) text(key string, mode interp.Mode) (string, error) {
	src, _ := in.raw[key].(string)
	out, err := render(src, mode, in.sc)
	if err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	if src != "" {
		in.values[key] = out
	}
	return out, nil
}

func (in *inputs) value(key string) (any, error) {
	src, _ := in.raw[key].(string)
	v, err := value(src, in.sc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	if src != "" {
		in.values[key] = plain(v)
	}
	return v, nil
}

func (in *inputs) str(key string) string {
	s, _ := in.raw[key].(string)
	return s
}

func (in *inputs) flag(key string, def bool) bool {
	if b, ok := in.raw[key].(bool); ok {
		return b
	}
	return def
}

// pairs fills a list of {key|field, value} inputs.
func (in *inputs) pairs(key, name string) ([][2]any, error) {
	list, _ := in.raw[key].([]any)
	var out [][2]any
	logged := map[string]any{}
	for _, item := range list {
		m, _ := item.(map[string]any)
		k, _ := m[name].(string)
		if strings.TrimSpace(k) == "" {
			continue
		}
		src, _ := m["value"].(string)
		v, err := value(src, in.sc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out = append(out, [2]any{k, v})
		logged[k] = plain(v)
	}
	if len(logged) > 0 {
		in.values[key] = logged
	}
	return out, nil
}

func (x *exec) step(s *Step, in *inputs, depth, item int) (any, error) {
	in.raw = s.Inputs
	ctx := x.ctx
	switch s.Type {
	case "create_row", "update_row":
		t, err := x.table(in)
		if err != nil {
			return nil, err
		}
		vals, err := x.rowValues(t, in)
		if err != nil {
			return nil, err
		}
		if s.Type == "create_row" {
			var row map[string]any
			if t.ext != nil {
				row, err = x.e.Ext.CreateRow(ctx, x.actor(), x.env, x.flow.AppID, t.id, vals)
			} else {
				row, err = x.e.Tables.CreateRow(ctx, x.actor(), x.flow.AppID, t.id, vals)
			}
			if err != nil {
				return nil, rowErr(err)
			}
			return map[string]any{"row": row, "id": row["_id"]}, nil
		}
		var row map[string]any
		if t.ext != nil {
			key, err := x.rowKey(in)
			if err != nil {
				return nil, err
			}
			row, err = x.e.Ext.UpdateRow(ctx, x.actor(), x.env, x.flow.AppID, t.id, key, vals)
			if err != nil {
				return nil, rowErr(err)
			}
		} else {
			id, err := x.rowID(in)
			if err != nil {
				return nil, err
			}
			if row, err = x.e.Tables.UpdateRow(ctx, x.actor(), x.flow.AppID, t.id, id, vals); err != nil {
				return nil, rowErr(err)
			}
		}
		return map[string]any{"row": row, "id": row["_id"]}, nil

	case "delete_row":
		t, err := x.table(in)
		if err != nil {
			return nil, err
		}
		var n int
		var id any
		if t.ext != nil {
			key, err := x.rowKey(in)
			if err != nil {
				return nil, err
			}
			if n, err = x.e.Ext.DeleteRows(ctx, x.actor(), x.env, x.flow.AppID, t.id, []string{key}); err != nil {
				return nil, rowErr(err)
			}
			id = key
		} else {
			rid, err := x.rowID(in)
			if err != nil {
				return nil, err
			}
			if n, err = x.e.Tables.DeleteRows(ctx, x.actor(), x.flow.AppID, t.id, []int64{rid}); err != nil {
				return nil, rowErr(err)
			}
			id = rid
		}
		if n == 0 && in.flag("mustExist", true) {
			return nil, fmt.Errorf("row %v of %s does not exist", id, t.name)
		}
		return map[string]any{"deleted": n, "id": id}, nil

	case "find_rows":
		t, err := x.table(in)
		if err != nil {
			return nil, err
		}
		filter, err := fillFormula(in.str("filter"), in.sc)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		if filter != "" {
			in.values["filter"] = filter
		}
		limit := 100
		if v, err := in.value("limit"); err != nil {
			return nil, err
		} else if n, ok := number(v); ok && n > 0 {
			limit = int(math.Min(n, 1000))
		}
		q := tables.Query{Filter: filter, Sort: in.str("sort"), Desc: in.flag("desc", false), Limit: limit}
		var page *tables.Page
		if t.ext != nil {
			page, err = x.e.Ext.ListRows(ctx, x.actor(), x.env, x.flow.AppID, t.id, q)
		} else {
			page, err = x.e.Tables.ListRows(ctx, x.actor(), x.flow.AppID, t.id, q)
		}
		if err != nil {
			return nil, rowErr(err)
		}
		var first any
		if len(page.Rows) > 0 {
			first = page.Rows[0]
		}
		return map[string]any{"rows": page.Rows, "count": len(page.Rows), "total": page.Total, "first": first}, nil

	case "run_query":
		q, err := x.e.Queries.Query(ctx, x.flow.AppID, in.str("query"))
		if err != nil {
			return nil, errors.New("choose the API query this step runs")
		}
		in.values["query"] = q.Name
		if err := x.actor().Require(q.Role, "running "+q.Name); err != nil {
			return nil, err
		}
		params := map[string]any{}
		if m, ok := s.Inputs["params"].(map[string]any); ok {
			for k, src := range m {
				t, _ := src.(string)
				if t == "" {
					continue
				}
				v, err := value(t, in.sc)
				if err != nil {
					return nil, fmt.Errorf("the parameter %s: %w", k, err)
				}
				params[k] = v
			}
		}
		if len(params) > 0 {
			in.values["params"] = plain(params)
		}
		user, _ := x.scope["user"].(map[string]any)
		app, _ := x.scope["app"].(map[string]any)
		res, err := x.e.Runner.Run(ctx, x.flow.AppID, q, queries.RunInput{Params: params, Env: x.env, User: user, App: app})
		if err != nil {
			return nil, err
		}
		out := map[string]any{"ok": res.OK, "status": res.Status, "body": res.Body, "rows": res.Rows, "headers": res.Headers}
		if !res.OK && in.flag("failOnError", true) {
			return out, fmt.Errorf("%s answered %d %s", q.Name, res.Status, res.StatusText)
		}
		return out, nil

	case "condition":
		left, err := in.value("left")
		if err != nil {
			return nil, err
		}
		right, err := in.value("right")
		if err != nil {
			return nil, err
		}
		op := in.str("operator")
		if op == "" {
			op = "equals"
		}
		in.values["operator"] = op
		ok, err := compare(left, op, right)
		if err != nil {
			return nil, err
		}
		branch := s.Else
		if ok {
			branch = s.Then
		}
		if err := x.list(branch, in.sc, depth+1, item); err != nil {
			return nil, err
		}
		return map[string]any{"result": ok}, nil

	case "loop":
		v, err := in.value("items")
		if err != nil {
			return nil, err
		}
		items := asList(v)
		if n, ok := v.(float64); ok && n >= 0 {
			// A number loops that many times.
			items = make([]any, int(math.Min(n, maxLoop)))
			for i := range items {
				items[i] = float64(i + 1)
			}
		}
		limit := maxLoop
		if mv, err := in.value("max"); err == nil {
			if n, ok := number(mv); ok && n > 0 && int(n) < limit {
				limit = int(n)
			}
		}
		if len(items) > limit {
			if !in.flag("truncate", true) {
				return nil, fmt.Errorf("there are %d items, more than the limit of %d", len(items), limit)
			}
			items = items[:limit]
		}
		in.values["items"] = fmt.Sprintf("%d items", len(items))
		if len(items) > 0 {
			// The builder shows the first item's keys in its binding picker.
			in.values["firstItem"] = plain(items[0])
		}
		for i, it := range items {
			sc := overlay{Names: map[string]any{"loop": map[string]any{"item": plain(it), "index": float64(i), "number": float64(i + 1), "count": float64(len(items))}}, Base: in.sc}
			if err := x.list(s.Do, sc, depth+1, i+1); err != nil {
				return nil, err
			}
			if x.returned {
				break
			}
		}
		return map[string]any{"count": len(items)}, nil

	case "delay":
		v, err := in.value("duration")
		if err != nil {
			return nil, err
		}
		n, ok := number(v)
		if !ok || n < 0 {
			return nil, fmt.Errorf("the duration %q is not a number", interp.ToText(v))
		}
		unit := in.str("unit")
		d := time.Duration(n * float64(time.Second))
		switch unit {
		case "minutes":
			d = time.Duration(n * float64(time.Minute))
		case "hours":
			d = time.Duration(n * float64(time.Hour))
		}
		if d > maxDelay {
			return nil, fmt.Errorf("a delay is at most %s", maxDelay)
		}
		// Show the waiting run in the history.
		x.save(context.Background())
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return map[string]any{"waitedSeconds": d.Seconds()}, nil

	case "set_variable":
		name := strings.TrimSpace(in.str("name"))
		if !identRe.MatchString(name) {
			return nil, errors.New("a variable name uses letters, digits and underscores and starts with a letter")
		}
		v, err := in.value("value")
		if err != nil {
			return nil, err
		}
		v = plain(v)
		switch in.str("mode") {
		case "append":
			x.vars[name] = append(asList(x.vars[name]), v)
		case "add":
			a, _ := number(x.vars[name])
			b, ok := number(v)
			if !ok {
				return nil, fmt.Errorf("%q is not a number to add", interp.ToText(v))
			}
			x.vars[name] = a + b
		default:
			x.vars[name] = v
		}
		return map[string]any{"name": name, "value": x.vars[name]}, nil

	case "return":
		v, err := in.value("value")
		if err != nil {
			return nil, err
		}
		x.run.Output = plain(v)
		x.returned = true
		return map[string]any{"value": v}, nil

	case "send_email":
		return x.sendEmail(in)
	case "http_request":
		return x.httpRequest(in)
	case "generate_pdf":
		return x.generatePDF(in)
	case "ai":
		return x.aiStep(in)
	}
	return nil, fmt.Errorf("the step type %q is unknown", s.Type)
}

func rowErr(err error) error {
	var ve *tables.ValidationError
	if errors.As(err, &ve) {
		return errors.New(ve.Error())
	}
	if tables.IsNotFound(err) {
		return errors.New("the row or table does not exist")
	}
	return err
}

// stepTable is the table a step works on: built-in or external.
type stepTable struct {
	id, name string
	builtin  *tables.Table
	ext      *exttables.Table
}

func (e *Engine) lookup(ctx context.Context, appID, id string) (*stepTable, error) {
	if exttables.Is(id) && e.Ext != nil {
		t, err := e.Ext.Get(ctx, appID, id)
		if err != nil {
			return nil, err
		}
		return &stepTable{id: t.ID, name: t.Name, ext: t}, nil
	}
	t, err := e.Tables.Get(ctx, appID, id)
	if err != nil {
		if e.Ext == nil {
			return nil, err
		}
		// A table chosen by name may be an external one.
		xt, xerr := e.Ext.Get(ctx, appID, id)
		if xerr != nil {
			return nil, err
		}
		return &stepTable{id: xt.ID, name: xt.Name, ext: xt}, nil
	}
	return &stepTable{id: t.ID, name: t.Name, builtin: t}, nil
}

func (x *exec) table(in *inputs) (*stepTable, error) {
	t, err := x.e.lookup(x.ctx, x.flow.AppID, in.str("table"))
	if err != nil {
		return nil, errors.New("the table of this step does not exist any more")
	}
	in.values["table"] = t.name
	return t, nil
}

// rowKey reads the rowId input as an external row's key: a number, a
// text, or a row with _id.
func (x *exec) rowKey(in *inputs) (string, error) {
	v, err := in.value("rowId")
	if err != nil {
		return "", err
	}
	if m, ok := v.(map[string]any); ok {
		v = m["_id"]
	}
	if n, ok := v.(float64); ok && n == math.Trunc(n) && math.Abs(n) < 1<<53 {
		return strconv.FormatInt(int64(n), 10), nil
	}
	key := strings.TrimSpace(interp.ToText(v))
	if key == "" {
		return "", errors.New("the row id is empty")
	}
	return key, nil
}

func (x *exec) rowID(in *inputs) (int64, error) {
	v, err := in.value("rowId")
	if err != nil {
		return 0, err
	}
	if m, ok := v.(map[string]any); ok {
		v = m["_id"]
	}
	n, ok := number(v)
	if !ok || n <= 0 || n != math.Trunc(n) {
		return 0, fmt.Errorf("%q is not a row id", interp.ToText(v))
	}
	return int64(n), nil
}

// rowValues fills a step's {field, value} list, converting text to the
// field's type as forms do.
func (x *exec) rowValues(st *stepTable, in *inputs) (map[string]any, error) {
	pairs, err := in.pairs("values", "field")
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if st.ext != nil {
		// The table converts text to each column's type; a link given as
		// JSON text, such as {{ row | json }}, is read as a row.
		for _, p := range pairs {
			name, v := p[0].(string), plain(p[1])
			if s, ok := v.(string); ok {
				if t := strings.TrimSpace(s); strings.HasPrefix(t, "{") {
					var js any
					if json.Unmarshal([]byte(t), &js) == nil {
						v = js
					}
				}
			}
			out[name] = v
		}
		return out, nil
	}
	t := st.builtin
	for _, p := range pairs {
		name, v := p[0].(string), plain(p[1])
		f := t.FieldByName(name)
		if f == nil {
			return nil, fmt.Errorf("%s has no field named %s", t.Name, name)
		}
		if s, ok := v.(string); ok {
			switch f.Type {
			case tables.TypeNumber:
				if strings.TrimSpace(s) == "" {
					v = nil
				} else if n, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
					v = n
				}
			case tables.TypeBoolean:
				v = truthy(s)
			case tables.TypeLink, tables.TypeAttachment, tables.TypeMultiSelect:
				// A list or object written as text, such as {{ x | json }}.
				if t := strings.TrimSpace(s); strings.HasPrefix(t, "[") || strings.HasPrefix(t, "{") {
					var js any
					if json.Unmarshal([]byte(t), &js) == nil {
						v = js
					}
				}
				if s == "" {
					v = nil
				}
			}
		}
		out[f.Name] = v
	}
	return out, nil
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// compare tests a condition. Numbers compare as numbers when both sides
// are numbers; anything else compares as text, ignoring case for
// contains.
func compare(left any, op string, right any) (bool, error) {
	ln, lok := number(left)
	rn, rok := number(right)
	both := lok && rok && interp.ToText(left) != "" && interp.ToText(right) != ""
	lt, rt := strings.TrimSpace(interp.ToText(left)), strings.TrimSpace(interp.ToText(right))
	switch op {
	case "equals":
		if both {
			return ln == rn, nil
		}
		return lt == rt, nil
	case "not_equals":
		if both {
			return ln != rn, nil
		}
		return lt != rt, nil
	case "gt", "gte", "lt", "lte":
		c := strings.Compare(lt, rt)
		if both {
			c = 0
			if ln < rn {
				c = -1
			} else if ln > rn {
				c = 1
			}
		}
		switch op {
		case "gt":
			return c > 0, nil
		case "gte":
			return c >= 0, nil
		case "lt":
			return c < 0, nil
		}
		return c <= 0, nil
	case "contains", "not_contains":
		has := false
		if list, ok := left.([]any); ok {
			for _, it := range list {
				has = has || strings.EqualFold(strings.TrimSpace(interp.ToText(it)), rt)
			}
		} else {
			has = strings.Contains(strings.ToLower(lt), strings.ToLower(rt))
		}
		return has == (op == "contains"), nil
	case "empty":
		return !truthyValue(left), nil
	case "not_empty":
		return truthyValue(left), nil
	case "true":
		return truthy(left), nil
	case "false":
		return !truthy(left), nil
	}
	return false, fmt.Errorf("the comparison %q is unknown", op)
}

// truthyValue is whether a value is filled in: 0 and false count.
func truthyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(x) != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func (x *exec) sendEmail(in *inputs) (any, error) {
	m, meta, err := x.buildEmail(in, true)
	if m != nil {
		x.e.fillSender(x.ctx, m)
		x.logMail(m, meta)
	}
	if err != nil {
		return nil, err
	}
	var rc *email.Receipt
	if x.dryRun {
		rc = &email.Receipt{Via: "dry run", Note: "not sent: this test run only previews emails"}
		if x.mail != nil {
			x.mail.DryRun = true
		}
	} else if rc, err = x.e.Mailer.Send(x.ctx, m); err != nil {
		return nil, err
	}
	out := map[string]any{"to": m.To, "subject": m.Subject, "delivery": rc.Note, "via": rc.Via, "attachments": meta.attachments}
	if len(m.Cc) > 0 {
		out["cc"] = m.Cc
	}
	if len(m.Bcc) > 0 {
		out["bcc"] = m.Bcc
	}
	if rc.ID != "" {
		out["messageId"] = rc.ID
	}
	if meta.template != "" {
		out["template"], out["templateVersion"] = meta.template, meta.version
	}
	if x.dryRun {
		out["dryRun"] = true
	}
	return out, nil
}

// mailMeta is what the log tells about an email besides the mail.
type mailMeta struct {
	template    string
	version     int
	attachments []string
	warnings    []string
}

// EmailLog is an email as a step made it, kept in the run log so the
// builder can show it.
type EmailLog struct {
	From        string   `json:"from,omitempty"`
	To          []string `json:"to"`
	Cc          []string `json:"cc,omitempty"`
	Bcc         []string `json:"bcc,omitempty"`
	ReplyTo     string   `json:"replyTo,omitempty"`
	Subject     string   `json:"subject"`
	HTML        string   `json:"html,omitempty"`
	Text        string   `json:"text,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
	Template    string   `json:"template,omitempty"`
	Version     int      `json:"version,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	DryRun      bool     `json:"dryRun,omitempty"`
	// Cut is true when the body was too large to keep.
	Cut bool `json:"cut,omitempty"`
}

const (
	maxLoggedMails = 20
	maxLoggedBody  = 256 << 10
)

func newEmailLog(m *email.Mail, meta *mailMeta) *EmailLog {
	l := &EmailLog{From: m.From, To: m.To, Cc: m.Cc, Bcc: m.Bcc, ReplyTo: m.ReplyTo, Subject: m.Subject, HTML: m.HTML, Text: m.Text,
		Attachments: meta.attachments, Template: meta.template, Version: meta.version, Warnings: meta.warnings}
	if len(l.HTML)+len(l.Text) > maxLoggedBody {
		l.HTML, l.Cut = "", true
		if len(l.Text) > maxLoggedBody {
			l.Text = l.Text[:maxLoggedBody]
		}
	}
	return l
}

// logMail keeps the email for the step's log line, for the first emails
// of a run.
func (x *exec) logMail(m *email.Mail, meta *mailMeta) {
	if x.mails >= maxLoggedMails {
		return
	}
	x.mails++
	l := newEmailLog(m, meta)
	l.HTML, l.Text = x.hide(l.HTML), x.hide(l.Text)
	x.mail = l
}

// buildEmail fills a Send email step's inputs into a mail: from a
// template, or from the step's own subject and message. With files, it
// loads the attachments; without, it only names them.
func (x *exec) buildEmail(in *inputs, files bool) (*email.Mail, *mailMeta, error) {
	meta := &mailMeta{attachments: []string{}}
	m := &email.Mail{}
	for _, f := range []struct {
		key string
		to  *[]string
	}{{"to", &m.To}, {"cc", &m.Cc}, {"bcc", &m.Bcc}} {
		v, err := in.value(f.key)
		if err != nil {
			return nil, meta, err
		}
		*f.to = email.Addresses(v)
	}
	var err error
	if m.ReplyTo, err = in.text("replyTo", interp.Text); err != nil {
		return nil, meta, err
	}
	m.ReplyTo = strings.TrimSpace(m.ReplyTo)
	if in.str("mode") == "template" {
		if err := x.fillTemplate(in, m, meta); err != nil {
			return nil, meta, err
		}
	} else {
		if m.Subject, err = in.text("subject", interp.Text); err != nil {
			return nil, meta, err
		}
		m.Subject = email.OneLine(m.Subject)
		if in.str("format") == "html" {
			if m.HTML, err = in.text("body", interp.HTML); err != nil {
				return nil, meta, err
			}
			m.Text = email.HTMLToText(m.HTML)
		} else if m.Text, err = in.text("body", interp.Text); err != nil {
			return nil, meta, err
		}
	}
	av, err := in.value("attachments")
	if err != nil {
		return m, meta, err
	}
	for _, a := range asList(av) {
		ref, ok := fileRef(a)
		if !ok {
			continue
		}
		f, data, err := x.e.Files.Get(x.ctx, x.flow.AppID, ref)
		if err != nil {
			return m, meta, fmt.Errorf("the attachment %s was not found", ref)
		}
		if files {
			m.Attachments = append(m.Attachments, email.Attachment{Name: f.Name, Type: f.Type, Data: data})
		}
		meta.attachments = append(meta.attachments, f.Name)
	}
	if err := m.Check(); err != nil {
		return m, meta, err
	}
	return m, meta, nil
}

// fillTemplate renders the step's template with its field values.
func (x *exec) fillTemplate(in *inputs, m *email.Mail, meta *mailMeta) error {
	if x.e.Emails == nil {
		return errors.New("email templates are not available on this server")
	}
	id := in.str("template")
	if id == "" {
		return errors.New("choose an email template")
	}
	t, err := x.e.Emails.Template(x.ctx, x.flow.AppID, id)
	if err != nil {
		if errors.Is(err, email.ErrNotFound) {
			return fmt.Errorf("the email template %s no longer exists", id)
		}
		return err
	}
	meta.template, meta.version = t.Name, t.Version
	data := map[string]any{}
	given, _ := in.raw["data"].(map[string]any)
	logged := map[string]any{}
	for _, p := range t.Params {
		src, _ := given[p.Name].(string)
		if strings.TrimSpace(src) == "" {
			continue
		}
		v, err := value(src, in.sc)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Name, err)
		}
		data[p.Name] = plain(v)
		logged[p.Name] = data[p.Name]
	}
	if len(logged) > 0 {
		in.values["data"] = logged
	}
	app, _ := x.scope["app"].(map[string]any)
	r, err := t.Render(data, app)
	if err != nil {
		return fmt.Errorf("the template %s: %w", t.Name, err)
	}
	m.Subject, m.HTML, m.Text, meta.warnings = r.Subject, r.HTML, r.Text, r.Warnings
	return nil
}

// fileRef reads a file id from a file object, a list of them or text.
func fileRef(v any) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		if f, ok := x["file"].(map[string]any); ok {
			x = f
		}
		id, _ := x["id"].(string)
		return id, strings.HasPrefix(id, "fil_")
	case string:
		return x, strings.HasPrefix(x, "fil_")
	}
	return "", false
}

func (x *exec) httpRequest(in *inputs) (any, error) {
	method := strings.ToUpper(strings.TrimSpace(in.str("method")))
	if method == "" {
		method = "POST"
	}
	addr, err := in.text("url", interp.URL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(addr))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not a web address", addr)
	}
	bodyType := in.str("bodyType")
	if bodyType == "" {
		bodyType = "json"
	}
	var body io.Reader
	var sent string
	contentType := ""
	switch bodyType {
	case "json":
		sent, err = in.text("body", interp.JSON)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(sent) != "" && !json.Valid([]byte(sent)) {
			return nil, errors.New("the JSON body is not valid once its bindings are filled")
		}
		contentType = "application/json"
	case "text":
		if sent, err = in.text("body", interp.Text); err != nil {
			return nil, err
		}
		contentType = "text/plain; charset=utf-8"
	}
	if sent != "" && method != "GET" && method != "HEAD" {
		body = strings.NewReader(sent)
	}
	ctx, cancel := context.WithTimeout(x.ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" && body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", "Pimpbase")
	pairs, err := in.pairs("headers", "key")
	if err != nil {
		return nil, err
	}
	for _, p := range pairs {
		req.Header.Set(p[0].(string), strings.NewReplacer("\r", "", "\n", "").Replace(interp.ToText(p[1])))
	}
	resp, err := x.e.Client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("no answer within 30 seconds")
		}
		if ue, ok := err.(*url.Error); ok {
			return nil, fmt.Errorf("%s could not be reached: %s", u.Host, ue.Err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody))
	if err != nil {
		return nil, err
	}
	headers := map[string]any{}
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ", ")
	}
	var answer any = string(data)
	if t := bytes.TrimSpace(data); len(t) > 0 && (strings.Contains(resp.Header.Get("Content-Type"), "json") || t[0] == '{' || t[0] == '[') {
		var js any
		if json.Unmarshal(t, &js) == nil {
			answer = js
		}
	}
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	out := map[string]any{"ok": ok, "status": resp.StatusCode, "headers": headers, "body": answer}
	if !ok && in.flag("failOnError", true) {
		return out, fmt.Errorf("%s answered %s", u.Host, resp.Status)
	}
	return out, nil
}

func (x *exec) generatePDF(in *inputs) (any, error) {
	st, err := x.table(in)
	if err != nil {
		return nil, errors.New("choose the table the PDF is stored with")
	}
	if st.builtin == nil {
		return nil, fmt.Errorf("PDFs are stored with built-in tables, and %s is in an external database; choose a built-in table", st.name)
	}
	t := st.builtin
	d := &Document{PageSize: in.str("pageSize"), Landscape: in.str("orientation") == "landscape", Accent: x.accent, Author: x.flow.Name}
	if d.Title, err = in.text("title", interp.Text); err != nil {
		return nil, err
	}
	if d.Content, err = in.text("content", interp.Text); err != nil {
		return nil, err
	}
	if d.Footer, err = in.text("footer", interp.Text); err != nil {
		return nil, err
	}
	name, err := in.text("fileName", interp.Text)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = d.Title
	}
	if name == "" {
		name = "document"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	data, err := d.PDF()
	if err != nil {
		return nil, err
	}
	f, err := x.e.Files.Save(x.ctx, x.flow.AppID, t.ID, x.actor().UserID, name, "application/pdf", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	file := map[string]any{"id": f.ID, "name": f.Name, "type": f.Type, "size": f.Size, "url": tables.FileURL(x.flow.AppID, f.ID)}
	out := map[string]any{"file": file, "url": file["url"], "attached": false}
	// Optionally add the PDF to an attachment field of a row.
	field := strings.TrimSpace(in.str("field"))
	if field == "" || strings.TrimSpace(in.str("rowId")) == "" {
		return out, nil
	}
	fd := t.FieldByName(field)
	if fd == nil || fd.Type != tables.TypeAttachment {
		return nil, fmt.Errorf("%s has no attachment field named %s", t.Name, field)
	}
	id, err := x.rowID(in)
	if err != nil {
		return nil, err
	}
	row, err := x.e.Tables.GetRow(x.ctx, x.actor(), x.flow.AppID, t.ID, id)
	if err != nil {
		return nil, rowErr(err)
	}
	list := asList(plain(row[fd.Name]))
	if in.str("attachMode") == "replace" {
		list = nil
	}
	list = append(list, file)
	if _, err := x.e.Tables.UpdateRow(x.ctx, x.actor(), x.flow.AppID, t.ID, id, map[string]any{fd.Name: list}); err != nil {
		return nil, rowErr(err)
	}
	in.values["field"] = fd.Name
	out["attached"] = true
	return out, nil
}

// plain turns any value into what JSON gives back: maps, lists, float64,
// strings, booleans and nil, which bindings can walk.
func plain(v any) any {
	switch v.(type) {
	case nil, string, bool, float64:
		return v
	}
	js, err := json.Marshal(v)
	if err != nil {
		return interp.ToText(v)
	}
	var out any
	json.Unmarshal(js, &out)
	return out
}

// trimLog keeps large values out of the run log.
func trimLog(v any, trimmed *bool) any {
	js, err := json.Marshal(v)
	if err != nil || len(js) <= maxLogValue {
		return v
	}
	// Keep the shape, so the builder can still offer the keys and show
	// sample values: cut long lists and texts, then cut harder.
	for _, lim := range []struct{ items, text int }{{20, 1000}, {5, 200}, {2, 80}} {
		short := shorten(v, lim.items, lim.text)
		if js, err := json.Marshal(short); err == nil && len(js) <= maxLogValue {
			*trimmed = true
			return short
		}
	}
	return map[string]any{"truncated": true, "preview": string(js[:2048]) + "…", "size": len(js)}
}

// shorten copies v keeping at most items list items and text bytes of
// each text.
func shorten(v any, items, text int) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, y := range x {
			out[k] = shorten(y, items, text)
		}
		return out
	case []any:
		n := min(len(x), items)
		out := make([]any, n)
		for i := range n {
			out[i] = shorten(x[i], items, text)
		}
		return out
	case string:
		if len(x) > text {
			cut := text
			for cut > 0 && !utf8.RuneStart(x[cut]) {
				cut--
			}
			return x[:cut] + "…"
		}
	}
	return v
}

// PreviewEmail fills a Send email step's inputs over a sample scope (what
// the builder shows for the step: trigger, steps, vars…) as a run would,
// without sending. files loads the attachments too, for a test send.
func (e *Engine) PreviewEmail(ctx context.Context, appID string, stepInputs, scope map[string]any, files bool) (*email.Mail, *EmailLog, error) {
	app, err := e.Apps.Get(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	sc := interp.Map{}
	for k, v := range scope {
		sc[k] = v
	}
	sc["app"] = map[string]any{"id": app.ID, "name": app.Name, "slug": app.Slug}
	for _, k := range []string{"trigger", "steps", "vars", "env", "user"} {
		if _, ok := sc[k]; !ok {
			sc[k] = map[string]any{}
		}
	}
	x := &exec{e: e, flow: &Flow{AppID: appID}, ctx: ctx, scope: sc, steps: map[string]any{}, vars: map[string]any{}}
	in := &inputs{x: x, sc: sc, values: map[string]any{}, raw: stepInputs}
	m, meta, err := x.buildEmail(in, files)
	if m == nil {
		return nil, nil, err
	}
	e.fillSender(ctx, m)
	return m, newEmailLog(m, meta), err
}

// fillSender gives a mail without a sender the workspace's, so logs and
// previews show who it comes from.
func (e *Engine) fillSender(ctx context.Context, m *email.Mail) {
	if m.From == "" && e.Emails != nil {
		m.From = e.Emails.Sender(ctx)
	}
}
