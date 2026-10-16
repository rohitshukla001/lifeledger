package actions

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rohitshukla001/lifeledger/internal/money"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

var rrules = map[store.Recurrence]string{
	store.RecurWeekly:    "FREQ=WEEKLY",
	store.RecurMonthly:   "FREQ=MONTHLY",
	store.RecurQuarterly: "FREQ=MONTHLY;INTERVAL=3",
	store.RecurYearly:    "FREQ=YEARLY",
}

func calendarFile(o store.Obligation, remindDaysBefore int, now time.Time) string {
	due := *o.DueOn
	desc := "Category: " + string(o.Category)
	if o.AmountMinor != nil {
		desc = fmt.Sprintf("Amount: %s %s\n%s", o.Currency, money.Format(*o.AmountMinor), desc)
	}
	if o.Notes != "" {
		desc += "\n" + o.Notes
	}

	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//LifeLedger//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:obligation-%d-%s@lifeledger", o.ID, due.Format("20060102")),
		"DTSTAMP:" + now.UTC().Format("20060102T150405Z"),
		"DTSTART;VALUE=DATE:" + due.Format("20060102"),
		"DTEND;VALUE=DATE:" + due.AddDate(0, 0, 1).Format("20060102"),
		"SUMMARY:" + escapeText("Due: "+o.Title),
		"DESCRIPTION:" + escapeText(desc),
	}
	if rule, ok := rrules[o.Recurrence]; ok {
		lines = append(lines, "RRULE:"+rule)
	}
	lines = append(lines,
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:"+escapeText(o.Title+" is due"),
		"TRIGGER;RELATED=START:"+trigger(remindDaysBefore),
		"END:VALARM",
		"END:VEVENT",
		"END:VCALENDAR",
	)

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(fold(l))
		b.WriteString("\r\n")
	}
	return b.String()
}

func trigger(daysBefore int) string {
	switch {
	case daysBefore <= 0:
		return "PT9H"
	case daysBefore == 1:
		return "-PT15H"
	default:
		return fmt.Sprintf("-P%dDT15H", daysBefore-1)
	}
}

func escapeText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(s)
}

func fold(line string) string {
	if len(line) <= 75 {
		return line
	}
	var b strings.Builder
	width := 0
	for _, r := range line {
		n := utf8.RuneLen(r)
		if width+n > 75 {
			b.WriteString("\r\n ")
			width = 1
		}
		b.WriteRune(r)
		width += n
	}
	return b.String()
}
