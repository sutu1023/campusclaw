package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

type mcpToolsResponse struct {
	Server        mcpServerDTO              `json:"server"`
	Tools         []mcp.Tool                `json:"tools"`
	PendingTools  []mcp.Tool                `json:"pending_tools,omitempty"`
	ApprovedTools []mcp.Tool                `json:"approved_tools,omitempty"`
	Change        *store.MCPDiscoveryChange `json:"change,omitempty"`
}

func (s *Server) mcpSecrets() []string {
	if s.Chat != nil {
		return []string{s.Chat.APIKey}
	}
	return nil
}

func (s *Server) mcpTarget(server *store.MCPServer) (mcp.Target, error) {
	ref, err := s.MCP.Registry.Reference(server.ClassID, server.Name, server.BaseURL)
	if err != nil {
		return mcp.Target{}, err
	}
	if ref != server.CredentialRef {
		return mcp.Target{}, mcp.ErrUnapproved
	}
	return s.MCP.Registry.Resolve(server.ClassID, server.Name, server.BaseURL)
}

// @Summary 连接并发现本班 MCP 工具（仅教师）
// @Description 使用服务端绑定凭据；完整发现整体替换。已有说明/Schema 变更返回 pending_approval，确认前不可用。远端 401 映射 503，不是应用登录失败。
// @Tags mcp
// @Produce json
// @Security BearerAuth
// @Param id path int true "服务器编号"
// @Success 200 {object} mcpServerDTO
// @Failure 400 {object} errorBody
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 409 {object} errorBody
// @Failure 503 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers/{id}/connect [post]
func (s *Server) connectMCPServer(w http.ResponseWriter, r *http.Request) {
	server, ok := s.mcpServerFromRequest(w, r)
	if !ok {
		return
	}
	operation, err := s.Store.BeginMCPConnect(r.Context(), server.ClassID, server.ID)
	if !writeMCPError(w, err) {
		return
	}
	target, err := s.mcpTarget(operation)
	if err != nil {
		if !writeMCPError(w, s.Store.FailMCPDiscovery(r.Context(), operation.ClassID, operation.ID, operation.Generation)) {
			return
		}
		writeMCPError(w, err)
		return
	}
	timeout := s.MCP.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*timeout)
	defer cancel()
	client, err := mcp.NewClient(target, timeout)
	var tools []mcp.Tool
	if err == nil {
		tools, err = client.Discover(ctx)
	}
	if err == nil {
		err = mcp.CheckDefinitions(target, tools, s.mcpSecrets()...)
	}
	if err != nil {
		if !writeMCPError(w, s.Store.FailMCPDiscovery(r.Context(), operation.ClassID, operation.ID, operation.Generation)) {
			return
		}
		writeError(w, 503, "MCP 连接或发现失败，请检查工具服务、协议及部署凭据")
		return
	}
	user, _ := currentUser(r)
	updated, err := s.Store.CompleteMCPDiscovery(r.Context(), operation.ClassID, operation.ID, operation.Generation, tools, user.ID)
	if errors.Is(err, mcp.ErrDefinitions) || errors.Is(err, mcp.ErrSchema) {
		writeError(w, 503, "MCP 工具清单无效")
		return
	}
	if !writeMCPError(w, err) {
		return
	}
	writeJSON(w, 200, s.mcpDTO(updated))
}

// @Summary 查看本班 MCP 工具完整目录（仅教师）
// @Description tools 只含可调用目录；pending_tools/approved_tools 为完整变更比较内容，不是调用许可。不返回 MCP 凭据。
// @Tags mcp
// @Produce json
// @Security BearerAuth
// @Param id path int true "服务器编号"
// @Success 200 {object} mcpToolsResponse
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 503 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers/{id}/tools [get]
func (s *Server) listMCPTools(w http.ResponseWriter, r *http.Request) {
	server, ok := s.mcpServerFromRequest(w, r)
	if !ok {
		return
	}
	directory, err := s.Store.ReadMCPDirectory(r.Context(), server.ClassID, server.ID)
	if !writeMCPError(w, err) {
		return
	}
	target, _ := s.mcpTarget(&directory.Server)
	for _, tools := range [][]mcp.Tool{directory.Tools, directory.PendingTools, directory.ApprovedTools} {
		if err := mcp.CheckDefinitions(target, tools, s.mcpSecrets()...); err != nil {
			writeError(w, 503, "MCP 工具数据未通过安全检查")
			return
		}
	}
	writeJSON(w, 200, mcpToolsResponse{Server: s.mcpDTO(&directory.Server), Tools: directory.Tools, PendingTools: directory.PendingTools, ApprovedTools: directory.ApprovedTools, Change: directory.Change})
}
