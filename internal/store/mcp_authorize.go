package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"campusclaw/internal/mcp"
)

// MCPToolCandidate is a snapshot for a single skill-authorized reference. It is
// not a permission to execute; ReserveMCPCall must reauthorize it atomically.
type MCPToolCandidate struct {
	Server MCPServer
	Tool   mcp.Tool
}

func (s *Store) ResolveMCPTool(ctx context.Context, classID int64, serverName, toolName string) (*MCPToolCandidate, error) {
	if !mcp.ValidServerName(serverName) || !mcp.ValidToolName(toolName) {
		return nil, ErrInvalidMCP
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	server, err := scanMCPServer(tx.QueryRowContext(ctx, `SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? AND name=? AND deleted_at IS NULL`, classID, serverName))
	if err != nil {
		return nil, err
	}
	candidate := &MCPToolCandidate{Server: *server, Tool: mcp.Tool{Name: toolName}}
	if server.State != MCPConnected || server.PendingHash != "" || server.ApprovedHash == "" {
		return candidate, ErrMCPDenied
	}
	var schema string
	err = tx.QueryRowContext(ctx, `SELECT description, input_schema FROM mcp_tools WHERE server_id=? AND name=? AND snapshot_hash=?`, server.ID, toolName, server.ApprovedHash).Scan(&candidate.Tool.Description, &schema)
	if errors.Is(err, sql.ErrNoRows) {
		return candidate, ErrMCPDenied
	}
	if err != nil {
		return nil, err
	}
	candidate.Tool.InputSchema = json.RawMessage(schema)
	if err := mcp.ValidateTools([]mcp.Tool{candidate.Tool}); err != nil {
		return candidate, ErrMCPDenied
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return candidate, nil
}

func candidateStillApproved(ctx context.Context, tx *sql.Tx, server *MCPServer, candidate MCPToolCandidate) error {
	before := candidate.Server
	if server.State != MCPConnected || server.PendingHash != "" || server.ApprovedHash == "" || server.Generation != before.Generation || server.ApprovedHash != before.ApprovedHash || server.CredentialRef != before.CredentialRef || server.BaseURL != before.BaseURL || server.ID != before.ID || server.ClassID != before.ClassID || server.Name != before.Name {
		return ErrMCPDenied
	}
	var tool mcp.Tool
	var schema string
	err := tx.QueryRowContext(ctx, `SELECT name, description, input_schema FROM mcp_tools WHERE server_id=? AND name=? AND snapshot_hash=?`, server.ID, candidate.Tool.Name, server.ApprovedHash).Scan(&tool.Name, &tool.Description, &schema)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMCPDenied
	}
	if err != nil {
		return err
	}
	if tool.Description != candidate.Tool.Description || !bytes.Equal([]byte(schema), candidate.Tool.InputSchema) {
		return ErrMCPDenied
	}
	return nil
}
