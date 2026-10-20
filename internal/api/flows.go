package api

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/flows"
	"github.com/bubibase/bubibase/internal/rbac"
	"github.com/bubibase/bubibase/internal/tables"
)

// Flows, for builders; running manual flows, for app users; webhooks, for
// anyone holding a flow's secret address.

// waitForRun is how long run requests wait for the flow to finish before
// answering with a run that is still going.
var waitForRun = 30 * time.Second

func (s *Server) listFlows(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Flows.Flows(r.Context(), a.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

func (s *Server) getFlow(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	f, err := s.Flows.Flow(r.Context(), a.ID, r.PathValue("flow"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, f)
}

func (s *Server) createFlow(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	data, err := readBody(r)
	if err != nil {
		return err
	}
	if err := s.bodyRoles(r, a.ID, data, "trigger", "role"); err != nil {
		return err
	}
	if err := s.bodyRoles(r, a.ID, data, "runRole"); err != nil {
		return err
	}
	f, err := s.Flows.CreateFlow(r.Context(), a.ID, data)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, f)
}

func (s *Server) updateFlow(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	data, err := readBody(r)
	if err != nil {
		return err
	}
	if err := s.bodyRoles(r, a.ID, data, "trigger", "role"); err != nil {
		return err
	}
	if err := s.bodyRoles(r, a.ID, data, "runRole"); err != nil {
		return err
	}
	f, err := s.Flows.UpdateFlow(r.Context(), a.ID, r.PathValue("flow"), data)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, f)
}

func (s *Server) deleteFlow(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if err := s.Flows.DeleteFlow(r.Context(), a.ID, r.PathValue("flow")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) newHook(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	f, err := s.Flows.NewHookToken(r.Context(), a.ID, r.PathValue("flow"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, f)
}

func (s *Server) flowRuns(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.Flows.Runs(r.Context(), a.ID, r.PathValue("flow"), limit)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

func (s *Server) flowRun(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	run, err := s.Flows.Run(r.Context(), a.ID, r.PathValue("run"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, run)
}

func (s *Server) userOf(r *http.Request, act rbac.Actor) map[string]any {
	if u := userFrom(r); u != nil {
		return map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": act.Role.String()}
	}
	return nil
}

// testFlow runs a flow from the builder, switched on or not, with sample
// trigger data and the development values of variables.
func (s *Server) testFlow(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	_, act, err := s.actor(r)
	if err != nil {
		return err
	}
	f, err := s.Flows.Flow(r.Context(), a.ID, r.PathValue("flow"))
	if err != nil {
		return err
	}
	var in struct {
		Trigger map[string]any `json:"trigger"`
	}
	if data, _ := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20)); len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &in); err != nil {
			return errStatus(400, "invalid JSON: "+err.Error())
		}
	}
	data := in.Trigger
	if data == nil {
		if data, err = s.sampleTrigger(r, f); err != nil {
			return err
		}
	}
	if f.Trigger.Type == flows.OnManual {
		given, _ := data["params"].(map[string]any)
		p, err := f.Trigger.Params(given)
		if err != nil {
			return err
		}
		data["params"] = p
	}
	in2 := flows.StartInput{Kind: f.Trigger.Type, Data: data, User: s.userOf(r, act), Test: true, Actor: &act}
	if f.Trigger.Unattended() {
		// Test as the real runs will go: with nobody, so as the run-as role.
		in2.User, in2.Actor = nil, nil
	}
	run, err := s.Engine.RunNow(r.Context(), f, in2, waitForRun)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, run)
}

// sampleTrigger makes trigger data for a test run: the newest row of a row
// trigger's table, the parameters' defaults, or the current time.
func (s *Server) sampleTrigger(r *http.Request, f *flows.Flow) (map[string]any, error) {
	ctx := r.Context()
	switch f.Trigger.Type {
	case flows.RowCreated, flows.RowUpdated, flows.RowDeleted:
		t, err := s.Tables.Get(ctx, f.AppID, f.Trigger.Table)
		if err != nil {
			return nil, errStatus(400, "the trigger's table does not exist")
		}
		page, err := s.Tables.ListRows(ctx, rbac.Actor{Role: rbac.Admin, Builder: true}, f.AppID, t.ID, tables.Query{Sort: "_id", Desc: true, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(page.Rows) == 0 {
			return nil, errStatus(400, "add a row to "+t.Name+" to test this flow with it")
		}
		data := map[string]any{"table": map[string]any{"id": t.ID, "name": t.Name}, "row": page.Rows[0]}
		if f.Trigger.Type == flows.RowUpdated {
			data["old"], data["changed"] = page.Rows[0], []any{}
		}
		return data, nil
	case flows.OnSchedule:
		loc, err := time.LoadLocation(f.Trigger.Timezone)
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc).Truncate(time.Minute)
		return map[string]any{"time": now.Format(time.RFC3339), "date": now.Format("2006-01-02"), "timezone": loc.String()}, nil
	case flows.OnWebhook:
		return map[string]any{"method": "POST", "body": map[string]any{}, "query": map[string]any{}, "headers": map[string]any{}}, nil
	}
	return map[string]any{"params": map[string]any{}}, nil
}

// runFlow runs a manual flow for an app user who holds its role, as a
// screen button does. It answers with the run's result, not its log.
func (s *Server) runFlow(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	f, err := s.Flows.Flow(r.Context(), a.ID, r.PathValue("flow"))
	if err != nil {
		return err
	}
	if f.Trigger.Type != flows.OnManual {
		return errStatus(400, f.Name+" is not started by buttons")
	}
	if err := act.Require(f.Trigger.Role, "running "+f.Name); err != nil {
		return err
	}
	var in struct {
		Params map[string]any `json:"params"`
		// Test, for builders, runs with development values even when the
		// flow is switched off.
		Test bool `json:"test"`
	}
	if data, _ := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20)); len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &in); err != nil {
			return errStatus(400, "invalid JSON: "+err.Error())
		}
	}
	test := in.Test && act.Builder
	if !f.Enabled && !test {
		return errStatus(409, f.Name+" is switched off")
	}
	params, err := f.Trigger.Params(in.Params)
	if err != nil {
		return err
	}
	run, err := s.Engine.RunNow(r.Context(), f, flows.StartInput{Kind: flows.OnManual, Data: map[string]any{"params": params}, User: s.userOf(r, act), Test: test, Actor: &act}, waitForRun)
	if err != nil {
		return err
	}
	out := map[string]any{"id": run.ID, "status": run.Status, "output": run.Output, "error": run.Error}
	if act.Builder {
		out["steps"] = run.Steps
	}
	return writeJSON(w, 200, out)
}

// webhook starts a webhook flow. The secret address is the only check, so
// it answers 404 for unknown and switched-off flows alike.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) error {
	f, err := s.Flows.ByHook(r.Context(), r.PathValue("token"))
	if err != nil || !f.Enabled || f.Trigger.Type != flows.OnWebhook {
		return errStatus(404, "no such webhook")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		return errStatus(413, "the body is larger than 1 MB")
	}
	var body any
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch {
	case len(strings.TrimSpace(string(raw))) == 0:
		body = map[string]any{}
	case ct == "application/x-www-form-urlencoded":
		vals, err := url.ParseQuery(string(raw))
		if err != nil {
			return errStatus(400, "the form body cannot be read")
		}
		body = flat(vals)
	case strings.Contains(ct, "json") || raw[0] == '{' || raw[0] == '[':
		if err := json.Unmarshal(raw, &body); err != nil {
			return errStatus(400, "invalid JSON: "+err.Error())
		}
	default:
		body = string(raw)
	}
	headers := map[string]any{}
	for k, v := range r.Header {
		switch strings.ToLower(k) {
		case "cookie", "authorization":
			continue
		}
		headers[k] = strings.Join(v, ", ")
	}
	data := map[string]any{"method": r.Method, "body": body, "query": flat(r.URL.Query()), "headers": headers}
	in := flows.StartInput{Kind: flows.OnWebhook, Data: data}
	if !f.Trigger.Respond {
		run, _, err := s.Engine.Begin(r.Context(), f, in)
		if err != nil {
			return err
		}
		return writeJSON(w, 202, map[string]any{"ok": true, "runId": run.ID})
	}
	run, err := s.Engine.RunNow(r.Context(), f, in, waitForRun)
	if err != nil {
		return err
	}
	switch run.Status {
	case flows.Succeeded:
		return writeJSON(w, 200, run.Output)
	case flows.Failed:
		return writeJSON(w, 500, map[string]any{"error": run.Error, "runId": run.ID})
	}
	return writeJSON(w, 202, map[string]any{"ok": true, "runId": run.ID, "status": run.Status})
}

func flat(v url.Values) map[string]any {
	out := map[string]any{}
	for k, list := range v {
		if len(list) == 1 {
			out[k] = list[0]
		} else {
			items := make([]any, len(list))
			for i, x := range list {
				items[i] = x
			}
			out[k] = items
		}
	}
	return out
}

// nextRuns shows when a schedule fires next.
func (s *Server) nextRuns(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.requireBuilder(r); err != nil {
		return err
	}
	q := r.URL.Query()
	tz := q.Get("timezone")
	if tz == "" {
		tz = "UTC"
	}
	times, err := flows.NextRuns(q.Get("cron"), tz, time.Now(), 5)
	if err != nil {
		return errStatus(400, err.Error())
	}
	out := make([]string, len(times))
	for i, t := range times {
		out[i] = t.Format(time.RFC3339)
	}
	return writeJSON(w, 200, map[string]any{"next": out})
}

// runtimeFlow is what a published app knows about a flow it may run.
type runtimeFlow struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Enabled    bool          `json:"enabled"`
	Parameters []flows.Param `json:"parameters"`
}

func (s *Server) runtimeFlows(r *http.Request, appID string, act rbac.Actor) ([]runtimeFlow, error) {
	all, err := s.Flows.Flows(r.Context(), appID)
	if err != nil {
		return nil, err
	}
	out := []runtimeFlow{}
	for _, f := range all {
		if f.Trigger.Type != flows.OnManual || (!act.Builder && (!act.Has(f.Trigger.Role) || !f.Enabled)) {
			continue
		}
		out = append(out, runtimeFlow{ID: f.ID, Name: f.Name, Enabled: f.Enabled, Parameters: f.Trigger.Parameters})
	}
	return out, nil
}
