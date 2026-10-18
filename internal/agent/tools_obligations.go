package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const obligationFields = `
				"title": {"type": "string"},
				"category": {"type": "string", "enum": ["bill", "subscription", "renewal", "warranty", "document", "deadline", "other"]},
				"amount": {"type": "string", "description": "Decimal amount without the currency symbol, for example 2340.50"},
				"currency": {"type": "string", "description": "ISO 4217 code, for example INR"},
				"due_on": {"type": "string", "description": "Due, renewal, or expiry date as YYYY-MM-DD"},
				"recurrence": {"type": "string", "enum": ["none", "weekly", "monthly", "quarterly", "yearly"]},
				"notes": {"type": "string"}`

func (a *Agent) view(o store.Obligation) ledger.View {
	return ledger.NewView(o, a.today())
}

func (a *Agent) addObligationTool() Tool {
	return Tool{
		Name:        "add_obligation",
		Description: "Record a bill, subscription, renewal, warranty, document, or deadline for the user.",
		Parameters:  `{"type": "object", "properties": {` + obligationFields + `}, "required": ["title"]}`,
		Run: func(ctx context.Context, call Call) (any, error) {
			var in ledger.Input
			if err := decodeArgs(call, &in); err != nil {
				return nil, err
			}
			in.Status = nil
			in.SnoozedUntil = nil
			o := store.Obligation{Source: "chat", SourceRef: fmt.Sprintf("conversation:%d", call.ConversationID)}
			if err := in.ApplyTo(&o, a.currency); err != nil {
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

			limit := 20
			if in.Limit > 0 {
				limit = min(in.Limit, 50)
			}
			f, err := ledger.Filter{Status: in.Status, Category: in.Category, DueWithinDays: in.DueWithinDays, Limit: limit}.Store(a.today())
			if err != nil {
				return nil, err
			}

			items, err := a.store.ListObligations(ctx, f)
			if err != nil {
				return nil, err
			}
			views := make([]ledger.View, 0, len(items))
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
				ledger.Input
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
			if err := in.ApplyTo(&o, a.currency); err != nil {
				return nil, err
			}
			if err := a.store.UpdateObligation(ctx, &o); err != nil {
				return nil, err
			}
			return a.view(o), nil
		},
	}
}
