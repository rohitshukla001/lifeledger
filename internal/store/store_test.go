package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "nested", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	clock := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	return s
}

func date(s string) *time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestOpenMigratesOnceAndReopens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ll.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateObligation(ctx, &Obligation{Title: "Rent"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if v, err := s.SchemaVersion(ctx); err != nil || v != 3 {
		t.Fatalf("schema version = %d, %v; want 3", v, err)
	}
	var journal string
	var foreignKeys int
	s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal)
	s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys)
	if journal != "wal" || foreignKeys != 1 {
		t.Fatalf("journal_mode = %q, foreign_keys = %d; want wal, 1", journal, foreignKeys)
	}
	if got, err := s.ListObligations(ctx, ObligationFilter{}); err != nil || len(got) != 1 {
		t.Fatalf("data lost across reopen: %v, %v", got, err)
	}
}

func TestFailedMigrationIsRolledBack(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	bad := fstest.MapFS{
		"migrations/0004_ok_then_broken.sql": {Data: []byte(`CREATE TABLE half (id INTEGER); INSERT INTO nope VALUES (1);`)},
	}
	if err := migrate(ctx, s.db, bad); err == nil {
		t.Fatal("want error from broken migration")
	}
	if v, _ := s.SchemaVersion(ctx); v != 3 {
		t.Fatalf("schema version = %d, want 3", v)
	}
	var n int
	s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 'half'`).Scan(&n)
	if n != 0 {
		t.Fatal("table from failed migration was left behind")
	}
}

func TestMigrationNeedsNumericPrefix(t *testing.T) {
	s := openTest(t)
	err := migrate(context.Background(), s.db, fstest.MapFS{"migrations/init.sql": {Data: []byte(`SELECT 1`)}})
	if err == nil {
		t.Fatal("want error for migration without numeric prefix")
	}
}

func TestObligationLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	amount := int64(234000)
	o := &Obligation{
		Title:       "Electricity bill",
		Category:    CategoryBill,
		AmountMinor: &amount,
		Currency:    "INR",
		DueOn:       date("2026-10-21"),
		Recurrence:  RecurMonthly,
		Source:      "ingest",
		SourceRef:   "email:abc",
	}
	if err := s.CreateObligation(ctx, o); err != nil {
		t.Fatal(err)
	}
	if o.ID == 0 || o.Status != StatusOpen || o.CreatedAt.IsZero() {
		t.Fatalf("defaults not applied: %+v", o)
	}

	got, err := s.GetObligation(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != o.Title || *got.AmountMinor != amount || got.Currency != "INR" ||
		!got.DueOn.Equal(*o.DueOn) || got.Recurrence != RecurMonthly || got.SnoozedUntil != nil {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	got.Status = StatusSnoozed
	got.SnoozedUntil = date("2026-10-18")
	if err := s.UpdateObligation(ctx, &got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetObligation(ctx, o.ID)
	if again.Status != StatusSnoozed || again.SnoozedUntil.Format(time.DateOnly) != "2026-10-18" || !again.UpdatedAt.After(again.CreatedAt) {
		t.Fatalf("update not saved: %+v", again)
	}

	if err := s.DeleteObligation(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetObligation(ctx, o.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if err := s.DeleteObligation(ctx, o.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := s.UpdateObligation(ctx, &again); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
}

func TestObligationOptionalFieldsStayEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	o := &Obligation{Title: "Passport copy", Category: CategoryDocument}
	if err := s.CreateObligation(ctx, o); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetObligation(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AmountMinor != nil || got.Currency != "" || got.DueOn != nil || got.Source != "user" {
		t.Fatalf("unexpected values: %+v", got)
	}
}

func TestObligationConstraints(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	negative := int64(-1)
	cases := map[string]*Obligation{
		"empty title":    {Title: ""},
		"bad category":   {Title: "x", Category: "tax"},
		"bad currency":   {Title: "x", Currency: "RUPEE"},
		"negative money": {Title: "x", AmountMinor: &negative},
		"bad recurrence": {Title: "x", Recurrence: "daily"},
	}
	for name, o := range cases {
		if err := s.CreateObligation(ctx, o); err == nil {
			t.Errorf("%s: want constraint error", name)
		}
	}
}

func TestListObligationsFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	seed := []*Obligation{
		{Title: "Insurance", Category: CategoryRenewal, DueOn: date("2026-11-30")},
		{Title: "Warranty card", Category: CategoryWarranty},
		{Title: "Rent", Category: CategoryBill, DueOn: date("2026-10-10")},
		{Title: "Old phone bill", Category: CategoryBill, DueOn: date("2026-09-01"), Status: StatusDone},
		{Title: "Internet", Category: CategoryBill, DueOn: date("2026-10-15"), Status: StatusSnoozed},
	}
	for _, o := range seed {
		if err := s.CreateObligation(ctx, o); err != nil {
			t.Fatal(err)
		}
	}

	titles := func(f ObligationFilter) []string {
		t.Helper()
		got, err := s.ListObligations(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, o := range got {
			out = append(out, o.Title)
		}
		return out
	}
	assert := func(name string, got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", name, got, want)
			}
		}
	}

	assert("all, undated last", titles(ObligationFilter{}),
		"Old phone bill", "Rent", "Internet", "Insurance", "Warranty card")
	assert("active", titles(ObligationFilter{Statuses: []Status{StatusOpen, StatusSnoozed}}),
		"Rent", "Internet", "Insurance", "Warranty card")
	assert("open bills", titles(ObligationFilter{Statuses: []Status{StatusOpen}, Category: CategoryBill}),
		"Rent")
	assert("due by Oct 15", titles(ObligationFilter{Statuses: []Status{StatusOpen, StatusSnoozed}, DueBefore: date("2026-10-15")}),
		"Rent", "Internet")
	assert("limit", titles(ObligationFilter{Limit: 2}),
		"Old phone bill", "Rent")
}

func TestMemoryCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	first := &Memory{Content: "Electricity provider is BESCOM"}
	second := &Memory{Kind: MemoryPreference, Content: "Remind me 3 days before a bill is due", Source: "chat", SourceRef: "conversation:1"}
	for _, m := range []*Memory{first, second} {
		if err := s.AddMemory(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if first.Kind != MemoryFact || first.Source != "user" {
		t.Fatalf("defaults not applied: %+v", first)
	}

	if err := s.UpdateMemoryContent(ctx, first.ID, "Electricity provider is BESCOM, account 1234"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMemory(ctx, first.ID)
	if err != nil || got.Content != "Electricity provider is BESCOM, account 1234" {
		t.Fatalf("get after update: %+v, %v", got, err)
	}

	list, err := s.ListMemories(ctx, 0)
	if err != nil || len(list) != 2 || list[0].ID != first.ID {
		t.Fatalf("list should be newest-updated first: %+v, %v", list, err)
	}

	if err := s.DeleteMemory(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMemory(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
	if err := s.UpdateMemoryContent(ctx, second.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update deleted: %v", err)
	}
	if err := s.AddMemory(ctx, &Memory{Content: ""}); err == nil {
		t.Fatal("empty memory content must be rejected")
	}
}

func TestConversationMessagesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	c, err := s.CreateConversation(ctx, "Bills")
	if err != nil {
		t.Fatal(err)
	}
	call := llm.ToolCall{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "list_bills", Arguments: `{"month":"2026-10"}`}}
	sent := []llm.Message{
		llm.System("You manage bills."),
		llm.User("What is due this month?"),
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		llm.ToolResult("call_1", `[{"title":"Rent"}]`),
		{Role: "assistant", Content: "Rent is due on the 10th."},
	}
	if err := s.AppendMessages(ctx, c.ID, sent[:2]...); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(ctx, c.ID, sent[2:]...); err != nil {
		t.Fatal(err)
	}

	got, err := s.Messages(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(sent) {
		t.Fatalf("got %d messages, want %d", len(got), len(sent))
	}
	if got[2].ToolCalls[0] != call || got[3].ToolCallID != "call_1" || got[4].Content != sent[4].Content {
		t.Fatalf("messages changed in storage: %+v", got)
	}

	reloaded, err := s.GetConversation(ctx, c.ID)
	if err != nil || !reloaded.UpdatedAt.After(c.UpdatedAt) {
		t.Fatalf("append must bump updated_at: %+v, %v", reloaded, err)
	}
}

func TestAppendToMissingConversation(t *testing.T) {
	s := openTest(t)
	err := s.AppendMessages(context.Background(), 404, llm.User("hello"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteConversationCascades(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	older, _ := s.CreateConversation(ctx, "older")
	newer, _ := s.CreateConversation(ctx, "newer")
	if err := s.AppendMessages(ctx, older.ID, llm.User("hi")); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListConversations(ctx, 0)
	if err != nil || len(list) != 2 || list[0].ID != older.ID {
		t.Fatalf("recently active conversation must come first: %+v, %v", list, err)
	}

	if err := s.DeleteConversation(ctx, older.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d messages left after deleting conversation", n)
	}
	if _, err := s.GetConversation(ctx, newer.ID); err != nil {
		t.Fatalf("other conversation affected: %v", err)
	}
}

func TestMemoryEmbeddings(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	a := &Memory{Content: "Car insurance renews in March"}
	b := &Memory{Content: "Prefers WhatsApp reminders"}
	for _, m := range []*Memory{a, b} {
		if err := s.AddMemory(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	missing, err := s.MemoriesWithoutEmbedding(ctx, "embed-v1", 10)
	if err != nil || len(missing) != 2 {
		t.Fatalf("missing = %v, %v; want both", missing, err)
	}

	vec := []float32{0.25, -1.5, 3e-7}
	if err := s.SetMemoryEmbedding(ctx, a.ID, "embed-v1", vec); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMemoryEmbedding(ctx, b.ID, "embed-v0", []float32{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMemoryEmbedding(ctx, 999, "embed-v1", vec); !errors.Is(err, ErrNotFound) {
		t.Fatalf("embedding for missing memory: %v", err)
	}

	got, err := s.MemoryEmbeddings(ctx, "embed-v1")
	if err != nil || len(got) != 1 || len(got[a.ID]) != 3 || got[a.ID][0] != 0.25 || got[a.ID][1] != -1.5 || got[a.ID][2] != 3e-7 {
		t.Fatalf("embeddings = %v, %v", got, err)
	}
	if missing, _ := s.MemoriesWithoutEmbedding(ctx, "embed-v1", 10); len(missing) != 1 || missing[0].ID != b.ID {
		t.Fatalf("an embedding from another model must count as missing: %v", missing)
	}

	if err := s.UpdateMemoryContent(ctx, a.ID, "Car insurance renews in April"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MemoryEmbeddings(ctx, "embed-v1"); len(got) != 0 {
		t.Fatal("editing the content must clear the stale embedding")
	}
}

func TestDecodeVectorRejectsTruncatedBlob(t *testing.T) {
	if _, err := decodeVector([]byte{1, 2, 3}); err == nil {
		t.Fatal("want error for 3-byte blob")
	}
}

func TestIngestionAndProposals(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	amount := int64(64900)
	in := &Ingestion{SourceKind: "pdf", SourceName: "netflix.pdf", ContentHash: "abc", Summary: "Netflix receipt"}
	props, err := s.CreateIngestion(ctx, in, []Proposal{
		{Title: "Netflix", Category: CategorySubscription, AmountMinor: &amount, Currency: "INR", DueOn: date("2026-11-01"), Recurrence: RecurMonthly, Evidence: "Next billing date: 1 Nov", Confidence: 0.9},
		{Title: "Card expiry", Category: CategoryDeadline, Recurrence: RecurNone, Confidence: 0.4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.ID == 0 || props[0].ID == 0 || props[1].Status != ProposalPending || props[0].IngestionID != in.ID {
		t.Fatalf("ingestion = %+v, proposals = %+v", in, props)
	}

	if got, err := s.IngestionByHash(ctx, "abc"); err != nil || got.ID != in.ID || got.SourceName != "netflix.pdf" {
		t.Fatalf("by hash: %+v, %v", got, err)
	}
	if _, err := s.IngestionByHash(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing hash: %v", err)
	}
	if _, err := s.CreateIngestion(ctx, &Ingestion{SourceKind: "text", ContentHash: "abc"}, nil); err == nil {
		t.Fatal("duplicate hash must be rejected")
	}

	o, err := s.AcceptProposal(ctx, props[0].ID, "ingest")
	if err != nil {
		t.Fatal(err)
	}
	if o.Title != "Netflix" || *o.AmountMinor != amount || o.Status != StatusOpen || o.SourceRef != fmt.Sprintf("ingestion:%d", in.ID) {
		t.Fatalf("obligation = %+v", o)
	}
	accepted, _ := s.GetProposal(ctx, props[0].ID)
	if accepted.Status != ProposalAccepted || *accepted.ObligationID != o.ID || accepted.DecidedAt == nil {
		t.Fatalf("accepted = %+v", accepted)
	}
	if _, err := s.AcceptProposal(ctx, props[0].ID, "ingest"); !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("second accept: %v", err)
	}

	if err := s.RejectProposal(ctx, props[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RejectProposal(ctx, props[1].ID); !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("second reject: %v", err)
	}
	if err := s.RejectProposal(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reject missing: %v", err)
	}
	if _, err := s.AcceptProposal(ctx, 999, "ingest"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("accept missing: %v", err)
	}

	pending, _ := s.ListProposals(ctx, ProposalFilter{Status: ProposalPending})
	all, _ := s.ListProposals(ctx, ProposalFilter{IngestionID: in.ID})
	if len(pending) != 0 || len(all) != 2 {
		t.Fatalf("pending = %d, all = %d", len(pending), len(all))
	}

	if err := s.DeleteObligation(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetProposal(ctx, props[0].ID); again.ObligationID != nil {
		t.Fatal("deleting the obligation must clear the proposal link")
	}
}

func TestCreateIngestionIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, err := s.CreateIngestion(ctx, &Ingestion{SourceKind: "text", ContentHash: "h"}, []Proposal{
		{Title: "ok", Category: CategoryBill, Recurrence: RecurNone, Confidence: 1},
		{Title: "bad", Category: "tax", Recurrence: RecurNone, Confidence: 1},
	})
	if err == nil {
		t.Fatal("want error for bad proposal")
	}
	if _, err := s.IngestionByHash(ctx, "h"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a failed ingestion must leave nothing behind")
	}
}
