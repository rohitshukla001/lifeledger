package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrAlreadyDecided = errors.New("store: proposal was already accepted or rejected")

type Ingestion struct {
	ID          int64
	SourceKind  string
	SourceName  string
	ContentHash string
	Summary     string
	CreatedAt   time.Time
}

type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalAccepted ProposalStatus = "accepted"
	ProposalRejected ProposalStatus = "rejected"
)

type Proposal struct {
	ID           int64
	IngestionID  int64
	Title        string
	Category     Category
	AmountMinor  *int64
	Currency     string
	DueOn        *time.Time
	Recurrence   Recurrence
	Notes        string
	Evidence     string
	Confidence   float64
	DuplicateOf  *int64
	Status       ProposalStatus
	ObligationID *int64
	CreatedAt    time.Time
	DecidedAt    *time.Time
}

const proposalColumns = `id, ingestion_id, title, category, amount_minor, currency, due_on, recurrence, notes,
	evidence, confidence, duplicate_of, status, obligation_id, created_at, decided_at`

func (s *Store) CreateIngestion(ctx context.Context, in *Ingestion, proposals []Proposal) ([]Proposal, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		now := s.stamp()
		res, err := tx.ExecContext(ctx, `INSERT INTO ingestions (source_kind, source_name, content_hash, summary, created_at)
			VALUES (?, ?, ?, ?, ?)`, in.SourceKind, in.SourceName, in.ContentHash, in.Summary, now)
		if err != nil {
			return fmt.Errorf("store: create ingestion: %w", err)
		}
		if in.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		in.CreatedAt = fromMillis(now)

		for i := range proposals {
			p := &proposals[i]
			p.IngestionID, p.Status, p.CreatedAt = in.ID, ProposalPending, fromMillis(now)
			res, err := tx.ExecContext(ctx, `INSERT INTO proposals (
				ingestion_id, title, category, amount_minor, currency, due_on, recurrence, notes,
				evidence, confidence, duplicate_of, status, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				p.IngestionID, p.Title, p.Category, p.AmountMinor, nullString(p.Currency), nullDate(p.DueOn), p.Recurrence,
				p.Notes, p.Evidence, p.Confidence, p.DuplicateOf, p.Status, now)
			if err != nil {
				return fmt.Errorf("store: create proposal %q: %w", p.Title, err)
			}
			if p.ID, err = res.LastInsertId(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return proposals, nil
}

func (s *Store) IngestionByHash(ctx context.Context, hash string) (Ingestion, error) {
	var in Ingestion
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, source_kind, source_name, content_hash, summary, created_at
		FROM ingestions WHERE content_hash = ?`, hash).
		Scan(&in.ID, &in.SourceKind, &in.SourceName, &in.ContentHash, &in.Summary, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Ingestion{}, ErrNotFound
	}
	if err != nil {
		return Ingestion{}, err
	}
	in.CreatedAt = fromMillis(created)
	return in, nil
}

type ProposalFilter struct {
	IngestionID int64
	Status      ProposalStatus
}

func (s *Store) ListProposals(ctx context.Context, f ProposalFilter) ([]Proposal, error) {
	var where []string
	var args []any
	if f.IngestionID != 0 {
		where = append(where, "ingestion_id = ?")
		args = append(args, f.IngestionID)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	query := `SELECT ` + proposalColumns + ` FROM proposals`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list proposals: %w", err)
	}
	defer rows.Close()

	var out []Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProposal(ctx context.Context, id int64) (Proposal, error) {
	p, err := scanProposal(s.db.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, ErrNotFound
	}
	return p, err
}

func (s *Store) AcceptProposal(ctx context.Context, id int64, source string) (Obligation, error) {
	var o Obligation
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		p, err := scanProposal(tx.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if p.Status != ProposalPending {
			return ErrAlreadyDecided
		}

		o = Obligation{
			Title:       p.Title,
			Category:    p.Category,
			AmountMinor: p.AmountMinor,
			Currency:    p.Currency,
			DueOn:       p.DueOn,
			Recurrence:  p.Recurrence,
			Status:      StatusOpen,
			Notes:       p.Notes,
			Source:      source,
			SourceRef:   fmt.Sprintf("ingestion:%d", p.IngestionID),
		}
		if err := s.insertObligation(ctx, tx, &o); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE proposals SET status = ?, obligation_id = ?, decided_at = ? WHERE id = ?`,
			ProposalAccepted, o.ID, s.stamp(), id)
		return err
	})
	if err != nil {
		return Obligation{}, err
	}
	return o, nil
}

func (s *Store) RejectProposal(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE proposals SET status = ?, decided_at = ? WHERE id = ? AND status = ?`,
		ProposalRejected, s.stamp(), id, ProposalPending)
	if err != nil {
		return fmt.Errorf("store: reject proposal %d: %w", id, err)
	}
	if err := checkAffected(res); err == nil {
		return nil
	}
	if _, err := s.GetProposal(ctx, id); err != nil {
		return err
	}
	return ErrAlreadyDecided
}

func scanProposal(sc scanner) (Proposal, error) {
	var (
		p                Proposal
		amount, dup, obl sql.NullInt64
		currency, due    sql.NullString
		created          int64
		decided          sql.NullInt64
	)
	err := sc.Scan(&p.ID, &p.IngestionID, &p.Title, &p.Category, &amount, &currency, &due, &p.Recurrence, &p.Notes,
		&p.Evidence, &p.Confidence, &dup, &p.Status, &obl, &created, &decided)
	if err != nil {
		return Proposal{}, err
	}
	if amount.Valid {
		p.AmountMinor = &amount.Int64
	}
	if dup.Valid {
		p.DuplicateOf = &dup.Int64
	}
	if obl.Valid {
		p.ObligationID = &obl.Int64
	}
	if decided.Valid {
		t := fromMillis(decided.Int64)
		p.DecidedAt = &t
	}
	p.Currency = currency.String
	if p.DueOn, err = parseDate(due); err != nil {
		return Proposal{}, fmt.Errorf("store: proposal %d due_on: %w", p.ID, err)
	}
	p.CreatedAt = fromMillis(created)
	return p, nil
}
