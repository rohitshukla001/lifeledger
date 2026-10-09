package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const (
	defaultMaxSteps     = 8
	defaultHistoryLimit = 40
	recallLimit         = 6
	replyMaxTokens      = 2048
	noAnswerReply       = "I could not finish that. Please try again with a little more detail."
)

type Chatter interface {
	Chat(ctx context.Context, req llm.Request) (*llm.Response, error)
}

type Options struct {
	LLM      Chatter
	Store    *store.Store
	Memory   *memory.Engine
	Location *time.Location
	Currency string
	Logger   *slog.Logger
	Now      func() time.Time
}

type Agent struct {
	llm          Chatter
	store        *store.Store
	memory       *memory.Engine
	loc          *time.Location
	currency     string
	log          *slog.Logger
	now          func() time.Time
	tools        *registry
	maxSteps     int
	historyLimit int
}

type ToolStep struct {
	Name      string
	Arguments string
	Result    string
	Failed    bool
}

type Result struct {
	ConversationID int64
	Reply          string
	Steps          []ToolStep
	Usage          llm.Usage
	CostUSD        float64
}

func New(o Options) *Agent {
	a := &Agent{
		llm:          o.LLM,
		store:        o.Store,
		memory:       o.Memory,
		loc:          o.Location,
		currency:     o.Currency,
		log:          o.Logger,
		now:          o.Now,
		maxSteps:     defaultMaxSteps,
		historyLimit: defaultHistoryLimit,
	}
	if a.loc == nil {
		a.loc = time.UTC
	}
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	if a.now == nil {
		a.now = time.Now
	}
	a.tools = newRegistry(
		a.rememberTool(),
		a.recallTool(),
		a.addObligationTool(),
		a.listObligationsTool(),
		a.updateObligationTool(),
	)
	return a
}

func (a *Agent) StartConversation(ctx context.Context, firstMessage string) (store.Conversation, error) {
	title := strings.Join(strings.Fields(firstMessage), " ")
	if utf8.RuneCountInString(title) > 60 {
		title = string([]rune(title)[:57]) + "..."
	}
	return a.store.CreateConversation(ctx, title)
}

func (a *Agent) Reply(ctx context.Context, conversationID int64, text string) (*Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("agent: message is empty")
	}
	if _, err := a.store.GetConversation(ctx, conversationID); err != nil {
		return nil, err
	}
	history, err := a.store.Messages(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	user := llm.User(text)
	msgs := append([]llm.Message{a.systemPrompt(ctx, text)}, trimHistory(history, a.historyLimit)...)
	msgs = append(msgs, user)
	turn := []llm.Message{user}
	res := &Result{ConversationID: conversationID}
	defs := a.tools.definitions()

	for step := 0; ; step++ {
		req := llm.Request{Tier: llm.Super, Messages: msgs, Tools: defs, MaxTokens: replyMaxTokens}
		final := step == a.maxSteps
		if final {
			req.Tools = nil
		}

		resp, err := a.llm.Chat(ctx, req)
		if err != nil {
			return nil, err
		}
		res.Usage.PromptTokens += resp.Usage.PromptTokens
		res.Usage.CompletionTokens += resp.Usage.CompletionTokens
		res.CostUSD += resp.CostUSD

		msg := resp.Message
		msg.Role = "assistant"
		if final {
			msg.ToolCalls = nil
		}
		if len(msg.ToolCalls) == 0 {
			msg.Content = strings.TrimSpace(msg.Content)
			if msg.Content == "" {
				msg.Content = noAnswerReply
			}
			turn = append(turn, msg)
			res.Reply = msg.Content
			break
		}

		for i := range msg.ToolCalls {
			if msg.ToolCalls[i].ID == "" {
				msg.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", step, i)
			}
			msg.ToolCalls[i].Type = "function"
		}
		msgs = append(msgs, msg)
		turn = append(turn, msg)

		for _, call := range msg.ToolCalls {
			out, err := a.tools.run(ctx, conversationID, call.Function)
			ts := ToolStep{Name: call.Function.Name, Arguments: call.Function.Arguments, Result: out}
			if err != nil {
				a.log.Debug("tool call failed", "tool", call.Function.Name, "err", err)
				b, _ := json.Marshal(map[string]string{"error": err.Error()})
				ts.Result, ts.Failed = string(b), true
			}
			res.Steps = append(res.Steps, ts)
			result := llm.ToolResult(call.ID, ts.Result)
			msgs = append(msgs, result)
			turn = append(turn, result)
		}
	}

	if err := a.store.AppendMessages(ctx, conversationID, turn...); err != nil {
		return nil, fmt.Errorf("agent: save conversation: %w", err)
	}
	return res, nil
}

func (a *Agent) today() time.Time {
	y, m, d := a.now().In(a.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (a *Agent) systemPrompt(ctx context.Context, userText string) llm.Message {
	today := a.today()
	var b strings.Builder
	fmt.Fprintf(&b, `You are LifeLedger, a private life-admin assistant for one person. You track their bills, subscriptions, renewals, warranties, documents, and deadlines, and you remember what they tell you.

Today is %s (%s). Time zone: %s. Default currency: %s.

Rules:
- Use tools for facts about the user's obligations. Never guess a date or an amount.
- When the user tells you a lasting fact or preference, save it with remember. Do not save small talk.
- When the user mentions something that is due, renews, or expires, record it with add_obligation. Call list_obligations first so that you do not add a duplicate.
- Use YYYY-MM-DD for dates in tool calls. Convert relative dates such as "next Friday" from today's date.
- If a required detail is missing, ask one short question.
- Keep replies short and concrete. Mention amounts with their currency.

What you already know about the user (from memory, may be incomplete):
`, today.Format("Monday, 2 January 2006"), today.Format(time.DateOnly), a.loc, a.currency)

	hits, err := a.memory.Recall(ctx, userText, recallLimit)
	if err != nil {
		a.log.Warn("memory recall failed", "err", err)
	}
	if len(hits) == 0 {
		b.WriteString("- Nothing relevant yet.\n")
	}
	for _, h := range hits {
		fmt.Fprintf(&b, "- [%s] %s (memory #%d)\n", h.Memory.Kind, h.Memory.Content, h.Memory.ID)
	}
	return llm.System(b.String())
}

func trimHistory(history []llm.Message, limit int) []llm.Message {
	if len(history) <= limit {
		return history
	}
	tail := history[len(history)-limit:]
	for i, m := range tail {
		if m.Role == "user" {
			return tail[i:]
		}
	}
	return nil
}
