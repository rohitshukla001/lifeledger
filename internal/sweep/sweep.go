package sweep

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const (
	actor       = "sweep"
	maxAttempts = 5
	retryWindow = 24 * time.Hour
)

type Chatter interface {
	Chat(ctx context.Context, req llm.Request) (*llm.Response, error)
}

type Notifier interface {
	Send(ctx context.Context, text string) error
}

type Schedule struct {
	Day  time.Weekday
	Hour int
}

type Options struct {
	Store      *store.Store
	LLM        Chatter
	Notifier   Notifier
	Location   *time.Location
	Currency   string
	RemindHour int
	Review     *Schedule
	Logger     *slog.Logger
	Now        func() time.Time
}

type Sweeper struct {
	store      *store.Store
	llm        Chatter
	notifier   Notifier
	loc        *time.Location
	currency   string
	remindHour int
	review     *Schedule
	log        *slog.Logger
	now        func() time.Time
}

type Report struct {
	Woken     int
	Reminders int
	Review    bool
	Delivered int
	Failed    int
}

func (r Report) String() string {
	review := "not due"
	if r.Review {
		review = "written"
	}
	return fmt.Sprintf("woke %d snoozed, %d new reminders, scheduled review %s, delivered %d, failed %d",
		r.Woken, r.Reminders, review, r.Delivered, r.Failed)
}

func New(o Options) *Sweeper {
	s := &Sweeper{
		store:      o.Store,
		llm:        o.LLM,
		notifier:   o.Notifier,
		loc:        o.Location,
		currency:   o.Currency,
		remindHour: o.RemindHour,
		review:     o.Review,
		log:        o.Logger,
		now:        o.Now,
	}
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

func (s *Sweeper) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		rep, err := s.RunOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			s.log.Warn("sweep finished with errors", "report", rep.String(), "err", err)
		case rep != Report{}:
			s.log.Info("sweep finished", "report", rep.String())
		default:
			s.log.Debug("sweep finished with nothing to do")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Sweeper) RunOnce(ctx context.Context) (Report, error) {
	var rep Report
	var errs []error
	now := s.now()
	local := now.In(s.loc)
	today := dateOf(local)

	woken, err := s.store.WakeSnoozed(ctx, today)
	if err != nil {
		errs = append(errs, err)
	}
	for _, o := range woken {
		s.audit(ctx, "obligation_woken", o.ID, "snooze ended")
	}
	rep.Woken = len(woken)

	if local.Hour() >= s.remindHour {
		n, err := s.remind(ctx, today)
		rep.Reminders = n
		if err != nil {
			errs = append(errs, err)
		}
	}

	if s.review != nil {
		made, err := s.scheduledReview(ctx, local)
		rep.Review = made
		if err != nil {
			errs = append(errs, err)
		}
	}

	if s.notifier != nil {
		delivered, failed, err := s.deliver(ctx, now)
		rep.Delivered, rep.Failed = delivered, failed
		if err != nil {
			errs = append(errs, err)
		}
	}
	return rep, errors.Join(errs...)
}

func (s *Sweeper) deliver(ctx context.Context, now time.Time) (delivered, failed int, err error) {
	pending, err := s.store.UndeliveredNotifications(ctx, now.Add(-retryWindow), maxAttempts)
	if err != nil {
		return 0, 0, err
	}

	var reminders []store.Notification
	var errs []error
	send := func(text string, batch ...store.Notification) {
		ids := make([]int64, len(batch))
		for i, n := range batch {
			ids[i] = n.ID
		}
		if sendErr := s.notifier.Send(ctx, text); sendErr != nil {
			s.log.Warn("notification not delivered", "ids", ids, "err", sendErr)
			failed += len(batch)
			if err := s.store.MarkNotificationsFailed(ctx, sendErr, ids...); err != nil {
				errs = append(errs, err)
			}
			return
		}
		delivered += len(batch)
		if err := s.store.MarkNotificationsDelivered(ctx, ids...); err != nil {
			errs = append(errs, err)
		}
	}

	for _, n := range pending {
		if n.Kind == store.NotificationReminder {
			reminders = append(reminders, n)
			continue
		}
		send(n.Title+"\n\n"+n.Body, n)
	}
	if len(reminders) > 0 {
		send(reminderMessage(reminders), reminders...)
	}
	return delivered, failed, errors.Join(errs...)
}

func reminderMessage(batch []store.Notification) string {
	if len(batch) == 1 {
		return batch[0].Title + "\n" + batch[0].Body
	}
	var b strings.Builder
	fmt.Fprintf(&b, "LifeLedger: %d reminders\n", len(batch))
	for _, n := range batch {
		fmt.Fprintf(&b, "\n• %s\n  %s", n.Title, n.Body)
	}
	return b.String()
}

func (s *Sweeper) audit(ctx context.Context, event string, obligationID int64, detail string) {
	if err := s.store.AddAudit(ctx, actor, event, fmt.Sprintf("obligation:%d", obligationID), detail); err != nil {
		s.log.Warn("audit entry not written", "event", event, "err", err)
	}
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
