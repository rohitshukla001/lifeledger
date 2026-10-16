package actions

import (
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

func TestCalendarFile(t *testing.T) {
	due := day("2026-10-21")
	amount := int64(234050)
	o := store.Obligation{
		ID: 7, Title: "BESCOM bill; Koramangala, flat 4B", Category: store.CategoryBill,
		AmountMinor: &amount, Currency: "INR", DueOn: &due, Recurrence: store.RecurMonthly,
		Notes: "Account 1234\nPay via UPI " + strings.Repeat("x", 80),
	}
	ics := calendarFile(o, 3, time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC))

	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n",
		"UID:obligation-7-20261021@lifeledger\r\n",
		"DTSTAMP:20261016T093000Z\r\n",
		"DTSTART;VALUE=DATE:20261021\r\n",
		"DTEND;VALUE=DATE:20261022\r\n",
		`SUMMARY:Due: BESCOM bill\; Koramangala\, flat 4B` + "\r\n",
		`DESCRIPTION:Amount: INR 2340.50\nCategory: bill\nAccount 1234\nPay via UPI`,
		"RRULE:FREQ=MONTHLY\r\n",
		"TRIGGER;RELATED=START:-P2DT15H\r\n",
		"END:VCALENDAR\r\n",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("missing %q in:\n%s", want, ics)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(ics, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("line longer than 75 octets: %q", line)
		}
		if strings.Contains(line, "\n") {
			t.Errorf("bare LF in line %q", line)
		}
	}
}

func TestCalendarTriggerAndNoRepeat(t *testing.T) {
	cases := map[int]string{0: "PT9H", 1: "-PT15H", 7: "-P6DT15H"}
	for days, want := range cases {
		if got := trigger(days); got != want {
			t.Errorf("trigger(%d) = %q, want %q", days, got, want)
		}
	}
	due := day("2026-12-01")
	ics := calendarFile(store.Obligation{ID: 1, Title: "Passport renewal", DueOn: &due, Recurrence: store.RecurNone}, 1, time.Now())
	if strings.Contains(ics, "RRULE") || strings.Contains(ics, "Amount") {
		t.Fatalf("one-off item without amount must have no RRULE or amount:\n%s", ics)
	}
}

func TestFoldKeepsMultibyteRunesWhole(t *testing.T) {
	line := "SUMMARY:" + strings.Repeat("₹", 40)
	folded := fold(line)
	for _, part := range strings.Split(folded, "\r\n ") {
		if !strings.HasPrefix(line, "SUMMARY:") || len(part) > 75 {
			t.Fatalf("part too long: %d", len(part))
		}
	}
	if strings.ReplaceAll(folded, "\r\n ", "") != line {
		t.Fatal("unfolding must give the original line")
	}
}
