package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type NotificationKind string

const (
	NotificationReminder NotificationKind = "reminder"
	NotificationReview   NotificationKind = "review"
)

type Notification struct {
	ID           int64
	Kind         NotificationKind
	DedupeKey    string
	ObligationID *int64
	Title        string
	Body         string
	Attempts     int
	LastError    string
	DeliveredAt  *time.Time
	CreatedAt    time.Time
}

const notificationColumns = `id, kind, dedupe_key, obligation_id, title, body, attempts, last_error, delivered_at, created_at`

func (s *Store) AddNotification(ctx context.Context, n *Notification) (bool, error) {
	now := s.stamp()
	res, err := s.db.ExecContext(ctx, `INSERT INTO notifications (kind, dedupe_key, obligation_id, title, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (dedupe_key) DO NOTHING`,
		n.Kind, n.DedupeKey, n.ObligationID, n.Title, n.Body, now)
	if err != nil {
		return false, fmt.Errorf("store: add notification %s: %w", n.DedupeKey, err)
	}
	if affected, err := res.RowsAffected(); err != nil || affected == 0 {
		return false, err
	}
	if n.ID, err = res.LastInsertId(); err != nil {
		return false, err
	}
	n.CreatedAt = fromMillis(now)
	return true, nil
}

func (s *Store) HasNotification(ctx context.Context, dedupeKey string) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM notifications WHERE dedupe_key = ?)`, dedupeKey).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("store: check notification %s: %w", dedupeKey, err)
	}
	return found, nil
}

func (s *Store) ListNotifications(ctx context.Context, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = -1
	}
	return s.queryNotifications(ctx, `SELECT `+notificationColumns+` FROM notifications ORDER BY id DESC LIMIT ?`, limit)
}

func (s *Store) UndeliveredNotifications(ctx context.Context, since time.Time, maxAttempts int) ([]Notification, error) {
	return s.queryNotifications(ctx, `SELECT `+notificationColumns+` FROM notifications
		WHERE delivered_at IS NULL AND attempts < ? AND created_at >= ? ORDER BY id`, maxAttempts, since.UnixMilli())
}

func (s *Store) MarkNotificationsDelivered(ctx context.Context, ids ...int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		now := s.stamp()
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE notifications SET delivered_at = ?, attempts = attempts + 1, last_error = ''
				WHERE id = ?`, now, id); err != nil {
				return fmt.Errorf("store: mark notification %d delivered: %w", id, err)
			}
		}
		return nil
	})
}

func (s *Store) MarkNotificationsFailed(ctx context.Context, sendErr error, ids ...int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE notifications SET attempts = attempts + 1, last_error = ? WHERE id = ?`,
				sendErr.Error(), id); err != nil {
				return fmt.Errorf("store: mark notification %d failed: %w", id, err)
			}
		}
		return nil
	})
}

func (s *Store) WakeSnoozed(ctx context.Context, today time.Time) ([]Obligation, error) {
	var woken []Obligation
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `UPDATE obligations SET status = ?, snoozed_until = NULL, updated_at = ?
			WHERE status = ? AND snoozed_until <= ? RETURNING `+obligationColumns,
			StatusOpen, s.stamp(), StatusSnoozed, nullDate(&today))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanObligation(rows)
			if err != nil {
				return err
			}
			woken = append(woken, o)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("store: wake snoozed obligations: %w", err)
	}
	return woken, nil
}

func (s *Store) queryNotifications(ctx context.Context, query string, args ...any) ([]Notification, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list notifications: %w", err)
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		var (
			n          Notification
			obligation sql.NullInt64
			delivered  sql.NullInt64
			created    int64
		)
		if err := rows.Scan(&n.ID, &n.Kind, &n.DedupeKey, &obligation, &n.Title, &n.Body, &n.Attempts, &n.LastError,
			&delivered, &created); err != nil {
			return nil, err
		}
		if obligation.Valid {
			n.ObligationID = &obligation.Int64
		}
		if delivered.Valid {
			t := fromMillis(delivered.Int64)
			n.DeliveredAt = &t
		}
		n.CreatedAt = fromMillis(created)
		out = append(out, n)
	}
	return out, rows.Err()
}
