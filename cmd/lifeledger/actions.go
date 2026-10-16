package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const actionsUsage = `usage: lifeledger actions <command>

  pending                 Show the actions that wait for approval (default).
  list [-n N]             Show recent actions.
  show ID                 Show one action with its result.
  approve ID...           Approve and run actions.
  reject ID...            Reject actions.
  ics ID [-o FILE]        Write the calendar file of a calendar action.
`

func (a *app) actionService(db *store.Store) *actions.Service {
	var mailer actions.Mailer
	if a.cfg.SMTPHost != "" {
		mailer = actions.SMTP{
			Host:     a.cfg.SMTPHost,
			Port:     a.cfg.SMTPPort,
			Username: a.cfg.SMTPUsername,
			Password: a.cfg.SMTPPassword,
			From:     a.cfg.SMTPFrom,
		}
	}
	return actions.New(actions.Options{Store: db, Mailer: mailer, Location: a.cfg.Location, Logger: a.log})
}

func (a *app) actions(ctx context.Context, args []string) int {
	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()
	svc := a.actionService(db)

	cmd, rest := "pending", args
	if len(args) > 0 {
		cmd, rest = args[0], args[1:]
	}
	switch cmd {
	case "pending":
		pending, err := svc.Pending(ctx)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		if len(pending) == 0 {
			fmt.Fprintln(a.stdout, "no actions wait for approval")
		}
		for _, act := range pending {
			a.printAction(act)
		}
		return 0
	case "list":
		fs := flag.NewFlagSet("actions list", flag.ContinueOnError)
		fs.SetOutput(a.stderr)
		n := fs.Int("n", 20, "number of actions")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		list, err := db.ListActions(ctx, "", *n)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		for _, act := range list {
			a.printAction(act)
		}
		return 0
	case "show":
		return a.withAction(ctx, svc, rest, func(act store.Action) int {
			a.printAction(act)
			fmt.Fprintf(a.stdout, "  payload: %s\n", act.Payload)
			if act.Result != "" && act.Kind != actions.KindCalendarEvent {
				fmt.Fprintf(a.stdout, "  result:  %s\n", act.Result)
			}
			return 0
		})
	case "approve", "reject":
		return a.decideActions(ctx, svc, cmd, rest)
	case "ics":
		return a.writeICS(ctx, svc, rest)
	default:
		fmt.Fprintf(a.stderr, "unknown actions command %q\n\n%s", cmd, actionsUsage)
		return 2
	}
}

func (a *app) decideActions(ctx context.Context, svc *actions.Service, cmd string, ids []string) int {
	if len(ids) == 0 {
		fmt.Fprint(a.stderr, actionsUsage)
		return 2
	}
	code := 0
	for _, arg := range ids {
		id, ok := a.parseID(arg)
		if !ok {
			return 2
		}
		if err := a.decideAction(ctx, svc, cmd, id); err != nil {
			code = 1
		}
	}
	return code
}

func (a *app) decideAction(ctx context.Context, svc *actions.Service, cmd string, id int64) error {
	var err error
	if cmd == "approve" {
		var act store.Action
		if act, err = svc.Approve(ctx, id, actions.ActorUser); err == nil {
			a.printAction(act)
			if act.Status == store.ActionFailed {
				err = errors.New(act.Error)
			}
		}
	} else if err = svc.Reject(ctx, id, actions.ActorUser); err == nil {
		fmt.Fprintf(a.stdout, "rejected action #%d\n", id)
	}

	switch {
	case errors.Is(err, store.ErrNotFound):
		fmt.Fprintf(a.stderr, "no action #%d\n", id)
	case errors.Is(err, store.ErrAlreadyDecided):
		fmt.Fprintf(a.stderr, "action #%d was already approved or rejected\n", id)
	case err != nil && cmd == "reject":
		fmt.Fprintf(a.stderr, "#%d: %v\n", id, err)
	}
	return err
}

func (a *app) writeICS(ctx context.Context, svc *actions.Service, args []string) int {
	fs := flag.NewFlagSet("actions ics", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	out := fs.String("o", "", "write to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return a.withAction(ctx, svc, fs.Args(), func(act store.Action) int {
		if act.Kind != actions.KindCalendarEvent || act.Status != store.ActionDone {
			fmt.Fprintf(a.stderr, "action #%d is not a finished calendar action\n", act.ID)
			return 1
		}
		if *out == "" {
			fmt.Fprint(a.stdout, act.Result)
			return 0
		}
		if err := os.WriteFile(*out, []byte(act.Result), 0o644); err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		fmt.Fprintf(a.stdout, "wrote %s\n", *out)
		return 0
	})
}

func (a *app) withAction(ctx context.Context, svc *actions.Service, args []string, fn func(store.Action) int) int {
	if len(args) != 1 {
		fmt.Fprint(a.stderr, actionsUsage)
		return 2
	}
	id, ok := a.parseID(args[0])
	if !ok {
		return 2
	}
	act, err := svc.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprintf(a.stderr, "no action #%d\n", id)
		return 1
	}
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	return fn(act)
}

func (a *app) printAction(act store.Action) {
	line := fmt.Sprintf("#%d [%s] %s — %s", act.ID, act.Status, act.Summary, act.RequestedBy)
	switch {
	case act.Status == store.ActionFailed:
		line += "  error: " + act.Error
	case act.Status == store.ActionDone && act.Kind == actions.KindCalendarEvent:
		line += fmt.Sprintf("  (lifeledger actions ics %d -o reminder.ics)", act.ID)
	case act.Status == store.ActionDone:
		line += "  → " + act.Result
	}
	fmt.Fprintln(a.stdout, line)
}

func (a *app) audit(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	n := fs.Int("n", 30, "number of entries")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()

	entries, err := db.ListAudit(ctx, *n)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		line := fmt.Sprintf("%s  %-7s %-17s %s", e.CreatedAt.In(a.cfg.Location).Format(time.DateTime), e.Actor, e.Event, e.Subject)
		if e.Detail != "" {
			line += "  " + e.Detail
		}
		fmt.Fprintln(a.stdout, line)
	}
	return 0
}
