package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/rohitshukla001/lifeledger/internal/agent"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

func (a *app) chat(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	convID := fs.Int64("c", 0, "continue the conversation with this id")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	client, err := a.llmClient()
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()

	bot := agent.New(agent.Options{
		LLM:      client,
		Store:    db,
		Memory:   memory.New(db, client, a.log),
		Location: a.cfg.Location,
		Currency: a.cfg.Currency,
		Logger:   a.log,
	})

	if *convID != 0 {
		c, err := db.GetConversation(ctx, *convID)
		if errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(a.stderr, "no conversation with id %d\n", *convID)
			return 1
		}
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		fmt.Fprintf(a.stdout, "Continuing conversation #%d: %s\n", c.ID, c.Title)
	}
	fmt.Fprintln(a.stdout, "Type a message. /new starts a new conversation, /quit exits.")

	in := bufio.NewScanner(a.stdin)
	in.Buffer(make([]byte, 64<<10), 1<<20)
	for {
		fmt.Fprint(a.stdout, "you> ")
		if !in.Scan() {
			fmt.Fprintln(a.stdout)
			return a.scanResult(in.Err())
		}
		line := strings.TrimSpace(in.Text())
		switch line {
		case "":
			continue
		case "/quit", "/exit":
			return 0
		case "/new":
			*convID = 0
			fmt.Fprintln(a.stdout, "Started a new conversation.")
			continue
		}

		if *convID == 0 {
			c, err := bot.StartConversation(ctx, line)
			if err != nil {
				fmt.Fprintln(a.stderr, err)
				return 1
			}
			*convID = c.ID
		}

		res, err := bot.Reply(ctx, *convID, line)
		if err != nil {
			fmt.Fprintln(a.stderr, "error:", err)
			if ctx.Err() != nil {
				return 1
			}
			continue
		}
		for _, s := range res.Steps {
			status := "ok"
			if s.Failed {
				status = "failed: " + s.Result
			}
			fmt.Fprintf(a.stdout, "  · %s %s → %s\n", s.Name, s.Arguments, status)
		}
		fmt.Fprintf(a.stdout, "ledger> %s\n", res.Reply)
		fmt.Fprintf(a.stderr, "  [conversation #%d, %d tool calls, %d tokens, $%.4f]\n",
			res.ConversationID, len(res.Steps), res.Usage.PromptTokens+res.Usage.CompletionTokens, res.CostUSD)
	}
}

func (a *app) scanResult(err error) int {
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	return 0
}
