package api

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pimpbase/pimpbase/internal/cluster"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/internal/store/storetest"
)

// TestTwoServers runs two servers on one database: a scheduled flow runs
// once per minute between them, and runs left by a server that died are
// marked failed by the other.
func TestTwoServers(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		var servers []*Server
		for i := 0; i < 2; i++ {
			srv := New(s, nil, log)
			srv.Node = cluster.New(s, "test", log)
			srv.Engine.Node = srv.Node
			if err := srv.Node.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if err := srv.Engine.Start(ctx); err != nil {
				t.Fatal(err)
			}
			servers = append(servers, srv)
		}
		a, b := servers[0], servers[1]
		defer a.Engine.Stop(ctx)
		hs := httptest.NewServer(a.Handler())
		defer hs.Close()
		owner := newClient(t, hs.URL)
		owner.do("POST", "/api/setup", map[string]any{"email": "owner@example.com", "name": "Owner", "password": "correct horse battery"}, 200)
		app := owner.do("POST", "/api/apps", map[string]any{"name": "Ops"}, 201)
		base := "/api/apps/" + app["id"].(string)
		sched := owner.do("POST", base+"/flows", map[string]any{
			"name": "Every minute", "enabled": true,
			"trigger": map[string]any{"type": "schedule", "cron": "* * * * *", "timezone": "UTC"},
			"steps":   []any{map[string]any{"name": "Stamp", "type": "set_variable", "inputs": map[string]any{"name": "at", "value": "{{ trigger.time }}"}}},
		}, 201)
		flow := base + "/flows/" + sched["id"].(string)

		alive := must(a.Node.Alive(ctx))
		if len(alive) != 2 {
			t.Fatalf("alive = %v", alive)
		}
		// Both servers reach the same three minutes at once.
		for m := 0; m < 3; m++ {
			at := time.Date(2026, 10, 20, 9, m, 0, 0, time.UTC)
			var wg sync.WaitGroup
			for _, srv := range servers {
				wg.Add(1)
				go func() { defer wg.Done(); srv.Engine.RunDue(at) }()
			}
			wg.Wait()
		}
		runs := waitRuns(t, owner, flow, 3)
		if len(runs) != 3 {
			t.Fatalf("runs = %d, want 3", len(runs))
		}

		// b dies in the middle of a run; a marks it failed, but not its own.
		insert := func(id, instance string) {
			_, err := s.Meta.Exec(ctx, `INSERT INTO app_flow_runs (id, app_id, flow_id, status, test, trigger_kind, trigger_data, steps, output, error, started_at, finished_at, instance_id)
				VALUES (?, ?, ?, 'running', 0, 'schedule', '{}', '[]', 'null', '', ?, '', ?)`, id, app["id"], sched["id"], time.Now().UTC().Format(time.RFC3339Nano), instance)
			if err != nil {
				t.Fatal(err)
			}
		}
		insert("run_b", b.Node.ID)
		insert("run_a", a.Node.ID)
		b.Engine.Stop(ctx)
		b.Node.Stop(ctx)
		if err := a.Engine.Start(ctx); err != nil { // the check runs each minute and at start
			t.Fatal(err)
		}
		if got := owner.do("GET", base+"/runs/run_b", nil, 200); got["status"] != "failed" || got["error"] != "the server stopped during the run" {
			t.Errorf("dead server's run = %v %v", got["status"], got["error"])
		}
		if got := owner.do("GET", base+"/runs/run_a", nil, 200); got["status"] != "running" {
			t.Errorf("live server's run = %v", got["status"])
		}
		if alive := must(a.Node.Alive(ctx)); len(alive) != 1 || !alive[0].Self {
			t.Errorf("alive after stop = %v", alive)
		}
	})
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
