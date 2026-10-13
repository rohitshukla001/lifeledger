package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/money"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const ingestUsage = `usage: lifeledger ingest <command>

  add [-name NAME] FILE    Extract obligations from a text, email, or PDF file. Use - for stdin.
  pending                  Show the proposals that wait for a decision.
  accept ID...             Turn proposals into obligations.
  reject ID...             Discard proposals.
`

func (a *app) ingest(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, ingestUsage)
		return 2
	}

	db, err := store.Open(ctx, a.cfg.DBPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defer db.Close()

	opts := ingest.Options{Store: db, Location: a.cfg.Location, Currency: a.cfg.Currency, Logger: a.log}
	cmd, rest := args[0], args[1:]
	if cmd == "add" {
		client, err := a.llmClient()
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		opts.LLM = client
	}
	svc := ingest.New(opts)

	switch cmd {
	case "add":
		return a.ingestAdd(ctx, svc, rest)
	case "pending":
		props, err := svc.Pending(ctx)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		if len(props) == 0 {
			fmt.Fprintln(a.stdout, "no pending proposals")
		}
		for _, p := range props {
			a.printProposal(p)
		}
		return 0
	case "accept", "reject":
		return a.ingestDecide(ctx, svc, cmd, rest)
	default:
		fmt.Fprintf(a.stderr, "unknown ingest command %q\n\n%s", cmd, ingestUsage)
		return 2
	}
}

func (a *app) ingestAdd(ctx context.Context, svc *ingest.Service, args []string) int {
	fs := flag.NewFlagSet("ingest add", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	name := fs.String("name", "", "name of the document (default: the file name)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(a.stderr, ingestUsage)
		return 2
	}

	path := fs.Arg(0)
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(io.LimitReader(a.stdin, ingest.MaxPDFBytes+1))
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if *name == "" {
		*name = filepath.Base(path)
	}

	var res *ingest.Result
	if strings.HasPrefix(string(data[:min(len(data), 5)]), "%PDF-") {
		res, err = svc.PDF(ctx, *name, data)
	} else {
		res, err = svc.Text(ctx, *name, string(data))
	}
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	if res.Reused {
		fmt.Fprintf(a.stdout, "already ingested as #%d; showing its proposals\n", res.Ingestion.ID)
	}
	if res.Truncated {
		fmt.Fprintln(a.stderr, "note: the document was long; only the first part was read")
	}
	fmt.Fprintf(a.stdout, "%s\n", res.Ingestion.Summary)
	if len(res.Proposals) == 0 {
		fmt.Fprintln(a.stdout, "no obligations found")
	}
	for _, p := range res.Proposals {
		a.printProposal(p)
	}
	return 0
}

func (a *app) ingestDecide(ctx context.Context, svc *ingest.Service, cmd string, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, ingestUsage)
		return 2
	}
	code := 0
	for _, arg := range args {
		id, ok := a.parseID(arg)
		if !ok {
			return 2
		}
		var err error
		if cmd == "accept" {
			var o store.Obligation
			if o, err = svc.Accept(ctx, id); err == nil {
				fmt.Fprintf(a.stdout, "accepted #%d as obligation #%d: %s\n", id, o.ID, o.Title)
			}
		} else if err = svc.Reject(ctx, id); err == nil {
			fmt.Fprintf(a.stdout, "rejected #%d\n", id)
		}
		switch {
		case errors.Is(err, store.ErrNotFound):
			fmt.Fprintf(a.stderr, "no proposal #%d\n", id)
			code = 1
		case err != nil:
			fmt.Fprintf(a.stderr, "#%d: %v\n", id, err)
			code = 1
		}
	}
	return code
}

func (a *app) printProposal(p store.Proposal) {
	var parts []string
	if p.AmountMinor != nil {
		parts = append(parts, p.Currency+" "+money.Format(*p.AmountMinor))
	}
	if p.DueOn != nil {
		parts = append(parts, "due "+p.DueOn.Format(time.DateOnly))
	}
	if p.Recurrence != store.RecurNone {
		parts = append(parts, string(p.Recurrence))
	}
	line := fmt.Sprintf("#%d [%s] %s", p.ID, p.Category, p.Title)
	if len(parts) > 0 {
		line += " — " + strings.Join(parts, ", ")
	}
	line += fmt.Sprintf("  (confidence %.0f%%, %s)", p.Confidence*100, p.Status)
	if p.DuplicateOf != nil {
		line += fmt.Sprintf("  possible duplicate of obligation #%d", *p.DuplicateOf)
	}
	fmt.Fprintln(a.stdout, line)
	if p.Evidence != "" {
		fmt.Fprintf(a.stdout, "    %q\n", p.Evidence)
	}
}
