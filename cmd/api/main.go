// Command api runs the CampusClaw HTTP server.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"campusclaw/internal/auth"
	"campusclaw/internal/chat"
	"campusclaw/internal/chunk"
	"campusclaw/internal/config"
	"campusclaw/internal/embed"
	"campusclaw/internal/httpapi"
	"campusclaw/internal/indexer"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/store"
	"campusclaw/internal/vector"
)

// ensureIndexed makes sure every material that has no ready chunk gets one.
// It reuses existing chunks when present, so it never duplicates slices.
func ensureIndexed(ctx context.Context, st *store.Store, idx *indexer.Indexer) error {
	refs, err := st.ListMaterialsToIndex(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		n, err := idx.EnsureMaterial(ctx, ref.ClassID, ref.ID, chunk.Options{})
		if err != nil {
			log.Printf("ensure-index material %d failed: %v", ref.ID, err)
			continue
		}
		if n > 0 {
			log.Printf("ensure-index material %d: %d chunks", ref.ID, n)
		}
	}
	return nil
}

// @title           CampusClaw API
// @version         1.0
// @description     CampusClaw 教学平台接口：认证、材料与知识库检索、智能体与技能、会话与流式问答。
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     输入 "Bearer <token>"
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	ctx := context.Background()

	st, err := store.Open(ctx, cfg.DBDSN, 60*time.Second)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if err := st.Seed(ctx); err != nil {
		log.Fatalf("seed: %v", err)
	}

	emb := embed.New(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel, cfg.EmbeddingDim)
	vec := vector.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.EmbeddingDim)
	if err := vec.EnsureCollection(ctx); err != nil {
		if errors.Is(err, vector.ErrDimensionMismatch) {
			log.Fatalf("qdrant: %v", err)
		}
		log.Printf("warning: qdrant not ready at startup (%v); vector search will degrade to 503", err)
	}

	idx := &indexer.Indexer{Store: st, Embed: emb, Vector: vec, Model: cfg.EmbeddingModel}
	ret := &retrieval.Service{Store: st, Embed: emb, Vector: vec, MinScore: cfg.VectorMinScore, RRFK: cfg.RRFK}

	srv := &httpapi.Server{
		Store:          st,
		TokenSecret:    cfg.TokenSecret,
		Auth:           &auth.Handler{Store: st, TokenSecret: cfg.TokenSecret, SessionTTL: cfg.SessionTTL},
		Retrieval:      ret,
		Chat:           chat.New(cfg.ChatBaseURL, cfg.ChatAPIKey, cfg.ChatModel),
		Indexer:        idx,
		UploadDir:      cfg.UploadDir,
		MaxUploadBytes: cfg.MaxUploadBytes,
		EmbeddingDim:   cfg.EmbeddingDim,
		EmbeddingModel: cfg.EmbeddingModel,
	}

	// Periodically drop expired sessions.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := st.DeleteExpiredSessions(context.Background()); err == nil && n > 0 {
				log.Printf("removed %d expired sessions", n)
			}
		}
	}()

	// Index seeded/legacy materials once, in the background so that /health is
	// available immediately. Failures are non-fatal and retryable via cmd/backfill.
	go func() {
		if err := ensureIndexed(context.Background(), st, idx); err != nil {
			log.Printf("ensure-index warning: %v", err)
		}
	}()

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("CampusClaw api listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}
