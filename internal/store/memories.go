package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
	res, err := s.db.ExecContext(ctx, `UPDATE memories SET content = ?, embedding = NULL, embedding_model = '', updated_at = ?
		WHERE id = ?`, content, s.stamp(), id)
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
		limit = -1
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

func (s *Store) SetMemoryEmbedding(ctx context.Context, id int64, model string, vec []float32) error {
	res, err := s.db.ExecContext(ctx, `UPDATE memories SET embedding = ?, embedding_model = ? WHERE id = ?`,
		encodeVector(vec), model, id)
	if err != nil {
		return fmt.Errorf("store: set embedding for memory %d: %w", id, err)
	}
	return checkAffected(res)
}

func (s *Store) MemoryEmbeddings(ctx context.Context, model string) (map[int64][]float32, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, embedding FROM memories
		WHERE embedding IS NOT NULL AND embedding_model = ?`, model)
	if err != nil {
		return nil, fmt.Errorf("store: load embeddings: %w", err)
	}
	defer rows.Close()

	out := map[int64][]float32{}
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		vec, err := decodeVector(blob)
		if err != nil {
			return nil, fmt.Errorf("store: memory %d: %w", id, err)
		}
		out[id] = vec
	}
	return out, rows.Err()
}

func (s *Store) MemoriesWithoutEmbedding(ctx context.Context, model string, limit int) ([]Memory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, content, source, source_ref, created_at, updated_at
		FROM memories WHERE embedding IS NULL OR embedding_model <> ? ORDER BY id LIMIT ?`, model, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list unembedded memories: %w", err)
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

func encodeVector(vec []float32) []byte {
	buf := make([]byte, 4*len(vec))
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(buf []byte) ([]float32, error) {
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("embedding blob has %d bytes, not a multiple of 4", len(buf))
	}
	vec := make([]float32, len(buf)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[4*i:]))
	}
	return vec, nil
}
