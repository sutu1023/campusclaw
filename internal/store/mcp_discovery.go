package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"campusclaw/internal/mcp"
)

// canonicalMCPTools hashes structure, not remote list order or JSON whitespace.
func canonicalMCPTools(tools []mcp.Tool) ([]mcp.Tool, string, string, error) {
	if err := mcp.ValidateTools(tools); err != nil {
		return nil, "", "", err
	}
	normalized := make([]mcp.Tool, len(tools))
	copy(normalized, tools)
	for i := range normalized {
		var schema any
		d := json.NewDecoder(bytes.NewReader(normalized[i].InputSchema))
		d.UseNumber()
		if d.Decode(&schema) != nil {
			return nil, "", "", mcp.ErrSchema
		}
		encoded, err := json.Marshal(schema)
		if err != nil {
			return nil, "", "", mcp.ErrSchema
		}
		normalized[i].InputSchema = encoded
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Name < normalized[j].Name })
	raw, err := json.Marshal(normalized)
	if err != nil || len(raw) > mcp.MaxResponseBytes {
		return nil, "", "", mcp.ErrDefinitions
	}
	hash := sha256.Sum256(raw)
	return normalized, string(raw), hex.EncodeToString(hash[:]), nil
}

// BeginMCPConnect revokes the old permission before any network I/O. The
// returned generation is this operation's immutable CAS token, not a later read.
func (s *Store) BeginMCPConnect(ctx context.Context, classID, id int64) (*MCPServer, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	server, err := lockMCPServer(ctx, tx, classID, id)
	if err != nil {
		return nil, err
	}
	if err := invalidateMCPServer(ctx, tx, classID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	server.Generation++
	server.State, server.PendingHash, server.LastError = MCPDisconnected, "", ""
	return server, nil
}

func snapshotTools(ctx context.Context, tx *sql.Tx, serverID int64, hash string) ([]mcp.Tool, error) {
	if hash == "" {
		return []mcp.Tool{}, nil
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT tools_json FROM mcp_discovery_snapshots WHERE server_id=? AND snapshot_hash=?`, serverID, hash).Scan(&raw); err != nil {
		return nil, err
	}
	return decodeMCPTools(raw)
}

func discoveryDiff(previous, next []mcp.Tool) ([]string, bool) {
	old, current := map[string]mcp.Tool{}, map[string]mcp.Tool{}
	for _, tool := range previous {
		old[tool.Name] = tool
	}
	for _, tool := range next {
		current[tool.Name] = tool
	}
	changed := map[string]bool{}
	approval := false
	for name, tool := range current {
		before, exists := old[name]
		if !exists {
			changed[name] = true
			continue
		}
		if before.Description != tool.Description || !bytes.Equal(before.InputSchema, tool.InputSchema) {
			changed[name], approval = true, true
		}
	}
	for name := range old {
		if _, exists := current[name]; !exists {
			changed[name] = true
		}
	}
	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, approval
}

func requireMCPTeacher(ctx context.Context, tx *sql.Tx, classID, teacherID int64) error {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE class_id=? AND id=? AND role='teacher'`, classID, teacherID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// CompleteMCPDiscovery validates everything before the short write transaction;
// no remote request is made with database locks held.
func (s *Store) CompleteMCPDiscovery(ctx context.Context, classID, id int64, generation uint64, tools []mcp.Tool, teacherID int64) (*MCPServer, error) {
	tools, raw, hash, err := canonicalMCPTools(tools)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	server, err := lockMCPServer(ctx, tx, classID, id)
	if err != nil {
		return nil, err
	}
	if server.Generation != generation || server.State != MCPDisconnected {
		return nil, ErrMCPStale
	}
	if err := requireMCPTeacher(ctx, tx, classID, teacherID); err != nil {
		return nil, err
	}
	previous, err := snapshotTools(ctx, tx, id, server.ApprovedHash)
	if err != nil {
		return nil, err
	}
	changed, needsApproval := discoveryDiff(previous, tools)
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_discovery_snapshots (server_id, snapshot_hash, tools_json) VALUES (?,?,?) ON DUPLICATE KEY UPDATE snapshot_hash=VALUES(snapshot_hash)`, id, hash, raw); err != nil {
		return nil, err
	}
	if hash != server.ApprovedHash {
		changedJSON, _ := json.Marshal(changed)
		var approvedBy any
		var approvedAt any
		if !needsApproval {
			approvedBy, approvedAt = teacherID, time.Now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_discovery_changes (class_id, server_id, previous_hash, snapshot_hash, changed_tools_json, approved_by, approved_at) VALUES (?,?,?,?,?,?,?)`, classID, id, server.ApprovedHash, hash, string(changedJSON), approvedBy, approvedAt); err != nil {
			return nil, err
		}
	}
	if needsApproval {
		if _, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET state='pending_approval', pending_hash=?, last_error='' WHERE class_id=? AND id=?`, hash, classID, id); err != nil {
			return nil, err
		}
		server.State, server.PendingHash = MCPPending, hash
	} else {
		if err := activateMCPSnapshot(ctx, tx, classID, id, hash, tools); err != nil {
			return nil, err
		}
		server.State, server.ApprovedHash, server.PendingHash = MCPConnected, hash, ""
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return server, nil
}

func activateMCPSnapshot(ctx context.Context, tx *sql.Tx, classID, id int64, hash string, tools []mcp.Tool) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_tools WHERE server_id=?`, id); err != nil {
		return err
	}
	for _, tool := range tools {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_tools (server_id, name, description, input_schema, snapshot_hash) VALUES (?,?,?,?,?)`, id, tool.Name, tool.Description, string(tool.InputSchema), hash); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET state='connected', approved_hash=?, pending_hash='', last_error='' WHERE class_id=? AND id=?`, hash, classID, id)
	return err
}

func (s *Store) ApproveMCPSnapshot(ctx context.Context, classID, id int64, generation uint64, hash string, teacherID int64) (*MCPServer, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	server, err := lockMCPServer(ctx, tx, classID, id)
	if err != nil {
		return nil, err
	}
	if server.Generation != generation || server.State != MCPPending || hash == "" || server.PendingHash != hash {
		return nil, ErrMCPStale
	}
	if err := requireMCPTeacher(ctx, tx, classID, teacherID); err != nil {
		return nil, err
	}
	tools, err := snapshotTools(ctx, tx, id, hash)
	if err != nil {
		return nil, err
	}
	if err := activateMCPSnapshot(ctx, tx, classID, id, hash, tools); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE mcp_discovery_changes SET approved_by=?, approved_at=NOW(6) WHERE class_id=? AND server_id=? AND snapshot_hash=? AND approved_at IS NULL ORDER BY id DESC LIMIT 1`, teacherID, classID, id, hash)
	if err != nil {
		return nil, err
	}
	if count, err := res.RowsAffected(); err != nil || count != 1 {
		return nil, ErrMCPStale
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	server.State, server.ApprovedHash, server.PendingHash = MCPConnected, hash, ""
	return server, nil
}

func (s *Store) FailMCPDiscovery(ctx context.Context, classID, id int64, generation uint64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	server, err := lockMCPServer(ctx, tx, classID, id)
	if err != nil {
		return err
	}
	if server.Generation != generation || server.State != MCPDisconnected {
		return ErrMCPStale
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET last_error='MCP 连接或发现失败，请检查服务、协议及部署凭据' WHERE class_id=? AND id=?`, classID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// InvalidateMCPConnections runs once at API startup. Registrations, approved
// comparison history, audit evidence and persistent usage are never reset.
func (s *Store) InvalidateMCPConnections(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET state='disconnected', generation=generation+1, pending_hash='' WHERE deleted_at IS NULL`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_tools`); err != nil {
		return err
	}
	return tx.Commit()
}
