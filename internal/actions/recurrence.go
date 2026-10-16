package actions

import (
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

func NextDue(due time.Time, r store.Recurrence) (time.Time, bool) {
	switch r {
	case store.RecurWeekly:
		return due.AddDate(0, 0, 7), true
	case store.RecurMonthly:
		return addMonths(due, 1), true
	case store.RecurQuarterly:
		return addMonths(due, 3), true
	case store.RecurYearly:
		return addMonths(due, 12), true
	}
	return due, false
}

func addMonths(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(t.Day(), last)-1)
}
