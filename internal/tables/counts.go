package tables

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bubibase/bubibase/internal/store"
)

// Counting the matches of a filter scans them all, which takes a while
// on big tables and happens on every page of a table component. Counts
// that took long are kept for a few seconds; any row change made through
// this server forgets the table's counts at once. Other servers sharing
// the database see a count up to countTTL old.
//
// One request works out a given answer at a time: while it does, others
// asking the same get the previous answer when there is one (so under
// steady writes a slow chart may lag one recount behind), or wait for it.
const (
	countTTL  = 10 * time.Second
	countSlow = 40 * time.Millisecond
)

type countCache struct {
	mu   sync.Mutex
	gen  map[string]uint64 // per table, bumped by row changes
	seen map[string]cachedValue
	busy map[string]*flight
}

type flight struct {
	done chan struct{}
	v    any
	err  error
}

type cachedValue struct {
	v   any
	gen uint64
	at  time.Time
}

func tableKey(appID, tableID string) string { return appID + "/" + tableID }

// touch forgets the counts of a table whose rows changed.
func (s *Service) touch(appID, tableID string) {
	s.counts.mu.Lock()
	defer s.counts.mu.Unlock()
	if s.counts.gen == nil {
		s.counts.gen = map[string]uint64{}
	}
	s.counts.gen[tableKey(appID, tableID)]++
}

// remember returns what fn computed for key less than countTTL ago, unless
// rows of the table changed since; answers fn took long to give are kept.
func remember[T any](s *Service, c *compiled, key string, fn func() (T, error)) (T, error) {
	tk := tableKey(c.AppID, c.ID)
	key = tk + "|" + key
	now := time.Now()
	s.counts.mu.Lock()
	gen := s.counts.gen[tk]
	hit, ok := s.counts.seen[key]
	ok = ok && now.Sub(hit.at) < countTTL
	if ok && hit.gen == gen {
		s.counts.mu.Unlock()
		return hit.v.(T), nil
	}
	if f := s.counts.busy[key]; f != nil {
		s.counts.mu.Unlock()
		if ok {
			return hit.v.(T), nil
		}
		<-f.done
		if f.err == nil {
			return f.v.(T), nil
		}
		s.counts.mu.Lock()
	}
	f := &flight{done: make(chan struct{})}
	if s.counts.busy == nil {
		s.counts.busy = map[string]*flight{}
	}
	s.counts.busy[key] = f
	s.counts.mu.Unlock()

	v, err := fn()
	s.counts.mu.Lock()
	if s.counts.busy[key] == f {
		delete(s.counts.busy, key)
	}
	if err == nil && time.Since(now) >= countSlow {
		if s.counts.seen == nil || len(s.counts.seen) > 1000 {
			s.counts.seen = map[string]cachedValue{}
		}
		s.counts.seen[key] = cachedValue{v: v, gen: gen, at: now}
	}
	s.counts.mu.Unlock()
	f.v, f.err = v, err
	close(f.done)
	return v, err
}

// count counts the rows of c matching where.
func (s *Service) count(ctx context.Context, db *store.DB, c *compiled, where string, args []any) (int, error) {
	return remember(s, c, fmt.Sprintf("count|%s|%v", where, args), func() (int, error) {
		q := "SELECT COUNT(*) FROM " + db.Table(c.sqlName())
		if where != "" {
			q += " WHERE " + where
		}
		var n int
		err := db.QueryRow(ctx, q, args...).Scan(&n)
		return n, err
	})
}
