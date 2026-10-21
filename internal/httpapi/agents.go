package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"campusclaw/internal/store"
)

const (
	maxAgentNameRunes   = 64
	maxAgentPromptRunes = 2000
)

type agentRequest struct {
	Name        string  `json:"name"`
	Prompt      string  `json:"prompt"`
	Enabled     bool    `json:"enabled"`
	SkillIDs    []int64 `json:"skill_ids"`
	MaterialIDs []int64 `json:"material_ids"`
}

// listAgents returns the class's agents.
//
// @Summary      列出本班智能体
// @Tags         agents
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  agentListResponse
// @Failure      401  {object}  errorBody
// @Router       /api/agents [get]
// listAgents returns the class's agents. Students only ever see enabled ones.
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	agents, err := s.Store.ListAgents(r.Context(), user.ClassID, user.Role != "teacher")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	if agents == nil {
		agents = []store.Agent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

// createAgent creates an agent with its skill and material bindings.
//
// @Summary      新建智能体
// @Tags         agents
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  agentRequest  true  "智能体"
// @Success      201  {object}  store.Agent
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      403  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/agents [post]
func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	req, ok := decodeAgentRequest(w, r)
	if !ok {
		return
	}
	agent, err := s.Store.CreateAgent(r.Context(), store.NewAgent{
		ClassID:     user.ClassID,
		Name:        req.Name,
		Prompt:      req.Prompt,
		Enabled:     req.Enabled,
		SkillIDs:    req.SkillIDs,
		MaterialIDs: req.MaterialIDs,
	})
	if !writeAgentError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, agent)
}

// getAgent returns one class-scoped agent.
//
// @Summary      读取一个智能体
// @Tags         agents
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "智能体编号"
// @Success      200  {object}  store.Agent
// @Failure      401  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/agents/{id} [get]
func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	agent, err := s.Store.GetAgent(r.Context(), user.ClassID, id, user.Role != "teacher")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent")
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

// updateAgent replaces an agent's fields and bindings.
//
// @Summary      修改智能体
// @Tags         agents
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int           true  "智能体编号"
// @Param        body  body  agentRequest  true  "智能体"
// @Success      200  {object}  store.Agent
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      403  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/agents/{id} [put]
func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	req, ok := decodeAgentRequest(w, r)
	if !ok {
		return
	}
	agent, err := s.Store.UpdateAgent(r.Context(), user.ClassID, id, store.NewAgent{
		ClassID:     user.ClassID,
		Name:        req.Name,
		Prompt:      req.Prompt,
		Enabled:     req.Enabled,
		SkillIDs:    req.SkillIDs,
		MaterialIDs: req.MaterialIDs,
	})
	if !writeAgentError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func decodeAgentRequest(w http.ResponseWriter, r *http.Request) (agentRequest, bool) {
	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return req, false
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return req, false
	}
	if utf8.RuneCountInString(req.Name) > maxAgentNameRunes {
		writeError(w, http.StatusBadRequest, "name exceeds 64 characters")
		return req, false
	}
	if utf8.RuneCountInString(req.Prompt) > maxAgentPromptRunes {
		writeError(w, http.StatusBadRequest, "prompt exceeds 2000 characters")
		return req, false
	}
	if req.SkillIDs == nil {
		req.SkillIDs = []int64{}
	}
	if req.MaterialIDs == nil {
		req.MaterialIDs = []int64{}
	}
	return req, true
}

// writeAgentError maps store errors to responses. It reports whether the
// handler should continue (true) or has already written an error (false).
func writeAgentError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, http.StatusBadRequest, "name already exists in this class")
	default:
		writeError(w, http.StatusInternalServerError, "failed to save agent")
	}
	return false
}
