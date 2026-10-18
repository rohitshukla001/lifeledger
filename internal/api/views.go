package api

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/money"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

type obligationJSON struct {
	ledger.View
	Source    string    `json:"source"`
	SourceRef string    `json:"source_ref,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *API) obligationView(o store.Obligation) obligationJSON {
	return obligationJSON{View: ledger.NewView(o, a.today()), Source: o.Source, SourceRef: o.SourceRef, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

type actionJSON struct {
	ID            int64           `json:"id"`
	Kind          string          `json:"kind"`
	ObligationID  *int64          `json:"obligation_id"`
	Payload       json.RawMessage `json:"payload"`
	Summary       string          `json:"summary"`
	Status        string          `json:"status"`
	NeedsApproval bool            `json:"needs_approval"`
	RequestedBy   string          `json:"requested_by"`
	SourceRef     string          `json:"source_ref,omitempty"`
	Result        string          `json:"result,omitempty"`
	CalendarURL   string          `json:"calendar_url,omitempty"`
	Error         string          `json:"error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	DecidedAt     *time.Time      `json:"decided_at"`
	ExecutedAt    *time.Time      `json:"executed_at"`
}

func actionView(a store.Action) actionJSON {
	v := actionJSON{
		ID:            a.ID,
		Kind:          a.Kind,
		ObligationID:  a.ObligationID,
		Payload:       json.RawMessage(a.Payload),
		Summary:       a.Summary,
		Status:        string(a.Status),
		NeedsApproval: actions.NeedsApproval(a.Kind),
		RequestedBy:   a.RequestedBy,
		SourceRef:     a.SourceRef,
		Result:        a.Result,
		Error:         a.Error,
		CreatedAt:     a.CreatedAt,
		DecidedAt:     a.DecidedAt,
		ExecutedAt:    a.ExecutedAt,
	}
	if a.Kind == actions.KindCalendarEvent && a.Status == store.ActionDone {
		v.Result = ""
		v.CalendarURL = fmt.Sprintf("/api/v1/actions/%d/calendar.ics", a.ID)
	}
	return v
}

type auditJSON struct {
	ID        int64     `json:"id"`
	Actor     string    `json:"actor"`
	Event     string    `json:"event"`
	Subject   string    `json:"subject"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type memoryJSON struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	SourceRef string    `json:"source_ref,omitempty"`
	Score     *float64  `json:"score,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func memoryView(m store.Memory) memoryJSON {
	return memoryJSON{ID: m.ID, Kind: string(m.Kind), Content: m.Content, Source: m.Source, SourceRef: m.SourceRef, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func hitView(h memory.Hit) memoryJSON {
	v := memoryView(h.Memory)
	score := math.Round(h.Score*1000) / 1000
	v.Score = &score
	return v
}

type conversationJSON struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func conversationView(c store.Conversation) conversationJSON {
	return conversationJSON{ID: c.ID, Title: c.Title, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

type toolCallJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type messageJSON struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

func messageView(m llm.Message) messageJSON {
	v := messageJSON{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
	for _, c := range m.ToolCalls {
		v.ToolCalls = append(v.ToolCalls, toolCallJSON{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
	}
	return v
}

type proposalJSON struct {
	ID           int64      `json:"id"`
	IngestionID  int64      `json:"ingestion_id"`
	Title        string     `json:"title"`
	Category     string     `json:"category"`
	Amount       string     `json:"amount,omitempty"`
	Currency     string     `json:"currency,omitempty"`
	DueOn        string     `json:"due_on,omitempty"`
	Recurrence   string     `json:"recurrence"`
	Notes        string     `json:"notes,omitempty"`
	Evidence     string     `json:"evidence,omitempty"`
	Confidence   float64    `json:"confidence"`
	DuplicateOf  *int64     `json:"duplicate_of"`
	Status       string     `json:"status"`
	ObligationID *int64     `json:"obligation_id"`
	CreatedAt    time.Time  `json:"created_at"`
	DecidedAt    *time.Time `json:"decided_at"`
}

func proposalView(p store.Proposal) proposalJSON {
	v := proposalJSON{
		ID:           p.ID,
		IngestionID:  p.IngestionID,
		Title:        p.Title,
		Category:     string(p.Category),
		Currency:     p.Currency,
		Recurrence:   string(p.Recurrence),
		Notes:        p.Notes,
		Evidence:     p.Evidence,
		Confidence:   p.Confidence,
		DuplicateOf:  p.DuplicateOf,
		Status:       string(p.Status),
		ObligationID: p.ObligationID,
		CreatedAt:    p.CreatedAt,
		DecidedAt:    p.DecidedAt,
	}
	if p.AmountMinor != nil {
		v.Amount = money.Format(*p.AmountMinor)
	}
	if p.DueOn != nil {
		v.DueOn = p.DueOn.Format(time.DateOnly)
	}
	return v
}

type ingestionJSON struct {
	ID         int64          `json:"id"`
	SourceKind string         `json:"source_kind"`
	SourceName string         `json:"source_name"`
	Summary    string         `json:"summary"`
	CreatedAt  time.Time      `json:"created_at"`
	Proposals  []proposalJSON `json:"proposals"`
	Reused     bool           `json:"reused"`
	Truncated  bool           `json:"truncated"`
}

func ingestionView(res *ingest.Result) ingestionJSON {
	v := ingestionJSON{
		ID:         res.Ingestion.ID,
		SourceKind: res.Ingestion.SourceKind,
		SourceName: res.Ingestion.SourceName,
		Summary:    res.Ingestion.Summary,
		CreatedAt:  res.Ingestion.CreatedAt,
		Proposals:  []proposalJSON{},
		Reused:     res.Reused,
		Truncated:  res.Truncated,
	}
	for _, p := range res.Proposals {
		v.Proposals = append(v.Proposals, proposalView(p))
	}
	return v
}

type notificationJSON struct {
	ID           int64      `json:"id"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	ObligationID *int64     `json:"obligation_id"`
	Attempts     int        `json:"attempts"`
	LastError    string     `json:"last_error,omitempty"`
	DeliveredAt  *time.Time `json:"delivered_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func notificationView(n store.Notification) notificationJSON {
	return notificationJSON{
		ID: n.ID, Kind: string(n.Kind), Title: n.Title, Body: n.Body, ObligationID: n.ObligationID,
		Attempts: n.Attempts, LastError: n.LastError, DeliveredAt: n.DeliveredAt, CreatedAt: n.CreatedAt,
	}
}

func mapSlice[T, V any](in []T, f func(T) V) []V {
	out := make([]V, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}
