package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/money"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

var ErrInvalid = errors.New("invalid input")

type invalidError struct{ err error }

func (e invalidError) Error() string   { return e.err.Error() }
func (e invalidError) Unwrap() []error { return []error{e.err, ErrInvalid} }

func Invalid(err error) error {
	if err == nil || errors.Is(err, ErrInvalid) {
		return err
	}
	return invalidError{err}
}

func Invalidf(format string, args ...any) error {
	return invalidError{fmt.Errorf(format, args...)}
}

func Today(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type View struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	Amount       string `json:"amount,omitempty"`
	Currency     string `json:"currency,omitempty"`
	DueOn        string `json:"due_on,omitempty"`
	DaysUntilDue *int   `json:"days_until_due,omitempty"`
	Recurrence   string `json:"recurrence"`
	Status       string `json:"status"`
	SnoozedUntil string `json:"snoozed_until,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

func NewView(o store.Obligation, today time.Time) View {
	v := View{
		ID:         o.ID,
		Title:      o.Title,
		Category:   string(o.Category),
		Currency:   o.Currency,
		Recurrence: string(o.Recurrence),
		Status:     string(o.Status),
		Notes:      o.Notes,
	}
	if o.AmountMinor != nil {
		v.Amount = money.Format(*o.AmountMinor)
	}
	if o.DueOn != nil {
		v.DueOn = o.DueOn.Format(time.DateOnly)
		days := int(o.DueOn.Sub(today).Hours() / 24)
		v.DaysUntilDue = &days
	}
	if o.SnoozedUntil != nil {
		v.SnoozedUntil = o.SnoozedUntil.Format(time.DateOnly)
	}
	return v
}

type Input struct {
	Title        *string         `json:"title"`
	Category     *string         `json:"category"`
	Amount       json.RawMessage `json:"amount"`
	Currency     *string         `json:"currency"`
	DueOn        *string         `json:"due_on"`
	Recurrence   *string         `json:"recurrence"`
	Status       *string         `json:"status"`
	SnoozedUntil *string         `json:"snoozed_until"`
	Notes        *string         `json:"notes"`
}

func (in Input) ApplyTo(o *store.Obligation, defaultCurrency string) error {
	return Invalid(in.apply(o, defaultCurrency))
}

func (in Input) apply(o *store.Obligation, defaultCurrency string) error {
	if in.Title != nil {
		o.Title = strings.TrimSpace(*in.Title)
	}
	if o.Title == "" {
		return errors.New("title is required")
	}
	if in.Category != nil {
		o.Category = store.Category(*in.Category)
		if !slices.Contains(store.Categories, o.Category) {
			return fmt.Errorf("unknown category %q", *in.Category)
		}
	}
	if in.Recurrence != nil {
		o.Recurrence = store.Recurrence(*in.Recurrence)
		if !slices.Contains(store.Recurrences, o.Recurrence) {
			return fmt.Errorf("unknown recurrence %q", *in.Recurrence)
		}
	}
	if in.Status != nil {
		o.Status = store.Status(*in.Status)
		if !slices.Contains(store.Statuses, o.Status) {
			return fmt.Errorf("unknown status %q", *in.Status)
		}
	}
	if len(in.Amount) > 0 && string(in.Amount) != "null" {
		raw := strings.Trim(string(in.Amount), `"`)
		if raw == "" {
			o.AmountMinor = nil
		} else {
			minor, err := money.Parse(raw)
			if err != nil {
				return err
			}
			o.AmountMinor = &minor
		}
	}
	if in.Currency != nil {
		o.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if o.AmountMinor != nil && o.Currency == "" {
		o.Currency = defaultCurrency
	}
	if o.Currency != "" && len(o.Currency) != 3 {
		return fmt.Errorf("currency must be a 3-letter ISO 4217 code, got %q", o.Currency)
	}
	var err error
	if in.DueOn != nil {
		if o.DueOn, err = ParseDate("due_on", *in.DueOn); err != nil {
			return err
		}
	}
	if in.SnoozedUntil != nil {
		if o.SnoozedUntil, err = ParseDate("snoozed_until", *in.SnoozedUntil); err != nil {
			return err
		}
	}
	if in.Notes != nil {
		o.Notes = strings.TrimSpace(*in.Notes)
	}
	return nil
}

func ParseDate(field, s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, Invalidf("%s must be YYYY-MM-DD, got %q", field, s)
	}
	return &t, nil
}

type Filter struct {
	Status        string
	Category      string
	DueWithinDays *int
	Limit         int
}

func (f Filter) Store(today time.Time) (store.ObligationFilter, error) {
	out := store.ObligationFilter{Category: store.Category(f.Category), Limit: f.Limit}
	if f.Category != "" && !slices.Contains(store.Categories, out.Category) {
		return out, Invalidf("unknown category %q", f.Category)
	}
	switch f.Status {
	case "", "active":
		out.Statuses = []store.Status{store.StatusOpen, store.StatusSnoozed}
	case "all":
	default:
		if !slices.Contains(store.Statuses, store.Status(f.Status)) {
			return out, Invalidf("unknown status %q", f.Status)
		}
		out.Statuses = []store.Status{store.Status(f.Status)}
	}
	if f.DueWithinDays != nil {
		if *f.DueWithinDays < 0 {
			return out, Invalidf("due_within_days must be 0 or more")
		}
		until := today.AddDate(0, 0, *f.DueWithinDays)
		out.DueBefore = &until
	}
	return out, nil
}
