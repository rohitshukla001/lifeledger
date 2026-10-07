package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type MemoryKind string

const (
	MemoryFact       MemoryKind = "fact"
	MemoryPreference MemoryKind = "preference"
	MemoryNote       MemoryKind = "note"
)

type Memory struct {
	ID        int64
	Kind      MemoryKind
	Content   string
	Source    string
	SourceRef string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Store) AddMemory(ctx context.Context, m *Memory) error {
	if m.Kind == "" {
		m.Kind = MemoryFact
	}
	if m.Source == "" {
		m.Source = "user"
	}
	now := s.stamp()
	res, err := s.db.ExecContext(ctx, `INSERT INTO memories (kind, content, source, source_ref, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, m.Kind, m.Content, m.Source, m.SourceRef, now, now)
	if err != nil {
		return fmt.Errorf("store: add memory: %w", err)
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	m.CreatedAt, m.UpdatedAt = fromMillis(now), fromMillis(now)
	return nil
}

func (s *Store) GetMemory(ctx context.Context, id int64) (Memory, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, kind, content, source, source_ref, created_at, updated_at
		FROM memories WHERE id = ?`, id)
	m, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, ErrNotFound
	}
	return m, err
}

func (s *Store) UpdateMemoryContent(ctx context.Context, id int64, content string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE memories SET content = ?, updated_at = ? WHERE id = ?`, content, s.stamp(), id)
	if err != nil {
		return fmt.Errorf("store: update memory %d: %w", id, err)
	}
	return checkAffected(res)
}

func (s *Store) DeleteMemory(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete memory %d: %w", id, err)
	}
	return checkAffected(res)
}

func (s *Store) ListMemories(ctx context.Context, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, content, source, source_ref, created_at, updated_at
		FROM memories ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list memories: %w", err)
	}
	defer rows.Close()

	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanMemory(sc scanner) (Memory, error) {
	var m Memory
	var created, modified int64
	if err := sc.Scan(&m.ID, &m.Kind, &m.Content, &m.Source, &m.SourceRef, &created, &modified); err != nil {
		return Memory{}, err
	}
	m.CreatedAt, m.UpdatedAt = fromMillis(created), fromMillis(modified)
	return m, nil
}
