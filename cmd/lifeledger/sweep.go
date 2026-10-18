package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/notify"
	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/sweep"
)

const notificationsUsage = `usage: lifeledger notifications <command>

  list [-n N]             Show recent reminders and reviews (default).
  test                    Send a test message to Telegram.
`

func (a *app) telegram() (*notify.Telegram, error) {
	return notify.NewTelegram(notify.TelegramOptions{Token: a.cfg.TelegramBotToken, ChatID: a.cfg.TelegramChatID})
}

func (a *app) sweeper(db *store.Store, client *llm.Client) *sweep.Sweeper {
	opts := sweep.Options{
		Store:      db,
		Location:   a.cfg.Location,
		Currency:   a.cfg.Currency,
		RemindHour: a.cfg.RemindHour,
		Logger:     a.log,
	}
	if a.cfg.ReviewEnabled {
		opts.Review = &sweep.Schedule{Day: a.cfg.ReviewDay, Hour: a.cfg.ReviewHour}
	}
	if client != nil {
		opts.LLM = client
	}
	if a.cfg.TelegramBotToken != "" {
		tg, err := a.telegram()
		if err != nil {
			a.log.Warn("Telegram delivery is off", "err", err)
		} else {
			opts.Notifier = tg
		}
	}
	return sweep.New(opts)
}

func (a *app) sweep(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("sweep", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	review := fs.Bool("review", false, "also write a weekly review now")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()
	s := a.sweeper(db, a.optionalLLM())

	if *review {
		n, err := s.Review(ctx)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		fmt.Fprintf(a.stdout, "%s\n\n%s\n\n", n.Title, n.Body)
	}
	rep, err := s.RunOnce(ctx)
	fmt.Fprintln(a.stdout, rep)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	return 0
}

func (a *app) notifications(ctx context.Context, args []string) int {
	cmd, rest := "list", args
	if len(args) > 0 {
		cmd, rest = args[0], args[1:]
	}
	switch cmd {
	case "list":
		return a.listNotifications(ctx, rest)
	case "test":
		tg, err := a.telegram()
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		if err := tg.Send(ctx, "LifeLedger test message. Reminders and weekly reviews will arrive here."); err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		fmt.Fprintln(a.stdout, "sent a test message to Telegram")
		return 0
	default:
		fmt.Fprint(a.stderr, notificationsUsage)
		return 2
	}
}

func (a *app) listNotifications(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("notifications list", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	n := fs.Int("n", 20, "number of notifications")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()

	items, err := db.ListNotifications(ctx, *n)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if len(items) == 0 {
		fmt.Fprintln(a.stdout, "no notifications yet")
		return 0
	}
	for _, it := range items {
		status := "not sent"
		switch {
		case it.DeliveredAt != nil:
			status = "sent"
		case it.LastError != "":
			status = fmt.Sprintf("failed %dx: %s", it.Attempts, it.LastError)
		}
		fmt.Fprintf(a.stdout, "#%d %s  %s  [%s]\n", it.ID, it.CreatedAt.In(a.cfg.Location).Format(time.DateTime), it.Title, status)
		if it.Kind == store.NotificationReview {
			for _, line := range strings.Split(it.Body, "\n") {
				fmt.Fprintf(a.stdout, "    %s\n", line)
			}
		}
	}
	return 0
}
