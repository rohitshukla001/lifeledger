package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const memoryUsage = `usage: lifeledger memory <command>

  add [-kind fact|preference|note] TEXT   Save a memory.
  list                                    Show all memories, newest first.
  recall [-n N] QUERY                     Find the memories that match QUERY.
  edit ID TEXT                            Change the text of a memory.
  forget ID                               Delete a memory permanently.
  reindex                                 Embed the memories that have no embedding.
`

func (a *app) memory(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, memoryUsage)
		return 2
	}

	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()

	var embedder memory.Embedder
	if client, err := a.llmClient(); err == nil {
		embedder = client
	} else {
		fmt.Fprintf(a.stderr, "note: %v; using keyword matching only\n", err)
	}
	engine := memory.New(db, embedder, a.log)

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		return a.memoryAdd(ctx, engine, rest)
	case "list":
		return a.memoryList(ctx, engine)
	case "recall":
		return a.memoryRecall(ctx, engine, rest)
	case "edit":
		return a.memoryEdit(ctx, engine, rest)
	case "forget":
		return a.memoryForget(ctx, engine, rest)
	case "reindex":
		n, err := engine.Reindex(ctx)
		fmt.Fprintf(a.stdout, "embedded %d memories\n", n)
		return a.memoryFail(err)
	default:
		fmt.Fprintf(a.stderr, "unknown memory command %q\n\n%s", cmd, memoryUsage)
		return 2
	}
}

func (a *app) memoryAdd(ctx context.Context, engine *memory.Engine, args []string) int {
	fs := flag.NewFlagSet("memory add", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	kind := fs.String("kind", string(store.MemoryFact), "fact, preference, or note")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	text := strings.Join(fs.Args(), " ")
	m, created, err := engine.Remember(ctx, store.Memory{Kind: store.MemoryKind(*kind), Content: text, Source: "cli"})
	if err != nil {
		return a.memoryFail(err)
	}
	if !created {
		fmt.Fprint(a.stdout, "already remembered: ")
	}
	a.printMemory(m)
	return 0
}

func (a *app) memoryList(ctx context.Context, engine *memory.Engine) int {
	memories, err := engine.List(ctx, 0)
	if err != nil {
		return a.memoryFail(err)
	}
	for _, m := range memories {
		a.printMemory(m)
	}
	return 0
}

func (a *app) memoryRecall(ctx context.Context, engine *memory.Engine, args []string) int {
	fs := flag.NewFlagSet("memory recall", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	n := fs.Int("n", 5, "maximum number of results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	hits, err := engine.Recall(ctx, strings.Join(fs.Args(), " "), *n)
	if err != nil {
		return a.memoryFail(err)
	}
	if len(hits) == 0 {
		fmt.Fprintln(a.stdout, "no matching memories")
	}
	for _, h := range hits {
		fmt.Fprintf(a.stdout, "%.2f  ", h.Score)
		a.printMemory(h.Memory)
	}
	return 0
}

func (a *app) memoryEdit(ctx context.Context, engine *memory.Engine, args []string) int {
	if len(args) < 2 {
		fmt.Fprint(a.stderr, memoryUsage)
		return 2
	}
	id, ok := a.parseID(args[0])
	if !ok {
		return 2
	}
	m, err := engine.Edit(ctx, id, strings.Join(args[1:], " "))
	if err != nil {
		return a.memoryFail(err)
	}
	a.printMemory(m)
	return 0
}

func (a *app) memoryForget(ctx context.Context, engine *memory.Engine, args []string) int {
	if len(args) != 1 {
		fmt.Fprint(a.stderr, memoryUsage)
		return 2
	}
	id, ok := a.parseID(args[0])
	if !ok {
		return 2
	}
	if err := engine.Forget(ctx, id); err != nil {
		return a.memoryFail(err)
	}
	fmt.Fprintf(a.stdout, "forgot #%d\n", id)
	return 0
}

func (a *app) printMemory(m store.Memory) {
	fmt.Fprintf(a.stdout, "#%d [%s] %s  (source: %s)\n", m.ID, m.Kind, m.Content, m.Source)
}

func (a *app) parseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	if err != nil || id <= 0 {
		fmt.Fprintf(a.stderr, "invalid memory id %q\n", s)
		return 0, false
	}
	return id, true
}

func (a *app) memoryFail(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprintln(a.stderr, "no memory with that id")
		return 1
	}
	fmt.Fprintln(a.stderr, err)
	return 1
}
