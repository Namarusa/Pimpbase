// Command bubibase runs the Bubibase platform: the builder, every app and
// their APIs, from one binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bubibase/bubibase/internal/api"
	"github.com/bubibase/bubibase/internal/backup"
	"github.com/bubibase/bubibase/internal/cluster"
	"github.com/bubibase/bubibase/internal/files"
	"github.com/bubibase/bubibase/internal/flows"
	"github.com/bubibase/bubibase/internal/sso/ssotest"
	"github.com/bubibase/bubibase/internal/store"
	"github.com/bubibase/bubibase/web"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage()
		return
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bubibase:", err)
			os.Exit(1)
		}
	case "backup":
		if err := backupCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bubibase:", err)
			os.Exit(1)
		}
	case "restore":
		if err := restoreCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bubibase:", err)
			os.Exit(1)
		}
	case "dev-idp":
		if err := devIdP(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bubibase:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("bubibase", version)
	default:
		fmt.Fprintf(os.Stderr, "bubibase: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Bubibase hosts low-code apps from one binary.

Usage:
  bubibase serve [flags]          start the server
  bubibase backup [flags]         copy a SQLite data folder into one zip, even while serving
  bubibase restore [flags] <zip>  put a backup into a data folder
  bubibase dev-idp [flags]        run a test single sign-on provider, never for real accounts
  bubibase version         print the version

Run "bubibase serve -h" for the server flags.
`)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", env("BUBIBASE_ADDR", ":8080"), "address to listen on (BUBIBASE_ADDR)")
	dataDir := fs.String("data", env("BUBIBASE_DATA", "./data"), "folder for SQLite databases and files (BUBIBASE_DATA)")
	pg := fs.String("postgres", env("BUBIBASE_POSTGRES", ""), "Postgres connection string; SQLite is used when empty (BUBIBASE_POSTGRES)")
	smtp := fs.String("smtp", env("BUBIBASE_SMTP", ""), "mail server for flow emails: smtp://user:pass@host:587, smtps://…, or log (BUBIBASE_SMTP)")
	mailFrom := fs.String("mail-from", env("BUBIBASE_MAIL_FROM", ""), "sender address of flow emails (BUBIBASE_MAIL_FROM)")
	secure := fs.Bool("secure-cookies", env("BUBIBASE_SECURE_COOKIES", "") == "true", "mark session cookies HTTPS-only (BUBIBASE_SECURE_COOKIES)")
	publicURL := fs.String("public-url", env("BUBIBASE_PUBLIC_URL", ""), "address people reach Bubibase at, e.g. https://apps.example.com; single sign-on sends people back there (BUBIBASE_PUBLIC_URL)")
	filesSpec := fs.String("files", env("BUBIBASE_FILES", "db"), "where uploaded files go: db (inside the app's database), dir:/path (a folder, which servers may share), or s3://bucket/prefix?region=…&endpoint=… with AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY (BUBIBASE_FILES)")
	rateLimit := fs.Float64("rate-limit", envFloat("BUBIBASE_RATE_LIMIT", 100), "API requests a second one client may make to this server; 0 turns the limit off (BUBIBASE_RATE_LIMIT)")
	rateBurst := fs.Int("rate-burst", int(envFloat("BUBIBASE_RATE_BURST", 200)), "requests a client may make at once before the rate limit applies (BUBIBASE_RATE_BURST)")
	behindProxy := fs.Bool("behind-proxy", env("BUBIBASE_BEHIND_PROXY", "") == "true", "trust the X-Forwarded-For address a load balancer or proxy adds (BUBIBASE_BEHIND_PROXY)")
	metricsToken := fs.String("metrics-token", env("BUBIBASE_METRICS_TOKEN", ""), "require this Bearer token to read /metrics; empty leaves it open (BUBIBASE_METRICS_TOKEN)")
	drain := fs.Duration("drain", envDuration("BUBIBASE_DRAIN", 0), "on shutdown, fail /readyz this long before closing, so a load balancer stops sending requests (BUBIBASE_DRAIN)")
	backupDir := fs.String("backup-dir", env("BUBIBASE_BACKUP_DIR", ""), "with SQLite, write a backup zip into this folder every --backup-every (BUBIBASE_BACKUP_DIR)")
	backupEvery := fs.Duration("backup-every", envDuration("BUBIBASE_BACKUP_EVERY", 24*time.Hour), "how often to back up into --backup-dir (BUBIBASE_BACKUP_EVERY)")
	backupKeep := fs.Int("backup-keep", int(envFloat("BUBIBASE_BACKUP_KEEP", 7)), "backups to keep in --backup-dir (BUBIBASE_BACKUP_KEEP)")
	anthropicKey := fs.String("anthropic-key", env("BUBIBASE_ANTHROPIC_KEY", os.Getenv("ANTHROPIC_API_KEY")), "Anthropic API key for the builder's AI assistant; kept on the server (BUBIBASE_ANTHROPIC_KEY or ANTHROPIC_API_KEY). Admins can also save one in the builder")
	fs.Parse(args)

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dataDir, *pg)
	if err != nil {
		return err
	}
	defer st.Close()

	blobs, err := files.OpenBlobs(*filesSpec)
	if err != nil {
		return err
	}
	srv := api.New(st, web.Builder(), log)
	srv.Secure = *secure
	srv.PublicURL = *publicURL
	srv.AI.EnvKey = *anthropicKey
	srv.Files.Blobs = blobs
	srv.RateLimit, srv.RateBurst = *rateLimit, *rateBurst
	srv.BehindProxy = *behindProxy
	srv.MetricsToken = *metricsToken
	srv.Version = version
	srv.Node = cluster.New(st, version, log)
	srv.Engine.Node = srv.Node
	if err := srv.Node.Start(ctx); err != nil {
		return fmt.Errorf("register server: %w", err)
	}
	defer srv.Node.Stop(context.Background())
	if *backupDir != "" {
		if *pg != "" {
			return errors.New("--backup-dir backs up SQLite; back up Postgres with pg_dump or your provider's snapshots")
		}
		go backupLoop(ctx, log, *dataDir, *backupDir, *backupEvery, *backupKeep)
	}
	mailer, err := flows.NewMailer(*smtp, *mailFrom)
	if err != nil {
		return err
	}
	srv.Engine.Mailer = mailer
	if err := srv.Engine.Start(ctx); err != nil {
		return fmt.Errorf("start flows: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Engine.Stop(stopCtx)
	}()
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	where := "SQLite in " + *dataDir
	if *pg != "" {
		where = "Postgres"
	}
	log.Info("bubibase is running", "version", version, "addr", *addr, "storage", where, "files", srv.Files.StorageName(), "server", srv.Node.ID)
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		srv.Drain()
		if *drain > 0 {
			time.Sleep(*drain)
		}
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shut)
	}
	return nil
}

func envFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func backupLoop(ctx context.Context, log *slog.Logger, dataDir, dir string, every time.Duration, keep int) {
	if every < time.Minute {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		start := time.Now()
		name, err := backup.ToFile(ctx, dataDir, dir, keep)
		if err != nil {
			log.Error("backup failed", "err", err)
			continue
		}
		log.Info("backup written", "file", name, "ms", time.Since(start).Milliseconds())
	}
}

func backupCmd(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	dataDir := fs.String("data", env("BUBIBASE_DATA", "./data"), "the SQLite data folder to back up (BUBIBASE_DATA)")
	out := fs.String("out", "", "zip file to write; default bubibase-<time>.zip in the current folder")
	fs.Parse(args)
	if os.Getenv("BUBIBASE_POSTGRES") != "" {
		return errors.New("this server keeps its data in Postgres; back it up with pg_dump or your provider's snapshots")
	}
	name := *out
	if name == "" {
		name = "bubibase-" + time.Now().UTC().Format("20060102-150405") + ".zip"
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	n, err := backup.Write(context.Background(), *dataDir, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	fmt.Fprintf(os.Stderr, "backed up %d databases into %s\n", n, name)
	return nil
}

func restoreCmd(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	dataDir := fs.String("data", env("BUBIBASE_DATA", "./data"), "the data folder to restore into; stop the server first (BUBIBASE_DATA)")
	force := fs.Bool("force", false, "replace the data already in the folder")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("name the backup zip: bubibase restore --data ./data backup.zip")
	}
	n, err := backup.Restore(fs.Arg(0), *dataDir, *force)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "restored %d databases into %s\n", n, *dataDir)
	return nil
}

// devIdP runs a test OpenID Connect provider with made-up people, so
// single sign-on and role mappings can be tried without a real provider.
func devIdP(args []string) error {
	fs := flag.NewFlagSet("dev-idp", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:9000", "address to listen on")
	issuer := fs.String("issuer", "", "issuer URL; defaults to http://<addr>")
	fs.Parse(args)
	if *issuer == "" {
		*issuer = "http://" + *addr
	}
	p := ssotest.New(*issuer, "bubibase-dev", "dev-secret",
		ssotest.User{Subject: "kim", Email: "kim.lee@acme.test", EmailVerified: true, Name: "Kim Lee", Groups: []string{"sales", "north"}},
		ssotest.User{Subject: "sam", Email: "sam.ortiz@acme.test", EmailVerified: true, Name: "Sam Ortiz", Groups: []string{"sales-leads", "north", "south"}},
		ssotest.User{Subject: "ida", Email: "ida.novak@acme.test", EmailVerified: true, Name: "Ida Novak", Groups: []string{"it"}},
		ssotest.User{Subject: "eve", Email: "eve@elsewhere.test", EmailVerified: true, Name: "Eve Outsider", Groups: []string{}},
	)
	p.Title = "Acme sign-in"
	fmt.Fprintf(os.Stderr, "Test identity provider at %s\n  issuer:        %s\n  client id:     bubibase-dev\n  client secret: dev-secret\nNever use it for real accounts.\n", *addr, *issuer)
	srv := &http.Server{Addr: *addr, Handler: p.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
