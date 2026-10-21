package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"campusclaw/internal/chat"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/skill"
	"campusclaw/internal/store"
)

const (
	minQuestionRunes    = 2
	maxQuestionRunes    = 500
	maxEvidenceRunes    = 800
	historyWindowSize   = 6
	historyMessageRunes = 500
)

type sendMessageRequest struct {
	Content string `json:"content"`
}

// sendMessage handles POST /api/conversations/{id}/messages. Everything that
// can fail deterministically happens before the first SSE event; after that the
// status code is fixed at 200 and failures are reported as an error event.
//
// @Summary      在会话中继续提问（SSE 流式）
// @Description  以 text/event-stream 返回 meta / delta / done / error 四类事件。
// @Tags         conversations
// @Accept       json
// @Produce      text/event-stream
// @Security     BearerAuth
// @Param        id    path  int                 true  "会话编号"
// @Param        body  body  sendMessageRequest  true  "提问"
// @Success      200  {string}  string  "SSE 事件流"
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Failure      503  {object}  errorBody
// @Router       /api/conversations/{id}/messages [post]
func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	convID, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	question := strings.TrimSpace(req.Content)
	if n := utf8.RuneCountInString(question); n < minQuestionRunes || n > maxQuestionRunes {
		writeError(w, http.StatusBadRequest, "question must be between 2 and 500 characters")
		return
	}

	conv, err := s.Store.GetConversation(ctx, user.ClassID, user.ID, convID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "conversation unavailable")
		return
	}
	agent, err := s.Store.GetAgent(ctx, user.ClassID, conv.AgentID, false)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	if !s.Chat.Configured() {
		writeError(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}

	candidates, err := s.Store.ListAgentCandidateSkills(ctx, user.ClassID, conv.AgentID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "skills unavailable")
		return
	}
	matched := routeSkill(question, candidates)

	// Persist the question before generating so it is part of the context.
	priorUser, err := s.Store.CountUserMessages(ctx, conv.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "conversation unavailable")
		return
	}
	questionMsgID, err := s.Store.AppendMessage(ctx, store.NewMessage{
		ConversationID: conv.ID, Role: "user", Content: question,
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to store question")
		return
	}
	if priorUser == 0 {
		_ = s.Store.UpdateConversationTitle(ctx, conv.ID, titleFromQuestion(question))
	} else {
		_ = s.Store.TouchConversation(ctx, conv.ID)
	}

	// Retrieval is restricted to the agent's bound materials.
	materialIDs, err := s.Store.AgentMaterialIDs(ctx, user.ClassID, conv.AgentID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "materials unavailable")
		return
	}
	hits := []retrieval.Hit{}
	if len(materialIDs) > 0 {
		hits, err = s.Retrieval.RetrieveInMaterials(ctx, user.ClassID, materialIDs, question, 4)
		if errors.Is(err, retrieval.ErrVectorUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "vector search unavailable")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "search unavailable")
			return
		}
	}
	if matched != nil {
		hits = retrieval.FilterBySubject(hits, matched.Subject)
	}
	evidences := buildEvidence(hits)
	citations := buildCitations(evidences)
	citationsJSON, _ := json.Marshal(citations)

	// Last window of history, excluding the question we just stored (it is
	// re-sent with its numbered evidence below).
	window, err := s.Store.RecentWindowMessages(ctx, conv.ID, historyWindowSize, historyMessageRunes)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "history unavailable")
		return
	}

	// First event boundary: from here on the status code is 200.
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeSSE(w, flusher, "meta", map[string]any{
		"status":    "ok",
		"skill":     skillPayload(matched),
		"citations": citations,
	})

	messages := []chat.Message{{Role: "system", Content: chat.BuildSystem(agent.Prompt, matched, len(evidences) > 0)}}
	for _, m := range chat.SanitizeMessages(historyMessages(window, questionMsgID)) {
		messages = append(messages, m)
	}
	messages = append(messages, chat.Message{Role: "user", Content: chat.BuildUser(question, evidences)})

	hasEvidence := len(evidences) > 0
	var final strings.Builder
	var stripper chat.MarkerStripper
	streamErr := s.Chat.CompleteMessagesStream(ctx, messages, func(text string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		out := text
		if !hasEvidence {
			out = stripper.Write(text)
		}
		if out == "" {
			return nil
		}
		final.WriteString(out)
		writeSSE(w, flusher, "delta", map[string]string{"text": out})
		return nil
	})
	if !hasEvidence {
		if tail := stripper.Flush(); tail != "" {
			final.WriteString(tail)
			writeSSE(w, flusher, "delta", map[string]string{"text": tail})
		}
	}

	// A failure before any output cannot be reported with a status code anymore.
	if streamErr != nil && final.Len() == 0 {
		writeSSE(w, flusher, "error", map[string]string{"error": "回答生成失败，请稍后重试"})
		return
	}

	skillName := ""
	if matched != nil {
		skillName = matched.Name
	}
	assistantID, err := s.Store.AppendMessage(ctx, store.NewMessage{
		ConversationID: conv.ID,
		Role:           "assistant",
		Content:        final.String(),
		Citations:      citationsJSON,
		SkillName:      skillName,
	})
	if err != nil {
		writeSSE(w, flusher, "error", map[string]string{"error": "回答保存失败"})
		return
	}
	_ = s.Store.TouchConversation(ctx, conv.ID)
	writeSSE(w, flusher, "done", map[string]any{"message_id": assistantID})
}

// historyMessages maps stored history to chat messages, dropping the question
// that was just stored (it is re-sent with its numbered evidence below).
func historyMessages(window []store.Message, skipID int64) []chat.Message {
	out := make([]chat.Message, 0, len(window))
	for _, m := range window {
		if m.ID == skipID {
			continue
		}
		out = append(out, chat.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// routeSkill converts stored candidates and runs the deterministic router.
func routeSkill(question string, candidates []store.Skill) *skill.Entry {
	entries := make([]skill.Entry, 0, len(candidates))
	for _, c := range candidates {
		entries = append(entries, skill.Entry{
			ID: c.ID, Name: c.Name, Trigger: c.Trigger, Subject: c.Subject,
			Description: c.Description, Enabled: c.Enabled,
		})
	}
	return skill.Route(question, entries)
}

// buildEvidence turns hits into numbered, size-capped evidence. Empty bodies are
// dropped before numbering so markers stay contiguous.
func buildEvidence(hits []retrieval.Hit) []chat.Evidence {
	out := []chat.Evidence{}
	for _, h := range hits {
		text := strings.TrimSpace(h.ChunkText)
		if text == "" {
			continue
		}
		out = append(out, chat.Evidence{
			Marker:        fmt.Sprintf("[%d]", len(out)+1),
			ChunkID:       h.ChunkID,
			MaterialID:    h.MaterialID,
			MaterialTitle: h.MaterialTitle,
			ChunkIndex:    h.ChunkIndex,
			StartOffset:   h.StartOffset,
			EndOffset:     h.EndOffset,
			Text:          truncateRunes(text, maxEvidenceRunes),
		})
	}
	return out
}

func buildCitations(evidences []chat.Evidence) []citation {
	out := make([]citation, 0, len(evidences))
	for i, e := range evidences {
		out = append(out, citation{
			Marker:        fmt.Sprintf("[%d]", i+1),
			ChunkID:       e.ChunkID,
			MaterialID:    e.MaterialID,
			MaterialTitle: e.MaterialTitle,
			ChunkIndex:    e.ChunkIndex,
			StartOffset:   e.StartOffset,
			EndOffset:     e.EndOffset,
		})
	}
	return out
}

func skillPayload(matched *skill.Entry) any {
	if matched == nil {
		return nil
	}
	return map[string]string{"name": matched.Name}
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// writeSSE writes one event frame and flushes it to the connection.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if flusher != nil {
		flusher.Flush()
	}
}
