package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

type mcpRegistrationRequest struct {
	Name    string `json:"name" example:"campus-tools"`
	BaseURL string `json:"base_url" example:"http://mcp-demo:8090"`
}

// This DTO is deliberately separate from the persistence model: no credential
// reference, environment variable name, raw token or internal generation leaks.
type mcpServerDTO struct {
	ID           int64     `json:"id"`
	ClassID      int64     `json:"class_id"`
	Name         string    `json:"name"`
	BaseURL      string    `json:"base_url"`
	State        string    `json:"state" enums:"disconnected,connected,pending_approval"`
	HasToken     bool      `json:"has_token"`
	ApprovedHash string    `json:"approved_hash,omitempty"`
	PendingHash  string    `json:"pending_hash,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type mcpServerListResponse struct {
	Servers []mcpServerDTO `json:"servers"`
}

func (s *Server) mcpDTO(server *store.MCPServer) mcpServerDTO {
	return mcpServerDTO{
		ID: server.ID, ClassID: server.ClassID, Name: server.Name, BaseURL: server.BaseURL, State: server.State,
		HasToken:     s.MCP.Registry.HasToken(server.ClassID, server.Name, server.BaseURL),
		ApprovedHash: server.ApprovedHash, PendingHash: server.PendingHash, LastError: server.LastError,
		CreatedAt: server.CreatedAt, UpdatedAt: server.UpdatedAt,
	}
}

func writeMCPError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		writeError(w, 404, "not found")
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, 400, "本班已使用该服务器名称（包括已删除名称）")
	case errors.Is(err, store.ErrMCPStale):
		writeError(w, 409, "操作已失效，请刷新服务器状态后重试")
	case errors.Is(err, store.ErrInvalidMCP):
		writeError(w, 400, "MCP 名称、地址或请求参数无效")
	case errors.Is(err, mcp.ErrUnapproved):
		writeError(w, 400, "该班级、服务器名称与地址尚未由部署方批准")
	case errors.Is(err, mcp.ErrCredentials):
		writeError(w, 400, "该服务器尚未配置有效的服务端凭据")
	default:
		writeError(w, 500, "MCP 操作失败")
	}
	return false
}

func (s *Server) mcpServerFromRequest(w http.ResponseWriter, r *http.Request) (*store.MCPServer, bool) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return nil, false
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, 404, "not found")
		return nil, false
	}
	server, err := s.Store.GetMCPServer(r.Context(), user.ClassID, id)
	return server, writeMCPError(w, err)
}

func decodeMCPBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if d.Decode(dest) != nil || d.Decode(new(any)) != io.EOF {
		writeError(w, 400, "invalid request")
		return false
	}
	return true
}

// @Summary 列出本班 MCP 服务器（仅教师）
// @Description 仅返回 has_token；MCP 密钥由服务器环境配置，应用登录 Bearer 不是 MCP Bearer。
// @Tags mcp
// @Produce json
// @Security BearerAuth
// @Success 200 {object} mcpServerListResponse
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers [get]
func (s *Server) listMCPServers(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	servers, err := s.Store.ListMCPServers(r.Context(), user.ClassID)
	if !writeMCPError(w, err) {
		return
	}
	out := mcpServerListResponse{Servers: []mcpServerDTO{}}
	for i := range servers {
		out.Servers = append(out.Servers, s.mcpDTO(&servers[i]))
	}
	writeJSON(w, 200, out)
}

// @Summary 登记本班 MCP 服务器（仅教师）
// @Description 名称创建后不可改，删除后不可复用；只接受部署方批准的班级/名称/origin，不接受浏览器凭据。
// @Tags mcp
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body mcpRegistrationRequest true "名称与 Base URL，无密钥"
// @Success 201 {object} mcpServerDTO
// @Failure 400 {object} errorBody
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers [post]
func (s *Server) createMCPServer(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	var req mcpRegistrationRequest
	if !decodeMCPBody(w, r, &req) {
		return
	}
	ref, err := s.MCP.Registry.Reference(user.ClassID, req.Name, req.BaseURL)
	if !writeMCPError(w, err) {
		return
	}
	server, err := s.Store.CreateMCPServer(r.Context(), user.ClassID, req.Name, req.BaseURL, ref)
	if !writeMCPError(w, err) {
		return
	}
	writeJSON(w, 201, s.mcpDTO(server))
}

// @Summary 读取本班 MCP 服务器（仅教师）
// @Tags mcp
// @Produce json
// @Security BearerAuth
// @Param id path int true "服务器编号"
// @Success 200 {object} mcpServerDTO
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers/{id} [get]
func (s *Server) getMCPServer(w http.ResponseWriter, r *http.Request) {
	server, ok := s.mcpServerFromRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, s.mcpDTO(server))
}

// @Summary 修改本班 MCP 地址（仅教师）
// @Description 修改地址/绑定立即失效且不自动重连；名称不可改，只能使用部署批准目标。
// @Tags mcp
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "服务器编号"
// @Param body body mcpRegistrationRequest true "新 Base URL（name 可省略，但不可更改）"
// @Success 200 {object} mcpServerDTO
// @Failure 400 {object} errorBody
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers/{id} [put]
func (s *Server) updateMCPServer(w http.ResponseWriter, r *http.Request) {
	server, ok := s.mcpServerFromRequest(w, r)
	if !ok {
		return
	}
	var req mcpRegistrationRequest
	if !decodeMCPBody(w, r, &req) {
		return
	}
	if req.Name != "" && req.Name != server.Name {
		writeError(w, 400, "服务器名称创建后不可修改")
		return
	}
	ref, err := s.MCP.Registry.Reference(server.ClassID, server.Name, req.BaseURL)
	if !writeMCPError(w, err) {
		return
	}
	updated, err := s.Store.UpdateMCPServer(r.Context(), server.ClassID, server.ID, req.BaseURL, ref)
	if !writeMCPError(w, err) {
		return
	}
	writeJSON(w, 200, s.mcpDTO(updated))
}

// @Summary 删除本班 MCP 服务器（仅教师）
// @Description 软删除保留名称、历史来源和调用审计，后续不能连接或调用。
// @Tags mcp
// @Security BearerAuth
// @Param id path int true "服务器编号"
// @Success 204
// @Failure 401 {object} errorBody
// @Failure 403 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 500 {object} errorBody
// @Router /api/mcp/servers/{id} [delete]
func (s *Server) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	server, ok := s.mcpServerFromRequest(w, r)
	if !ok {
		return
	}
	if !writeMCPError(w, s.Store.DeleteMCPServer(r.Context(), server.ClassID, server.ID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
