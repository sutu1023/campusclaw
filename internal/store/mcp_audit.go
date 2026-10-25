package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"unicode/utf8"

	"campusclaw/internal/mcp"
)

var mcpCallIDRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// MCPCallAttempt intentionally has no arguments, prompts, credentials or remote
// output fields. Only bounded, already-sanitized summaries enter the audit log.
type MCPCallAttempt struct {
	ClassID           int64
	ServerID          int64
	ToolName          string
	UserID            int64
	ConversationID    int64
	QuestionMessageID int64
	CallID            string
	Round             int
}

func validMCPAttempt(a MCPCallAttempt) bool {
	return a.ClassID > 0 && a.ServerID > 0 && a.UserID > 0 && a.ConversationID > 0 && a.QuestionMessageID > 0 && mcp.ValidToolName(a.ToolName) && mcpCallIDRE.MatchString(a.CallID) && a.Round >= 0 && a.Round <= 64
}

func validMCPSummary(text string) bool {
	return utf8.ValidString(text) && text != "" && utf8.RuneCountInString(text) <= 500
}

func lockMCPQuestion(ctx context.Context, tx *sql.Tx, a MCPCallAttempt) error {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM conversations WHERE class_id=? AND user_id=? AND id=? FOR UPDATE`, a.ClassID, a.UserID, a.ConversationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM messages WHERE conversation_id=? AND id=? AND role='user'`, a.ConversationID, a.QuestionMessageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func insertMCPAudit(ctx context.Context, tx *sql.Tx, a MCPCallAttempt, server *MCPServer, status, text string, complete bool) (int64, error) {
	completed := "NULL"
	if complete {
		completed = "NOW(6)"
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO mcp_tool_calls (class_id, server_id, server_name, tool_ref, user_id, conversation_id, question_message_id, call_id, round, status, safe_text, completed_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,`+completed+`)`, a.ClassID, server.ID, server.Name, a.ToolName+"@"+server.Name, a.UserID, a.ConversationID, a.QuestionMessageID, a.CallID, a.Round, status, text)
	if isDuplicateKey(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ReserveMCPCall commits a durable interrupted/error audit row and consumes one
// remote-action credit before allowing network I/O. A returned denial may have
// an audit ID; a failed transaction never grants a permit or consumes credit.
func (s *Store) ReserveMCPCall(ctx context.Context, a MCPCallAttempt, candidate MCPToolCandidate, limit int) (int64, error) {
	if !validMCPAttempt(a) || a.ServerID != candidate.Server.ID || a.ClassID != candidate.Server.ClassID || a.ToolName != candidate.Tool.Name || limit <= 0 || limit > 10000 {
		return 0, ErrInvalidMCP
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockMCPQuestion(ctx, tx, a); err != nil {
		return 0, err
	}
	server, err := lockMCPServer(ctx, tx, a.ClassID, a.ServerID)
	if err != nil {
		return 0, err
	}
	if err := candidateStillApproved(ctx, tx, server, candidate); err != nil {
		if !errors.Is(err, ErrMCPDenied) {
			return 0, err
		}
		id, err := insertMCPAudit(ctx, tx, a, server, "denied", "MCP 工具连接或批准快照已失效", true)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return id, ErrMCPDenied
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_conversation_usage (conversation_id, reserved_count) VALUES (?,0) ON DUPLICATE KEY UPDATE reserved_count=reserved_count`, a.ConversationID); err != nil {
		return 0, err
	}
	var used int
	if err := tx.QueryRowContext(ctx, `SELECT reserved_count FROM mcp_conversation_usage WHERE conversation_id=? FOR UPDATE`, a.ConversationID).Scan(&used); err != nil {
		return 0, err
	}
	if used >= limit {
		id, err := insertMCPAudit(ctx, tx, a, server, "denied", "MCP 会话累计调用额度已耗尽", true)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return id, ErrMCPQuota
	}
	id, err := insertMCPAudit(ctx, tx, a, server, "error", "interrupted: 工具调用尚未完成", false)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mcp_conversation_usage SET reserved_count=reserved_count+1 WHERE conversation_id=?`, a.ConversationID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// RecordMCPAttempt records local parameter errors or denials without consuming
// remote-action credit. Missing/deleted/foreign servers cannot produce a row.
func (s *Store) RecordMCPAttempt(ctx context.Context, a MCPCallAttempt, status, safeText string) (int64, error) {
	if !validMCPAttempt(a) || (status != "error" && status != "denied") || !validMCPSummary(safeText) {
		return 0, ErrInvalidMCP
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockMCPQuestion(ctx, tx, a); err != nil {
		return 0, err
	}
	server, err := lockMCPServer(ctx, tx, a.ClassID, a.ServerID)
	if err != nil {
		return 0, err
	}
	id, err := insertMCPAudit(ctx, tx, a, server, status, safeText, true)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) FinishMCPCall(ctx context.Context, classID, auditID int64, status, safeText string, durationMS uint64) error {
	if (status != "ok" && status != "error") || !validMCPSummary(safeText) {
		return ErrInvalidMCP
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE mcp_tool_calls SET status=?, safe_text=?, duration_ms=?, completed_at=NOW(6) WHERE class_id=? AND id=? AND completed_at IS NULL`, status, safeText, durationMS, classID, auditID)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err != nil || count != 1 {
		return ErrMCPStale
	}
	return nil
}

func (s *Store) MCPConversationUsage(ctx context.Context, classID, userID, conversationID int64) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(u.reserved_count,0) FROM conversations c LEFT JOIN mcp_conversation_usage u ON u.conversation_id=c.id WHERE c.class_id=? AND c.user_id=? AND c.id=?`, classID, userID, conversationID).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return count, err
}

func (s *Store) ListMCPAudit(ctx context.Context, classID int64, page, size int, serverID int64, status string) ([]MCPToolCall, int, error) {
	if status != "" && status != "ok" && status != "error" && status != "denied" {
		return nil, 0, ErrInvalidMCP
	}
	if serverID != 0 {
		var id int64
		// A local tombstone can still be used to filter its historical evidence.
		err := s.DB.QueryRowContext(ctx, `SELECT id FROM mcp_servers WHERE class_id=? AND id=?`, classID, serverID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, ErrNotFound
		}
		if err != nil {
			return nil, 0, err
		}
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	where, args := "class_id=?", []any{classID}
	if serverID != 0 {
		where += " AND server_id=?"
		args = append(args, serverID)
	}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_tool_calls WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, class_id, server_id, server_name, tool_ref, user_id, conversation_id, question_message_id, call_id, round, status, safe_text, duration_ms, created_at, completed_at FROM mcp_tool_calls WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	calls := []MCPToolCall{}
	for rows.Next() {
		var call MCPToolCall
		if err := rows.Scan(&call.ID, &call.ClassID, &call.ServerID, &call.ServerName, &call.Ref, &call.UserID, &call.ConversationID, &call.QuestionMessageID, &call.CallID, &call.Round, &call.Status, &call.Text, &call.DurationMS, &call.CreatedAt, &call.CompletedAt); err != nil {
			return nil, 0, err
		}
		calls = append(calls, call)
	}
	return calls, total, rows.Err()
}
