// Package cluster lets several Pimpbase servers share one database: each
// server registers itself and says it is alive every few seconds, and jobs
// that must run once, such as a scheduled flow, are claimed in the
// database by whichever server gets there first.
//
// A lone server uses the same code; the tables then simply hold one row.
package cluster

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/pimpbase/pimpbase/internal/ids"
	"github.com/pimpbase/pimpbase/internal/store"
)

const (
	// Heartbeat is how often a server says it is alive.
	Heartbeat = 10 * time.Second
	// DeadAfter is how long a silent server counts as alive. Its unfinished
	// flow runs are marked failed after that.
	DeadAfter = 45 * time.Second
	// claimsKept is how long claims stay to stop a job running twice.
	claimsKept = 48 * time.Hour
)

// stampLayout has a fixed width, so stamps compare as text in SQL.
const stampLayout = "2006-01-02T15:04:05.000Z"

// Stamp formats a time the way instance and claim rows store it.
func Stamp(t time.Time) string { return t.UTC().Format(stampLayout) }

// Instance is a server sharing the database.
type Instance struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
	SeenAt    time.Time `json:"seenAt"`
	// Self marks the server answering.
	Self bool `json:"self"`
}

// Node is this server.
type Node struct {
	ID      string
	Host    string
	Version string
	Log     *slog.Logger

	db      *store.DB
	dialect store.Dialect
	now     func() time.Time
	started time.Time

	stop chan struct{}
	wg   sync.WaitGroup
}

func New(st *store.Store, version string, log *slog.Logger) *Node {
	host, _ := os.Hostname()
	return &Node{ID: ids.New("ins"), Host: host, Version: version, Log: log, db: st.Meta, dialect: st.Dialect(), now: time.Now, stop: make(chan struct{})}
}

// Start registers the server and keeps saying it is alive until Stop.
func (n *Node) Start(ctx context.Context) error {
	n.started = n.now()
	at := Stamp(n.started)
	if _, err := n.db.Exec(ctx, `INSERT INTO instances (id, host, version, started_at, seen_at) VALUES (?, ?, ?, ?, ?)`, n.ID, n.Host, n.Version, at, at); err != nil {
		return err
	}
	if n.dialect == store.SQLite {
		if others, err := n.Alive(ctx); err == nil && len(others) > 1 {
			n.Log.Warn("another server is using the same SQLite data folder; run several servers on Postgres only", "servers", len(others))
		}
	}
	n.wg.Add(1)
	go n.loop()
	return nil
}

// Stop removes the server from the list.
func (n *Node) Stop(ctx context.Context) {
	select {
	case <-n.stop:
		return
	default:
	}
	close(n.stop)
	n.wg.Wait()
	n.db.Exec(ctx, `DELETE FROM instances WHERE id = ?`, n.ID)
}

func (n *Node) loop() {
	defer n.wg.Done()
	t := time.NewTicker(Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), Heartbeat)
		if err := n.beat(ctx); err != nil {
			n.Log.Error("heartbeat failed", "err", err)
		}
		cancel()
	}
}

func (n *Node) beat(ctx context.Context) error {
	now := n.now()
	res, err := n.db.Exec(ctx, `UPDATE instances SET seen_at = ? WHERE id = ?`, Stamp(now), n.ID)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		// Another server tidied us away after a long pause; come back.
		if _, err := n.db.Exec(ctx, `INSERT INTO instances (id, host, version, started_at, seen_at) VALUES (?, ?, ?, ?, ?)`, n.ID, n.Host, n.Version, Stamp(n.started), Stamp(now)); err != nil {
			return err
		}
	}
	// Tidy up after servers that died long ago, and old claims.
	n.db.Exec(ctx, `DELETE FROM instances WHERE seen_at < ?`, Stamp(now.Add(-24*time.Hour)))
	n.db.Exec(ctx, `DELETE FROM job_claims WHERE claimed_at < ?`, Stamp(now.Add(-claimsKept)))
	return nil
}

// Claim reports whether this server won the job named key. Every server
// asking for the same key gets one answer true, ever (or for claimsKept).
func (n *Node) Claim(ctx context.Context, key string) (bool, error) {
	res, err := n.db.Exec(ctx, `INSERT INTO job_claims (key, instance_id, claimed_at) VALUES (?, ?, ?) ON CONFLICT (key) DO NOTHING`, key, n.ID, Stamp(n.now()))
	if err != nil {
		return false, err
	}
	k, err := res.RowsAffected()
	return k == 1, err
}

// AliveSince is the oldest heartbeat of a server still counted alive.
func (n *Node) AliveSince() string { return Stamp(n.now().Add(-DeadAfter)) }

// Alive lists the servers that said they were alive recently.
func (n *Node) Alive(ctx context.Context) ([]Instance, error) {
	rows, err := n.db.Query(ctx, `SELECT id, host, version, started_at, seen_at FROM instances WHERE seen_at >= ? ORDER BY started_at, id`, n.AliveSince())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Instance{}
	for rows.Next() {
		var in Instance
		var started, seen string
		if err := rows.Scan(&in.ID, &in.Host, &in.Version, &started, &seen); err != nil {
			return nil, err
		}
		in.StartedAt, _ = time.Parse(stampLayout, started)
		in.SeenAt, _ = time.Parse(stampLayout, seen)
		in.Self = in.ID == n.ID
		out = append(out, in)
	}
	return out, rows.Err()
}
