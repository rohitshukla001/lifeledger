package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNotificationDedupeAndDelivery(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	o := &Obligation{Title: "Rent", DueOn: date("2026-10-10")}
	if err := s.CreateObligation(ctx, o); err != nil {
		t.Fatal(err)
	}

	first := &Notification{Kind: NotificationReminder, DedupeKey: "reminder:1:2026-10-10:1d", ObligationID: &o.ID, Title: "Due tomorrow: Rent", Body: "Rent is due tomorrow."}
	if created, err := s.AddNotification(ctx, first); err != nil || !created || first.ID != 1 {
		t.Fatalf("add: created=%v id=%d err=%v", created, first.ID, err)
	}
	again := &Notification{Kind: NotificationReminder, DedupeKey: first.DedupeKey, Title: "x"}
	if created, err := s.AddNotification(ctx, again); err != nil || created || again.ID != 0 {
		t.Fatalf("duplicate: created=%v id=%d err=%v", created, again.ID, err)
	}
	if found, _ := s.HasNotification(ctx, first.DedupeKey); !found {
		t.Fatal("HasNotification = false")
	}
	if found, _ := s.HasNotification(ctx, "review:2026-10-11"); found {
		t.Fatal("HasNotification = true for an unknown key")
	}
	review := &Notification{Kind: NotificationReview, DedupeKey: "review:2026-10-11", Title: "Weekly review", Body: "All good."}
	if _, err := s.AddNotification(ctx, review); err != nil {
		t.Fatal(err)
	}

	since := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	pending, err := s.UndeliveredNotifications(ctx, since, 3)
	if err != nil || len(pending) != 2 || *pending[0].ObligationID != o.ID || pending[1].ObligationID != nil {
		t.Fatalf("pending = %+v, %v", pending, err)
	}

	if err := s.MarkNotificationsFailed(ctx, errors.New("telegram: 502"), 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkNotificationsDelivered(ctx, 2); err != nil {
		t.Fatal(err)
	}
	pending, _ = s.UndeliveredNotifications(ctx, since, 3)
	if len(pending) != 1 || pending[0].Attempts != 1 || pending[0].LastError != "telegram: 502" {
		t.Fatalf("after one failure = %+v", pending)
	}
	if pending, _ = s.UndeliveredNotifications(ctx, since, 1); len(pending) != 0 {
		t.Fatalf("notifications at the attempt limit must be skipped: %+v", pending)
	}
	if pending, _ = s.UndeliveredNotifications(ctx, since.Add(48*time.Hour), 3); len(pending) != 0 {
		t.Fatalf("old notifications must be skipped: %+v", pending)
	}

	all, err := s.ListNotifications(ctx, 0)
	if err != nil || len(all) != 2 || all[0].ID != 2 || all[0].DeliveredAt == nil || all[0].LastError != "" || all[0].Attempts != 2 {
		t.Fatalf("list = %+v, %v", all, err)
	}

	if err := s.DeleteObligation(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ = s.ListNotifications(ctx, 0); len(all) != 1 {
		t.Fatalf("reminders must go with their obligation: %+v", all)
	}
}

func TestWakeSnoozed(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	for _, o := range []*Obligation{
		{Title: "Ends today", Status: StatusSnoozed, SnoozedUntil: date("2026-10-10")},
		{Title: "Ended", Status: StatusSnoozed, SnoozedUntil: date("2026-10-01")},
		{Title: "Later", Status: StatusSnoozed, SnoozedUntil: date("2026-10-11")},
		{Title: "Open", Status: StatusOpen},
	} {
		if err := s.CreateObligation(ctx, o); err != nil {
			t.Fatal(err)
		}
	}

	woken, err := s.WakeSnoozed(ctx, *date("2026-10-10"))
	if err != nil || len(woken) != 2 {
		t.Fatalf("woken = %+v, %v", woken, err)
	}
	for _, o := range woken {
		if o.Status != StatusOpen || o.SnoozedUntil != nil {
			t.Errorf("woken = %+v", o)
		}
	}
	later, _ := s.GetObligation(ctx, 3)
	if later.Status != StatusSnoozed {
		t.Fatalf("later = %+v", later)
	}
	if woken, _ = s.WakeSnoozed(ctx, *date("2026-10-10")); len(woken) != 0 {
		t.Fatalf("second wake = %+v", woken)
	}
}
