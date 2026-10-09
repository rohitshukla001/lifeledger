package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

type scriptedLLM struct {
	mu       sync.Mutex
	script   []func(req llm.Request) (*llm.Response, error)
	requests []llm.Request
}

func (s *scriptedLLM) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if len(s.script) == 0 {
		return nil, errors.New("scriptedLLM: no more responses")
	}
	next := s.script[0]
	s.script = s.script[1:]
	return next(req)
}

func say(text string) func(llm.Request) (*llm.Response, error) {
	return func(llm.Request) (*llm.Response, error) {
		return &llm.Response{Message: llm.Message{Role: "assistant", Content: text}, Usage: llm.Usage{PromptTokens: 100, CompletionTokens: 10}, CostUSD: 0.001}, nil
	}
}

func callTool(name, args string) func(llm.Request) (*llm.Response, error) {
	return func(llm.Request) (*llm.Response, error) {
		return &llm.Response{
			Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
				ID: "call_" + name, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args},
			}}},
			Usage:   llm.Usage{PromptTokens: 100, CompletionTokens: 20},
			CostUSD: 0.002,
		}, nil
	}
}

type fixture struct {
	agent *Agent
	store *store.Store
	llm   *scriptedLLM
	conv  store.Conversation
}

func newFixture(t *testing.T, script ...func(llm.Request) (*llm.Response, error)) *fixture {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	fake := &scriptedLLM{script: script}
	kolkata, _ := time.LoadLocation("Asia/Kolkata")
	a := New(Options{
		LLM:      fake,
		Store:    s,
		Memory:   memory.New(s, nil, nil),
		Location: kolkata,
		Currency: "INR",
		Now:      func() time.Time { return time.Date(2026, 10, 9, 20, 0, 0, 0, time.UTC) },
	})
	conv, err := a.StartConversation(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{agent: a, store: s, llm: fake, conv: conv}
}

func (f *fixture) reply(t *testing.T, text string) *Result {
	t.Helper()
	res, err := f.agent.Reply(context.Background(), f.conv.ID, text)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPlainReplyIsSavedWithContext(t *testing.T) {
	f := newFixture(t, say("Hi! How can I help with your bills?"))

	res := f.reply(t, "hi there")
	if res.Reply != "Hi! How can I help with your bills?" || len(res.Steps) != 0 || res.CostUSD != 0.001 {
		t.Fatalf("unexpected result: %+v", res)
	}

	req := f.llm.requests[0]
	system := req.Messages[0]
	if system.Role != "system" || !strings.Contains(system.Content, "Saturday, 10 October 2026 (2026-10-10)") ||
		!strings.Contains(system.Content, "Asia/Kolkata") || !strings.Contains(system.Content, "Default currency: INR") {
		t.Fatalf("system prompt must carry the local date, time zone, and currency:\n%s", system.Content)
	}
	if req.Tier != llm.Super || len(req.Tools) != 5 {
		t.Fatalf("tier = %s, tools = %d", req.Tier, len(req.Tools))
	}

	saved, _ := f.store.Messages(context.Background(), f.conv.ID)
	if len(saved) != 2 || saved[0].Content != "hi there" || saved[1].Role != "assistant" {
		t.Fatalf("saved = %+v", saved)
	}
}

func TestToolRoundTripCreatesObligation(t *testing.T) {
	f := newFixture(t,
		callTool("add_obligation", `{"title":"Electricity bill","category":"bill","amount":"2,340.50","due_on":"2026-10-21","recurrence":"monthly"}`),
		func(req llm.Request) (*llm.Response, error) {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "call_add_obligation" {
				t.Errorf("tool result not sent back: %+v", last)
			}
			var v obligationView
			if err := json.Unmarshal([]byte(last.Content), &v); err != nil || v.Amount != "2340.50" || v.Currency != "INR" || *v.DaysUntilDue != 11 {
				t.Errorf("tool result = %s", last.Content)
			}
			return say("Saved: electricity bill of INR 2,340.50, due 21 October.")(req)
		},
	)

	res := f.reply(t, "My electricity bill of 2340.50 is due on the 21st, every month")
	if len(res.Steps) != 1 || res.Steps[0].Failed || res.Usage.PromptTokens != 200 {
		t.Fatalf("result = %+v", res)
	}

	items, _ := f.store.ListObligations(context.Background(), store.ObligationFilter{})
	if len(items) != 1 || *items[0].AmountMinor != 234050 || items[0].Source != "chat" ||
		items[0].SourceRef != "conversation:1" || items[0].Recurrence != store.RecurMonthly {
		t.Fatalf("obligations = %+v", items)
	}

	saved, _ := f.store.Messages(context.Background(), f.conv.ID)
	var roles []string
	for _, m := range saved {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,tool,assistant" {
		t.Fatalf("saved roles = %v", roles)
	}
}

func TestMemoryCarriesAcrossConversations(t *testing.T) {
	f := newFixture(t,
		callTool("remember", `{"content":"Car insurance is with Acko","kind":"fact"}`),
		say("Noted."),
		say("Your car insurance is with Acko."),
	)
	f.reply(t, "FYI my car insurance is with Acko")

	next, err := f.agent.StartConversation(context.Background(), "Who insures my car?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.agent.Reply(context.Background(), next.ID, "Who insures my car?"); err != nil {
		t.Fatal(err)
	}

	req := f.llm.requests[2]
	if !strings.Contains(req.Messages[0].Content, "[fact] Car insurance is with Acko (memory #1)") {
		t.Fatalf("new conversation did not get the memory:\n%s", req.Messages[0].Content)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("new conversation must not include old messages, got %d", len(req.Messages))
	}
}

func TestToolErrorsGoBackToTheModel(t *testing.T) {
	f := newFixture(t,
		func(llm.Request) (*llm.Response, error) {
			return &llm.Response{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{
				{ID: "a", Function: llm.FunctionCall{Name: "add_obligation", Arguments: `{"title":"Rent","due_on":"next week"}`}},
				{ID: "b", Function: llm.FunctionCall{Name: "pay_bill", Arguments: `{}`}},
				{ID: "c", Function: llm.FunctionCall{Name: "recall", Arguments: `{not json`}},
				{ID: "d", Function: llm.FunctionCall{Name: "update_obligation", Arguments: `{"id":99,"status":"done"}`}},
			}}}, nil
		},
		say("Which date is the rent due?"),
	)

	res := f.reply(t, "add my rent")
	want := []string{"due_on must be YYYY-MM-DD", "unknown tool", "not valid JSON", "no obligation with id 99"}
	if len(res.Steps) != len(want) {
		t.Fatalf("steps = %+v", res.Steps)
	}
	for i, w := range want {
		if !res.Steps[i].Failed || !strings.Contains(res.Steps[i].Result, w) {
			t.Errorf("step %d = %+v, want error containing %q", i, res.Steps[i], w)
		}
	}
	if res.Reply != "Which date is the rent due?" {
		t.Fatalf("reply = %q", res.Reply)
	}
}

func TestStepLimitForcesAnAnswer(t *testing.T) {
	var script []func(llm.Request) (*llm.Response, error)
	for range defaultMaxSteps + 1 {
		script = append(script, callTool("list_obligations", `{}`))
	}
	f := newFixture(t, script...)

	res := f.reply(t, "loop forever")
	if len(f.llm.requests) != defaultMaxSteps+1 {
		t.Fatalf("calls = %d, want %d", len(f.llm.requests), defaultMaxSteps+1)
	}
	if last := f.llm.requests[defaultMaxSteps]; last.Tools != nil {
		t.Fatal("the final call must not offer tools")
	}
	if res.Reply != noAnswerReply {
		t.Fatalf("reply = %q", res.Reply)
	}

	saved, _ := f.store.Messages(context.Background(), f.conv.ID)
	if final := saved[len(saved)-1]; final.Role != "assistant" || len(final.ToolCalls) != 0 {
		t.Fatalf("history must end with a plain answer so the next turn is valid: %+v", final)
	}
}

func TestModelFailureSavesNothing(t *testing.T) {
	f := newFixture(t, func(llm.Request) (*llm.Response, error) { return nil, llm.ErrBudgetExceeded })

	if _, err := f.agent.Reply(context.Background(), f.conv.ID, "hello"); !errors.Is(err, llm.ErrBudgetExceeded) {
		t.Fatalf("err = %v", err)
	}
	if saved, _ := f.store.Messages(context.Background(), f.conv.ID); len(saved) != 0 {
		t.Fatalf("saved = %+v", saved)
	}
}

func TestReplyRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	if _, err := f.agent.Reply(context.Background(), f.conv.ID, "   "); err == nil {
		t.Fatal("want error for empty message")
	}
	if _, err := f.agent.Reply(context.Background(), 404, "hi"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestHistoryIsTrimmedAtAUserTurn(t *testing.T) {
	f := newFixture(t, say("ok"))
	f.agent.historyLimit = 3

	ctx := context.Background()
	f.store.AppendMessages(ctx, f.conv.ID,
		llm.User("first"),
		llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "x", Type: "function", Function: llm.FunctionCall{Name: "recall", Arguments: "{}"}}}},
		llm.ToolResult("x", "{}"),
		llm.Message{Role: "assistant", Content: "done"},
		llm.User("second"),
		llm.Message{Role: "assistant", Content: "sure"},
	)
	f.reply(t, "third")

	var got []string
	for _, m := range f.llm.requests[0].Messages[1:] {
		got = append(got, m.Role+":"+m.Content)
	}
	if strings.Join(got, "|") != "user:second|assistant:sure|user:third" {
		t.Fatalf("history sent = %v", got)
	}
}

func TestStartConversationTitle(t *testing.T) {
	f := newFixture(t)
	c, err := f.agent.StartConversation(context.Background(), "  please   track my\nphone bill and also the broadband bill and the water bill too  ")
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(c.Title) != 60 || !strings.HasPrefix(c.Title, "please track my phone bill") || !strings.HasSuffix(c.Title, "...") {
		t.Fatalf("title = %q", c.Title)
	}
}
