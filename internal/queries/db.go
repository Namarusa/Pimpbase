package queries

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/extdb"
	"github.com/bubibase/bubibase/internal/interp"
)

// Settings fills the bindings of a database data source's settings with
// the variables of one environment.
func (s *Service) Settings(ctx context.Context, ds *Datasource, env string) (extdb.Settings, error) {
	if ds.Config.DB == nil {
		return extdb.Settings{}, &RunError{Message: ds.Name + " is not a database"}
	}
	vals, _, err := s.values(ctx, ds.AppID, env)
	if err != nil {
		return extdb.Settings{}, err
	}
	return fillSettings(*ds.Config.DB, interp.Map{"env": vals})
}

func fillSettings(set extdb.Settings, scope interp.Scope) (extdb.Settings, error) {
	set.Options = append([]extdb.Option(nil), set.Options...)
	for _, p := range set.Text() {
		t, err := interp.Parse(*p)
		if err != nil {
			return set, &RunError{Message: err.Error(), Binding: true}
		}
		out, err := t.Render(interp.Text, scope)
		if err != nil {
			return set, &RunError{Message: "the connection settings: " + err.Error(), Binding: true}
		}
		*p = out
	}
	return set, nil
}

// Source opens (or reuses) the connection pool of a database data source
// for an environment.
func (r *Runner) Source(ctx context.Context, ds *Datasource, env string) (extdb.Source, error) {
	if env != Development {
		env = Production
	}
	set, err := r.svc.Settings(ctx, ds, env)
	if err != nil {
		return nil, err
	}
	src, err := r.pool.Get(ds.ID+"/"+env, ds.Kind, set)
	if err != nil {
		return nil, &RunError{Message: fmt.Sprintf("%s: %s", ds.Name, err)}
	}
	return src, nil
}

// Forget closes the connections of a data source.
func (r *Runner) Forget(id string) {
	r.pool.Forget(id + "/" + Development)
	r.pool.Forget(id + "/" + Production)
}

// TestConnection tries a database data source's settings, saved or not.
func (r *Runner) TestConnection(ctx context.Context, appID string, ds *Datasource, env string) (*extdb.TestResult, error) {
	if err := ds.Check(); err != nil {
		return nil, err
	}
	if !ds.Database() {
		return nil, invalid("kind", "only databases have a connection to test")
	}
	if env != Development {
		env = Production
	}
	vals, secrets, err := r.svc.values(ctx, appID, env)
	if err != nil {
		return nil, err
	}
	set, err := fillSettings(*ds.Config.DB, interp.Map{"env": vals})
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := extdb.Test(cctx, ds.Kind, set)
	if err != nil {
		return nil, &RunError{Message: err.Error()}
	}
	st := &run{secrets: secrets}
	res.Message = st.hide(res.Message)
	if set.Password != "" && len(set.Password) >= 3 {
		res.Message = strings.ReplaceAll(res.Message, set.Password, "••••••")
	}
	return res, nil
}

// address shows where a database is, without its password.
func address(kind string, set extdb.Settings) string {
	if set.URI != "" {
		u := set.URI
		if at := strings.LastIndex(u, "@"); at >= 0 {
			if scheme := strings.Index(u, "://"); scheme >= 0 && scheme < at {
				u = u[:scheme+3] + "••••••" + u[at:]
			} else if scheme < 0 {
				u = "••••••" + u[at:]
			}
		}
		return u
	}
	host := set.Host
	if set.Port != "" {
		host = net.JoinHostPort(set.Host, set.Port)
	}
	return kind + "://" + host + "/" + set.Database
}

// runDB sends a query to a database data source.
func (st *run) runDB(ctx context.Context, ds *Datasource, q *Query, scope interp.Scope) (*Result, error) {
	src, err := st.r.Source(ctx, ds, st.in.Env)
	if err != nil {
		return nil, err
	}
	set, _ := st.r.svc.Settings(ctx, ds, st.in.Env)
	stmt := extdb.Statement{MaxRows: 5000}
	echo := &Echo{URL: address(ds.Kind, set), Headers: []Pair{}}
	if ds.Kind == "mongodb" {
		fill := func(field, src string, mode interp.Mode) (string, error) {
			t, err := interp.Parse(src)
			if err != nil {
				return "", &RunError{Message: fmt.Sprintf("%s: %s", field, err), Binding: true}
			}
			out, err := t.Render(mode, scope)
			if err != nil {
				var re *RunError
				if errors.As(err, &re) {
					return "", err
				}
				return "", &RunError{Message: fmt.Sprintf("%s: %s", field, err), Binding: true}
			}
			return out, nil
		}
		stmt.Operation = q.Operation
		if stmt.Collection, err = fill("the collection", q.Collection, interp.Text); err != nil {
			return nil, err
		}
		if stmt.Filter, err = fill("the filter", q.Filter, interp.JSON); err != nil {
			return nil, err
		}
		if stmt.Document, err = fill("the document", q.Document, interp.JSON); err != nil {
			return nil, err
		}
		if stmt.Options, err = fill("the options", q.Options, interp.JSON); err != nil {
			return nil, err
		}
		echo.Method = strings.ToUpper(stmt.Operation)
		echo.URL += "/" + stmt.Collection
		parts := []string{}
		for _, p := range [][2]string{{"filter", stmt.Filter}, {"document", stmt.Document}, {"options", stmt.Options}} {
			if strings.TrimSpace(p[1]) != "" {
				parts = append(parts, p[0]+": "+p[1])
			}
		}
		echo.Body = strings.Join(parts, "\n")
	} else {
		stmt.SQL, stmt.Args, err = extdb.BindSQL(ds.Kind, q.SQL, scope)
		if err != nil {
			var re *RunError
			if errors.As(err, &re) {
				return nil, err
			}
			return nil, &RunError{Message: err.Error(), Binding: true}
		}
		if strings.TrimSpace(stmt.SQL) == "" {
			return nil, &RunError{Message: "write the SQL to run", Binding: true}
		}
		echo.Method = "SQL"
		echo.Body = stmt.SQL
		echo.Params = make([]any, len(stmt.Args))
		for i, a := range stmt.Args {
			if t, ok := a.(time.Time); ok {
				a = t.Format(time.RFC3339)
			}
			echo.Params[i] = a
		}
	}

	key := ""
	if q.CacheSeconds > 0 {
		h := sha256.New()
		args, _ := json.Marshal(stmt.Args)
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", st.appID, q.ID, st.in.Env, stmt.SQL, args, stmt.Collection, stmt.Operation, stmt.Filter, stmt.Document, stmt.Options)
		key = hex.EncodeToString(h.Sum(nil))
	}
	st.redact(echo)
	for i, p := range echo.Params {
		if s, ok := p.(string); ok {
			echo.Params[i] = st.hide(s)
		}
	}
	if key != "" && !(st.in.Test && len(st.stack) == 1) {
		if res := st.r.cached(key); res != nil {
			res.Request = echo
			st.done[q.ID] = res
			return res, nil
		}
	}

	timeout := time.Duration(ds.Config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	out, err := src.Run(cctx, stmt)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.DeadlineExceeded) || cctx.Err() != nil {
			msg = fmt.Sprintf("no answer within %s", timeout)
		}
		if errors.Is(err, extdb.ErrReadOnly) {
			msg = ds.Name + " is read only; this statement would change data"
		}
		return nil, &RunError{Message: fmt.Sprintf("%s failed: %s", q.Name, st.hide(msg))}
	}
	rows := make([]any, len(out.Rows))
	for i, r := range out.Rows {
		for k, v := range r {
			if t, ok := v.(time.Time); ok {
				r[k] = t.UTC().Format("2006-01-02T15:04:05Z")
			}
		}
		rows[i] = r
	}
	res := &Result{
		OK: true, Status: 200, StatusText: "OK", Headers: map[string]string{}, Format: "json",
		Rows: rows, DurationMs: time.Since(start).Milliseconds(), Request: echo,
		RowsAffected: out.RowsAffected, Truncated: out.Truncated, Columns: out.Columns,
	}
	if out.Query {
		res.Body = rows
	} else {
		res.Body = map[string]any{"rowsAffected": float64(out.RowsAffected), "rows": rows}
	}
	res.Fields = fields(rows)
	// Columns keep the order the database gave them.
	if len(out.Columns) > 0 {
		byName := map[string]FieldInfo{}
		for _, f := range res.Fields {
			byName[f.Name] = f
		}
		ordered := make([]FieldInfo, 0, len(out.Columns))
		for _, c := range out.Columns {
			if f, ok := byName[c]; ok {
				ordered = append(ordered, f)
			} else {
				ordered = append(ordered, FieldInfo{Name: c, Type: "text"})
			}
		}
		res.Fields = ordered
	}
	if raw, err := json.Marshal(res.Body); err == nil {
		res.Size = len(raw)
		if len(raw) > maxRaw {
			raw = raw[:maxRaw]
		}
		res.Raw = string(raw)
	}
	if key != "" {
		st.r.store(key, res, time.Duration(q.CacheSeconds)*time.Second)
	}
	st.done[q.ID] = res
	return res, nil
}
