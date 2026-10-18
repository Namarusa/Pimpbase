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
	secure := fs.Bool("secure-cookies", env("BUBIBASE_SECURE_COOKIES", "") == "true", "mark session cookies HTTPS-only (BUBIBASE_SECURE_COOKIES)")
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
