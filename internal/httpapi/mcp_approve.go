package httpapi

import (
	"net/http"

	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

type mcpApprovalRequest struct {
	SnapshotHash string `json:"snapshot_hash"`
}

// @Summary 确认本班 MCP 工具变更快照（仅教师）
// @Description 查看 tools 接口中的完整 pending_tools 与 approved_tools 后确认当前 hash；过期/并发确认返回 409，不会恢复失效连接。
// @Tags mcp
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "服务器编号"
// @Param body body mcpApprovalRequest true "当前待确认快照 hash"
// @Success 200 {object} mcpServerDTO
// @Failure 400 {object} errorBody
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 409 {object} errorBody
// @Failure 503 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers/{id}/approve [post]
func (s *Server) approveMCPServer(w http.ResponseWriter, r *http.Request) {
	server, ok := s.mcpServerFromRequest(w, r)
	if !ok {
		return
	}
	var req mcpApprovalRequest
	if !decodeMCPBody(w, r, &req) {
		return
	}
	if req.SnapshotHash == "" {
		writeError(w, 400, "snapshot_hash is required")
		return
	}
	if server.State != store.MCPPending || server.PendingHash != req.SnapshotHash {
		writeMCPError(w, store.ErrMCPStale)
		return
	}
	target, err := s.mcpTarget(server)
	if !writeMCPError(w, err) {
		return
	}
	directory, err := s.Store.ReadMCPDirectory(r.Context(), server.ClassID, server.ID)
	if !writeMCPError(w, err) {
		return
	}
	if directory.Server.Generation != server.Generation || directory.Server.PendingHash != req.SnapshotHash {
		writeMCPError(w, store.ErrMCPStale)
		return
	}
	if err := mcp.CheckDefinitions(target, directory.PendingTools, s.mcpSecrets()...); err != nil {
		writeError(w, 503, "MCP 待确认数据未通过安全检查")
		return
	}
	user, _ := currentUser(r)
	updated, err := s.Store.ApproveMCPSnapshot(r.Context(), server.ClassID, server.ID, server.Generation, req.SnapshotHash, user.ID)
	if !writeMCPError(w, err) {
		return
	}
	writeJSON(w, 200, s.mcpDTO(updated))
}
