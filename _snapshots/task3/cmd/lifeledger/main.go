package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/server"
	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/version"
)

const usage = `LifeLedger: a private life-admin agent.

Usage:
  lifeledger [-env-file PATH] <command>

Commands:
  serve                 Start the HTTP server.
  models                List Token Factory models and check the configured ones.
  ask [-tier T] PROMPT  Send one prompt to Nemotron (T is nano, super, or ultra).
  version               Print the build version.
  help                  Show this help.

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lifeledger", flag.ContinueOnError)
	fs.SetOutput(stderr)
	envFile := fs.String("env-file", ".env", "path to an optional .env file")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	cmd := fs.Arg(0)
	switch cmd {
	case "version":
		fmt.Fprintln(stdout, "lifeledger", version.String())
		return 0
	case "help":
		fs.Usage()
		return 0
	case "":
		fs.Usage()
		return 2
	}

	cfg, err := config.Load(*envFile)
	if err != nil {
		fmt.Fprintln(stderr, "config error:", err)
		return 1
	}
	a := &app{cfg: cfg, log: newLogger(cfg, stderr), stdout: stdout, stderr: stderr}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		return a.serve(ctx)
	case "models":
		return a.models(ctx)
	case "ask":
		return a.ask(ctx, fs.Args()[1:])
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		fs.Usage()
		return 2
	}
}

type app struct {
	cfg    config.Config
	log    *slog.Logger
	stdout io.Writer
	stderr io.Writer
}

func (a *app) serve(ctx context.Context) int {
	a.log.Info("starting lifeledger", "version", version.String(), "env", a.cfg.Env)

	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		a.log.Error("cannot open database", "path", a.cfg.DBPath, "err", err)
		return 1
	}
	defer db.Close()
	if v, err := db.SchemaVersion(ctx); err == nil {
		a.log.Info("database ready", "path", a.cfg.DBPath, "schema_version", v)
	}

	if err := server.New(a.cfg.Addr, a.log, db).Run(ctx); err != nil {
		a.log.Error("server stopped with error", "err", err)
		return 1
	}
	return 0
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.Env == config.EnvProd {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
