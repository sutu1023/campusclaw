package httpapi

import "net/http"

// exampleQuestion is one server-authored prompt for the demo UI.
type exampleQuestion struct {
	Text       string `json:"text"`
	NoEvidence bool   `json:"no_evidence"`
}

// exampleQuestions are answerable from the seeded demo corpus (both classes),
// plus one intentionally unsupported prompt to demonstrate "no evidence".
var exampleQuestions = []exampleQuestion{
	{Text: "课堂提问有哪些有效技巧？"},
	{Text: "分数的意义是什么？"},
	{Text: "今天天气怎么样？", NoEvidence: true},
}

// examples returns the server-authored example questions.
//
// @Summary      列出示例问句
// @Tags         retrieval
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  exampleListResponse
// @Failure      401  {object}  errorBody
// @Router       /api/examples [get]
func (s *Server) examples(w http.ResponseWriter, r *http.Request) {
	if _, ok := currentUser(r); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"examples": exampleQuestions})
}
