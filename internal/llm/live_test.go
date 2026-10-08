package llm_test

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/llm"
)

func liveClient(t *testing.T) *llm.Client {
	t.Helper()
	if os.Getenv("LIFELEDGER_LIVE_TEST") != "1" {
		t.Skip("set LIFELEDGER_LIVE_TEST=1 to call the real Token Factory API")
	}
	cfg, err := config.Load(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := llm.New(llm.Options{
		BaseURL:        cfg.NebiusBaseURL,
		APIKey:         cfg.NebiusAPIKey,
		Models:         map[llm.Tier]string{llm.Nano: cfg.ModelNano, llm.Super: cfg.ModelSuper, llm.Ultra: cfg.ModelUltra},
		EmbedModel:     cfg.ModelEmbed,
		DailyBudgetUSD: 0.10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func liveCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func TestLiveConfiguredModelsExist(t *testing.T) {
	c := liveClient(t)
	ids, err := c.ListModels(liveCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tier := range []llm.Tier{llm.Nano, llm.Super, llm.Ultra} {
		if !slices.Contains(ids, c.Model(tier)) {
			t.Errorf("%s model %q not in catalog", tier, c.Model(tier))
		}
	}
	if !slices.Contains(ids, c.EmbeddingModel()) {
		t.Errorf("embedding model %q not in catalog", c.EmbeddingModel())
	}
}

func TestLiveEmbeddingsRankByMeaning(t *testing.T) {
	c := liveClient(t)
	vecs, err := c.Embed(liveCtx(t), []string{
		"Car insurance renews in March",
		"My vehicle policy expires soon",
		"The electricity bill is due on the 5th",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs[0]) == 0 || len(vecs[0]) != len(vecs[1]) || len(vecs[1]) != len(vecs[2]) {
		t.Fatalf("bad dimensions: %d, %d, %d", len(vecs[0]), len(vecs[1]), len(vecs[2]))
	}
	related, unrelated := cosine(vecs[0], vecs[1]), cosine(vecs[0], vecs[2])
	t.Logf("dims=%d related=%.3f unrelated=%.3f spend=$%.6f", len(vecs[0]), related, unrelated, c.Budget().Spent())
	if related <= unrelated {
		t.Errorf("related pair (%.3f) must score above unrelated pair (%.3f)", related, unrelated)
	}
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return dot / math.Sqrt(na*nb)
}

func TestLiveChatEachTier(t *testing.T) {
	c := liveClient(t)
	for _, tier := range []llm.Tier{llm.Nano, llm.Super, llm.Ultra} {
		t.Run(string(tier), func(t *testing.T) {
			resp, err := c.Chat(liveCtx(t), llm.Request{
				Tier:      tier,
				Messages:  []llm.Message{llm.User("Reply with the single word: pong")},
				MaxTokens: 512,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("model=%s finish=%s tokens=%d/%d cost=$%.6f content=%q",
				resp.Model, resp.FinishReason, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.CostUSD, resp.Message.Content)
			if resp.Model != c.Model(tier) {
				t.Errorf("served by %s, want %s (fallback happened)", resp.Model, c.Model(tier))
			}
			if !strings.Contains(strings.ToLower(resp.Message.Content), "pong") {
				t.Errorf("content = %q, want it to contain pong", resp.Message.Content)
			}
		})
	}
}

func TestLiveToolCallRoundTrip(t *testing.T) {
	c := liveClient(t)
	ctx := liveCtx(t)

	tool := llm.FunctionTool("get_due_date", "Look up the next due date of a bill by its name.",
		json.RawMessage(`{"type":"object","properties":{"bill":{"type":"string"}},"required":["bill"]}`))
	msgs := []llm.Message{
		llm.System("You manage the user's bills. Use tools to look up facts. Never guess dates."),
		llm.User("When is my electricity bill due?"),
	}

	first, err := c.Chat(ctx, llm.Request{Tier: llm.Super, Messages: msgs, Tools: []llm.Tool{tool}, MaxTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first: finish=%s content=%q calls=%+v", first.FinishReason, first.Message.Content, first.Message.ToolCalls)
	if len(first.Message.ToolCalls) == 0 {
		t.Fatal("model did not call the tool")
	}
	call := first.Message.ToolCalls[0]
	var args struct {
		Bill string `json:"bill"`
	}
	if call.Function.Name != "get_due_date" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args.Bill == "" {
		t.Fatalf("bad tool call: %+v", call)
	}

	msgs = append(msgs, first.Message, llm.ToolResult(call.ID, `{"bill":"electricity","due":"2026-10-21","amount":"INR 2,340"}`))
	final, err := c.Chat(ctx, llm.Request{Tier: llm.Super, Messages: msgs, Tools: []llm.Tool{tool}, MaxTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("final: %q (total spend $%.6f)", final.Message.Content, c.Budget().Spent())
	if !strings.Contains(final.Message.Content, "21") {
		t.Errorf("final answer does not mention the due date: %q", final.Message.Content)
	}
}
