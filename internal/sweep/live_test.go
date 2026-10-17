package sweep_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/sweep"
)

func TestLiveWeeklyReviewOnUltra(t *testing.T) {
	if os.Getenv("LIFELEDGER_LIVE_TEST") != "1" {
		t.Skip("set LIFELEDGER_LIVE_TEST=1 to call the real Token Factory API")
	}
	cfg, err := config.Load(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := llm.New(llm.Options{
		BaseURL:        cfg.NebiusBaseURL,
		APIKey:         cfg.NebiusAPIKey,
		Models:         map[llm.Tier]string{llm.Nano: cfg.ModelNano, llm.Super: cfg.ModelSuper, llm.Ultra: cfg.ModelUltra},
		EmbedModel:     cfg.ModelEmbed,
		DailyBudgetUSD: 0.10,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	kolkata, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	db.SetClock(func() time.Time { return now })

	minor := func(v int64) *int64 { return &v }
	date := func(s string) *time.Time { d, _ := time.Parse(time.DateOnly, s); return &d }
	for _, o := range []store.Obligation{
		{Title: "Rent", Category: store.CategoryBill, AmountMinor: minor(2500000), Currency: "INR", DueOn: date("2026-10-15"), Recurrence: store.RecurMonthly},
		{Title: "Acko car insurance", Category: store.CategoryRenewal, AmountMinor: minor(1240000), Currency: "INR", DueOn: date("2026-10-22"), Recurrence: store.RecurYearly},
		{Title: "Airtel broadband", Category: store.CategoryBill, AmountMinor: minor(99900), Currency: "INR", DueOn: date("2026-10-27"), Recurrence: store.RecurMonthly},
	} {
		if err := db.CreateObligation(ctx, &o); err != nil {
			t.Fatal(err)
		}
	}

	s := sweep.New(sweep.Options{Store: db, LLM: client, Location: kolkata, Currency: "INR", Now: func() time.Time { return now }})
	n, err := s.Review(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s\n\n%s", n.Title, n.Body)
	summary, _, found := strings.Cut(n.Body, "\n\nDetails:")
	if !found || strings.Contains(summary, "model was not available") {
		t.Fatalf("the review has no model summary: %q", n.Body)
	}
	if !strings.Contains(strings.ToLower(summary), "rent") {
		t.Errorf("the summary does not mention the overdue rent: %q", summary)
	}
	t.Logf("spend $%.5f", client.Budget().Spent())
}
