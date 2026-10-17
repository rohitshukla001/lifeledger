package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/agent"
	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/tavily"
)

type liveRig struct {
	cfg    config.Config
	client *llm.Client
	db     *store.Store
	ctx    context.Context
}

func newLiveRig(t *testing.T) *liveRig {
	t.Helper()
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
		DailyBudgetUSD: 0.40,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &liveRig{cfg: cfg, client: client, db: db, ctx: ctx}
}

func (r *liveRig) agent(search agent.Searcher) *agent.Agent {
	return agent.New(agent.Options{
		LLM:      r.client,
		Store:    r.db,
		Memory:   memory.New(r.db, r.client, nil),
		Actions:  actions.New(actions.Options{Store: r.db, Location: r.cfg.Location}),
		Search:   search,
		Location: r.cfg.Location,
		Currency: "INR",
	})
}

func (r *liveRig) say(t *testing.T, bot *agent.Agent, text string) *agent.Result {
	t.Helper()
	conv, err := bot.StartConversation(r.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	res, err := bot.Reply(r.ctx, conv.ID, text)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Steps {
		t.Logf("  tool %s %s -> %s", s.Name, s.Arguments, s.Result)
	}
	t.Logf("reply: %s (cost $%.5f)", res.Reply, res.CostUSD)
	return res
}

func TestLiveAgentTracksAndRemembers(t *testing.T) {
	rig := newLiveRig(t)
	ctx, db, bot := rig.ctx, rig.db, rig.agent(nil)
	say := func(text string) *agent.Result {
		t.Helper()
		return rig.say(t, bot, text)
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

	say("I just paid the Acko car insurance premium.")
	paid, _ := db.GetObligation(ctx, items[0].ID)
	if paid.Status != store.StatusDone && (paid.DueOn == nil || paid.DueOn.Format(time.DateOnly) != "2028-03-15") {
		t.Errorf("after payment the obligation must be done or move to 2028-03-15: %+v", paid)
	}

	say("Draft an email to renewals@acko.example asking them to send the renewal invoice.")
	pending, _ := db.ListActions(ctx, store.ActionPending, 0)
	if len(pending) != 1 || pending[0].Kind != actions.KindSendEmail {
		t.Errorf("want one email waiting for approval, got %+v", pending)
	}
	t.Logf("total spend $%.5f", rig.client.Budget().Spent())
}

func TestLiveAgentResearchesAndCites(t *testing.T) {
	rig := newLiveRig(t)
	if rig.cfg.TavilyAPIKey == "" {
		t.Skip("TAVILY_API_KEY is not set")
	}
	search, err := tavily.New(tavily.Options{APIKey: rig.cfg.TavilyAPIKey, BaseURL: rig.cfg.TavilyBaseURL})
	if err != nil {
		t.Fatal(err)
	}
	bot := rig.agent(search)

	res := rig.say(t, bot, "Search the web for the official website of the Income Tax e-filing portal of India. Save its address to memory with the source.")
	searched := false
	for _, s := range res.Steps {
		searched = searched || (s.Name == "web_search" && !s.Failed)
	}
	if !searched {
		t.Fatal("the agent did not search the web")
	}
	mems, err := rig.db.ListMemories(rig.ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	var cited []store.Memory
	for _, m := range mems {
		if m.Source == "web" && strings.HasPrefix(m.SourceRef, "http") {
			cited = append(cited, m)
		}
	}
	if len(cited) == 0 {
		t.Fatalf("no memory with a web source: %+v", mems)
	}
	if !strings.Contains(res.Reply, "http") {
		t.Errorf("reply does not cite a URL: %q", res.Reply)
	}
	t.Logf("total spend $%.5f", rig.client.Budget().Spent())
}
