package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Conversation is one continuous question thread locked to a single agent.
type Conversation struct {
	ID        int64     `json:"id"`
	ClassID   int64     `json:"class_id"`
	UserID    int64     `json:"user_id"`
	AgentID   int64     `json:"agent_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message is one stored turn. Citations holds the assistant-side reference list
// as raw JSON so the store stays independent from the HTTP citation shape.
type Message struct {
	ID             int64           `json:"id"`
	ConversationID int64           `json:"conversation_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Citations      json.RawMessage `json:"citations"`
	SkillName      string          `json:"skill_name"`
	ToolCalls      json.RawMessage `json:"tool_calls"`
	CreatedAt      time.Time       `json:"created_at"`
}

// NewMessage is the input for AppendMessage.
type NewMessage struct {
	ConversationID int64
	Role           string
	Content        string
	Citations      json.RawMessage
	SkillName      string
	ToolCalls      json.RawMessage
}

// CreateConversation inserts a conversation with its placeholder title.
func (s *Store) CreateConversation(ctx context.Context, classID, userID, agentID int64, title string) (*Conversation, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO conversations (class_id, user_id, agent_id, title) VALUES (?,?,?,?)`,
		classID, userID, agentID, title)
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetConversation(ctx, classID, userID, id)
}

// ListConversations returns a user's conversations, most recently updated first.
func (s *Store) ListConversations(ctx context.Context, classID, userID int64) ([]Conversation, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, class_id, user_id, agent_id, title, created_at, updated_at
		 FROM conversations WHERE class_id=? AND user_id=? ORDER BY updated_at DESC, id DESC`, classID, userID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	return collectConversations(rows)
}

// GetConversation fetches a conversation scoped to both its owner and its class.
// Any mismatch returns ErrNotFound so existence is never leaked.
func (s *Store) GetConversation(ctx context.Context, classID, userID, id int64) (*Conversation, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, class_id, user_id, agent_id, title, created_at, updated_at
		 FROM conversations WHERE id=? AND class_id=? AND user_id=?`, id, classID, userID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	defer rows.Close()
	list, err := collectConversations(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// UpdateConversationTitle overwrites a conversation title.
func (s *Store) UpdateConversationTitle(ctx context.Context, id int64, title string) error {
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE conversations SET title=? WHERE id=?`, title, id); err != nil {
		return fmt.Errorf("update conversation title: %w", err)
	}
	return nil
}

// TouchConversation refreshes updated_at so the list order follows activity.
func (s *Store) TouchConversation(ctx context.Context, id int64) error {
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE conversations SET updated_at=NOW(6) WHERE id=?`, id); err != nil {
		return fmt.Errorf("touch conversation: %w", err)
	}
	return nil
}

// CountUserMessages reports how many user turns a conversation already has.
func (s *Store) CountUserMessages(ctx context.Context, conversationID int64) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE conversation_id=? AND role='user'`, conversationID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count user messages: %w", err)
	}
	return n, nil
}

// AppendMessage writes one turn and returns its id.
func (s *Store) AppendMessage(ctx context.Context, nm NewMessage) (int64, error) {
	citations := nm.Citations
	if len(citations) == 0 {
		citations = json.RawMessage("[]")
	}
	toolCalls := normalizeToolCalls(string(nm.ToolCalls))
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, role, content, citations_json, skill_name, tool_calls_json) VALUES (?,?,?,?,?,?)`,
		nm.ConversationID, nm.Role, nm.Content, string(citations), nm.SkillName, string(toolCalls))
	if err != nil {
		return 0, fmt.Errorf("append message: %w", err)
	}
	return res.LastInsertId()
}

// ListMessages returns every message of a conversation in write order.
func (s *Store) ListMessages(ctx context.Context, conversationID int64) ([]Message, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, citations_json, skill_name, tool_calls_json, created_at
		 FROM messages WHERE conversation_id=? ORDER BY id`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	return collectMessages(rows)
}

// RecentWindowMessages returns the last `limit` messages in write order, each
// body truncated to maxChars runes. Records whose role is not user/assistant or
// whose content is empty are skipped.
func (s *Store) RecentWindowMessages(ctx context.Context, conversationID int64, limit, maxChars int) ([]Message, error) {
	if limit <= 0 {
		limit = 6
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, citations_json, skill_name, tool_calls_json, created_at
		 FROM messages WHERE conversation_id=? ORDER BY id DESC LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent messages: %w", err)
	}
	defer rows.Close()
	got, err := collectMessages(rows)
	if err != nil {
		return nil, err
	}
	// Reverse into write order while filtering invalid rows.
	out := make([]Message, 0, len(got))
	for i := len(got) - 1; i >= 0; i-- {
		m := got[i]
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.Content == "" {
			continue
		}
		if maxChars > 0 {
			m.Content = truncateRunes(m.Content, maxChars)
		}
		out = append(out, m)
	}
	return out, nil
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func collectConversations(rows *sql.Rows) ([]Conversation, error) {
	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.ClassID, &c.UserID, &c.AgentID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func collectMessages(rows *sql.Rows) ([]Message, error) {
	var out []Message
	for rows.Next() {
		var m Message
		var citations, toolCalls sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &citations, &m.SkillName, &toolCalls, &m.CreatedAt); err != nil {
			return nil, err
		}
		if citations.Valid && citations.String != "" {
			m.Citations = json.RawMessage(citations.String)
		} else {
			m.Citations = json.RawMessage("[]")
		}
		m.ToolCalls = normalizeToolCalls(toolCalls.String)
		out = append(out, m)
	}
	return out, rows.Err()
}

func normalizeToolCalls(raw string) json.RawMessage {
	var calls []json.RawMessage
	if json.Unmarshal([]byte(raw), &calls) != nil || calls == nil {
		return json.RawMessage("[]")
	}
	return json.RawMessage(raw)
}

// EnsureConversationExists is a small helper used by tests and callers that
// need to distinguish "not found" from a database error.
func (s *Store) EnsureConversationExists(ctx context.Context, classID, userID, id int64) error {
	_, err := s.GetConversation(ctx, classID, userID, id)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return err
}
