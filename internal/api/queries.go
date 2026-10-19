package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/bubibase/bubibase/internal/apps"
	"github.com/bubibase/bubibase/internal/queries"
	"github.com/bubibase/bubibase/internal/rbac"
)

// API data sources, queries and variables, for builders; running queries,
// for app users.

func readBody(r *http.Request) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		return nil, errStatus(400, "the request body is too large")
	}
	if !json.Valid(data) {
		return nil, errStatus(400, "invalid JSON")
	}
	return data, nil
}

func (s *Server) listDatasources(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Queries.Datasources(r.Context(), a.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

func (s *Server) createDatasource(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	data, err := readBody(r)
	if err != nil {
		return err
	}
	d, err := s.Queries.CreateDatasource(r.Context(), a.ID, data)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, d)
}

func (s *Server) updateDatasource(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	data, err := readBody(r)
	if err != nil {
		return err
	}
	d, err := s.Queries.UpdateDatasource(r.Context(), a.ID, r.PathValue("ds"), data)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, d)
}

func (s *Server) deleteDatasource(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if err := s.Queries.DeleteDatasource(r.Context(), a.ID, r.PathValue("ds")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) listQueries(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Queries.Queries(r.Context(), a.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

func (s *Server) getQuery(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	q, err := s.Queries.Query(r.Context(), a.ID, r.PathValue("query"))
	if err != nil {
		return err
	}
	return writeJSON(w, 200, q)
}

func (s *Server) createQuery(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	data, err := readBody(r)
	if err != nil {
		return err
	}
	q, err := s.Queries.CreateQuery(r.Context(), a.ID, data)
	if err != nil {
		return err
	}
	return writeJSON(w, 201, q)
}

func (s *Server) updateQuery(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	data, err := readBody(r)
	if err != nil {
		return err
	}
	q, err := s.Queries.UpdateQuery(r.Context(), a.ID, r.PathValue("query"), data)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, q)
}

func (s *Server) deleteQuery(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if err := s.Queries.DeleteQuery(r.Context(), a.ID, r.PathValue("query")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

type runRequest struct {
	Params map[string]any `json:"params"`
	// Env is "development" or "production"; only builders may use
	// development values.
	Env string `json:"env"`
	// Query is an unsaved query, for test runs from the API studio.
	Query json.RawMessage `json:"query"`
}

func (s *Server) runInput(r *http.Request, a *apps.App, act rbac.Actor, in runRequest) queries.RunInput {
	ri := queries.RunInput{
		Params: in.Params,
		Env:    queries.Production,
		App:    map[string]any{"id": a.ID, "name": a.Name, "slug": a.Slug},
	}
	if in.Env == queries.Development && act.Builder {
		ri.Env = queries.Development
	}
	if u := userFrom(r); u != nil {
		ri.User = map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": act.Role.String()}
	}
	return ri
}

// testQuery runs a query as the API studio has it, saved or not, with the
// parameters' test values.
func (s *Server) testQuery(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	if userFrom(r) == nil {
		return errLogin
	}
	if !act.Builder {
		return errStatus(403, "only builders can test queries")
	}
	var in runRequest
	if err := readJSON(r, &in); err != nil {
		return err
	}
	q, err := s.Queries.CheckQuery(r.Context(), a.ID, in.Query)
	if err != nil {
		return err
	}
	if in.Env == "" {
		in.Env = queries.Development
	}
	ri := s.runInput(r, a, act, in)
	ri.Test = true
	res, err := s.Runner.Run(r.Context(), a.ID, q, ri)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, res)
}

// runQuery runs a saved query for an app user who holds its role.
func (s *Server) runQuery(w http.ResponseWriter, r *http.Request) error {
	a, act, err := s.actor(r)
	if err != nil {
		return err
	}
	// The body is optional: no parameters runs with the defaults.
	data, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		return errStatus(400, "the request body is too large")
	}
	var in runRequest
	if len(strings.TrimSpace(string(data))) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.UseNumber()
		if err := dec.Decode(&in); err != nil {
			return errStatus(400, "invalid JSON: "+err.Error())
		}
	}
	q, err := s.Queries.Query(r.Context(), a.ID, r.PathValue("query"))
	if err != nil {
		return err
	}
	if err := act.Require(q.Role, "running "+q.Name); err != nil {
		return err
	}
	res, err := s.Runner.Run(r.Context(), a.ID, q, s.runInput(r, a, act, in))
	if err != nil {
		return err
	}
	if !act.Builder {
		res.Request = nil
	}
	return writeJSON(w, 200, res)
}

func (s *Server) listVariables(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	list, err := s.Queries.Variables(r.Context(), a.ID, false)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, list)
}

func (s *Server) setVariable(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	var in queries.VariableInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	v, err := s.Queries.SetVariable(r.Context(), a.ID, r.PathValue("name"), in)
	if err != nil {
		return err
	}
	return writeJSON(w, 200, v)
}

func (s *Server) deleteVariable(w http.ResponseWriter, r *http.Request) error {
	a, err := s.builderApp(r)
	if err != nil {
		return err
	}
	if err := s.Queries.DeleteVariable(r.Context(), a.ID, r.PathValue("name")); err != nil {
		return err
	}
	return writeJSON(w, 200, map[string]any{"ok": true})
}

// runtimeQuery is what a published app knows about a query it may run.
type runtimeQuery struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Parameters []runtimeParam      `json:"parameters"`
	Fields     []queries.FieldInfo `json:"fields"`
}

type runtimeParam struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default"`
}

func (s *Server) runtimeQueries(r *http.Request, appID string, act rbac.Actor) ([]runtimeQuery, error) {
	all, err := s.Queries.Queries(r.Context(), appID)
	if err != nil {
		return nil, err
	}
	out := []runtimeQuery{}
	for _, q := range all {
		if !act.Builder && act.Role < q.Role {
			continue
		}
		rq := runtimeQuery{ID: q.ID, Name: q.Name, Parameters: []runtimeParam{}, Fields: q.Fields}
		for _, p := range q.Parameters {
			rq.Parameters = append(rq.Parameters, runtimeParam{p.Name, p.Type, p.Default})
		}
		out = append(out, rq)
	}
	return out, nil
}
