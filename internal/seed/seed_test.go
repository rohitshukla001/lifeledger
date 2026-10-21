package seed

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

func TestDemoSeedsAnEmptyLedgerOnce(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	today := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

	sum, err := Demo(ctx, s, today)
	if err != nil {
		t.Fatal(err)
	}
	if sum.String() != "9 obligations, 4 memories, 1 actions, 2 proposals" {
		t.Fatalf("summary = %s", sum)
	}

	open, _ := s.ListObligations(ctx, store.ObligationFilter{Statuses: []store.Status{store.StatusOpen}})
	if len(open) != 8 || open[0].Title != "Rent" || open[0].DueOn.Format(time.DateOnly) != "2026-10-18" || *open[0].AmountMinor != 2500000 || open[0].Currency != "INR" {
		t.Fatalf("first open item = %+v", open[0])
	}
	pending, _ := s.ListActions(ctx, store.ActionPending, 0)
	if len(pending) != 1 || pending[0].Kind != "send_email" || pending[0].RequestedBy != "agent" {
		t.Fatalf("pending = %+v", pending)
	}
	proposals, _ := s.ListProposals(ctx, store.ProposalFilter{Status: store.ProposalPending})
	if len(proposals) != 2 || proposals[0].DuplicateOf == nil || proposals[0].DueOn.Format(time.DateOnly) != "2026-11-15" {
		t.Fatalf("proposals = %+v", proposals)
	}

	text := strings.Replace(airtelEmail, "%s", "15-Nov-2026", 1)
	if _, err := s.IngestionByHash(ctx, ingest.Hash(text)); err != nil {
		t.Fatalf("the demo email must be known to the importer: %v", err)
	}

	if _, err := Demo(ctx, s, today); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("second seed: err = %v", err)
	}
}
