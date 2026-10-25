package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"campusclaw/internal/mcp"
)

// ApprovedTools is comparison data only and is populated only during pending
// approval. Runtime candidates must exclusively use Tools and current state.
type MCPToolDirectory struct {
	Server        MCPServer
	Tools         []mcp.Tool
	PendingTools  []mcp.Tool
	ApprovedTools []mcp.Tool
	Change        *MCPDiscoveryChange
}

func (s *Store) ReadMCPDirectory(ctx context.Context, classID, id int64) (*MCPToolDirectory, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	server, err := scanMCPServer(tx.QueryRowContext(ctx, `SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? AND id=? AND deleted_at IS NULL`, classID, id))
	if err != nil {
		return nil, err
	}
	directory := &MCPToolDirectory{Server: *server, Tools: []mcp.Tool{}}
	if server.State == MCPConnected && server.ApprovedHash != "" && server.PendingHash == "" {
		rows, err := tx.QueryContext(ctx, `SELECT name, description, input_schema FROM mcp_tools WHERE server_id=? AND snapshot_hash=? ORDER BY name`, id, server.ApprovedHash)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var tool mcp.Tool
			var schema string
			if err := rows.Scan(&tool.Name, &tool.Description, &schema); err != nil {
				rows.Close()
				return nil, err
			}
			tool.InputSchema = json.RawMessage(schema)
			directory.Tools = append(directory.Tools, tool)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if err := mcp.ValidateTools(directory.Tools); err != nil {
			return nil, err
		}
	}
	if server.State == MCPPending && server.PendingHash != "" {
		directory.PendingTools, err = snapshotTools(ctx, tx, id, server.PendingHash)
		if err != nil {
			return nil, err
		}
		directory.ApprovedTools, err = snapshotTools(ctx, tx, id, server.ApprovedHash)
		if err != nil {
			return nil, err
		}
	}
	var change MCPDiscoveryChange
	var names string
	err = tx.QueryRowContext(ctx, `SELECT id, server_id, previous_hash, snapshot_hash, changed_tools_json, created_at, approved_by, approved_at FROM mcp_discovery_changes WHERE class_id=? AND server_id=? ORDER BY id DESC LIMIT 1`, classID, id).Scan(&change.ID, &change.ServerID, &change.PreviousHash, &change.SnapshotHash, &names, &change.CreatedAt, &change.ApprovedBy, &change.ApprovedAt)
	if err == nil {
		if json.Unmarshal([]byte(names), &change.ChangedTools) != nil {
			return nil, mcp.ErrDefinitions
		}
		directory.Change = &change
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return directory, nil
}
