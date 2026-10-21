package main

import (
	"context"
	"errors"
	"sync"

	"github.com/rohitshukla001/lifeledger/internal/api"
	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/seed"
	"github.com/rohitshukla001/lifeledger/internal/server"
	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/version"
)

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
	if a.cfg.APIToken == "" {
		a.log.Warn("LIFELEDGER_API_TOKEN is empty, so the API accepts requests without a token")
	}

	client := a.optionalLLM()
	if a.cfg.SeedDemo {
		sum, err := a.seedDemo(ctx, db, client)
		switch {
		case errors.Is(err, seed.ErrNotEmpty):
			a.log.Info("demo data not added; the database already has data")
		case err != nil:
			a.log.Error("cannot add demo data", "err", err)
			return 1
		default:
			a.log.Info("demo data added", "summary", sum.String())
		}
	}
	opts := api.Options{
		Store:    db,
		Actions:  a.actionService(db),
		Search:   a.searcher(),
		Token:    a.cfg.APIToken,
		Location: a.cfg.Location,
		Currency: a.cfg.Currency,
		Logger:   a.log,
	}
	ingestOpts := ingest.Options{Store: db, Location: a.cfg.Location, Currency: a.cfg.Currency, Logger: a.log}
	if client != nil {
		opts.Memory = memory.New(db, client, a.log)
		opts.Agent = a.newAgent(db, client, opts.Memory, opts.Actions)
		ingestOpts.LLM = client
	} else {
		opts.Memory = memory.New(db, nil, a.log)
	}
	opts.Ingest = ingest.New(ingestOpts)
	opts.Sweeper = a.sweeper(db, client)

	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	if a.cfg.SweepInterval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			opts.Sweeper.Run(ctx, a.cfg.SweepInterval)
		}()
		a.log.Info("sweep started", "every", a.cfg.SweepInterval.String())
	}

	site, err := server.Site(a.cfg.WebDir)
	if err != nil {
		a.log.Info("web UI is off; build it with make web", "reason", err)
	}
	srv := server.New(server.Options{Addr: a.cfg.Addr, Logger: a.log, DB: db, API: api.New(opts), Site: site})
	if err := srv.Run(ctx); err != nil {
		a.log.Error("server stopped with error", "err", err)
		return 1
	}
	return 0
}
