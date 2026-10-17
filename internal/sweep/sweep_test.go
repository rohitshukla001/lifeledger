package sweep

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

var kolkata = mustLoad("Asia/Kolkata")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(local string) {
	t, err := time.ParseInLocation("2006-01-02 15:04", local, kolkata)
	if err != nil {
		panic(err)
	}
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

type recorder struct {
	mu       sync.Mutex
	messages []string
	fail     error
}

func (r *recorder) Send(_ context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.messages = append(r.messages, text)
	return nil
}

type fakeLLM struct {
	mu       sync.Mutex
	reply    string
	err      error
	requests []llm.Request
}

func (f *fakeLLM) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Model: "ultra", Message: llm.Message{Role: "assistant", Content: f.reply}}, nil
}

type rig struct {
	store *store.Store
	clock *clock
	sent  *recorder
	llm   *fakeLLM
	sweep *Sweeper
}

func newRig(t *testing.T, start string, review *Schedule) *rig {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "sweep.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c := &clock{}
	c.set(start)
	s.SetClock(c.now)
	r := &rig{store: s, clock: c, sent: &recorder{}, llm: &fakeLLM{reply: "All under control."}}
	r.sweep = New(Options{
		Store: s, LLM: r.llm, Notifier: r.sent, Location: kolkata, Currency: "INR",
		RemindHour: 9, Review: review, Now: c.now,
	})
	return r
}

func (r *rig) remindHourOff() { r.sweep.remindHour = 24 }

func (r *rig) add(t *testing.T, o store.Obligation) store.Obligation {
	t.Helper()
	if err := r.store.CreateObligation(context.Background(), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func (r *rig) run(t *testing.T, at string) Report {
	t.Helper()
	r.clock.set(at)
	rep, err := r.sweep.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("sweep at %s: %v", at, err)
	}
	return rep
}

func (r *rig) titles(t *testing.T) []string {
	t.Helper()
	all, err := r.store.ListNotifications(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(all) - 1; i >= 0; i-- {
		out = append(out, all[i].Title)
	}
	return out
}

func day(s string) *time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func amount(v int64) *int64 { return &v }

func TestRemindersFollowTheStages(t *testing.T) {
	r := newRig(t, "2026-10-12 10:00", nil)
	r.sweep.notifier = nil
	r.add(t, store.Obligation{Title: "Airtel broadband", Category: store.CategoryBill, AmountMinor: amount(99900), Currency: "INR", DueOn: day("2026-10-20"), Recurrence: store.RecurMonthly})

	steps := []struct {
		at  string
		new int
	}{
		{"2026-10-12 10:00", 0},
		{"2026-10-13 10:00", 1},
		{"2026-10-13 18:00", 0},
		{"2026-10-17 10:00", 1},
		{"2026-10-18 10:00", 0},
		{"2026-10-19 10:00", 1},
		{"2026-10-20 10:00", 1},
		{"2026-10-21 10:00", 1},
		{"2026-10-25 10:00", 0},
	}
	for _, st := range steps {
		if rep := r.run(t, st.at); rep.Reminders != st.new {
			t.Errorf("%s: %d new reminders, want %d", st.at, rep.Reminders, st.new)
		}
	}

	want := []string{
		"Due in 7 days: Airtel broadband",
		"Due in 3 days: Airtel broadband",
		"Due tomorrow: Airtel broadband",
		"Due today: Airtel broadband",
		"Overdue by 1 day: Airtel broadband",
	}
	if got := r.titles(t); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("titles = %q", got)
	}
	all, _ := r.store.ListNotifications(context.Background(), 1)
	if all[0].Body != "INR 999.00, due Tue 20 Oct 2026, repeats monthly, item #1." {
		t.Fatalf("body = %q", all[0].Body)
	}
}

func TestRemindersWaitForTheRemindHour(t *testing.T) {
	r := newRig(t, "2026-10-19 08:59", nil)
	r.add(t, store.Obligation{Title: "Rent", DueOn: day("2026-10-19")})

	if rep := r.run(t, "2026-10-19 08:59"); rep.Reminders != 0 || len(r.sent.messages) != 0 {
		t.Fatalf("before 09:00 local: %+v", rep)
	}
	if rep := r.run(t, "2026-10-19 09:00"); rep.Reminders != 1 || rep.Delivered != 1 {
		t.Fatalf("at 09:00 local: %+v", rep)
	}
}

func TestRemindersSkipItemsThatNeedNoReminder(t *testing.T) {
	r := newRig(t, "2026-10-19 10:00", nil)
	r.add(t, store.Obligation{Title: "Paid", DueOn: day("2026-10-20"), Status: store.StatusDone})
	r.add(t, store.Obligation{Title: "Cancelled", DueOn: day("2026-10-20"), Status: store.StatusCancelled})
	r.add(t, store.Obligation{Title: "Snoozed", DueOn: day("2026-10-20"), Status: store.StatusSnoozed, SnoozedUntil: day("2026-10-22")})
	r.add(t, store.Obligation{Title: "No date"})
	r.add(t, store.Obligation{Title: "Far away", DueOn: day("2026-11-30")})

	if rep := r.run(t, "2026-10-19 10:00"); rep.Reminders != 0 {
		t.Fatalf("reminders = %d, titles = %v", rep.Reminders, r.titles(t))
	}
}

func TestSnoozedItemWakesAndIsReminded(t *testing.T) {
	r := newRig(t, "2026-10-18 10:00", nil)
	o := r.add(t, store.Obligation{Title: "Water bill", DueOn: day("2026-10-19"), Status: store.StatusSnoozed, SnoozedUntil: day("2026-10-19")})

	if rep := r.run(t, "2026-10-18 10:00"); rep.Woken != 0 || rep.Reminders != 0 {
		t.Fatalf("before the snooze ends: %+v", rep)
	}
	if rep := r.run(t, "2026-10-19 00:30"); rep.Woken != 1 || rep.Reminders != 0 {
		t.Fatalf("after midnight: %+v", rep)
	}
	if rep := r.run(t, "2026-10-19 09:15"); rep.Reminders != 1 {
		t.Fatalf("at the remind hour: %+v", rep)
	}

	got, _ := r.store.GetObligation(context.Background(), o.ID)
	if got.Status != store.StatusOpen || got.SnoozedUntil != nil {
		t.Fatalf("obligation = %+v", got)
	}
	audit, _ := r.store.ListAudit(context.Background(), 0)
	if len(audit) != 1 || audit[0].Actor != "sweep" || audit[0].Event != "obligation_woken" || audit[0].Subject != "obligation:1" {
		t.Fatalf("audit = %+v", audit)
	}
}

func TestDeliveryGroupsReminders(t *testing.T) {
	r := newRig(t, "2026-10-19 10:00", nil)
	r.add(t, store.Obligation{Title: "Electricity", AmountMinor: amount(234050), DueOn: day("2026-10-20")})
	r.add(t, store.Obligation{Title: "Passport renewal", Category: store.CategoryDocument, DueOn: day("2026-10-19")})

	rep := r.run(t, "2026-10-19 10:00")
	if rep.Reminders != 2 || rep.Delivered != 2 || len(r.sent.messages) != 1 {
		t.Fatalf("report = %+v, messages = %q", rep, r.sent.messages)
	}
	want := "LifeLedger: 2 reminders\n\n" +
		"• Due today: Passport renewal\n  Due Mon 19 Oct 2026, item #2.\n" +
		"• Due tomorrow: Electricity\n  INR 2340.50, due Tue 20 Oct 2026, item #1."
	if r.sent.messages[0] != want {
		t.Fatalf("message =\n%s\nwant\n%s", r.sent.messages[0], want)
	}

	if rep := r.run(t, "2026-10-19 10:15"); rep.Delivered != 0 || len(r.sent.messages) != 1 {
		t.Fatalf("delivered notifications must not be sent again: %+v", rep)
	}

	r.add(t, store.Obligation{Title: "Gym", DueOn: day("2026-10-20")})
	r.run(t, "2026-10-19 10:30")
	if last := r.sent.messages[1]; last != "Due tomorrow: Gym\nDue Tue 20 Oct 2026, item #3." {
		t.Fatalf("single reminder = %q", last)
	}
}

func TestDeliveryRetriesThenGivesUp(t *testing.T) {
	r := newRig(t, "2026-10-19 10:00", nil)
	r.add(t, store.Obligation{Title: "Rent", DueOn: day("2026-10-19")})
	r.sent.fail = errors.New("telegram: 502 Bad Gateway")

	for i := range maxAttempts {
		rep := r.run(t, fmt.Sprintf("2026-10-19 %d:00", 10+i))
		if rep.Failed != 1 {
			t.Fatalf("attempt %d: %+v", i+1, rep)
		}
	}
	if rep := r.run(t, "2026-10-19 16:00"); rep.Failed != 0 {
		t.Fatalf("after %d attempts the notification must not be sent: %+v", maxAttempts, rep)
	}
	n, _ := r.store.ListNotifications(context.Background(), 0)
	if n[0].Attempts != maxAttempts || n[0].LastError != "telegram: 502 Bad Gateway" || n[0].DeliveredAt != nil {
		t.Fatalf("notification = %+v", n[0])
	}
}

func TestDeliveryRecoversAndSkipsOldNotifications(t *testing.T) {
	r := newRig(t, "2026-10-19 10:00", nil)
	r.add(t, store.Obligation{Title: "Rent", DueOn: day("2026-10-19")})
	r.sent.fail = errors.New("telegram: timeout")
	r.run(t, "2026-10-19 10:00")

	r.sent.fail = nil
	if rep := r.run(t, "2026-10-19 10:15"); rep.Delivered != 1 {
		t.Fatalf("retry: %+v", rep)
	}

	r.sweep.notifier = nil
	r.add(t, store.Obligation{Title: "Insurance", DueOn: day("2026-10-20")})
	r.run(t, "2026-10-19 11:00")
	r.sweep.notifier = r.sent
	rep := r.run(t, "2026-10-21 11:00")
	if rep.Delivered != 2 || len(r.sent.messages) != 2 {
		t.Fatalf("report = %+v, messages = %q", rep, r.sent.messages)
	}
	if msg := r.sent.messages[1]; strings.Contains(msg, "Due tomorrow: Insurance") || !strings.Contains(msg, "Overdue by 1 day: Insurance") {
		t.Fatalf("a reminder older than %s must not be sent: %q", retryWindow, msg)
	}
}

func TestRunStopsWhenTheContextEnds(t *testing.T) {
	r := newRig(t, "2026-10-19 10:00", nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.sweep.Run(ctx, time.Millisecond)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}
