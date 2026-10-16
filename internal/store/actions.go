package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ActionStatus string

const (
	ActionPending  ActionStatus = "pending"
	ActionApproved ActionStatus = "approved"
	ActionRejected ActionStatus = "rejected"
	ActionDone     ActionStatus = "done"
	ActionFailed   ActionStatus = "failed"
)

type Action struct {
	ID           int64
	Kind         string
	ObligationID *int64
	Payload      string
	Summary      string
	Status       ActionStatus
	RequestedBy  string
	SourceRef    string
	Result       string
	Error        string
	CreatedAt    time.Time
	DecidedAt    *time.Time
	ExecutedAt   *time.Time
}

type AuditEntry struct {
	ID        int64
	Actor     string
	Event     string
	Subject   string
	Detail    string
	CreatedAt time.Time
}

const actionColumns = `id, kind, obligation_id, payload, summary, status, requested_by, source_ref,
	result, error, created_at, decided_at, executed_at`

func (s *Store) CreateAction(ctx context.Context, a *Action) error {
	now := s.stamp()
	a.Status = ActionPending
	res, err := s.db.ExecContext(ctx, `INSERT INTO actions (kind, obligation_id, payload, summary, status, requested_by, source_ref, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.ObligationID, a.Payload, a.Summary, a.Status, a.RequestedBy, a.SourceRef, now)
	if err != nil {
		return fmt.Errorf("store: create action: %w", err)
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	a.CreatedAt = fromMillis(now)
	return nil
}

func (s *Store) GetAction(ctx context.Context, id int64) (Action, error) {
	a, err := scanAction(s.db.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM actions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Action{}, ErrNotFound
	}
	return a, err
}

func (s *Store) ListActions(ctx context.Context, status ActionStatus, limit int) ([]Action, error) {
	if limit <= 0 {
		limit = -1
	}
	query := `SELECT ` + actionColumns + ` FROM actions`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("store: list actions: %w", err)
	}
	defer rows.Close()

	var out []Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DecideAction(ctx context.Context, id int64, to ActionStatus) error {
	if to != ActionApproved && to != ActionRejected {
		return fmt.Errorf("store: cannot decide action %d as %q", id, to)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE actions SET status = ?, decided_at = ? WHERE id = ? AND status = ?`,
		to, s.stamp(), id, ActionPending)
	if err != nil {
		return fmt.Errorf("store: decide action %d: %w", id, err)
	}
	if checkAffected(res) == nil {
		return nil
	}
	if _, err := s.GetAction(ctx, id); err != nil {
		return err
	}
	return ErrAlreadyDecided
}

func (s *Store) FinishAction(ctx context.Context, id int64, result string, runErr error) error {
	status, msg := ActionDone, ""
	if runErr != nil {
		status, msg = ActionFailed, runErr.Error()
	}
	res, err := s.db.ExecContext(ctx, `UPDATE actions SET status = ?, result = ?, error = ?, executed_at = ?
		WHERE id = ? AND status = ?`, status, result, msg, s.stamp(), id, ActionApproved)
	if err != nil {
		return fmt.Errorf("store: finish action %d: %w", id, err)
	}
	if err := checkAffected(res); err != nil {
		return fmt.Errorf("store: action %d is not approved", id)
	}
	return nil
}

func (s *Store) AddAudit(ctx context.Context, actor, event, subject, detail string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log (actor, event, subject, detail, created_at) VALUES (?, ?, ?, ?, ?)`,
		actor, event, subject, detail, s.stamp())
	if err != nil {
		return fmt.Errorf("store: audit %s: %w", event, err)
	}
	return nil
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor, event, subject, detail, created_at FROM audit_log
		ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var created int64
		if err := rows.Scan(&e.ID, &e.Actor, &e.Event, &e.Subject, &e.Detail, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = fromMillis(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanAction(sc scanner) (Action, error) {
	var (
		a                 Action
		obligation        sql.NullInt64
		created           int64
		decided, executed sql.NullInt64
	)
	err := sc.Scan(&a.ID, &a.Kind, &obligation, &a.Payload, &a.Summary, &a.Status, &a.RequestedBy, &a.SourceRef,
		&a.Result, &a.Error, &created, &decided, &executed)
	if err != nil {
		return Action{}, err
	}
	if obligation.Valid {
		a.ObligationID = &obligation.Int64
	}
	a.CreatedAt = fromMillis(created)
	if decided.Valid {
		t := fromMillis(decided.Int64)
		a.DecidedAt = &t
	}
	if executed.Valid {
		t := fromMillis(executed.Int64)
		a.ExecutedAt = &t
	}
	return a, nil
}
