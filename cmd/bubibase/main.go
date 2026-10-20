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
	"syscall"
	"time"

	"github.com/bubibase/bubibase/internal/api"
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
  bubibase serve [flags]   start the server
  bubibase dev-idp [flags] run a test single sign-on provider, never for real accounts
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

	srv := api.New(st, web.Builder(), log)
	srv.Secure = *secure
	srv.PublicURL = *publicURL
	srv.AI.EnvKey = *anthropicKey
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
	log.Info("bubibase is running", "version", version, "addr", *addr, "storage", where)
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shut)
	}
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
