package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

const (
	KindMarkPaid      = "mark_paid"
	KindSnooze        = "snooze"
	KindCalendarEvent = "calendar_event"
	KindSendEmail     = "send_email"

	ActorUser   = "user"
	ActorAgent  = "agent"
	ActorPolicy = "policy"
)

type Options struct {
	Store    *store.Store
	Mailer   Mailer
	Location *time.Location
	Logger   *slog.Logger
	Now      func() time.Time
}

type Service struct {
	store  *store.Store
	mailer Mailer
	loc    *time.Location
	log    *slog.Logger
	now    func() time.Time
}

type Request struct {
	Kind         string
	ObligationID *int64
	Payload      json.RawMessage
	RequestedBy  string
	SourceRef    string
}

func New(o Options) *Service {
	s := &Service{store: o.Store, mailer: o.Mailer, loc: o.Location, log: o.Logger, now: o.Now}
	if s.loc == nil {
		s.loc = time.UTC
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

func NeedsApproval(kind string) bool {
	k, ok := kinds[kind]
	return !ok || k.needsApproval
}

func (s *Service) Request(ctx context.Context, r Request) (store.Action, error) {
	k, ok := kinds[r.Kind]
	if !ok {
		return store.Action{}, fmt.Errorf("unknown action %q", r.Kind)
	}

	var o *store.Obligation
	if r.ObligationID != nil {
		got, err := s.store.GetObligation(ctx, *r.ObligationID)
		if errors.Is(err, store.ErrNotFound) {
			return store.Action{}, fmt.Errorf("no obligation with id %d", *r.ObligationID)
		}
		if err != nil {
			return store.Action{}, err
		}
		o = &got
	} else if k.needsObligation {
		return store.Action{}, fmt.Errorf("%s needs an obligation id", r.Kind)
	}

	raw := r.Payload
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	payload, summary, err := k.prepare(s, o, raw)
	if err != nil {
		return store.Action{}, err
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return store.Action{}, err
	}

	a := store.Action{
		Kind:         r.Kind,
		ObligationID: r.ObligationID,
		Payload:      string(b),
		Summary:      summary,
		RequestedBy:  r.RequestedBy,
		SourceRef:    r.SourceRef,
	}
	if err := s.store.CreateAction(ctx, &a); err != nil {
		return store.Action{}, err
	}
	s.audit(ctx, r.RequestedBy, "action.requested", a.ID, summary)

	if k.needsApproval {
		return a, nil
	}
	return s.execute(ctx, a.ID, ActorPolicy)
}

func (s *Service) Approve(ctx context.Context, id int64, actor string) (store.Action, error) {
	return s.execute(ctx, id, actor)
}

func (s *Service) Reject(ctx context.Context, id int64, actor string) error {
	if err := s.store.DecideAction(ctx, id, store.ActionRejected); err != nil {
		return err
	}
	s.audit(ctx, actor, "action.rejected", id, "")
	return nil
}

func (s *Service) Pending(ctx context.Context) ([]store.Action, error) {
	return s.store.ListActions(ctx, store.ActionPending, 0)
}

func (s *Service) Get(ctx context.Context, id int64) (store.Action, error) {
	return s.store.GetAction(ctx, id)
}

func (s *Service) execute(ctx context.Context, id int64, actor string) (store.Action, error) {
	if err := s.store.DecideAction(ctx, id, store.ActionApproved); err != nil {
		return store.Action{}, err
	}
	s.audit(ctx, actor, "action.approved", id, "")

	a, err := s.store.GetAction(ctx, id)
	if err != nil {
		return store.Action{}, err
	}
	result, runErr := s.run(ctx, a)
	if err := s.store.FinishAction(ctx, id, result, runErr); err != nil {
		return store.Action{}, err
	}
	if runErr != nil {
		s.log.Warn("action failed", "action_id", id, "kind", a.Kind, "err", runErr)
		s.audit(ctx, ActorPolicy, "action.failed", id, runErr.Error())
	} else {
		s.audit(ctx, ActorPolicy, "action.done", id, a.Summary)
	}
	return s.store.GetAction(ctx, id)
}

func (s *Service) run(ctx context.Context, a store.Action) (string, error) {
	k := kinds[a.Kind]
	var o *store.Obligation
	if a.ObligationID != nil {
		got, err := s.store.GetObligation(ctx, *a.ObligationID)
		if err != nil {
			return "", fmt.Errorf("load obligation %d: %w", *a.ObligationID, err)
		}
		o = &got
	} else if k.needsObligation {
		return "", errors.New("the obligation of this action was deleted")
	}
	return k.run(ctx, s, a, o)
}

func (s *Service) audit(ctx context.Context, actor, event string, actionID int64, detail string) {
	if err := s.store.AddAudit(ctx, actor, event, fmt.Sprintf("action:%d", actionID), detail); err != nil {
		s.log.Error("cannot write audit entry", "event", event, "action_id", actionID, "err", err)
	}
}

func (s *Service) today() time.Time {
	y, m, d := s.now().In(s.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
