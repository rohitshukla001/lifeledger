package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

var (
	categories = []store.Category{
		store.CategoryBill, store.CategorySubscription, store.CategoryRenewal, store.CategoryWarranty,
		store.CategoryDocument, store.CategoryDeadline, store.CategoryOther,
	}
	recurrences = []store.Recurrence{
		store.RecurNone, store.RecurWeekly, store.RecurMonthly, store.RecurQuarterly, store.RecurYearly,
	}
	statuses = []store.Status{store.StatusOpen, store.StatusDone, store.StatusSnoozed, store.StatusCancelled}
)

const obligationFields = `
				"title": {"type": "string"},
				"category": {"type": "string", "enum": ["bill", "subscription", "renewal", "warranty", "document", "deadline", "other"]},
				"amount": {"type": "string", "description": "Decimal amount without the currency symbol, for example 2340.50"},
				"currency": {"type": "string", "description": "ISO 4217 code, for example INR"},
				"due_on": {"type": "string", "description": "Due, renewal, or expiry date as YYYY-MM-DD"},
				"recurrence": {"type": "string", "enum": ["none", "weekly", "monthly", "quarterly", "yearly"]},
				"notes": {"type": "string"}`

type obligationView struct {
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

func (a *Agent) view(o store.Obligation) obligationView {
	v := obligationView{
		ID:         o.ID,
		Title:      o.Title,
		Category:   string(o.Category),
		Currency:   o.Currency,
		Recurrence: string(o.Recurrence),
		Status:     string(o.Status),
		Notes:      o.Notes,
	}
	if o.AmountMinor != nil {
		v.Amount = formatAmount(*o.AmountMinor)
	}
	if o.DueOn != nil {
		v.DueOn = o.DueOn.Format(time.DateOnly)
		days := int(o.DueOn.Sub(a.today()).Hours() / 24)
		v.DaysUntilDue = &days
	}
	if o.SnoozedUntil != nil {
		v.SnoozedUntil = o.SnoozedUntil.Format(time.DateOnly)
	}
	return v
}

type obligationInput struct {
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

func (in obligationInput) applyTo(o *store.Obligation, defaultCurrency string) error {
	if in.Title != nil {
		o.Title = strings.TrimSpace(*in.Title)
	}
	if o.Title == "" {
		return errors.New("title is required")
	}
	if in.Category != nil {
		o.Category = store.Category(*in.Category)
		if !slices.Contains(categories, o.Category) {
			return fmt.Errorf("unknown category %q", *in.Category)
		}
	}
	if in.Recurrence != nil {
		o.Recurrence = store.Recurrence(*in.Recurrence)
		if !slices.Contains(recurrences, o.Recurrence) {
			return fmt.Errorf("unknown recurrence %q", *in.Recurrence)
		}
	}
	if in.Status != nil {
		o.Status = store.Status(*in.Status)
		if !slices.Contains(statuses, o.Status) {
			return fmt.Errorf("unknown status %q", *in.Status)
		}
	}
	if len(in.Amount) > 0 && string(in.Amount) != "null" {
		raw := strings.Trim(string(in.Amount), `"`)
		if raw == "" {
			o.AmountMinor = nil
		} else {
			minor, err := parseAmount(raw)
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
		if o.DueOn, err = parseOptionalDate("due_on", *in.DueOn); err != nil {
			return err
		}
	}
	if in.SnoozedUntil != nil {
		if o.SnoozedUntil, err = parseOptionalDate("snoozed_until", *in.SnoozedUntil); err != nil {
			return err
		}
	}
	if in.Notes != nil {
		o.Notes = strings.TrimSpace(*in.Notes)
	}
	return nil
}

func parseOptionalDate(field, s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, fmt.Errorf("%s must be YYYY-MM-DD, got %q", field, s)
	}
	return &t, nil
}

func (a *Agent) addObligationTool() Tool {
	return Tool{
		Name:        "add_obligation",
		Description: "Record a bill, subscription, renewal, warranty, document, or deadline for the user.",
		Parameters:  `{"type": "object", "properties": {` + obligationFields + `}, "required": ["title"]}`,
		Run: func(ctx context.Context, call Call) (any, error) {
			var in obligationInput
			if err := decodeArgs(call, &in); err != nil {
				return nil, err
			}
			in.Status = nil
			in.SnoozedUntil = nil
			o := store.Obligation{Source: "chat", SourceRef: fmt.Sprintf("conversation:%d", call.ConversationID)}
			if err := in.applyTo(&o, a.currency); err != nil {
				return nil, err
			}
			if err := a.store.CreateObligation(ctx, &o); err != nil {
				return nil, err
			}
			return a.view(o), nil
		},
	}
}

func (a *Agent) listObligationsTool() Tool {
	return Tool{
		Name:        "list_obligations",
		Description: "List the user's obligations, soonest due first. By default it shows open and snoozed items.",
		Parameters: `{
			"type": "object",
			"properties": {
				"status": {"type": "string", "enum": ["active", "open", "snoozed", "done", "cancelled", "all"]},
				"category": {"type": "string", "enum": ["bill", "subscription", "renewal", "warranty", "document", "deadline", "other"]},
				"due_within_days": {"type": "integer", "minimum": 0, "description": "Only items due on or before today plus this many days"},
				"limit": {"type": "integer", "minimum": 1, "maximum": 50}
			}
		}`,
		Run: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Status        string `json:"status"`
				Category      string `json:"category"`
				DueWithinDays *int   `json:"due_within_days"`
				Limit         int    `json:"limit"`
			}
			if err := decodeArgs(call, &in); err != nil {
				return nil, err
			}

			f := store.ObligationFilter{Category: store.Category(in.Category), Limit: 20}
			if in.Limit > 0 {
				f.Limit = min(in.Limit, 50)
			}
			switch in.Status {
			case "", "active":
				f.Statuses = []store.Status{store.StatusOpen, store.StatusSnoozed}
			case "all":
			default:
				if !slices.Contains(statuses, store.Status(in.Status)) {
					return nil, fmt.Errorf("unknown status %q", in.Status)
				}
				f.Statuses = []store.Status{store.Status(in.Status)}
			}
			if in.DueWithinDays != nil {
				until := a.today().AddDate(0, 0, *in.DueWithinDays)
				f.DueBefore = &until
			}

			items, err := a.store.ListObligations(ctx, f)
			if err != nil {
				return nil, err
			}
			views := make([]obligationView, 0, len(items))
			for _, o := range items {
				views = append(views, a.view(o))
			}
			return map[string]any{"today": a.today().Format(time.DateOnly), "obligations": views}, nil
		},
	}
}

func (a *Agent) updateObligationTool() Tool {
	return Tool{
		Name:        "update_obligation",
		Description: "Change an existing obligation. Send only the fields that change. Use status done when it is paid or finished, snoozed with snoozed_until to postpone reminders, cancelled when it no longer applies.",
		Parameters: `{"type": "object", "properties": {
				"id": {"type": "integer"},` + obligationFields + `,
				"status": {"type": "string", "enum": ["open", "done", "snoozed", "cancelled"]},
				"snoozed_until": {"type": "string", "description": "YYYY-MM-DD"}
			}, "required": ["id"]}`,
		Run: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ID int64 `json:"id"`
				obligationInput
			}
			if err := decodeArgs(call, &in); err != nil {
				return nil, err
			}
			o, err := a.store.GetObligation(ctx, in.ID)
			if errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("no obligation with id %d", in.ID)
			}
			if err != nil {
				return nil, err
			}
			if err := in.applyTo(&o, a.currency); err != nil {
				return nil, err
			}
			if err := a.store.UpdateObligation(ctx, &o); err != nil {
				return nil, err
			}
			return a.view(o), nil
		},
	}
}
