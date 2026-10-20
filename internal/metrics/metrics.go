// Package metrics counts what the server does and writes it in the
// Prometheus text format, without outside libraries.
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry holds counters, gauges and histograms by name and labels.
type Registry struct {
	mu     sync.Mutex
	help   map[string]string
	kinds  map[string]string
	series map[string]map[string]*series // name → label string → series
	// Collect runs before each write, for values read on demand.
	collect []func(r *Registry)
}

type series struct {
	labels string
	value  atomic.Uint64 // float64 bits for gauges and counters
	// Histograms.
	buckets []float64
	counts  []atomic.Uint64
	sum     atomic.Uint64
	count   atomic.Uint64
}

func New() *Registry {
	return &Registry{help: map[string]string{}, kinds: map[string]string{}, series: map[string]map[string]*series{}}
}

// Default is the registry the server writes at /metrics.
var Default = New()

// DurationBuckets suit HTTP requests and database calls, in seconds.
var DurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Describe sets the help text and kind ("counter", "gauge", "histogram")
// of a metric.
func (r *Registry) Describe(name, kind, help string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.help[name], r.kinds[name] = help, kind
}

// OnCollect adds a function that sets gauges just before a write.
func (r *Registry) OnCollect(fn func(r *Registry)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collect = append(r.collect, fn)
}

func labelString(kv []string) string {
	if len(kv) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(kv[i])
		b.WriteString(`="`)
		b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(kv[i+1]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func (r *Registry) get(name string, kv []string, buckets []float64) *series {
	ls := labelString(kv)
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.series[name]
	if m == nil {
		m = map[string]*series{}
		r.series[name] = m
	}
	s := m[ls]
	if s == nil {
		s = &series{labels: ls, buckets: buckets}
		if buckets != nil {
			s.counts = make([]atomic.Uint64, len(buckets))
		}
		m[ls] = s
	}
	return s
}

func addFloat(u *atomic.Uint64, d float64) {
	for {
		old := u.Load()
		if u.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+d)) {
			return
		}
	}
}

// Add adds d to a counter or gauge. Labels come as name, value pairs.
func (r *Registry) Add(name string, d float64, labels ...string) {
	addFloat(&r.get(name, labels, nil).value, d)
}

// Inc adds one.
func (r *Registry) Inc(name string, labels ...string) { r.Add(name, 1, labels...) }

// Set sets a gauge.
func (r *Registry) Set(name string, v float64, labels ...string) {
	r.get(name, labels, nil).value.Store(math.Float64bits(v))
}

// Observe records one value in a histogram.
func (r *Registry) Observe(name string, v float64, buckets []float64, labels ...string) {
	s := r.get(name, labels, buckets)
	for i, b := range s.buckets {
		if v <= b {
			s.counts[i].Add(1)
		}
	}
	addFloat(&s.sum, v)
	s.count.Add(1)
}

// Since observes the seconds since start.
func (r *Registry) Since(name string, start time.Time, labels ...string) {
	r.Observe(name, time.Since(start).Seconds(), DurationBuckets, labels...)
}

// Value reads a counter or gauge, for tests.
func (r *Registry) Value(name string, labels ...string) float64 {
	return math.Float64frombits(r.get(name, labels, nil).value.Load())
}

func num(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// Write writes every metric in the Prometheus text format.
func (r *Registry) Write(w io.Writer) error {
	r.mu.Lock()
	collect := append([]func(*Registry){}, r.collect...)
	r.mu.Unlock()
	for _, fn := range collect {
		fn(r)
	}
	r.mu.Lock()
	names := make([]string, 0, len(r.series))
	for n := range r.series {
		names = append(names, n)
	}
	sort.Strings(names)
	type snap struct {
		name, help, kind string
		list             []*series
	}
	var snaps []snap
	for _, n := range names {
		var list []*series
		for _, s := range r.series[n] {
			list = append(list, s)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].labels < list[j].labels })
		snaps = append(snaps, snap{n, r.help[n], r.kinds[n], list})
	}
	r.mu.Unlock()
	var b strings.Builder
	for _, sn := range snaps {
		if sn.help != "" {
			fmt.Fprintf(&b, "# HELP %s %s\n", sn.name, sn.help)
		}
		kind := sn.kind
		if kind == "" {
			kind = "untyped"
		}
		fmt.Fprintf(&b, "# TYPE %s %s\n", sn.name, kind)
		for _, s := range sn.list {
			if s.buckets == nil {
				fmt.Fprintf(&b, "%s%s %s\n", sn.name, s.labels, num(math.Float64frombits(s.value.Load())))
				continue
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(s.labels, "{"), "}")
			sep := ""
			if inner != "" {
				sep = ","
			}
			for i, le := range s.buckets {
				fmt.Fprintf(&b, "%s_bucket{%s%sle=\"%s\"} %d\n", sn.name, inner, sep, num(le), s.counts[i].Load())
			}
			fmt.Fprintf(&b, "%s_bucket{%s%sle=\"+Inf\"} %d\n", sn.name, inner, sep, s.count.Load())
			fmt.Fprintf(&b, "%s_sum%s %s\n", sn.name, s.labels, num(math.Float64frombits(s.sum.Load())))
			fmt.Fprintf(&b, "%s_count%s %d\n", sn.name, s.labels, s.count.Load())
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
