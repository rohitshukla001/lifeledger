package ledger

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

func TestInvalidKeepsTheMessageAndTheCause(t *testing.T) {
	cause := errors.New("title is required")
	err := Invalid(cause)
	if err.Error() != "title is required" || !errors.Is(err, ErrInvalid) || !errors.Is(err, cause) {
		t.Fatalf("err = %v", err)
	}
	if Invalid(err) != err || Invalid(nil) != nil {
		t.Fatal("Invalid must not wrap twice or wrap nil")
	}
	if err := Invalidf("bad %d", 7); err.Error() != "bad 7" || !errors.Is(err, ErrInvalid) {
		t.Fatalf("Invalidf = %v", err)
	}
}

func TestToday(t *testing.T) {
	kolkata, _ := time.LoadLocation("Asia/Kolkata")
	got := Today(time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC), kolkata)
	if !got.Equal(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("today = %v", got)
	}
}

func TestInputPatchesOnlyGivenFields(t *testing.T) {
	due, _ := time.Parse(time.DateOnly, "2026-10-21")
	minor := int64(99900)
	o := store.Obligation{Title: "Broadband", Category: store.CategoryBill, AmountMinor: &minor, Currency: "INR", DueOn: &due, Notes: "Airtel"}

	var in Input
	if err := json.Unmarshal([]byte(`{"amount":"1,299.50","due_on":"","notes":"  Airtel fiber "}`), &in); err != nil {
		t.Fatal(err)
	}
	if err := in.ApplyTo(&o, "USD"); err != nil {
		t.Fatal(err)
	}
	if *o.AmountMinor != 129950 || o.Currency != "INR" || o.DueOn != nil || o.Notes != "Airtel fiber" || o.Title != "Broadband" {
		t.Fatalf("obligation = %+v", o)
	}
}

func TestInputErrorsAreInvalid(t *testing.T) {
	for raw, want := range map[string]string{
		`{"title":" "}`:          "title is required",
		`{"category":"tax"}`:     "unknown category",
		`{"status":"lost"}`:      "unknown status",
		`{"currency":"RUPEES"}`:  "3-letter",
		`{"due_on":"tomorrow"}`:  "YYYY-MM-DD",
		`{"amount":"-1"}`:        "not a positive number",
		`{"recurrence":"daily"}`: "unknown recurrence",
	} {
		var in Input
		json.Unmarshal([]byte(raw), &in)
		o := store.Obligation{Title: "x"}
		err := in.ApplyTo(&o, "INR")
		if err == nil || !strings.Contains(err.Error(), want) || !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want invalid %q", raw, err, want)
		}
	}
}

func TestFilter(t *testing.T) {
	today := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	week := 7

	f, err := Filter{DueWithinDays: &week, Limit: 5}.Store(today)
	if err != nil || len(f.Statuses) != 2 || f.DueBefore.Format(time.DateOnly) != "2026-10-25" || f.Limit != 5 {
		t.Fatalf("active filter = %+v, %v", f, err)
	}
	if f, _ := (Filter{Status: "all"}).Store(today); f.Statuses != nil {
		t.Fatalf("all = %+v", f)
	}
	if f, _ := (Filter{Status: "done", Category: "bill"}).Store(today); f.Statuses[0] != store.StatusDone || f.Category != store.CategoryBill {
		t.Fatalf("done = %+v", f)
	}

	negative := -1
	for _, bad := range []Filter{{Status: "lost"}, {Category: "tax"}, {DueWithinDays: &negative}} {
		if _, err := bad.Store(today); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: err = %v", bad, err)
		}
	}
}
