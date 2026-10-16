package actions

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

type sentMail struct{ to, subject, body string }

type fakeMailer struct {
	sent []sentMail
	err  error
}

func (f *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentMail{to, subject, body})
	return nil
}

func newService(t *testing.T, mailer Mailer) (*Service, *store.Store) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "actions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(Options{
		Store:  db,
		Mailer: mailer,
		Now:    func() time.Time { return time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) },
	}), db
}

func obligation(t *testing.T, db *store.Store, o store.Obligation) *int64 {
	t.Helper()
	if err := db.CreateObligation(context.Background(), &o); err != nil {
		t.Fatal(err)
	}
	return &o.ID
}

func ptr[T any](v T) *T { return &v }

func auditEvents(t *testing.T, db *store.Store) string {
	t.Helper()
	entries, err := db.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(entries) - 1; i >= 0; i-- {
		out = append(out, entries[i].Actor+":"+entries[i].Event)
	}
	return strings.Join(out, " ")
}

func TestMarkPaidRunsAtOnceAndAdvancesRecurringBill(t *testing.T) {
	svc, db := newService(t, nil)
	ctx := context.Background()
	id := obligation(t, db, store.Obligation{Title: "Rent", DueOn: ptr(day("2026-10-10")), Recurrence: store.RecurMonthly,
		Status: store.StatusSnoozed, SnoozedUntil: ptr(day("2026-10-08"))})

	a, err := svc.Request(ctx, Request{Kind: KindMarkPaid, ObligationID: id, RequestedBy: ActorAgent, SourceRef: "conversation:3"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != store.ActionDone || a.Result != "next due 2026-11-10" || a.Summary != `Mark "Rent" paid; next due 2026-11-10` {
		t.Fatalf("action = %+v", a)
	}
	o, _ := db.GetObligation(ctx, *id)
	if o.DueOn.Format(time.DateOnly) != "2026-11-10" || o.Status != store.StatusOpen || o.SnoozedUntil != nil {
		t.Fatalf("obligation = %+v", o)
	}
	if got := auditEvents(t, db); got != "agent:action.requested policy:action.approved policy:action.done" {
		t.Fatalf("audit = %s", got)
	}
}

func TestMarkPaidClosesOneOffAndRefusesDone(t *testing.T) {
	svc, db := newService(t, nil)
	ctx := context.Background()
	id := obligation(t, db, store.Obligation{Title: "Traffic fine", DueOn: ptr(day("2026-10-20"))})

	a, err := svc.Request(ctx, Request{Kind: KindMarkPaid, ObligationID: id, RequestedBy: ActorUser})
	if err != nil || a.Result != "marked done" {
		t.Fatalf("action = %+v, %v", a, err)
	}
	if o, _ := db.GetObligation(ctx, *id); o.Status != store.StatusDone {
		t.Fatalf("status = %s", o.Status)
	}
	if _, err := svc.Request(ctx, Request{Kind: KindMarkPaid, ObligationID: id, RequestedBy: ActorUser}); err == nil || !strings.Contains(err.Error(), "already done") {
		t.Fatalf("second mark paid: %v", err)
	}
}

func TestSnoozeValidatesDate(t *testing.T) {
	svc, db := newService(t, nil)
	ctx := context.Background()
	id := obligation(t, db, store.Obligation{Title: "Broadband", DueOn: ptr(day("2026-10-25"))})

	for payload, want := range map[string]string{
		`{"until":"2026-10-16"}`: "after today",
		`{"until":"next week"}`:  "YYYY-MM-DD",
		`{"until":`:              "invalid payload",
	} {
		if _, err := svc.Request(ctx, Request{Kind: KindSnooze, ObligationID: id, Payload: json.RawMessage(payload), RequestedBy: ActorUser}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", payload, err, want)
		}
	}

	a, err := svc.Request(ctx, Request{Kind: KindSnooze, ObligationID: id, Payload: json.RawMessage(`{"until":" 2026-10-22 "}`), RequestedBy: ActorUser})
	if err != nil || a.Status != store.ActionDone || a.Payload != `{"until":"2026-10-22"}` {
		t.Fatalf("snooze = %+v, %v", a, err)
	}
	if o, _ := db.GetObligation(ctx, *id); o.Status != store.StatusSnoozed || o.SnoozedUntil.Format(time.DateOnly) != "2026-10-22" {
		t.Fatalf("obligation = %+v", o)
	}
}

func TestCalendarEvent(t *testing.T) {
	svc, db := newService(t, nil)
	ctx := context.Background()
	withDate := obligation(t, db, store.Obligation{Title: "Car insurance", DueOn: ptr(day("2027-03-15")), Recurrence: store.RecurYearly})
	noDate := obligation(t, db, store.Obligation{Title: "Warranty card"})

	a, err := svc.Request(ctx, Request{Kind: KindCalendarEvent, ObligationID: withDate, RequestedBy: ActorAgent})
	if err != nil || a.Status != store.ActionDone || !strings.Contains(a.Result, "DTSTART;VALUE=DATE:20270315") ||
		!strings.Contains(a.Result, "TRIGGER;RELATED=START:-PT15H") || !strings.Contains(a.Result, "RRULE:FREQ=YEARLY") {
		t.Fatalf("calendar = %+v, %v", a, err)
	}
	if _, err := svc.Request(ctx, Request{Kind: KindCalendarEvent, ObligationID: noDate, RequestedBy: ActorAgent}); err == nil || !strings.Contains(err.Error(), "no due date") {
		t.Fatalf("no due date: %v", err)
	}
	if _, err := svc.Request(ctx, Request{Kind: KindCalendarEvent, ObligationID: withDate, Payload: json.RawMessage(`{"remind_days_before":45}`), RequestedBy: ActorAgent}); err == nil {
		t.Fatal("45 days before must be rejected")
	}
}

func TestEmailNeedsApproval(t *testing.T) {
	mailer := &fakeMailer{}
	svc, db := newService(t, mailer)
	ctx := context.Background()

	a, err := svc.Request(ctx, Request{
		Kind:        KindSendEmail,
		Payload:     json.RawMessage(`{"to":"Airtel Billing <billing@airtel.example>","subject":" Late fee ","body":"Please waive the late fee."}`),
		RequestedBy: ActorAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != store.ActionPending || len(mailer.sent) != 0 || a.Summary != `Email billing@airtel.example: "Late fee"` {
		t.Fatalf("email must wait for approval: %+v, sent = %v", a, mailer.sent)
	}
	if pending, _ := svc.Pending(ctx); len(pending) != 1 {
		t.Fatalf("pending = %d", len(pending))
	}

	done, err := svc.Approve(ctx, a.ID, ActorUser)
	if err != nil || done.Status != store.ActionDone || done.Result != "sent to billing@airtel.example" {
		t.Fatalf("approve = %+v, %v", done, err)
	}
	if len(mailer.sent) != 1 || mailer.sent[0] != (sentMail{"billing@airtel.example", "Late fee", "Please waive the late fee."}) {
		t.Fatalf("sent = %+v", mailer.sent)
	}
	if _, err := svc.Approve(ctx, a.ID, ActorUser); !errors.Is(err, store.ErrAlreadyDecided) {
		t.Fatalf("second approve: %v", err)
	}
	if len(mailer.sent) != 1 {
		t.Fatal("a second approval must not send again")
	}
	if got := auditEvents(t, db); got != "agent:action.requested user:action.approved policy:action.done" {
		t.Fatalf("audit = %s", got)
	}
}

func TestEmailFailuresAreRecorded(t *testing.T) {
	ctx := context.Background()
	request := Request{Kind: KindSendEmail, Payload: json.RawMessage(`{"to":"a@b.example","subject":"s","body":"b"}`), RequestedBy: ActorAgent}

	svc, _ := newService(t, nil)
	a, _ := svc.Request(ctx, request)
	failed, err := svc.Approve(ctx, a.ID, ActorUser)
	if err != nil || failed.Status != store.ActionFailed || !strings.Contains(failed.Error, "email is not configured") {
		t.Fatalf("no mailer: %+v, %v", failed, err)
	}

	svc, db := newService(t, &fakeMailer{err: errors.New("smtp: 535 authentication failed")})
	a, _ = svc.Request(ctx, request)
	failed, _ = svc.Approve(ctx, a.ID, ActorUser)
	if failed.Status != store.ActionFailed || failed.Error != "smtp: 535 authentication failed" {
		t.Fatalf("smtp error: %+v", failed)
	}
	if got := auditEvents(t, db); !strings.HasSuffix(got, "policy:action.failed") {
		t.Fatalf("audit = %s", got)
	}
}

func TestEmailValidationAndReject(t *testing.T) {
	svc, db := newService(t, &fakeMailer{})
	ctx := context.Background()
	bad := map[string]string{
		`{"to":"not-an-email","subject":"s","body":"b"}`:                               "invalid recipient",
		`{"to":"a@b.example","subject":"","body":"b"}`:                                 "required",
		`{"to":"a@b.example","subject":"x\r\nBcc: victim@evil.example","body":"b"}`:    "one line",
		`{"to":"a@b.example","subject":"` + strings.Repeat("s", 201) + `","body":"b"}`: "200 characters",
	}
	for payload, want := range bad {
		if _, err := svc.Request(ctx, Request{Kind: KindSendEmail, Payload: json.RawMessage(payload), RequestedBy: ActorAgent}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.40s: err = %v, want %q", payload, err, want)
		}
	}

	a, _ := svc.Request(ctx, Request{Kind: KindSendEmail, Payload: json.RawMessage(`{"to":"a@b.example","subject":"s","body":"b"}`), RequestedBy: ActorAgent})
	if err := svc.Reject(ctx, a.ID, ActorUser); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, a.ID, ActorUser); !errors.Is(err, store.ErrAlreadyDecided) {
		t.Fatalf("approve after reject: %v", err)
	}
	if err := svc.Reject(ctx, 999, ActorUser); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reject missing: %v", err)
	}
	if got := auditEvents(t, db); got != "agent:action.requested user:action.rejected" {
		t.Fatalf("audit = %s", got)
	}
}

func TestRequestErrors(t *testing.T) {
	svc, db := newService(t, nil)
	ctx := context.Background()

	if _, err := svc.Request(ctx, Request{Kind: "wire_money", RequestedBy: ActorAgent}); err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := svc.Request(ctx, Request{Kind: KindMarkPaid, RequestedBy: ActorAgent}); err == nil || !strings.Contains(err.Error(), "needs an obligation") {
		t.Fatalf("missing obligation: %v", err)
	}
	if _, err := svc.Request(ctx, Request{Kind: KindMarkPaid, ObligationID: ptr(int64(77)), RequestedBy: ActorAgent}); err == nil || !strings.Contains(err.Error(), "no obligation with id 77") {
		t.Fatalf("unknown obligation: %v", err)
	}
	if NeedsApproval(KindMarkPaid) || !NeedsApproval(KindSendEmail) || !NeedsApproval("anything else") {
		t.Fatal("approval policy is wrong")
	}
	if all, _ := db.ListActions(ctx, "", 0); len(all) != 0 {
		t.Fatalf("invalid requests must not be stored: %+v", all)
	}
}

func TestDeletedObligationFailsApprovedAction(t *testing.T) {
	svc, db := newService(t, &fakeMailer{})
	ctx := context.Background()
	id := obligation(t, db, store.Obligation{Title: "Gym"})

	a := store.Action{Kind: KindMarkPaid, ObligationID: id, Payload: "{}", Summary: "Mark Gym paid", RequestedBy: ActorUser}
	if err := db.CreateAction(ctx, &a); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteObligation(ctx, *id); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Approve(ctx, a.ID, ActorUser)
	if err != nil || got.Status != store.ActionFailed || !strings.Contains(got.Error, "deleted") {
		t.Fatalf("got = %+v, %v", got, err)
	}
}
