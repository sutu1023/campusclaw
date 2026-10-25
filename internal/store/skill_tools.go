package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"campusclaw/internal/skill"
)

// SkillToolError only contains a syntactically validated reference and a fixed
// local explanation, never remote diagnostics or credentials.
type SkillToolError struct{ Ref, Reason string }

func (e *SkillToolError) Error() string {
	return fmt.Sprintf("工具 %s 不可保存：%s", e.Ref, e.Reason)
}
func (e *SkillToolError) Unwrap() error { return ErrMCPDenied }

// validateSkillTools runs inside the skill write transaction. Locking servers
// in a stable order prevents disconnect/snapshot replacement from interleaving
// between reference validation and the skill write (without any network I/O).
func validateSkillTools(ctx context.Context, tx *sql.Tx, classID int64, tools []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	byServer := map[string][]skill.ToolReference{}
	for _, raw := range tools {
		ref, err := skill.ParseToolReference(raw)
		if err != nil {
			return nil, ErrInvalidMCP
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, raw)
		if len(out) > skill.MaxTools {
			return nil, ErrInvalidMCP
		}
		if !ref.Builtin {
			byServer[ref.Server] = append(byServer[ref.Server], ref)
		}
	}
	names := make([]string, 0, len(byServer))
	for name := range byServer {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		server, err := scanMCPServer(tx.QueryRowContext(ctx, `SELECT `+mcpServerColumns+` FROM mcp_servers WHERE class_id=? AND name=? AND deleted_at IS NULL FOR UPDATE`, classID, name))
		if err != nil {
			return nil, err
		}
		for _, ref := range byServer[name] {
			if server.State != MCPConnected || server.PendingHash != "" || server.ApprovedHash == "" {
				return nil, &SkillToolError{Ref: ref.String(), Reason: "服务器未连接或正在等待变更确认"}
			}
			var toolName string
			err = tx.QueryRowContext(ctx, `SELECT name FROM mcp_tools WHERE server_id=? AND name=? AND snapshot_hash=?`, server.ID, ref.Tool, server.ApprovedHash).Scan(&toolName)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, &SkillToolError{Ref: ref.String(), Reason: "工具不在当前已批准的发现目录"}
			}
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
