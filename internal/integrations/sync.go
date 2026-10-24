package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/formula"
	"github.com/pimpbase/pimpbase/internal/interp"
	"github.com/pimpbase/pimpbase/internal/rbac"
	"github.com/pimpbase/pimpbase/internal/tables"
)

const (
	maxSyncItems = 20000
	maxSyncPages = 400
)

func syncActor(c *Connection) rbac.Actor {
	return rbac.Actor{Name: "Sync " + c.Name, Role: rbac.Admin, Builder: true}
}

// fields are a sync's table fields: the manifest's and those detected.
func syncFields(def *Sync, sc *SyncConfig) []SyncField {
	out := append([]SyncField{}, def.Fields...)
	for _, f := range sc.Fields {
		dup := false
		for _, g := range out {
			dup = dup || strings.EqualFold(g.Name, f.Name)
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out
}

func keyField(fields []SyncField) SyncField {
	for _, f := range fields {
		if f.Key {
			return f
		}
	}
	return fields[0]
}

func fieldType(t string) tables.FieldType {
	switch t {
	case "number":
		return tables.TypeNumber
	case "boolean":
		return tables.TypeBoolean
	case "date":
		return tables.TypeDate
	case "longtext":
		return tables.TypeLongText
	}
	return tables.TypeText
}

// table finds the sync's table, making it or adding missing fields.
func (s *Service) table(ctx context.Context, c *Connection, def *Sync, sc *SyncConfig) (*tables.Table, error) {
	fields := syncFields(def, sc)
	var t *tables.Table
	if sc.Table != "" {
		if got, err := s.Tables.Get(ctx, c.AppID, sc.Table); err == nil {
			t = got
		}
	}
	if t == nil {
		list, err := s.Tables.List(ctx, c.AppID)
		if err != nil {
			return nil, err
		}
		name := def.Table
		if c.Name != s.Catalog.Get(c.Kind).Name {
			name = def.Table + " (" + c.Name + ")"
		}
		taken := map[string]bool{}
		for _, x := range list {
			taken[strings.ToLower(x.Name)] = true
		}
		base := name
		for i := 2; taken[strings.ToLower(name)]; i++ {
			name = fmt.Sprintf("%s %d", base, i)
		}
		key := keyField(fields)
		in := []tables.FieldInput{{Name: key.Name, Type: fieldType(key.Type), Indexed: true}}
		for _, f := range fields {
			if f.Name != key.Name {
				in = append(in, tables.FieldInput{Name: f.Name, Type: fieldType(f.Type)})
			}
		}
		if t, err = s.Tables.CreateTable(ctx, c.AppID, name, in); err != nil {
			return nil, err
		}
		sc.Table = t.ID
		return t, nil
	}
	for _, f := range fields {
		if t.FieldByName(f.Name) == nil {
			var err error
			if t, _, err = s.Tables.AddField(ctx, c.AppID, t.ID, tables.FieldInput{Name: f.Name, Type: fieldType(f.Type)}); err != nil {
				return nil, fmt.Errorf("adding the field %s: %w", f.Name, err)
			}
		}
	}
	return t, nil
}

// syncValue turns an item's value into a field value.
func syncValue(f SyncField, item any) any {
	v := Dig(item, f.Path)
	switch f.Transform {
	case "cents":
		if n, ok := toNumber(v); ok {
			return n / 100
		}
		return nil
	case "unix":
		if n, ok := toNumber(v); ok && n > 0 {
			return time.Unix(int64(n), 0).UTC()
		}
		return nil
	case "upper":
		return strings.ToUpper(interp.ToText(v))
	case "join":
		if l, ok := v.([]any); ok {
			parts := make([]string, 0, len(l))
			for _, x := range l {
				if m, ok := x.(map[string]any); ok {
					if n, ok := m["name"]; ok {
						x = n
					} else if n, ok := m["login"]; ok {
						x = n
					}
				}
				parts = append(parts, interp.ToText(x))
			}
			return strings.Join(parts, ", ")
		}
	}
	switch f.Type {
	case "number":
		if n, ok := toNumber(v); ok {
			return n
		}
		return nil
	case "boolean":
		b, _ := v.(bool)
		return b
	case "date":
		switch x := v.(type) {
		case float64:
			return time.Unix(int64(x), 0).UTC()
		case string:
			if t, err := time.Parse(time.RFC3339, x); err == nil {
				return t.UTC()
			}
			if t, err := formula.ParseDate(x); err == nil {
				return t
			}
		}
		return nil
	}
	switch v.(type) {
	case nil:
		return ""
	case map[string]any, []any:
		b, _ := json.Marshal(v)
		return string(b)
	}
	return interp.ToText(v)
}

func same(a, b any) bool {
	norm := func(v any) string {
		switch x := v.(type) {
		case nil:
			return ""
		case time.Time:
			return x.UTC().Truncate(time.Second).Format(time.RFC3339)
		case float64:
			if x == math.Trunc(x) {
				return fmt.Sprint(int64(x))
			}
		case string:
			if t, err := formula.ParseDate(x); err == nil && len(x) >= 10 && strings.Count(x, "-") >= 2 {
				return t.UTC().Truncate(time.Second).Format(time.RFC3339)
			}
		}
		return interp.ToText(v)
	}
	return norm(a) == norm(b)
}

func rowID(r map[string]any) int64 {
	switch x := r["_id"].(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}

// existing loads the table's rows by key value.
func (s *Service) existing(ctx context.Context, c *Connection, t *tables.Table, key string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for off := 0; ; off += 1000 {
		page, err := s.Tables.ListRows(ctx, syncActor(c), c.AppID, t.ID, tables.Query{Sort: "_id", Limit: 1000, Offset: off})
		if err != nil {
			return nil, err
		}
		for _, r := range page.Rows {
			if k := interp.ToText(r[key]); k != "" {
				out[k] = r
			}
		}
		if len(page.Rows) < 1000 || off > maxSyncItems*2 {
			return out, nil
		}
	}
}

// upsert writes one item to its row; it reports what it did.
func (s *Service) upsert(ctx context.Context, c *Connection, t *tables.Table, fields []SyncField, item any, rows map[string]map[string]any) (string, error) {
	key := keyField(fields)
	k := interp.ToText(Dig(item, key.Path))
	if k == "" {
		return "", nil
	}
	vals := map[string]any{}
	for _, f := range fields {
		if t.FieldByName(f.Name) != nil {
			vals[f.Name] = syncValue(f, item)
		}
	}
	if old := rows[k]; old != nil {
		changed := map[string]any{}
		for name, v := range vals {
			if !same(old[name], v) {
				changed[name] = v
			}
		}
		if len(changed) == 0 {
			return "same", nil
		}
		row, err := s.Tables.UpdateRow(ctx, syncActor(c), c.AppID, t.ID, rowID(old), changed)
		if err != nil {
			return "", err
		}
		rows[k] = row
		return "updated", nil
	}
	row, err := s.Tables.CreateRow(ctx, syncActor(c), c.AppID, t.ID, vals)
	if err != nil {
		return "", err
	}
	if rows != nil {
		rows[k] = row
	}
	return "created", nil
}

// detect adds fields for keys found under the sync's Detect path.
func detect(def *Sync, sc *SyncConfig, items []any) {
	if def.Detect == "" {
		return
	}
	have := map[string]bool{}
	for _, f := range syncFields(def, sc) {
		have[strings.ToLower(f.Name)] = true
	}
	found := map[string]string{}
	var order []string
	for _, it := range items {
		m, _ := Dig(it, def.Detect).(map[string]any)
		for k, v := range m {
			if have[strings.ToLower(k)] || strings.HasPrefix(k, "_") {
				continue
			}
			typ := "text"
			switch v.(type) {
			case float64:
				typ = "number"
			case bool:
				typ = "boolean"
			}
			if prev, ok := found[k]; ok && prev != typ {
				typ = "text"
			} else if !ok {
				order = append(order, k)
			}
			found[k] = typ
		}
	}
	sort.Strings(order)
	for _, k := range order {
		path := def.Detect + "." + k
		if !keyRe.MatchString(k) {
			b, _ := json.Marshal(k)
			path = def.Detect + "[" + string(b) + "]"
		}
		sc.Fields = append(sc.Fields, SyncField{Name: k, Path: path, Type: found[k]})
	}
}

// RunSync reads the whole list and brings the table up to date: new
// items become rows, changed ones update theirs, and rows whose item is
// gone are deleted (only when the whole list was read).
func (s *Service) RunSync(ctx context.Context, appID, connID, key string) (*SyncConfig, error) {
	c, err := s.Connection(ctx, appID, connID)
	if err != nil {
		return nil, err
	}
	m := s.Catalog.Get(c.Kind)
	if m == nil {
		return nil, invalid("the integration %q is no longer installed", c.Kind)
	}
	def := m.Sync(key)
	if def == nil {
		return nil, invalid("%s has no sync %q", m.Name, key)
	}
	sc := c.Sync(key)
	if sc == nil {
		sc = &SyncConfig{Key: key, Enabled: true, Params: map[string]string{}}
		c.Syncs = append(c.Syncs, sc)
		if err := s.save(ctx, c, false); err != nil {
			return nil, err
		}
	}
	run := *sc
	run.Created, run.Updated, run.Deleted, run.LastCount = 0, 0, 0, 0
	run.LastRun = s.now().UTC().Format(time.RFC3339)
	err = s.syncAll(ctx, c, m, def, &run)
	run.LastOK, run.LastError = err == nil, ""
	if err != nil {
		run.LastError = err.Error()
	}
	if serr := s.saveSync(context.WithoutCancel(ctx), appID, c.ID, &run); serr != nil && err == nil {
		err = serr
	}
	return &run, err
}

func (s *Service) syncAll(ctx context.Context, c *Connection, m *Manifest, def *Sync, sc *SyncConfig) error {
	if missing := c.Missing(m, Live); len(missing) > 0 {
		return invalid("fill in %s first", strings.Join(missing, ", "))
	}
	cx := &callCtx{s: s, conn: c, m: m, vals: c.Values(Live), env: Live}
	given := map[string]any{}
	for k, v := range sc.Params {
		given[k] = v
	}
	var items []any
	cursor, complete := "", false
	for page := 1; page <= maxSyncPages && len(items) < maxSyncItems; page++ {
		req := def.Request
		req.Query = map[string]string{}
		for k, v := range def.Request.Query {
			req.Query[k] = v
		}
		switch def.Page.Type {
		case "cursor":
			if cursor != "" {
				req.Query[def.Page.Param] = cursor
			}
		case "page":
			req.Query[def.Page.Param] = fmt.Sprint(page)
		}
		b, err := cx.build(req, def.Inputs, given)
		if err != nil {
			return err
		}
		body, err := cx.do(ctx, b)
		if err != nil {
			return err
		}
		list, _ := Dig(body, def.Items).([]any)
		if def.Items == "" {
			list, _ = body.([]any)
		}
		items = append(items, list...)
		more := len(list) > 0
		switch def.Page.Type {
		case "none":
			more = false
		case "cursor":
			if def.Page.LastID != "" && len(list) > 0 {
				cursor = interp.ToText(Dig(list[len(list)-1], def.Page.LastID))
			} else {
				cursor = interp.ToText(Dig(body, def.Page.Next))
			}
			if def.Page.More != "" {
				more, _ = Dig(body, def.Page.More).(bool)
			}
			more = more && cursor != ""
		}
		if !more {
			complete = true
			break
		}
	}
	sc.LastCount = len(items)
	detect(def, sc, items)
	t, err := s.table(ctx, c, def, sc)
	if err != nil {
		return err
	}
	fields := syncFields(def, sc)
	key := keyField(fields)
	rows, err := s.existing(ctx, c, t, key.Name)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		what, err := s.upsert(ctx, c, t, fields, it, rows)
		if err != nil {
			return fmt.Errorf("item %s: %w", interp.ToText(Dig(it, key.Path)), err)
		}
		seen[interp.ToText(Dig(it, key.Path))] = true
		switch what {
		case "created":
			sc.Created++
		case "updated":
			sc.Updated++
		}
	}
	if complete {
		var gone []int64
		for k, r := range rows {
			if !seen[k] {
				gone = append(gone, rowID(r))
			}
		}
		if len(gone) > 0 {
			n, err := s.Tables.DeleteRows(ctx, syncActor(c), c.AppID, t.ID, gone)
			if err != nil {
				return err
			}
			sc.Deleted = n
		}
	} else if len(items) >= maxSyncItems {
		return fmt.Errorf("stopped after %d items; the rest were not read", len(items))
	}
	return nil
}

// applyEvent keeps synced tables up to date from a webhook event; it
// returns how many rows it changed.
func (s *Service) applyEvent(ctx context.Context, c *Connection, m *Manifest, ev *Event) (int, error) {
	n := 0
	var errs []error
	for _, def := range m.Syncs {
		sc := c.Sync(def.Key)
		if sc == nil || !sc.Enabled || !sc.Live || sc.Table == "" {
			continue
		}
		del := MatchEvent(def.Deleted, ev.Type) && len(def.Deleted) > 0
		if !del && !(len(def.Events) > 0 && MatchEvent(def.Events, ev.Type)) {
			continue
		}
		t, err := s.Tables.Get(ctx, c.AppID, sc.Table)
		if err != nil {
			continue
		}
		fields := syncFields(def, sc)
		key := keyField(fields)
		item := ev.Object
		if def.EventItem != "" {
			item = Dig(ev.Body, def.EventItem)
		}
		k := interp.ToText(Dig(item, key.Path))
		if k == "" {
			continue
		}
		q := tables.Query{Filter: "[" + strings.ReplaceAll(key.Name, "]", "") + "] = \"" + strings.ReplaceAll(k, `"`, `""`) + "\"", Limit: 1}
		page, err := s.Tables.ListRows(ctx, syncActor(c), c.AppID, t.ID, q)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		rows := map[string]map[string]any{}
		if len(page.Rows) > 0 {
			rows[k] = page.Rows[0]
		}
		if del {
			if old := rows[k]; old != nil {
				if _, err := s.Tables.DeleteRows(ctx, syncActor(c), c.AppID, t.ID, []int64{rowID(old)}); err != nil {
					errs = append(errs, err)
				} else {
					n++
				}
			}
			continue
		}
		what, err := s.upsert(ctx, c, t, fields, item, rows)
		if err != nil {
			errs = append(errs, err)
		} else if what == "created" || what == "updated" {
			n++
		}
	}
	return n, errors.Join(errs...)
}

// Start runs scheduled syncs until Stop.
func (s *Service) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.RunDue(ctx)
			}
		}
	}()
}

// Stop ends the schedule and waits for a sync in progress.
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// RunDue runs the syncs whose time has come.
func (s *Service) RunDue(ctx context.Context) {
	all, err := s.list(ctx, `1 = 1`)
	if err != nil {
		s.Log.Error("listing connections for syncs failed", "err", err)
		return
	}
	now := s.now().UTC()
	for _, c := range all {
		for _, sc := range c.Syncs {
			if !sc.Enabled || sc.Every <= 0 {
				continue
			}
			if last, err := time.Parse(time.RFC3339, sc.LastRun); err == nil && now.Sub(last) < time.Duration(sc.Every)*time.Minute-30*time.Second {
				continue
			}
			if s.Node != nil {
				ok, err := s.Node.Claim(ctx, "isync:"+c.ID+":"+sc.Key+":"+sc.LastRun)
				if err != nil || !ok {
					continue
				}
			}
			if _, err := s.RunSync(ctx, c.AppID, c.ID, sc.Key); err != nil && s.Log != nil {
				s.Log.Warn("a scheduled sync failed", "connection", c.Name, "sync", sc.Key, "err", err)
			}
		}
	}
}
