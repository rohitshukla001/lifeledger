package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
)

type Conversation struct {
	ID        int64
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Store) CreateConversation(ctx context.Context, title string) (Conversation, error) {
	now := s.stamp()
	res, err := s.db.ExecContext(ctx, `INSERT INTO conversations (title, created_at, updated_at) VALUES (?, ?, ?)`, title, now, now)
	if err != nil {
		return Conversation{}, fmt.Errorf("store: create conversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Conversation{}, err
	}
	return Conversation{ID: id, Title: title, CreatedAt: fromMillis(now), UpdatedAt: fromMillis(now)}, nil
}

func (s *Store) GetConversation(ctx context.Context, id int64) (Conversation, error) {
	var c Conversation
	var created, modified int64
	err := s.db.QueryRowContext(ctx, `SELECT id, title, created_at, updated_at FROM conversations WHERE id = ?`, id).
		Scan(&c.ID, &c.Title, &created, &modified)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, err
	}
	c.CreatedAt, c.UpdatedAt = fromMillis(created), fromMillis(modified)
	return c, nil
}

func (s *Store) ListConversations(ctx context.Context, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, created_at, updated_at FROM conversations
		ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list conversations: %w", err)
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var c Conversation
		var created, modified int64
		if err := rows.Scan(&c.ID, &c.Title, &created, &modified); err != nil {
			return nil, err
		}
		c.CreatedAt, c.UpdatedAt = fromMillis(created), fromMillis(modified)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteConversation(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete conversation %d: %w", id, err)
	}
	return checkAffected(res)
}

func (s *Store) AppendMessages(ctx context.Context, conversationID int64, msgs ...llm.Message) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		now := s.stamp()
		res, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now, conversationID)
		if err != nil {
			return err
		}
		if err := checkAffected(res); err != nil {
			return err
		}

		stmt, err := tx.PrepareContext(ctx, `INSERT INTO messages (conversation_id, role, content, tool_calls, tool_call_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, m := range msgs {
			var calls any
			if len(m.ToolCalls) > 0 {
				b, err := json.Marshal(m.ToolCalls)
				if err != nil {
					return err
				}
				calls = string(b)
			}
			if _, err := stmt.ExecContext(ctx, conversationID, m.Role, m.Content, calls, m.ToolCallID, now); err != nil {
				return fmt.Errorf("store: append %s message: %w", m.Role, err)
			}
		}
		return nil
	})
}

func (s *Store) Messages(ctx context.Context, conversationID int64) ([]llm.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role, content, tool_calls, tool_call_id FROM messages
		WHERE conversation_id = ? ORDER BY id`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("store: load messages: %w", err)
	}
	defer rows.Close()

	var out []llm.Message
	for rows.Next() {
		var m llm.Message
		var calls sql.NullString
		if err := rows.Scan(&m.Role, &m.Content, &calls, &m.ToolCallID); err != nil {
			return nil, err
		}
		if calls.Valid {
			if err := json.Unmarshal([]byte(calls.String), &m.ToolCalls); err != nil {
				return nil, fmt.Errorf("store: decode tool calls: %w", err)
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
