// Command lifeledger is the LifeLedger personal life-admin agent.
//
// Usage:
//
//	lifeledger serve     Start the HTTP server.
//	lifeledger version   Print the build version.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"lifeledger/internal/config"
	"lifeledger/internal/server"
	"lifeledger/internal/version"
)

const usage = `LifeLedger: a private life-admin agent.

Usage:
  lifeledger [-env-file PATH] <command>

Commands:
  serve     Start the HTTP server.
  version   Print the build version.
  help      Show this help.

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
		return 2
	}

	cmd := fs.Arg(0)
	switch cmd {
	case "version":
		fmt.Fprintln(stdout, "lifeledger", version.String())
		return 0
	case "", "help", "-h", "--help":
		fs.Usage()
		if cmd == "" {
			return 2
		}
		return 0
	}

	cfg, err := config.Load(*envFile)
	if err != nil {
		fmt.Fprintln(stderr, "config error:", err)
		return 1
	}
	log, err := newLogger(cfg, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "config error:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		log.Info("starting lifeledger", "version", version.String(), "env", cfg.Env)
		if err := server.New(cfg.Addr, log).Run(ctx); err != nil {
			log.Error("server stopped with error", "err", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		fs.Usage()
		return 2
	}
}

func newLogger(cfg config.Config, w io.Writer) (*slog.Logger, error) {
	level, err := cfg.SlogLevel()
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.Env == config.EnvProd {
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	}
	return slog.New(slog.NewTextHandler(w, opts)), nil
}
