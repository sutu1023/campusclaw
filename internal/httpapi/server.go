// Package httpapi wires the HTTP surface together.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"campusclaw/internal/auth"
	"campusclaw/internal/chat"
	"campusclaw/internal/indexer"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/store"
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	Store          *store.Store
	TokenSecret    string
	Auth           *auth.Handler
	Retrieval      *retrieval.Service
	Chat           *chat.Client
	Indexer        *indexer.Indexer
	UploadDir      string
	MaxUploadBytes int64
	EmbeddingDim   int
	EmbeddingModel string
}

// Router builds the application mux. Routes under /api (except login) require
// a valid Bearer token; /health is public.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	// Documentation routes are mounted before the business routes so they are
	// never shadowed.
	mountDocs(mux)
	s.Auth.Register(mux)

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	authed := auth.Middleware(s.Store, s.TokenSecret)
	teacher := auth.RequireTeacher

	mux.Handle("GET /api/materials", authed(http.HandlerFunc(s.listMaterials)))
	mux.Handle("POST /api/materials", authed(teacher(http.HandlerFunc(s.uploadMaterial))))
	mux.Handle("GET /api/materials/{id}", authed(http.HandlerFunc(s.getMaterial)))
	mux.Handle("GET /api/materials/{id}/vectors", authed(http.HandlerFunc(s.materialVectors)))
	mux.Handle("POST /api/materials/{id}/reindex", authed(teacher(http.HandlerFunc(s.reindexMaterial))))
	mux.Handle("GET /api/agents", authed(http.HandlerFunc(s.listAgents)))
	mux.Handle("POST /api/agents", authed(teacher(http.HandlerFunc(s.createAgent))))
	mux.Handle("GET /api/agents/{id}", authed(http.HandlerFunc(s.getAgent)))
	mux.Handle("PUT /api/agents/{id}", authed(teacher(http.HandlerFunc(s.updateAgent))))
	mux.Handle("GET /api/skills", authed(http.HandlerFunc(s.listSkills)))
	mux.Handle("POST /api/skills", authed(teacher(http.HandlerFunc(s.createSkill))))
	mux.Handle("PUT /api/skills/{id}", authed(teacher(http.HandlerFunc(s.updateSkill))))
	mux.Handle("DELETE /api/skills/{id}", authed(teacher(http.HandlerFunc(s.deleteSkill))))
	mux.Handle("GET /api/conversations", authed(http.HandlerFunc(s.listConversations)))
	mux.Handle("POST /api/conversations", authed(http.HandlerFunc(s.createConversation)))
	mux.Handle("GET /api/conversations/{id}", authed(http.HandlerFunc(s.getConversation)))
	mux.Handle("POST /api/conversations/{id}/messages", authed(http.HandlerFunc(s.sendMessage)))
	mux.Handle("POST /api/search", authed(http.HandlerFunc(s.search)))
	mux.Handle("POST /api/ask", authed(http.HandlerFunc(s.ask)))
	mux.Handle("GET /api/examples", authed(http.HandlerFunc(s.examples)))

	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// pathID reads the {id} wildcard.
func pathID(r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func currentUser(r *http.Request) (*store.User, bool) { return auth.UserFrom(r.Context()) }
