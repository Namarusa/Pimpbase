// Command pimpbase runs the Pimpbase platform: the builder, every app and
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
	"strings"
	"syscall"
	"time"

	"github.com/pimpbase/pimpbase/internal/ai"
	"github.com/pimpbase/pimpbase/internal/api"
	"github.com/pimpbase/pimpbase/internal/backup"
	"github.com/pimpbase/pimpbase/internal/cluster"
	"github.com/pimpbase/pimpbase/internal/files"
	"github.com/pimpbase/pimpbase/internal/flows"
	"github.com/pimpbase/pimpbase/internal/sso/ssotest"
	"github.com/pimpbase/pimpbase/internal/store"
	"github.com/pimpbase/pimpbase/web"
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
			fmt.Fprintln(os.Stderr, "pimpbase:", err)
			os.Exit(1)
		}
	case "backup":
		if err := backupCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "pimpbase:", err)
			os.Exit(1)
		}
	case "restore":
		if err := restoreCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "pimpbase:", err)
			os.Exit(1)
		}
	case "dev-idp":
		if err := devIdP(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "pimpbase:", err)
			os.Exit(1)
		}
	case "healthcheck":
		if err := healthcheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "pimpbase:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("pimpbase", version)
	default:
		fmt.Fprintf(os.Stderr, "pimpbase: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Pimpbase hosts low-code apps from one binary.

Usage:
  pimpbase serve [flags]          start the server
  pimpbase backup [flags]         copy a SQLite data folder into one zip, even while serving
  pimpbase restore [flags] <zip>  put a backup into a data folder
  pimpbase dev-idp [flags]        run a test single sign-on provider, never for real accounts
  pimpbase healthcheck [url]      exit 0 when the local server is ready, for containers
  pimpbase version                print the version

Run "pimpbase serve -h" for the server flags.
`)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// aiSetting is a flag giving one provider setting.
type aiSetting struct {
	provider, field string
	value           *string
}

type aiOptions struct {
	provider, model *string
	settings        []aiSetting
}

// aiFlags declares the AI assistant's flags. Each reads an environment
// variable, and keys also the provider's usual one.
func aiFlags(fs *flag.FlagSet) *aiOptions {
	o := &aiOptions{
		provider: fs.String("ai-provider", env("PIMPBASE_AI_PROVIDER", ""), "provider of the builder's AI assistant: anthropic, openai, gemini, azure, mistral, xai, deepseek, groq or openai_compatible; empty lets admins pick (PIMPBASE_AI_PROVIDER)"),
		model:    fs.String("ai-model", env("PIMPBASE_AI_MODEL", ""), "model of --ai-provider (for Azure, the deployment) (PIMPBASE_AI_MODEL)"),
	}
	add := func(provider, field, name, usage string, envs ...string) {
		def := ""
		for i := len(envs) - 1; i >= 0; i-- {
			def = env(envs[i], def)
		}
		o.settings = append(o.settings, aiSetting{provider, field, fs.String(name, def, usage+" ("+strings.Join(envs, " or ")+")")})
	}
	add(ai.Anthropic, "key", "anthropic-key", "Anthropic API key for the builder's AI assistant; kept on the server. Admins can also save keys in the builder", "PIMPBASE_ANTHROPIC_KEY", "ANTHROPIC_API_KEY")
	add(ai.OpenAI, "key", "openai-key", "OpenAI API key for the AI assistant", "PIMPBASE_OPENAI_KEY", "OPENAI_API_KEY")
	add(ai.Gemini, "key", "gemini-key", "Google Gemini API key for the AI assistant", "PIMPBASE_GEMINI_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY")
	add(ai.Mistral, "key", "mistral-key", "Mistral API key for the AI assistant", "PIMPBASE_MISTRAL_KEY", "MISTRAL_API_KEY")
	add(ai.Azure, "key", "azure-openai-key", "Azure OpenAI key for the AI assistant", "PIMPBASE_AZURE_OPENAI_KEY", "AZURE_OPENAI_API_KEY")
	add(ai.Azure, "url", "azure-openai-endpoint", "Azure OpenAI resource endpoint, e.g. https://name.openai.azure.com", "PIMPBASE_AZURE_OPENAI_ENDPOINT", "AZURE_OPENAI_ENDPOINT")
	add(ai.Azure, "model", "azure-openai-deployment", "Azure OpenAI deployment name", "PIMPBASE_AZURE_OPENAI_DEPLOYMENT")
	add(ai.Azure, "version", "azure-openai-api-version", "Azure OpenAI API version, or v1 for the v1 API (default "+ai.DefaultAzureAPIVersion+")", "PIMPBASE_AZURE_OPENAI_API_VERSION", "OPENAI_API_VERSION")
	add(ai.XAI, "key", "xai-key", "xAI (Grok) API key for the AI assistant", "PIMPBASE_XAI_KEY", "XAI_API_KEY")
	add(ai.DeepSeek, "key", "deepseek-key", "DeepSeek API key for the AI assistant", "PIMPBASE_DEEPSEEK_KEY", "DEEPSEEK_API_KEY")
	add(ai.Groq, "key", "groq-key", "Groq API key for the AI assistant", "PIMPBASE_GROQ_KEY", "GROQ_API_KEY")
	add(ai.Compatible, "url", "openai-compatible-url", "address of an OpenAI-compatible server for the AI assistant, e.g. http://localhost:11434/v1 for Ollama", "PIMPBASE_OPENAI_COMPATIBLE_URL")
	add(ai.Compatible, "key", "openai-compatible-key", "API key of the OpenAI-compatible server, if it needs one", "PIMPBASE_OPENAI_COMPATIBLE_KEY")
	add(ai.Compatible, "model", "openai-compatible-model", "model on the OpenAI-compatible server", "PIMPBASE_OPENAI_COMPATIBLE_MODEL")
	return o
}

func (o *aiOptions) apply(svc *ai.Service) error {
	for _, s := range o.settings {
		v := strings.TrimSpace(*s.value)
		if v == "" {
			continue
		}
		c := svc.Server[s.provider]
		switch s.field {
		case "key":
			c.APIKey = v
		case "url":
			c.BaseURL = strings.TrimRight(v, "/")
		case "model":
			c.Model = v
		case "version":
			c.APIVersion = v
		}
		svc.Server[s.provider] = c
	}
	p := strings.TrimSpace(*o.provider)
	if p == "" {
		if *o.model != "" {
			return fmt.Errorf("--ai-model needs --ai-provider")
		}
		return nil
	}
	known := false
	for _, x := range ai.Providers {
		known = known || x.ID == p
	}
	if !known {
		return fmt.Errorf("--ai-provider: unknown provider %q", p)
	}
	svc.ServerProvider = p
	if m := strings.TrimSpace(*o.model); m != "" {
		c := svc.Server[p]
		c.Model = m
		svc.Server[p] = c
	}
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", env("PIMPBASE_ADDR", ":8080"), "address to listen on (PIMPBASE_ADDR)")
	dataDir := fs.String("data", env("PIMPBASE_DATA", "./data"), "folder for SQLite databases and files (PIMPBASE_DATA)")
	pg := fs.String("postgres", env("PIMPBASE_POSTGRES", ""), "Postgres connection string; SQLite is used when empty (PIMPBASE_POSTGRES)")
	smtp := fs.String("smtp", env("PIMPBASE_SMTP", ""), "mail server for flow emails: smtp://user:pass@host:587, smtps://…, or log (PIMPBASE_SMTP)")
	mailFrom := fs.String("mail-from", env("PIMPBASE_MAIL_FROM", ""), "sender address of flow emails (PIMPBASE_MAIL_FROM)")
	secure := fs.Bool("secure-cookies", env("PIMPBASE_SECURE_COOKIES", "") == "true", "mark session cookies HTTPS-only (PIMPBASE_SECURE_COOKIES)")
	publicURL := fs.String("public-url", env("PIMPBASE_PUBLIC_URL", ""), "address people reach Pimpbase at, e.g. https://apps.example.com; single sign-on sends people back there (PIMPBASE_PUBLIC_URL)")
	filesSpec := fs.String("files", env("PIMPBASE_FILES", "db"), "where uploaded files go: db (inside the app's database), dir:/path (a folder, which servers may share), or s3://bucket/prefix?region=…&endpoint=… with AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY (PIMPBASE_FILES)")
	rateLimit := fs.Float64("rate-limit", envFloat("PIMPBASE_RATE_LIMIT", 100), "API requests a second one client may make to this server; 0 turns the limit off (PIMPBASE_RATE_LIMIT)")
	rateBurst := fs.Int("rate-burst", int(envFloat("PIMPBASE_RATE_BURST", 200)), "requests a client may make at once before the rate limit applies (PIMPBASE_RATE_BURST)")
	behindProxy := fs.Bool("behind-proxy", env("PIMPBASE_BEHIND_PROXY", "") == "true", "trust the X-Forwarded-For address a load balancer or proxy adds (PIMPBASE_BEHIND_PROXY)")
	metricsToken := fs.String("metrics-token", env("PIMPBASE_METRICS_TOKEN", ""), "require this Bearer token to read /metrics; empty leaves it open (PIMPBASE_METRICS_TOKEN)")
	drain := fs.Duration("drain", envDuration("PIMPBASE_DRAIN", 0), "on shutdown, fail /readyz this long before closing, so a load balancer stops sending requests (PIMPBASE_DRAIN)")
	backupDir := fs.String("backup-dir", env("PIMPBASE_BACKUP_DIR", ""), "with SQLite, write a backup zip into this folder every --backup-every (PIMPBASE_BACKUP_DIR)")
	backupEvery := fs.Duration("backup-every", envDuration("PIMPBASE_BACKUP_EVERY", 24*time.Hour), "how often to back up into --backup-dir (PIMPBASE_BACKUP_EVERY)")
	backupKeep := fs.Int("backup-keep", int(envFloat("PIMPBASE_BACKUP_KEEP", 7)), "backups to keep in --backup-dir (PIMPBASE_BACKUP_KEEP)")
	aiFlags := aiFlags(fs)
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
	if err := aiFlags.apply(srv.AI); err != nil {
		return err
	}
	srv.Files.Blobs = blobs
	srv.RateLimit, srv.RateBurst = *rateLimit, *rateBurst
	srv.BehindProxy = *behindProxy
	srv.MetricsToken = *metricsToken
	srv.Version = version
	srv.Bundles.Server = version
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
	log.Info("pimpbase is running", "version", version, "addr", *addr, "storage", where, "files", srv.Files.StorageName(), "server", srv.Node.ID)
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
	dataDir := fs.String("data", env("PIMPBASE_DATA", "./data"), "the SQLite data folder to back up (PIMPBASE_DATA)")
	out := fs.String("out", "", "zip file to write; default pimpbase-<time>.zip in the current folder")
	fs.Parse(args)
	if os.Getenv("PIMPBASE_POSTGRES") != "" {
		return errors.New("this server keeps its data in Postgres; back it up with pg_dump or your provider's snapshots")
	}
	name := *out
	if name == "" {
		name = "pimpbase-" + time.Now().UTC().Format("20060102-150405") + ".zip"
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
	dataDir := fs.String("data", env("PIMPBASE_DATA", "./data"), "the data folder to restore into; stop the server first (PIMPBASE_DATA)")
	force := fs.Bool("force", false, "replace the data already in the folder")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("name the backup zip: pimpbase restore --data ./data backup.zip")
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
	p := ssotest.New(*issuer, "pimpbase-dev", "dev-secret",
		ssotest.User{Subject: "kim", Email: "kim.lee@acme.test", EmailVerified: true, Name: "Kim Lee", Groups: []string{"sales", "north"}},
		ssotest.User{Subject: "sam", Email: "sam.ortiz@acme.test", EmailVerified: true, Name: "Sam Ortiz", Groups: []string{"sales-leads", "north", "south"}},
		ssotest.User{Subject: "ida", Email: "ida.novak@acme.test", EmailVerified: true, Name: "Ida Novak", Groups: []string{"it"}},
		ssotest.User{Subject: "eve", Email: "eve@elsewhere.test", EmailVerified: true, Name: "Eve Outsider", Groups: []string{}},
	)
	p.Title = "Acme sign-in"
	fmt.Fprintf(os.Stderr, "Test identity provider at %s\n  issuer:        %s\n  client id:     pimpbase-dev\n  client secret: dev-secret\nNever use it for real accounts.\n", *addr, *issuer)
	srv := &http.Server{Addr: *addr, Handler: p.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// healthcheck asks a server's /readyz, so container images need no curl.
func healthcheck(args []string) error {
	addr := env("PIMPBASE_ADDR", ":8080")
	url := "http://127.0.0.1" + addr[strings.LastIndex(addr, ":"):] + "/readyz"
	if len(args) > 0 {
		url = args[0]
	}
	c := &http.Client{Timeout: 5 * time.Second}
	res, err := c.Get(url)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", url, res.Status)
	}
	return nil
}
