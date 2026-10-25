package httpapi

import "net/http"

// @Summary 断开本班 MCP 服务器（仅教师）
// @Description 幂等撤销新调用许可并清空可用/待确认目录；保留登记、批准历史及审计，不隐式重连。在途已获准的只读调用不撤回。
// @Tags mcp
// @Security BearerAuth
// @Param id path int true "服务器编号"
// @Success 204
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers/{id}/disconnect [post]
func (s *Server) disconnectMCPServer(w http.ResponseWriter, r *http.Request) {
	server, ok := s.mcpServerFromRequest(w, r)
	if !ok {
		return
	}
	if !writeMCPError(w, s.Store.DisconnectMCPServer(r.Context(), server.ClassID, server.ID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
