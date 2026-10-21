// Command pimpbase-load measures a running Pimpbase server, or a load
// balancer in front of several, with a mix of the requests apps make:
// row pages, filtered pages, single rows, chart aggregates, inserts and
// updates. It creates its own app ("Load test") with a seeded table, then
// prints a table of latencies and throughput.
//
//	pimpbase-load -url http://localhost:8080 -email ada@example.com -password '…' -rows 100000 -c 32 -d 30s
//
// Run the servers with --rate-limit 0, or the limit is what gets measured.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var stages = []string{"Lead", "Qualified", "Proposal", "Won", "Lost"}

type op struct {
	name   string
	weight int
	run    func(c *client, r *rand.Rand) error
}

func main() {
	base := flag.String("url", "http://localhost:8080", "server or load balancer address")
	email := flag.String("email", "load@example.com", "builder account; created by setup on a new server")
	password := flag.String("password", "load test password", "its password")
	rows := flag.Int("rows", 100000, "rows to seed; a table that already holds them is reused, and reads and updates pick among the first this many")
	conc := flag.Int("c", 32, "concurrent clients")
	dur := flag.Duration("d", 30*time.Second, "how long to run")
	warm := flag.Duration("warmup", 3*time.Second, "unmeasured time first")
	slug := flag.String("app", "load-test", "URL name of the app to create or reuse")
	flag.Parse()

	c, err := newClient(*base)
	check(err)
	check(c.signIn(*email, *password))
	tbl, err := c.prepare(*slug, *rows)
	check(err)
	maxID := int64(*rows)

	t := "/api/apps/" + *slug + "/tables/" + tbl
	ops := []op{
		{"page", 35, func(c *client, r *rand.Rand) error {
			return c.get(fmt.Sprintf("%s/rows?limit=25&offset=%d&sort=_id&desc=1", t, 25*r.IntN(20)))
		}},
		{"filter", 20, func(c *client, r *rand.Rand) error {
			return c.get(fmt.Sprintf("%s/rows?limit=25&filter=%s", t, urlq(fmt.Sprintf(`[Owner] = "user%d@example.com"`, r.IntN(200)))))
		}},
		{"row", 15, func(c *client, r *rand.Rand) error {
			return c.get(fmt.Sprintf("%s/rows/%d", t, 1+r.Int64N(maxID)))
		}},
		{"chart", 10, func(c *client, r *rand.Rand) error {
			return c.get(t + "/aggregate?groupBy=Stage&values=sum:Amount&values=count")
		}},
		{"insert", 12, func(c *client, r *rand.Rand) error {
			return c.send("POST", t+"/rows", map[string]any{"Title": "New deal", "Owner": fmt.Sprintf("user%d@example.com", r.IntN(200)), "Stage": stages[r.IntN(5)], "Amount": r.IntN(10000)})
		}},
		{"update", 8, func(c *client, r *rand.Rand) error {
			return c.send("PATCH", fmt.Sprintf("%s/rows/%d", t, 1+r.Int64N(maxID)), map[string]any{"Amount": r.IntN(10000)})
		}},
	}
	total := 0
	for _, o := range ops {
		total += o.weight
	}

	type sample struct {
		op  int
		d   time.Duration
		err bool
	}
	var (
		mu       sync.Mutex
		samples  []sample
		measured atomic.Bool
		stop     atomic.Bool
		wg       sync.WaitGroup
		firstErr atomic.Value
	)
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(seed, 7))
			var local []sample
			for !stop.Load() {
				n, i := r.IntN(total), 0
				for n >= ops[i].weight {
					n -= ops[i].weight
					i++
				}
				start := time.Now()
				err := ops[i].run(c, r)
				if err != nil && firstErr.Load() == nil {
					firstErr.Store(err.Error())
				}
				if measured.Load() {
					local = append(local, sample{i, time.Since(start), err != nil})
				}
			}
			mu.Lock()
			samples = append(samples, local...)
			mu.Unlock()
		}(uint64(w))
	}
	time.Sleep(*warm)
	measured.Store(true)
	began := time.Now()
	time.Sleep(*dur)
	stop.Store(true)
	elapsed := time.Since(began)
	wg.Wait()

	fmt.Printf("%s, %d clients, %s, %d rows asked for\n\n", *base, *conc, dur.String(), *rows)
	fmt.Println("| request | count | req/s | p50 ms | p95 ms | p99 ms | errors |")
	fmt.Println("|---|---:|---:|---:|---:|---:|---:|")
	row := func(name string, ds []time.Duration, errs int) {
		slices.Sort(ds)
		fmt.Printf("| %s | %d | %.0f | %.1f | %.1f | %.1f | %d |\n", name, len(ds), float64(len(ds))/elapsed.Seconds(), ms(pct(ds, 50)), ms(pct(ds, 95)), ms(pct(ds, 99)), errs)
	}
	var all []time.Duration
	allErrs := 0
	for i, o := range ops {
		var ds []time.Duration
		errs := 0
		for _, s := range samples {
			if s.op == i {
				ds = append(ds, s.d)
				if s.err {
					errs++
				}
			}
		}
		all = append(all, ds...)
		allErrs += errs
		row(o.name, ds, errs)
	}
	row("**all**", all, allErrs)
	if e := firstErr.Load(); e != nil {
		fmt.Println("\nfirst error:", e)
	}
}

func pct(ds []time.Duration, p int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	return ds[min(len(ds)-1, len(ds)*p/100)]
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func urlq(s string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", `"`, "%22", "[", "%5B", "]", "%5D", "=", "%3D", "@", "%40", "&", "%26", "+", "%2B").Replace(s)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pimpbase-load:", err)
		os.Exit(1)
	}
}

type client struct {
	base string
	http *http.Client
}

func newClient(base string) (*client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns, tr.MaxIdleConnsPerHost = 1024, 1024
	return &client{strings.TrimRight(base, "/"), &http.Client{Jar: jar, Transport: tr, Timeout: 60 * time.Second}}, nil
}

func (c *client) do(method, path, ct string, body []byte, out any) (int, error) {
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Origin", c.base)
	res, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, err
	}
	if res.StatusCode >= 300 {
		return res.StatusCode, fmt.Errorf("%s %s: %s: %.200s", method, path, res.Status, data)
	}
	if out != nil {
		return res.StatusCode, json.Unmarshal(data, out)
	}
	return res.StatusCode, nil
}

func (c *client) get(path string) error {
	_, err := c.do("GET", path, "", nil, nil)
	return err
}

func (c *client) send(method, path string, v any) error {
	b, _ := json.Marshal(v)
	_, err := c.do(method, path, "application/json", b, nil)
	return err
}

func (c *client) signIn(email, password string) error {
	var setup struct {
		Needed bool `json:"needsSetup"`
	}
	if _, err := c.do("GET", "/api/setup", "", nil, &setup); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"email": email, "name": "Load test", "password": password})
	path := "/api/auth/login"
	if setup.Needed {
		path = "/api/setup"
	}
	_, err := c.do("POST", path, "application/json", body, nil)
	return err
}

// prepare finds or makes the app and its Deals table, seeded with rows.
func (c *client) prepare(slug string, rows int) (string, error) {
	app := "/api/apps/" + slug
	if code, err := c.do("GET", app, "", nil, nil); code == 404 {
		if err := c.send("POST", "/api/apps", map[string]any{"name": "Load test", "slug": slug}); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	var list []struct{ ID, Name string }
	if _, err := c.do("GET", app+"/tables", "", nil, &list); err != nil {
		return "", err
	}
	id := ""
	for _, t := range list {
		if t.Name == "Deals" {
			id = t.ID
		}
	}
	if id == "" {
		var t struct{ ID string }
		b, _ := json.Marshal(map[string]any{"name": "Deals", "fields": []any{
			map[string]any{"name": "Title", "type": "text"},
			map[string]any{"name": "Owner", "type": "text", "indexed": true},
			map[string]any{"name": "Stage", "type": "single_select", "options": stages, "indexed": true},
			map[string]any{"name": "Amount", "type": "number"},
		}})
		if _, err := c.do("POST", app+"/tables", "application/json", b, &t); err != nil {
			return "", err
		}
		id = t.ID
	}
	var page struct {
		Total int `json:"total"`
	}
	if _, err := c.do("GET", app+"/tables/"+id+"/rows?limit=1", "", nil, &page); err != nil {
		return "", err
	}
	r := rand.New(rand.NewPCG(1, 2))
	for have := page.Total; have < rows; {
		n := min(20000, rows-have)
		var buf bytes.Buffer
		buf.WriteString("Title,Owner,Stage,Amount\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&buf, "Deal %d,user%d@example.com,%s,%d\n", have+i+1, r.IntN(200), stages[r.IntN(5)], r.IntN(10000))
		}
		if _, err := c.do("POST", app+"/tables/"+id+"/import", "text/csv", buf.Bytes(), nil); err != nil {
			return "", err
		}
		have += n
		fmt.Fprintf(os.Stderr, "seeded %d/%d rows\n", have, rows)
	}
	return id, nil
}
