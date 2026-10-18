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
	_ "time/tzdata"

	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/version"
)

const usage = `LifeLedger: a private life-admin agent.

Usage:
  lifeledger [-env-file PATH] <command>

Commands:
  serve                 Start the HTTP server.
  models                List Token Factory models and check the configured ones.
  ask [-tier T] PROMPT  Send one prompt to Nemotron (T is nano, super, or ultra).
  memory <command>      Add, list, recall, edit, or forget memories.
  chat [-c ID]          Talk to the LifeLedger agent in the terminal.
  ingest <command>      Extract obligations from an email or PDF for review.
  actions <command>     Review, approve, or reject actions.
  audit [-n N]          Show the audit log.
  search [flags] QUERY  Search the web with Tavily and show cited sources.
  sweep [-review]       Run one sweep: wake snoozed items, remind, review, deliver.
  notifications <cmd>   List reminders and reviews, or send a Telegram test.
  version               Print the build version.
  help                  Show this help.

Flags:
`

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithInput(args, os.Stdin, stdout, stderr)
}

func runWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
	a := &app{cfg: cfg, log: newLogger(cfg, stderr), stdin: stdin, stdout: stdout, stderr: stderr}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		return a.serve(ctx)
	case "models":
		return a.models(ctx)
	case "ask":
		return a.ask(ctx, fs.Args()[1:])
	case "memory":
		return a.memory(ctx, fs.Args()[1:])
	case "chat":
		return a.chat(ctx, fs.Args()[1:])
	case "ingest":
		return a.ingest(ctx, fs.Args()[1:])
	case "actions":
		return a.actions(ctx, fs.Args()[1:])
	case "audit":
		return a.audit(ctx, fs.Args()[1:])
	case "search":
		return a.search(ctx, fs.Args()[1:])
	case "sweep":
		return a.sweep(ctx, fs.Args()[1:])
	case "notifications":
		return a.notifications(ctx, fs.Args()[1:])
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		fs.Usage()
		return 2
	}
}

type app struct {
	cfg    config.Config
	log    *slog.Logger
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.Env == config.EnvProd {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
