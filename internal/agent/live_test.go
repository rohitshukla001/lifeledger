package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/agent"
	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

func TestLiveAgentTracksAndRemembers(t *testing.T) {
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
		DailyBudgetUSD: 0.25,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bot := agent.New(agent.Options{LLM: client, Store: db, Memory: memory.New(db, client, nil), Location: cfg.Location, Currency: "INR"})

	say := func(text string) *agent.Result {
		t.Helper()
		conv, err := bot.StartConversation(ctx, text)
		if err != nil {
			t.Fatal(err)
		}
		res, err := bot.Reply(ctx, conv.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range res.Steps {
			t.Logf("  tool %s %s -> %s", s.Name, s.Arguments, s.Result)
		}
		t.Logf("reply: %s (cost $%.5f)", res.Reply, res.CostUSD)
		return res
	}

	first := say("My car insurance with Acko renews on 15 March 2027. The premium is 12,400 rupees a year. Please track it.")
	if len(first.Steps) == 0 {
		t.Fatal("the agent did not use any tool")
	}
	items, _ := db.ListObligations(ctx, store.ObligationFilter{})
	if len(items) != 1 || items[0].DueOn == nil || items[0].DueOn.Format(time.DateOnly) != "2027-03-15" {
		t.Fatalf("obligations = %+v", items)
	}

	second := say("When does my car insurance renew, and who is it with?")
	reply := strings.ToLower(second.Reply)
	if !strings.Contains(reply, "march") && !strings.Contains(reply, "2027-03-15") {
		t.Errorf("reply does not give the renewal date: %q", second.Reply)
	}
	t.Logf("total spend $%.5f", client.Budget().Spent())
}
