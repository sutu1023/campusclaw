package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"campusclaw/internal/store"
)

// placeholderTitle is the title a conversation carries until its first question.
const placeholderTitle = "新的提问"

// titleFromQuestion builds a conversation title from the first question: its
// first 24 runes, or the placeholder when that would be empty.
func titleFromQuestion(question string) string {
	q := []rune(strings.TrimSpace(question))
	if len(q) == 0 {
		return placeholderTitle
	}
	if len(q) > 24 {
		q = q[:24]
	}
	title := strings.TrimSpace(string(q))
	if title == "" {
		return placeholderTitle
	}
	return title
}

type createConversationRequest struct {
	AgentID int64 `json:"agent_id"`
}

// listConversations returns the caller's conversations, newest first.
//
// @Summary      列出我的会话
// @Tags         conversations
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  conversationListResponse
// @Failure      401  {object}  errorBody
// @Router       /api/conversations [get]
func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	conversations, err := s.Store.ListConversations(r.Context(), user.ClassID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list conversations")
		return
	}
	if conversations == nil {
		conversations = []store.Conversation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
}

// createConversation creates a conversation bound to an enabled agent.
//
// @Summary      按智能体新建会话
// @Tags         conversations
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  createConversationRequest  true  "智能体编号"
// @Success      201  {object}  store.Conversation
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/conversations [post]
func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req createConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// A conversation must be created against an enabled agent of the caller's
	// class. If the class has no enabled agent at all, that is a 400 (not a 404
	// about a specific agent).
	enabled, err := s.Store.ListAgents(r.Context(), user.ClassID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agents")
		return
	}
	if len(enabled) == 0 {
		writeError(w, http.StatusBadRequest, "本班暂无可用的智能体")
		return
	}
	if req.AgentID <= 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	agent, err := s.Store.GetAgent(r.Context(), user.ClassID, req.AgentID, true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent")
		return
	}

	conv, err := s.Store.CreateConversation(r.Context(), user.ClassID, user.ID, agent.ID, placeholderTitle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

// getConversation returns a conversation and its messages.
//
// @Summary      读取会话和消息
// @Tags         conversations
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "会话编号"
// @Success      200  {object}  conversationDetailResponse
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/conversations/{id} [get]
func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
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
	conv, err := s.Store.GetConversation(r.Context(), user.ClassID, user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load conversation")
		return
	}
	messages, err := s.Store.ListMessages(r.Context(), conv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}
	if messages == nil {
		messages = []store.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": conv, "messages": messages})
}
