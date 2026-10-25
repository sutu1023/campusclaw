package httpapi

import (
	"net/http"
	"strconv"

	"campusclaw/internal/store"
)

type mcpAuditResponse struct {
	Calls    []store.MCPToolCall `json:"calls"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// @Summary 分页查询本班 MCP 调用审计（仅教师）
// @Description 按时间/id 倒序，支持服务器和 ok/error/denied 筛选；只含安全摘要，不保存参数、提示词或密钥。可筛选本班已删除服务器的历史。
// @Tags mcp
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码，默认 1"
// @Param page_size query int false "每页条数，默认 20，最大 100"
// @Param server_id query int false "本班服务器编号"
// @Param status query string false "ok / error / denied"
// @Success 200 {object} mcpAuditResponse
// @Failure 400 {object} errorBody
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/audit [get]
func (s *Server) listMCPAudit(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	page, size := 1, 20
	for _, param := range []struct {
		key  string
		max  int
		dest *int
	}{{"page", 1000000, &page}, {"page_size", 100, &size}} {
		if raw := r.URL.Query().Get(param.key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > param.max {
				writeError(w, 400, "审计分页参数无效")
				return
			}
			*param.dest = n
		}
	}
	var serverID int64
	if raw := r.URL.Query().Get("server_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, 400, "server_id 无效")
			return
		}
		serverID = id
	}
	calls, total, err := s.Store.ListMCPAudit(r.Context(), user.ClassID, page, size, serverID, r.URL.Query().Get("status"))
	if !writeMCPError(w, err) {
		return
	}
	writeJSON(w, 200, mcpAuditResponse{Calls: calls, Total: total, Page: page, PageSize: size})
}
