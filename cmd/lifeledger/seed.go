package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/seed"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

func (a *app) seed(ctx context.Context) int {
	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()

	sum, err := a.seedDemo(ctx, db, a.optionalLLM())
	if errors.Is(err, seed.ErrNotEmpty) {
		fmt.Fprintln(a.stderr, "the database already has data; seed only an empty database")
		return 1
	}
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	fmt.Fprintf(a.stdout, "seeded %s into %s\n", sum, a.cfg.DBPath)
	return 0
}

func (a *app) seedDemo(ctx context.Context, db *store.Store, client *llm.Client) (seed.Summary, error) {
	sum, err := seed.Demo(ctx, db, ledger.Today(time.Now(), a.cfg.Location))
	if err != nil {
		return sum, err
	}
	if client != nil {
		if _, err := memory.New(db, client, a.log).Reindex(ctx); err != nil {
			a.log.Warn("demo memories have no embeddings yet; run lifeledger memory reindex", "err", err)
		}
	}
	return sum, nil
}
