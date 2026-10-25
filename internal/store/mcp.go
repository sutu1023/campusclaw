package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"campusclaw/internal/mcp"
)

const (
	MCPDisconnected = "disconnected"
	MCPConnected    = "connected"
	MCPPending      = "pending_approval"
)

var (
	ErrInvalidMCP = errors.New("invalid MCP registration")
	ErrMCPStale   = errors.New("MCP operation has been superseded")
	ErrMCPDenied  = errors.New("MCP tool is not currently authorized")
	ErrMCPQuota   = errors.New("MCP conversation quota exhausted")
)

// These persistence models are not HTTP DTOs. CredentialRef is a non-secret
// binding hash, but is still intentionally excluded from JSON.
type MCPServer struct {
	ID            int64
	ClassID       int64
	Name          string
	BaseURL       string
	CredentialRef string `json:"-"`
	State         string
	Generation    uint64
	ApprovedHash  string
	PendingHash   string
	LastError     string
	DeletedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type MCPDiscoverySnapshot struct {
	ServerID int64
	Hash     string
	Tools    []mcp.Tool
}

type MCPDiscoveryChange struct {
	ID           int64      `json:"id"`
	ServerID     int64      `json:"server_id"`
	PreviousHash string     `json:"previous_hash"`
	SnapshotHash string     `json:"snapshot_hash"`
	ChangedTools []string   `json:"changed_tools"`
	CreatedAt    time.Time  `json:"created_at"`
	ApprovedBy   *int64     `json:"approved_by,omitempty"`
	ApprovedAt   *time.Time `json:"approved_at,omitempty"`
}

type MCPToolCall struct {
	ID                int64      `json:"id"`
	ClassID           int64      `json:"class_id"`
	ServerID          int64      `json:"server_id"`
	ServerName        string     `json:"server_name"`
	Ref               string     `json:"ref"`
	UserID            int64      `json:"user_id"`
	ConversationID    int64      `json:"conversation_id"`
	QuestionMessageID int64      `json:"question_message_id"`
	CallID            string     `json:"call_id"`
	Round             int        `json:"round"`
	Status            string     `json:"status"`
	Text              string     `json:"text"`
	DurationMS        uint64     `json:"duration_ms"`
	CreatedAt         time.Time  `json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

type MCPConversationUsage struct {
	ConversationID int64
	ReservedCount  int
}

const mcpServerColumns = `id, class_id, name, base_url, credential_ref, state, generation, approved_hash, pending_hash, last_error, deleted_at, created_at, updated_at`

type mcpScanner interface{ Scan(...any) error }

func scanMCPServer(row mcpScanner) (*MCPServer, error) {
	var server MCPServer
	if err := row.Scan(&server.ID, &server.ClassID, &server.Name, &server.BaseURL, &server.CredentialRef, &server.State, &server.Generation, &server.ApprovedHash, &server.PendingHash, &server.LastError, &server.DeletedAt, &server.CreatedAt, &server.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &server, nil
}

func validCredentialReference(ref string) bool {
	if len(ref) != 64 {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

func (s *Store) CreateMCPServer(ctx context.Context, classID int64, name, baseURL, credentialRef string) (*MCPServer, error) {
	origin, err := mcp.NormalizeOrigin(baseURL)
	if err != nil || classID <= 0 || !mcp.ValidServerName(name) || !validCredentialReference(credentialRef) {
		return nil, ErrInvalidMCP
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO mcp_servers (class_id, name, base_url, credential_ref) VALUES (?,?,?,?)`, classID, name, origin, credentialRef)
	if err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicate
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetMCPServer(ctx, classID, id)
}

func (s *Store) GetMCPServer(ctx context.Context, classID, id int64) (*MCPServer, error) {
	return scanMCPServer(s.DB.QueryRowContext(ctx, `SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? AND id=? AND deleted_at IS NULL`, classID, id))
}

func (s *Store) ListMCPServers(ctx context.Context, classID int64) ([]MCPServer, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? AND deleted_at IS NULL ORDER BY id`, classID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	servers := []MCPServer{}
	for rows.Next() {
		server, err := scanMCPServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, *server)
	}
	return servers, rows.Err()
}

func lockMCPServer(ctx context.Context, tx *sql.Tx, classID, id int64) (*MCPServer, error) {
	return scanMCPServer(tx.QueryRowContext(ctx, `SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? AND id=? AND deleted_at IS NULL FOR UPDATE`, classID, id))
}

// invalidateMCPServer retains the last approved hash and immutable history. It
// only revokes the live directory and pending approval; it never reconnects.
func invalidateMCPServer(ctx context.Context, tx *sql.Tx, classID, id int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_tools WHERE server_id=?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET state='disconnected', generation=generation+1, pending_hash='', last_error='' WHERE class_id=? AND id=?`, classID, id)
	return err
}

func (s *Store) UpdateMCPServer(ctx context.Context, classID, id int64, baseURL, credentialRef string) (*MCPServer, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	server, err := lockMCPServer(ctx, tx, classID, id)
	if err != nil {
		return nil, err
	}
	origin, err := mcp.NormalizeOrigin(baseURL)
	if err != nil || !validCredentialReference(credentialRef) {
		return nil, ErrInvalidMCP
	}
	if server.BaseURL != origin || server.CredentialRef != credentialRef {
		if err := invalidateMCPServer(ctx, tx, classID, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET base_url=?, credential_ref=? WHERE class_id=? AND id=?`, origin, credentialRef, classID, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMCPServer(ctx, classID, id)
}

func (s *Store) DisconnectMCPServer(ctx context.Context, classID, id int64) error {
	return s.revokeMCPServer(ctx, classID, id, false)
}

func (s *Store) DeleteMCPServer(ctx context.Context, classID, id int64) error {
	return s.revokeMCPServer(ctx, classID, id, true)
}

func (s *Store) revokeMCPServer(ctx context.Context, classID, id int64, deleted bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := lockMCPServer(ctx, tx, classID, id); err != nil {
		return err
	}
	if err := invalidateMCPServer(ctx, tx, classID, id); err != nil {
		return err
	}
	if deleted {
		if _, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET deleted_at=NOW(6) WHERE class_id=? AND id=?`, classID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func decodeMCPTools(raw string) ([]mcp.Tool, error) {
	tools := []mcp.Tool{}
	if len(raw) > mcp.MaxResponseBytes || json.Unmarshal([]byte(raw), &tools) != nil || tools == nil || mcp.ValidateTools(tools) != nil {
		return nil, mcp.ErrDefinitions
	}
	return tools, nil
}
