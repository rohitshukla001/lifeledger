package sweep

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/money"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const reminderLeadDays = 7

func stage(days int) (string, bool) {
	switch {
	case days < 0:
		return "overdue", true
	case days == 0:
		return "today", true
	case days == 1:
		return "1d", true
	case days <= 3:
		return "3d", true
	case days <= reminderLeadDays:
		return "7d", true
	}
	return "", false
}

func (s *Sweeper) remind(ctx context.Context, today time.Time) (int, error) {
	horizon := today.AddDate(0, 0, reminderLeadDays)
	items, err := s.store.ListObligations(ctx, store.ObligationFilter{
		Statuses:  []store.Status{store.StatusOpen},
		DueBefore: &horizon,
	})
	if err != nil {
		return 0, err
	}

	created := 0
	for _, o := range items {
		days := daysBetween(today, *o.DueOn)
		st, ok := stage(days)
		if !ok {
			continue
		}
		n := &store.Notification{
			Kind:         store.NotificationReminder,
			DedupeKey:    fmt.Sprintf("reminder:%d:%s:%s", o.ID, o.DueOn.Format(time.DateOnly), st),
			ObligationID: &o.ID,
			Title:        reminderTitle(o.Title, days),
			Body:         s.describe(o),
		}
		added, err := s.store.AddNotification(ctx, n)
		if err != nil {
			return created, err
		}
		if added {
			created++
		}
	}
	return created, nil
}

func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

func reminderTitle(title string, days int) string {
	switch {
	case days < -1:
		return fmt.Sprintf("Overdue by %d days: %s", -days, title)
	case days == -1:
		return "Overdue by 1 day: " + title
	case days == 0:
		return "Due today: " + title
	case days == 1:
		return "Due tomorrow: " + title
	}
	return fmt.Sprintf("Due in %d days: %s", days, title)
}

func (s *Sweeper) describe(o store.Obligation) string {
	var parts []string
	if o.AmountMinor != nil {
		parts = append(parts, s.describeAmount(o))
	}
	if o.DueOn != nil {
		parts = append(parts, "due "+o.DueOn.Format("Mon 2 Jan 2006"))
	}
	if o.Recurrence != store.RecurNone && o.Recurrence != "" {
		parts = append(parts, "repeats "+string(o.Recurrence))
	}
	parts = append(parts, fmt.Sprintf("item #%d", o.ID))
	text := strings.Join(parts, ", ")
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

func (s *Sweeper) describeAmount(o store.Obligation) string {
	currency := o.Currency
	if currency == "" {
		currency = s.currency
	}
	return strings.TrimSpace(currency + " " + money.Format(*o.AmountMinor))
}
