package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"campusclaw/internal/chat"
	"campusclaw/internal/retrieval"
)

type searchRequest struct {
	Q      string `json:"q"`
	Mode   string `json:"mode"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// search runs a class-scoped keyword / vector / hybrid retrieval.
//
// @Summary      检索本班切片
// @Tags         retrieval
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  searchRequest  true  "检索请求"
// @Success      200  {object}  searchResponse
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      503  {object}  errorBody
// @Router       /api/search [post]
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(req.Q) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	// Any class_id in the request body is ignored: the class always comes from
	// the authenticated session.
	result, err := s.Retrieval.Search(r.Context(), user.ClassID, req.Q, req.Mode, req.Limit, req.Offset)
	if errors.Is(err, retrieval.ErrVectorUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "vector search unavailable")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

const askSystemPrompt = "你是 CampusClaw 教学助理。只能依据编号资料回答，并用 [n] 标注出处；资料不足时明确说明未找到，不得编造。"

type askRequest struct {
	Q       string         `json:"q"`
	History []chat.Message `json:"history"`
}

type citation struct {
	Marker        string `json:"marker"`
	ChunkID       int64  `json:"chunk_id"`
	MaterialID    int64  `json:"material_id"`
	MaterialTitle string `json:"material_title"`
	ChunkIndex    int    `json:"chunk_index"`
	StartOffset   int    `json:"start_offset"`
	EndOffset     int    `json:"end_offset"`
}

var citationRE = regexp.MustCompile(`\[(\d+)\]`)

// ask answers a single question from class materials with citations.
//
// @Summary      按本班资料作简短回答
// @Tags         retrieval
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  askRequest  true  "问答请求"
// @Success      200  {object}  askResponse
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      503  {object}  errorBody
// @Router       /api/ask [post]
func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req askRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(req.Q) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	result, err := s.Retrieval.Search(r.Context(), user.ClassID, req.Q, "hybrid", 4, 0)
	if errors.Is(err, retrieval.ErrVectorUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "vector search unavailable")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	if len(result.Hits) == 0 {
		// No evidence: never call the model.
		writeJSON(w, http.StatusOK, map[string]any{
			"answer":    retrieval.NoEvidenceMessage,
			"status":    "no_evidence",
			"citations": []citation{},
		})
		return
	}
	if !s.Chat.Configured() {
		writeError(w, http.StatusServiceUnavailable, "chat gateway unavailable")
		return
	}

	msgs := []chat.Message{{Role: "system", Content: askSystemPrompt}}
	for _, m := range req.History {
		// Client-authored system messages are dropped; only the server writes system.
		if m.Role == "user" || m.Role == "assistant" {
			msgs = append(msgs, chat.Message{Role: m.Role, Content: m.Content})
		}
	}
	msgs = append(msgs, chat.Message{Role: "user", Content: buildAskContext(result.Hits) + "\n问题：" + req.Q})

	answer, err := s.Chat.CompleteMessages(r.Context(), msgs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "chat gateway failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"answer":    answer,
		"status":    "ok",
		"citations": extractCitations(answer, result.Hits),
	})
}

func buildAskContext(hits []retrieval.Hit) string {
	var b strings.Builder
	b.WriteString("以下是与问题最相关的资料，请优先据此回答：\n\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] 材料：%s（切片 %d，字符 %d-%d）\n%s\n\n",
			i+1, h.MaterialTitle, h.ChunkIndex, h.StartOffset, h.EndOffset, h.Snippet)
	}
	return b.String()
}

func extractCitations(answer string, hits []retrieval.Hit) []citation {
	seen := map[int]bool{}
	out := make([]citation, 0)
	for _, m := range citationRE.FindAllStringSubmatch(answer, -1) {
		n, err := jsonNumber(m[1])
		if err != nil || n < 1 || n > len(hits) {
			continue // drop out-of-range references
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		h := hits[n-1]
		out = append(out, citation{
			Marker:        m[0],
			ChunkID:       h.ChunkID,
			MaterialID:    h.MaterialID,
			MaterialTitle: h.MaterialTitle,
			ChunkIndex:    h.ChunkIndex,
			StartOffset:   h.StartOffset,
			EndOffset:     h.EndOffset,
		})
	}
	return out
}

func jsonNumber(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
