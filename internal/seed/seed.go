package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const (
	source   = "demo"
	currency = "INR"
)

var ErrNotEmpty = errors.New("seed: the database already has data")

type Summary struct {
	Obligations int
	Memories    int
	Actions     int
	Proposals   int
}

func (s Summary) String() string {
	return fmt.Sprintf("%d obligations, %d memories, %d actions, %d proposals", s.Obligations, s.Memories, s.Actions, s.Proposals)
}

type item struct {
	title      string
	category   store.Category
	amount     int64
	dueIn      *int
	recurrence store.Recurrence
	status     store.Status
	notes      string
}

func days(n int) *int { return &n }

const airtelEmail = `From: Airtel <ebill@airtel.example>
Subject: Your Airtel Xstream Fiber bill for November 2026

Dear customer,

Your bill for account 7712-0094 is ready.
Amount due: Rs 999.00
Pay by: %s

Your router warranty is valid till 01-Jun-2027.

Thank you for choosing Airtel.`

func Demo(ctx context.Context, s *store.Store, today time.Time) (Summary, error) {
	var sum Summary
	if empty, err := isEmpty(ctx, s); err != nil || !empty {
		if err == nil {
			err = ErrNotEmpty
		}
		return sum, err
	}

	items := []item{
		{"Rent", store.CategoryBill, 2500000, days(-3), store.RecurMonthly, store.StatusOpen, "Landlord accepts UPI only."},
		{"BESCOM electricity", store.CategoryBill, 234050, days(2), store.RecurMonthly, store.StatusOpen, ""},
		{"Acko car insurance", store.CategoryRenewal, 1240000, days(6), store.RecurYearly, store.StatusOpen, "Policy for the Swift."},
		{"Airtel broadband", store.CategoryBill, 99900, days(12), store.RecurMonthly, store.StatusOpen, ""},
		{"Netflix", store.CategorySubscription, 64900, days(20), store.RecurMonthly, store.StatusOpen, ""},
		{"Passport renewal", store.CategoryDocument, 0, days(104), store.RecurNone, store.StatusOpen, "Book a slot at the Passport Seva Kendra six weeks before."},
		{"Laptop warranty", store.CategoryWarranty, 0, days(140), store.RecurNone, store.StatusOpen, "Invoice is in Google Drive."},
		{"Find old PAN card", store.CategoryOther, 0, nil, store.RecurNone, store.StatusOpen, ""},
		{"Gym membership", store.CategorySubscription, 150000, days(-10), store.RecurNone, store.StatusDone, ""},
	}
	ids := map[string]int64{}
	for _, it := range items {
		o := store.Obligation{
			Title:      it.title,
			Category:   it.category,
			Recurrence: it.recurrence,
			Status:     it.status,
			Notes:      it.notes,
			Source:     source,
		}
		if it.amount > 0 {
			amount := it.amount
			o.AmountMinor, o.Currency = &amount, currency
		}
		if it.dueIn != nil {
			due := today.AddDate(0, 0, *it.dueIn)
			o.DueOn = &due
		}
		if err := s.CreateObligation(ctx, &o); err != nil {
			return sum, err
		}
		ids[it.title] = o.ID
		sum.Obligations++
	}

	memories := []store.Memory{
		{Kind: store.MemoryFact, Content: "Car insurance is with Acko."},
		{Kind: store.MemoryFact, Content: "Electricity provider is BESCOM in Bengaluru."},
		{Kind: store.MemoryPreference, Content: "Prefers reminders in the evening, not in the morning."},
		{Kind: store.MemoryNote, Content: "Landlord accepts UPI only."},
	}
	for i := range memories {
		memories[i].Source = source
		if err := s.AddMemory(ctx, &memories[i]); err != nil {
			return sum, err
		}
		sum.Memories++
	}

	payload, err := json.Marshal(map[string]string{
		"to":      "billing@airtel.example",
		"subject": "Request to waive the late fee",
		"body":    "Hello,\n\nI paid my October bill two days late because of a bank holiday. Please waive the late fee of Rs 100 on account 7712-0094.\n\nThank you.",
	})
	if err != nil {
		return sum, err
	}
	svc := actions.New(actions.Options{Store: s, Now: func() time.Time { return today }})
	if _, err := svc.Request(ctx, actions.Request{
		Kind:        actions.KindSendEmail,
		Payload:     payload,
		RequestedBy: actions.ActorAgent,
		SourceRef:   source,
	}); err != nil {
		return sum, err
	}
	sum.Actions++

	payBy := today.AddDate(0, 0, 25)
	warranty := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	amount := int64(99900)
	airtel := ids["Airtel broadband"]
	text := fmt.Sprintf(airtelEmail, payBy.Format("02-Jan-2006"))
	proposals, err := s.CreateIngestion(ctx, &store.Ingestion{
		SourceKind:  "text",
		SourceName:  "airtel-november.eml",
		ContentHash: ingest.Hash(text),
		Summary:     "Airtel fiber bill for November 2026 and the router warranty date.",
	}, []store.Proposal{
		{Title: "Airtel broadband", Category: store.CategoryBill, AmountMinor: &amount, Currency: currency, DueOn: &payBy,
			Recurrence: store.RecurMonthly, Evidence: "Amount due: Rs 999.00 Pay by: " + payBy.Format("02-Jan-2006"), Confidence: 0.94, DuplicateOf: &airtel},
		{Title: "Airtel router warranty", Category: store.CategoryWarranty, DueOn: &warranty, Recurrence: store.RecurNone,
			Evidence: "Your router warranty is valid till 01-Jun-2027.", Confidence: 0.71},
	})
	if err != nil {
		return sum, err
	}
	sum.Proposals = len(proposals)
	return sum, nil
}

func isEmpty(ctx context.Context, s *store.Store) (bool, error) {
	obligations, err := s.ListObligations(ctx, store.ObligationFilter{Limit: 1})
	if err != nil {
		return false, err
	}
	memories, err := s.ListMemories(ctx, 1)
	if err != nil {
		return false, err
	}
	return len(obligations) == 0 && len(memories) == 0, nil
}
