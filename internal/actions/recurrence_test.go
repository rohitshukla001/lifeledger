package actions

import (
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextDue(t *testing.T) {
	cases := []struct {
		from string
		r    store.Recurrence
		want string
	}{
		{"2026-10-21", store.RecurWeekly, "2026-10-28"},
		{"2026-10-21", store.RecurMonthly, "2026-11-21"},
		{"2027-01-31", store.RecurMonthly, "2027-02-28"},
		{"2028-01-31", store.RecurMonthly, "2028-02-29"},
		{"2026-12-15", store.RecurMonthly, "2027-01-15"},
		{"2026-11-30", store.RecurQuarterly, "2027-02-28"},
		{"2028-02-29", store.RecurYearly, "2029-02-28"},
	}
	for _, c := range cases {
		got, ok := NextDue(day(c.from), c.r)
		if !ok || got.Format(time.DateOnly) != c.want {
			t.Errorf("NextDue(%s, %s) = %s, %v; want %s", c.from, c.r, got.Format(time.DateOnly), ok, c.want)
		}
	}
	if _, ok := NextDue(day("2026-10-21"), store.RecurNone); ok {
		t.Error("a one-off obligation has no next due date")
	}
}
