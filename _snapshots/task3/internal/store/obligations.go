package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Category string

const (
	CategoryBill         Category = "bill"
	CategorySubscription Category = "subscription"
	CategoryRenewal      Category = "renewal"
	CategoryWarranty     Category = "warranty"
	CategoryDocument     Category = "document"
	CategoryDeadline     Category = "deadline"
	CategoryOther        Category = "other"
)

type Recurrence string

const (
	RecurNone      Recurrence = "none"
	RecurWeekly    Recurrence = "weekly"
	RecurMonthly   Recurrence = "monthly"
	RecurQuarterly Recurrence = "quarterly"
	RecurYearly    Recurrence = "yearly"
)

type Status string

const (
	StatusOpen      Status = "open"
	StatusDone      Status = "done"
	StatusSnoozed   Status = "snoozed"
	StatusCancelled Status = "cancelled"
)

type Obligation struct {
	ID           int64
	Title        string
	Category     Category
	AmountMinor  *int64
	Currency     string
	DueOn        *time.Time
	Recurrence   Recurrence
	Status       Status
	SnoozedUntil *time.Time
	Notes        string
	Source       string
	SourceRef    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ObligationFilter struct {
	Statuses  []Status
	Category  Category
	DueBefore *time.Time
	Limit     int
}

const obligationColumns = `id, title, category, amount_minor, currency, due_on, recurrence, status,
	snoozed_until, notes, source, source_ref, created_at, updated_at`

func (s *Store) CreateObligation(ctx context.Context, o *Obligation) error {
	if o.Category == "" {
		o.Category = CategoryOther
	}
	if o.Recurrence == "" {
		o.Recurrence = RecurNone
	}
	if o.Status == "" {
		o.Status = StatusOpen
	}
	if o.Source == "" {
		o.Source = "user"
	}
	now := s.stamp()

	res, err := s.db.ExecContext(ctx, `INSERT INTO obligations (
		title, category, amount_minor, currency, due_on, recurrence, status,
		snoozed_until, notes, source, source_ref, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.Title, o.Category, o.AmountMinor, nullString(o.Currency), nullDate(o.DueOn), o.Recurrence, o.Status,
		nullDate(o.SnoozedUntil), o.Notes, o.Source, o.SourceRef, now, now)
	if err != nil {
		return fmt.Errorf("store: create obligation: %w", err)
	}
	if o.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	o.CreatedAt, o.UpdatedAt = fromMillis(now), fromMillis(now)
	return nil
}

func (s *Store) UpdateObligation(ctx context.Context, o *Obligation) error {
	now := s.stamp()
	res, err := s.db.ExecContext(ctx, `UPDATE obligations SET
		title = ?, category = ?, amount_minor = ?, currency = ?, due_on = ?, recurrence = ?, status = ?,
		snoozed_until = ?, notes = ?, source = ?, source_ref = ?, updated_at = ?
	WHERE id = ?`,
		o.Title, o.Category, o.AmountMinor, nullString(o.Currency), nullDate(o.DueOn), o.Recurrence, o.Status,
		nullDate(o.SnoozedUntil), o.Notes, o.Source, o.SourceRef, now, o.ID)
	if err != nil {
		return fmt.Errorf("store: update obligation %d: %w", o.ID, err)
	}
	if err := checkAffected(res); err != nil {
		return err
	}
	o.UpdatedAt = fromMillis(now)
	return nil
}

func (s *Store) GetObligation(ctx context.Context, id int64) (Obligation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+obligationColumns+` FROM obligations WHERE id = ?`, id)
	o, err := scanObligation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Obligation{}, ErrNotFound
	}
	return o, err
}

func (s *Store) DeleteObligation(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM obligations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete obligation %d: %w", id, err)
	}
	return checkAffected(res)
}

func (s *Store) ListObligations(ctx context.Context, f ObligationFilter) ([]Obligation, error) {
	var where []string
	var args []any
	if len(f.Statuses) > 0 {
		where = append(where, "status IN (?"+strings.Repeat(", ?", len(f.Statuses)-1)+")")
		for _, st := range f.Statuses {
			args = append(args, st)
		}
	}
	if f.Category != "" {
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.DueBefore != nil {
		where = append(where, "due_on <= ?")
		args = append(args, nullDate(f.DueBefore))
	}

	query := `SELECT ` + obligationColumns + ` FROM obligations`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY due_on IS NULL, due_on, id`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list obligations: %w", err)
	}
	defer rows.Close()

	var out []Obligation
	for rows.Next() {
		o, err := scanObligation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanObligation(sc scanner) (Obligation, error) {
	var (
		o                 Obligation
		amount            sql.NullInt64
		currency          sql.NullString
		dueOn, snoozed    sql.NullString
		created, modified int64
	)
	err := sc.Scan(&o.ID, &o.Title, &o.Category, &amount, &currency, &dueOn, &o.Recurrence, &o.Status,
		&snoozed, &o.Notes, &o.Source, &o.SourceRef, &created, &modified)
	if err != nil {
		return Obligation{}, err
	}
	if amount.Valid {
		o.AmountMinor = &amount.Int64
	}
	o.Currency = currency.String
	if o.DueOn, err = parseDate(dueOn); err != nil {
		return Obligation{}, fmt.Errorf("store: obligation %d due_on: %w", o.ID, err)
	}
	if o.SnoozedUntil, err = parseDate(snoozed); err != nil {
		return Obligation{}, fmt.Errorf("store: obligation %d snoozed_until: %w", o.ID, err)
	}
	o.CreatedAt, o.UpdatedAt = fromMillis(created), fromMillis(modified)
	return o, nil
}
