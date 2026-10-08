package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var testModels = map[Tier]string{
	Nano:  "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B",
	Super: "nvidia/nemotron-3-super-120b-a12b",
	Ultra: "nvidia/Nemotron-3-Ultra-550b-a55b",
}

type fakeTF struct {
	t          *testing.T
	mu         sync.Mutex
	requests   []chatRequest
	reply      func(n int, req chatRequest) (int, string)
	embedCalls int
	embedReply func(inputs []string) (int, string)
}

func (f *fakeTF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
		f.t.Errorf("Authorization = %q", got)
	}
	if r.URL.Path == "/v1/embeddings" {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "Qwen/Qwen3-Embedding-8B" {
			f.t.Errorf("embedding model = %q", req.Model)
		}
		f.embedCalls++
		if f.embedReply != nil {
			status, body := f.embedReply(req.Input)
			w.WriteHeader(status)
			w.Write([]byte(body))
			return
		}
		var data []string
		for i := len(req.Input) - 1; i >= 0; i-- {
			data = append(data, fmt.Sprintf(`{"index":%d,"embedding":[%d,0.5]}`, i, len(req.Input[i])))
		}
		fmt.Fprintf(w, `{"data":[%s],"usage":{"prompt_tokens":1000000}}`, strings.Join(data, ","))
		return
	}
	if r.URL.Path == "/v1/models" {
		w.Write([]byte(`{"data":[{"id":"b-model"},{"id":"a-model"}]}`))
		return
	}
	if r.URL.Path != "/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Fatalf("decode request: %v", err)
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	n := len(f.requests)
	f.mu.Unlock()

	status, body := f.reply(n, req)
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func (f *fakeTF) models() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		out = append(out, r.Model)
	}
	return out
}

func okBody(content string) string {
	return `{"model":"x","choices":[{"message":{"role":"assistant","content":"` + content +
		`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000000,"completion_tokens":1000000}}`
}

func newTestClient(t *testing.T, reply func(int, chatRequest) (int, string), budget float64) (*Client, *fakeTF) {
	t.Helper()
	fake := &fakeTF{t: t, reply: reply}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	c, err := New(Options{BaseURL: srv.URL + "/v1", APIKey: "test-key", Models: testModels, EmbedModel: "Qwen/Qwen3-Embedding-8B", DailyBudgetUSD: budget})
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return c, fake
}

func TestChatSendsRequestAndParsesReply(t *testing.T) {
	c, fake := newTestClient(t, func(int, chatRequest) (int, string) { return 200, okBody("hi") }, 0)

	temp := 0.2
	resp, err := c.Chat(context.Background(), Request{
		Tier:        Nano,
		Messages:    []Message{System("be brief"), User("hello")},
		Tools:       []Tool{FunctionTool("noop", "does nothing", json.RawMessage(`{"type":"object"}`))},
		Temperature: &temp,
		MaxTokens:   64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Content != "hi" || resp.FinishReason != "stop" || resp.Model != testModels[Nano] {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if want := 0.30; resp.CostUSD != want {
		t.Fatalf("cost = %v, want %v", resp.CostUSD, want)
	}

	req := fake.requests[0]
	if req.Model != testModels[Nano] || len(req.Messages) != 2 || len(req.Tools) != 1 || req.MaxTokens != 64 || *req.Temperature != temp {
		t.Fatalf("unexpected request: %+v", req)
	}
}

func TestChatDefaultsToSuper(t *testing.T) {
	c, fake := newTestClient(t, func(int, chatRequest) (int, string) { return 200, okBody("ok") }, 0)
	if _, err := c.Chat(context.Background(), Request{Messages: []Message{User("x")}}); err != nil {
		t.Fatal(err)
	}
	if got := fake.models(); got[0] != testModels[Super] {
		t.Fatalf("model = %s, want super", got[0])
	}
}

func TestChatParsesToolCalls(t *testing.T) {
	body := `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
		{"id":"call_1","type":"function","function":{"name":"add_bill","arguments":"{\"amount\":42}"}}]},
		"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`
	c, _ := newTestClient(t, func(int, chatRequest) (int, string) { return 200, body }, 0)

	resp, err := c.Chat(context.Background(), Request{Messages: []Message{User("x")}})
	if err != nil {
		t.Fatal(err)
	}
	calls := resp.Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Name != "add_bill" || calls[0].Function.Arguments != `{"amount":42}` {
		t.Fatalf("tool calls = %+v", calls)
	}
}

func TestChatRetriesTransientErrors(t *testing.T) {
	c, fake := newTestClient(t, func(n int, _ chatRequest) (int, string) {
		switch n {
		case 1:
			return 503, `{"error":{"message":"overloaded"}}`
		case 2:
			return 429, `{"detail":"slow down"}`
		}
		return 200, okBody("ok")
	}, 0)

	if _, err := c.Chat(context.Background(), Request{Tier: Super, Messages: []Message{User("x")}}); err != nil {
		t.Fatal(err)
	}
	if got := fake.models(); len(got) != 3 || got[2] != testModels[Super] {
		t.Fatalf("calls = %v, want 3 calls to super", got)
	}
}

func TestChatFallsBackToLowerTier(t *testing.T) {
	c, fake := newTestClient(t, func(_ int, req chatRequest) (int, string) {
		if req.Model == testModels[Ultra] {
			return 500, "boom"
		}
		return 200, okBody("from super")
	}, 0)

	resp, err := c.Chat(context.Background(), Request{Tier: Ultra, Messages: []Message{User("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Model != testModels[Super] {
		t.Fatalf("model = %s, want super", resp.Model)
	}
	if got := fake.models(); len(got) != 4 {
		t.Fatalf("calls = %v, want 3 ultra attempts then super", got)
	}
}

func TestChatNanoHasNoFallback(t *testing.T) {
	c, fake := newTestClient(t, func(int, chatRequest) (int, string) { return 502, "bad gateway" }, 0)

	_, err := c.Chat(context.Background(), Request{Tier: Nano, Messages: []Message{User("x")}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 502 || apiErr.Message != "bad gateway" {
		t.Fatalf("err = %v", err)
	}
	if got := fake.models(); len(got) != 3 {
		t.Fatalf("calls = %d, want 3", len(got))
	}
}

func TestChatDoesNotRetryClientErrors(t *testing.T) {
	c, fake := newTestClient(t, func(int, chatRequest) (int, string) {
		return 400, `{"error":{"message":"bad tool schema"}}`
	}, 0)

	_, err := c.Chat(context.Background(), Request{Tier: Ultra, Messages: []Message{User("x")}})
	if err == nil || !strings.Contains(err.Error(), "bad tool schema") {
		t.Fatalf("err = %v", err)
	}
	if got := fake.models(); len(got) != 1 {
		t.Fatalf("calls = %d, want 1", len(got))
	}
}

func TestChatOutOfCredits(t *testing.T) {
	c, fake := newTestClient(t, func(int, chatRequest) (int, string) { return 402, `{"detail":"insufficient balance"}` }, 0)

	_, err := c.Chat(context.Background(), Request{Tier: Ultra, Messages: []Message{User("x")}})
	if !errors.Is(err, ErrOutOfCredits) {
		t.Fatalf("err = %v, want ErrOutOfCredits", err)
	}
	if got := fake.models(); len(got) != 1 {
		t.Fatalf("calls = %d, want 1", len(got))
	}
}

func TestChatStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, fake := newTestClient(t, func(int, chatRequest) (int, string) {
		cancel()
		return 503, "busy"
	}, 0)

	_, err := c.Chat(ctx, Request{Tier: Ultra, Messages: []Message{User("x")}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := fake.models(); len(got) != 1 {
		t.Fatalf("calls = %d, want 1", len(got))
	}
}

func TestChatEnforcesDailyBudget(t *testing.T) {
	c, fake := newTestClient(t, func(int, chatRequest) (int, string) { return 200, okBody("ok") }, 0.25)

	if _, err := c.Chat(context.Background(), Request{Tier: Nano, Messages: []Message{User("x")}}); err != nil {
		t.Fatal(err)
	}
	_, err := c.Chat(context.Background(), Request{Tier: Nano, Messages: []Message{User("x")}})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}
	if got := fake.models(); len(got) != 1 {
		t.Fatalf("calls = %d, want 1", len(got))
	}
}

func TestBudgetResetsEachUTCDay(t *testing.T) {
	now := time.Date(2026, 10, 5, 23, 0, 0, 0, time.UTC)
	b := NewBudget(1)
	b.now = func() time.Time { return now }

	b.add(1.5)
	if err := b.check(); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}
	now = now.Add(2 * time.Hour)
	if err := b.check(); err != nil {
		t.Fatalf("after midnight: %v", err)
	}
	if b.Spent() != 0 {
		t.Fatalf("spent = %v, want 0", b.Spent())
	}
}

func TestUnknownModelIsPricedConservatively(t *testing.T) {
	u := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	if got := priceFor("someone/else").cost(u); got != 4.0 {
		t.Fatalf("cost = %v, want ultra price 4.0", got)
	}
}

func TestListModelsIsSorted(t *testing.T) {
	c, _ := newTestClient(t, nil, 0)
	ids, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "a-model,b-model" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestNewValidatesOptions(t *testing.T) {
	cases := map[string]Options{
		"missing key":   {BaseURL: "https://x/v1", Models: testModels, EmbedModel: "e"},
		"bad url":       {BaseURL: "not a url", APIKey: "k", Models: testModels, EmbedModel: "e"},
		"missing model": {BaseURL: "https://x/v1", APIKey: "k", Models: map[Tier]string{Nano: "a", Super: "b"}, EmbedModel: "e"},
		"missing embed": {BaseURL: "https://x/v1", APIKey: "k", Models: testModels},
	}
	for name, o := range cases {
		if _, err := New(o); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestBackoffHonoursRetryAfter(t *testing.T) {
	err := &APIError{Status: 429, RetryAfter: 7 * time.Second}
	if got := backoff(1, err); got != 7*time.Second {
		t.Fatalf("backoff = %v, want 7s", got)
	}
	if got := backoff(2, &transportError{errors.New("reset")}); got < time.Second || got >= 1500*time.Millisecond {
		t.Fatalf("backoff = %v, want [1s, 1.5s)", got)
	}
}

func TestEmbedOrdersByIndexAndChargesBudget(t *testing.T) {
	c, fake := newTestClient(t, nil, 0)

	vecs, err := c.Embed(context.Background(), []string{"a", "bbb", "cc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 3 || vecs[0][0] != 1 || vecs[1][0] != 3 || vecs[2][0] != 2 {
		t.Fatalf("vectors out of order: %v", vecs)
	}
	if got := c.Budget().Spent(); got != 0.01 {
		t.Fatalf("spent = %v, want 0.01", got)
	}

	if vecs, err := c.Embed(context.Background(), nil); err != nil || vecs != nil || fake.embedCalls != 1 {
		t.Fatalf("empty input must not call the API: %v, %v, calls=%d", vecs, err, fake.embedCalls)
	}
}

func TestEmbedRejectsShortOrBrokenReplies(t *testing.T) {
	replies := map[string]string{
		"too few":   `{"data":[{"index":0,"embedding":[1]}]}`,
		"duplicate": `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`,
		"bad index": `{"data":[{"index":0,"embedding":[1]},{"index":7,"embedding":[2]}]}`,
	}
	for name, body := range replies {
		c, fake := newTestClient(t, nil, 0)
		fake.embedReply = func([]string) (int, string) { return 200, body }
		if _, err := c.Embed(context.Background(), []string{"x", "y"}); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestEmbedRetriesServerErrors(t *testing.T) {
	c, fake := newTestClient(t, nil, 0)
	fake.embedReply = func(in []string) (int, string) {
		if fake.embedCalls == 1 {
			return 503, "busy"
		}
		return 200, `{"data":[{"index":0,"embedding":[1,2]}]}`
	}
	if _, err := c.Embed(context.Background(), []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if fake.embedCalls != 2 {
		t.Fatalf("calls = %d, want 2", fake.embedCalls)
	}
}
