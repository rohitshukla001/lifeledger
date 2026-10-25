package sweep

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const (
	reviewLookahead = 14
	reviewStale     = 72 * time.Hour
	reviewMaxTokens = 8192
	reviewPrompt    = `You write a short weekly review of one person's life admin. Use only the data in the user message. Never invent items, dates, or amounts.

Write plain text for a chat message: no markdown, no tables, no headings. Use at most 180 words.
1. One line with the overall status.
2. What needs action in the next 7 days, in date order.
3. Overdue items, if any.
4. Approvals that wait for the user, if any.
5. One practical suggestion.
Write dates like "Mon 19 Oct" and amounts with their currency.`
)

type digestItem struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Category   string `json:"category"`
	Due        string `json:"due,omitempty"`
	Amount     string `json:"amount,omitempty"`
	Recurrence string `json:"recurrence,omitempty"`
	Until      string `json:"snoozed_until,omitempty"`
}

type digest struct {
	Today        string       `json:"today"`
	Overdue      []digestItem `json:"overdue"`
	Upcoming     []digestItem `json:"due_next_14_days"`
	Snoozed      []digestItem `json:"snoozed"`
	Undated      int          `json:"open_items_without_due_date"`
	Approvals    []string     `json:"waiting_for_approval"`
	DoneThisWeek []string     `json:"done_in_last_7_days"`
}

func lastSlot(local time.Time, sch Schedule) time.Time {
	back := (int(local.Weekday()) - int(sch.Day) + 7) % 7
	y, m, d := local.Date()
	slot := time.Date(y, m, d-back, sch.Hour, 0, 0, 0, local.Location())
	if slot.After(local) {
		slot = time.Date(y, m, d-back-7, sch.Hour, 0, 0, 0, local.Location())
	}
	return slot
}

func (s *Sweeper) scheduledReview(ctx context.Context, local time.Time) (bool, error) {
	slot := lastSlot(local, *s.review)
	if local.Sub(slot) > reviewStale {
		return false, nil
	}
	key := "review:" + slot.Format(time.DateOnly)
	if done, err := s.store.HasNotification(ctx, key); err != nil || done {
		return false, err
	}
	n, err := s.writeReview(ctx, key, local)
	if err != nil {
		return false, err
	}
	return n.ID != 0, nil
}

func (s *Sweeper) Review(ctx context.Context) (store.Notification, error) {
	now := s.now()
	return s.writeReview(ctx, fmt.Sprintf("review:manual:%d", now.UnixMilli()), now.In(s.loc))
}

func (s *Sweeper) writeReview(ctx context.Context, key string, local time.Time) (store.Notification, error) {
	d, err := s.digest(ctx, local)
	if err != nil {
		return store.Notification{}, err
	}
	body := s.summarize(ctx, d) + "\n\n" + d.text()
	n := store.Notification{
		Kind:      store.NotificationReview,
		DedupeKey: key,
		Title:     "LifeLedger weekly review, " + local.Format("Mon 2 Jan 2006"),
		Body:      body,
	}
	if _, err := s.store.AddNotification(ctx, &n); err != nil {
		return store.Notification{}, err
	}
	return n, nil
}

func (s *Sweeper) summarize(ctx context.Context, d digest) string {
	if s.llm == nil {
		return "Summary from your ledger:"
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "Summary from your ledger:"
	}
	temp := 0.3
	resp, err := s.llm.Chat(ctx, llm.Request{
		Tier:        llm.Ultra,
		Messages:    []llm.Message{llm.System(reviewPrompt), llm.User("Data (JSON):\n" + string(data))},
		Temperature: &temp,
		MaxTokens:   reviewMaxTokens,
	})
	if err != nil {
		s.log.Warn("weekly review written without the model", "err", err)
		return "The model was not available, so this review lists your ledger only:"
	}
	text := strings.TrimSpace(resp.Message.Content)
	if text == "" {
		return "Summary from your ledger:"
	}
	s.log.Info("weekly review written", "model", resp.Model, "cost_usd", resp.CostUSD)
	return text + "\n\nDetails:"
}

func (s *Sweeper) digest(ctx context.Context, local time.Time) (digest, error) {
	today := dateOf(local)
	d := digest{Today: today.Format("Mon 2 Jan 2006")}

	items, err := s.store.ListObligations(ctx, store.ObligationFilter{Statuses: []store.Status{store.StatusOpen, store.StatusSnoozed}})
	if err != nil {
		return d, err
	}
	horizon := today.AddDate(0, 0, reviewLookahead)
	for _, o := range items {
		item := digestItem{ID: o.ID, Title: o.Title, Category: string(o.Category)}
		if o.DueOn != nil {
			item.Due = o.DueOn.Format("Mon 2 Jan 2006")
		}
		if o.AmountMinor != nil {
			item.Amount = s.describeAmount(o)
		}
		if o.Recurrence != store.RecurNone {
			item.Recurrence = string(o.Recurrence)
		}
		switch {
		case o.Status == store.StatusSnoozed:
			if o.SnoozedUntil != nil {
				item.Until = o.SnoozedUntil.Format("Mon 2 Jan 2006")
			}
			d.Snoozed = append(d.Snoozed, item)
		case o.DueOn == nil:
			d.Undated++
		case o.DueOn.Before(today):
			d.Overdue = append(d.Overdue, item)
		case !o.DueOn.After(horizon):
			d.Upcoming = append(d.Upcoming, item)
		}
	}

	pending, err := s.store.ListActions(ctx, store.ActionPending, 20)
	if err != nil {
		return d, err
	}
	for _, a := range pending {
		d.Approvals = append(d.Approvals, fmt.Sprintf("%s (action #%d)", a.Summary, a.ID))
	}
	done, err := s.store.ListActions(ctx, store.ActionDone, 50)
	if err != nil {
		return d, err
	}
	weekAgo := local.Add(-7 * 24 * time.Hour)
	for _, a := range done {
		if a.ExecutedAt != nil && a.ExecutedAt.After(weekAgo) {
			d.DoneThisWeek = append(d.DoneThisWeek, a.Summary)
		}
	}
	return d, nil
}

func (d digest) text() string {
	var b strings.Builder
	section := func(name string, items []digestItem, line func(digestItem) string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", name, len(items))
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", line(it))
		}
	}
	due := func(prefix string) func(digestItem) string {
		return func(it digestItem) string {
			parts := []string{it.Title}
			if it.Amount != "" {
				parts = append(parts, it.Amount)
			}
			parts = append(parts, prefix+" "+it.Due)
			return fmt.Sprintf("%s (#%d)", strings.Join(parts, ", "), it.ID)
		}
	}

	section("Overdue", d.Overdue, due("was due"))
	section("Due in the next 14 days", d.Upcoming, due("due"))
	section("Snoozed", d.Snoozed, func(it digestItem) string {
		if it.Until == "" {
			return fmt.Sprintf("%s (#%d)", it.Title, it.ID)
		}
		return fmt.Sprintf("%s, until %s (#%d)", it.Title, it.Until, it.ID)
	})
	lines := func(name string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", name, len(items))
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it)
		}
	}
	lines("Waiting for your approval", d.Approvals)
	lines("Done in the last 7 days", d.DoneThisWeek)
	if d.Undated > 0 {
		fmt.Fprintf(&b, "\nOpen items without a due date: %d\n", d.Undated)
	}
	if b.Len() == 0 {
		return "Nothing is overdue or due in the next 14 days, and no action waits for you."
	}
	return strings.TrimSpace(b.String())
}
