package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

type kind struct {
	needsApproval   bool
	needsObligation bool
	prepare         func(s *Service, o *store.Obligation, raw json.RawMessage) (payload any, summary string, err error)
	run             func(ctx context.Context, s *Service, a store.Action, o *store.Obligation) (string, error)
}

var kinds = map[string]kind{
	KindMarkPaid:      {needsObligation: true, prepare: prepareMarkPaid, run: runMarkPaid},
	KindSnooze:        {needsObligation: true, prepare: prepareSnooze, run: runSnooze},
	KindCalendarEvent: {needsObligation: true, prepare: prepareCalendar, run: runCalendar},
	KindSendEmail:     {needsApproval: true, prepare: prepareEmail, run: runEmail},
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}
	return nil
}

func requireActive(o *store.Obligation) error {
	if o.Status == store.StatusDone || o.Status == store.StatusCancelled {
		return fmt.Errorf("obligation %d is already %s", o.ID, o.Status)
	}
	return nil
}

func prepareMarkPaid(_ *Service, o *store.Obligation, _ json.RawMessage) (any, string, error) {
	if err := requireActive(o); err != nil {
		return nil, "", err
	}
	if o.DueOn != nil {
		if next, ok := NextDue(*o.DueOn, o.Recurrence); ok {
			return struct{}{}, fmt.Sprintf("Mark %q paid; next due %s", o.Title, next.Format(time.DateOnly)), nil
		}
	}
	return struct{}{}, fmt.Sprintf("Mark %q paid", o.Title), nil
}

func runMarkPaid(ctx context.Context, s *Service, _ store.Action, o *store.Obligation) (string, error) {
	if err := requireActive(o); err != nil {
		return "", err
	}
	result := "marked done"
	o.Status = store.StatusDone
	if o.DueOn != nil {
		if next, ok := NextDue(*o.DueOn, o.Recurrence); ok {
			o.DueOn, o.Status, o.SnoozedUntil = &next, store.StatusOpen, nil
			result = "next due " + next.Format(time.DateOnly)
		}
	}
	if err := s.store.UpdateObligation(ctx, o); err != nil {
		return "", err
	}
	return result, nil
}

type snoozePayload struct {
	Until string `json:"until"`
}

func prepareSnooze(s *Service, o *store.Obligation, raw json.RawMessage) (any, string, error) {
	var p snoozePayload
	if err := decode(raw, &p); err != nil {
		return nil, "", err
	}
	if err := requireActive(o); err != nil {
		return nil, "", err
	}
	until, err := time.Parse(time.DateOnly, strings.TrimSpace(p.Until))
	if err != nil {
		return nil, "", fmt.Errorf("until must be YYYY-MM-DD, got %q", p.Until)
	}
	if !until.After(s.today()) {
		return nil, "", errors.New("until must be after today")
	}
	p.Until = until.Format(time.DateOnly)
	return p, fmt.Sprintf("Snooze %q until %s", o.Title, p.Until), nil
}

func runSnooze(ctx context.Context, s *Service, a store.Action, o *store.Obligation) (string, error) {
	var p snoozePayload
	if err := decode(json.RawMessage(a.Payload), &p); err != nil {
		return "", err
	}
	until, err := time.Parse(time.DateOnly, p.Until)
	if err != nil {
		return "", err
	}
	o.Status, o.SnoozedUntil = store.StatusSnoozed, &until
	if err := s.store.UpdateObligation(ctx, o); err != nil {
		return "", err
	}
	return "snoozed until " + p.Until, nil
}

type calendarPayload struct {
	RemindDaysBefore *int `json:"remind_days_before"`
}

func prepareCalendar(_ *Service, o *store.Obligation, raw json.RawMessage) (any, string, error) {
	var p calendarPayload
	if err := decode(raw, &p); err != nil {
		return nil, "", err
	}
	if o.DueOn == nil {
		return nil, "", fmt.Errorf("obligation %d has no due date", o.ID)
	}
	if p.RemindDaysBefore == nil {
		one := 1
		p.RemindDaysBefore = &one
	}
	if *p.RemindDaysBefore < 0 || *p.RemindDaysBefore > 30 {
		return nil, "", errors.New("remind_days_before must be from 0 to 30")
	}
	return p, fmt.Sprintf("Calendar reminder for %q on %s", o.Title, o.DueOn.Format(time.DateOnly)), nil
}

func runCalendar(_ context.Context, s *Service, a store.Action, o *store.Obligation) (string, error) {
	var p calendarPayload
	if err := decode(json.RawMessage(a.Payload), &p); err != nil {
		return "", err
	}
	if o.DueOn == nil {
		return "", fmt.Errorf("obligation %d has no due date", o.ID)
	}
	return calendarFile(*o, *p.RemindDaysBefore, s.now()), nil
}

type emailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func prepareEmail(_ *Service, _ *store.Obligation, raw json.RawMessage) (any, string, error) {
	var p emailPayload
	if err := decode(raw, &p); err != nil {
		return nil, "", err
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(p.To))
	if err != nil {
		return nil, "", fmt.Errorf("invalid recipient %q", p.To)
	}
	p.To = addr.Address
	p.Subject = strings.TrimSpace(p.Subject)
	p.Body = strings.TrimSpace(p.Body)
	switch {
	case p.Subject == "" || p.Body == "":
		return nil, "", errors.New("subject and body are required")
	case strings.ContainsAny(p.Subject, "\r\n"):
		return nil, "", errors.New("subject must be one line")
	case utf8.RuneCountInString(p.Subject) > 200:
		return nil, "", errors.New("subject is longer than 200 characters")
	case utf8.RuneCountInString(p.Body) > 10_000:
		return nil, "", errors.New("body is longer than 10000 characters")
	}
	return p, fmt.Sprintf("Email %s: %q", p.To, p.Subject), nil
}

func runEmail(ctx context.Context, s *Service, a store.Action, _ *store.Obligation) (string, error) {
	if s.mailer == nil {
		return "", ErrEmailNotConfigured
	}
	var p emailPayload
	if err := decode(json.RawMessage(a.Payload), &p); err != nil {
		return "", err
	}
	if err := s.mailer.Send(ctx, p.To, p.Subject, p.Body); err != nil {
		return "", err
	}
	return "sent to " + p.To, nil
}
