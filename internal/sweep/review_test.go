package sweep

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

var sundayEvening = &Schedule{Day: time.Sunday, Hour: 18}

func TestLastSlot(t *testing.T) {
	cases := map[string]string{
		"2026-10-18 18:00": "2026-10-18 18:00",
		"2026-10-18 17:59": "2026-10-11 18:00",
		"2026-10-19 09:00": "2026-10-18 18:00",
		"2026-10-24 23:59": "2026-10-18 18:00",
		"2026-10-25 18:30": "2026-10-25 18:00",
	}
	for at, want := range cases {
		local, _ := time.ParseInLocation("2006-01-02 15:04", at, kolkata)
		if got := lastSlot(local, *sundayEvening).Format("2006-01-02 15:04"); got != want {
			t.Errorf("%s: slot = %s, want %s", at, got, want)
		}
	}
}

func seedLedger(t *testing.T, r *rig) {
	t.Helper()
	ctx := context.Background()
	r.add(t, store.Obligation{Title: "Rent", Category: store.CategoryBill, AmountMinor: amount(2500000), Currency: "INR", DueOn: day("2026-10-15"), Recurrence: store.RecurMonthly})
	r.add(t, store.Obligation{Title: "Car insurance", Category: store.CategoryRenewal, AmountMinor: amount(1240000), DueOn: day("2026-10-25")})
	r.add(t, store.Obligation{Title: "Laptop warranty", Category: store.CategoryWarranty, DueOn: day("2027-03-01")})
	r.add(t, store.Obligation{Title: "Gym", Status: store.StatusSnoozed, SnoozedUntil: day("2026-10-30")})
	r.add(t, store.Obligation{Title: "Find old PAN card"})
	r.add(t, store.Obligation{Title: "Netflix", Status: store.StatusDone, DueOn: day("2026-10-01")})

	pending := &store.Action{Kind: "send_email", Payload: `{}`, Summary: "Email billing@airtel.example: Late fee", RequestedBy: "agent"}
	done := &store.Action{Kind: "mark_paid", Payload: `{}`, Summary: `Mark "Electricity" as paid`, RequestedBy: "agent"}
	for _, a := range []*store.Action{pending, done} {
		if err := r.store.CreateAction(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.store.DecideAction(ctx, done.ID, store.ActionApproved); err != nil {
		t.Fatal(err)
	}
	if err := r.store.FinishAction(ctx, done.ID, "next due 2026-11-16", nil); err != nil {
		t.Fatal(err)
	}
}

func TestWeeklyReviewRunsOncePerSlot(t *testing.T) {
	r := newRig(t, "2026-10-16 10:00", sundayEvening)
	r.remindHourOff()
	seedLedger(t, r)

	if rep := r.run(t, "2026-10-18 17:59"); rep.Review {
		t.Fatal("review written before the slot")
	}
	rep := r.run(t, "2026-10-18 18:00")
	if !rep.Review || rep.Delivered != 1 || len(r.llm.requests) != 1 {
		t.Fatalf("report = %+v, model calls = %d", rep, len(r.llm.requests))
	}

	req := r.llm.requests[0]
	if req.Tier != llm.Ultra || req.Messages[0].Role != "system" || *req.Temperature != 0.3 {
		t.Fatalf("request = %+v", req)
	}
	data := req.Messages[1].Content
	for _, want := range []string{`"today": "Sun 18 Oct 2026"`, `"title": "Rent"`, `"amount": "INR 25000.00"`, `"title": "Car insurance"`,
		`"snoozed_until": "Fri 30 Oct 2026"`, `"open_items_without_due_date": 1`, "Late fee (action #1)", `Mark \"Electricity\" as paid`} {
		if !strings.Contains(data, want) {
			t.Errorf("model data has no %s:\n%s", want, data)
		}
	}
	if strings.Contains(data, "Netflix") || strings.Contains(data, "Laptop warranty") {
		t.Errorf("done items and items after 14 days must not be in the data:\n%s", data)
	}

	msg := r.sent.messages[0]
	wantMsg := `LifeLedger weekly review, Sun 18 Oct 2026

All under control.

Details:

Overdue (1):
- Rent, INR 25000.00, was due Thu 15 Oct 2026 (#1)

Due in the next 14 days (1):
- Car insurance, INR 12400.00, due Sun 25 Oct 2026 (#2)

Snoozed (1):
- Gym, until Fri 30 Oct 2026 (#4)

Waiting for your approval (1):
- Email billing@airtel.example: Late fee (action #1)

Done in the last 7 days (1):
- Mark "Electricity" as paid

Open items without a due date: 1`
	if msg != wantMsg {
		t.Fatalf("message =\n%s\n\nwant\n%s", msg, wantMsg)
	}

	if rep := r.run(t, "2026-10-18 22:00"); rep.Review || len(r.llm.requests) != 1 {
		t.Fatalf("second run in the same slot: %+v, model calls = %d", rep, len(r.llm.requests))
	}
}

func TestWeeklyReviewCatchesUpButNotWhenStale(t *testing.T) {
	r := newRig(t, "2026-10-19 08:00", sundayEvening)
	r.remindHourOff()

	if rep := r.run(t, "2026-10-19 08:00"); !rep.Review {
		t.Fatal("a review missed last night must be written the next morning")
	}
	if found, _ := r.store.HasNotification(context.Background(), "review:2026-10-18"); !found {
		t.Fatal("the review must use the key of its slot")
	}
	if rep := r.run(t, "2026-10-28 18:01"); rep.Review {
		t.Fatal("a review more than 72 hours after its slot must be skipped")
	}
	if rep := r.run(t, "2026-11-01 18:00"); !rep.Review {
		t.Fatal("the next slot must get a review")
	}
}

func TestWeeklyReviewWithoutTheModel(t *testing.T) {
	r := newRig(t, "2026-10-18 18:00", sundayEvening)
	r.remindHourOff()
	r.add(t, store.Obligation{Title: "Rent", DueOn: day("2026-10-15")})

	r.llm.err = llm.ErrBudgetExceeded
	r.run(t, "2026-10-18 18:00")
	if body := r.sent.messages[0]; !strings.Contains(body, "The model was not available") || !strings.Contains(body, "Rent, was due Thu 15 Oct 2026 (#1)") {
		t.Fatalf("fallback review = %q", body)
	}

	r.sweep.llm = nil
	n, err := r.sweep.Review(context.Background())
	if err != nil || !strings.HasPrefix(n.Body, "Summary from your ledger:") || !strings.HasPrefix(n.DedupeKey, "review:manual:") {
		t.Fatalf("manual review = %+v, %v", n, err)
	}
}

func TestReviewOfAnEmptyLedger(t *testing.T) {
	r := newRig(t, "2026-10-18 18:00", nil)
	r.sweep.llm = nil
	n, err := r.sweep.Review(context.Background())
	if err != nil || !strings.HasSuffix(n.Body, "Nothing is overdue or due in the next 14 days, and no action waits for you.") {
		t.Fatalf("review = %+v, %v", n, err)
	}
}

func TestDeliveryFailureIsCountedNotFatal(t *testing.T) {
	r := newRig(t, "2026-10-18 18:00", sundayEvening)
	r.add(t, store.Obligation{Title: "Rent", DueOn: day("2026-10-19")})
	r.sent.fail = errors.New("telegram: 403 Forbidden: bot was blocked by the user")

	rep := r.run(t, "2026-10-18 18:00")
	if rep.Reminders != 1 || !rep.Review || rep.Failed != 2 || rep.Delivered != 0 {
		t.Fatalf("report = %+v", rep)
	}
}
