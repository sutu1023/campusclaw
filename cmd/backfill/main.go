// Command backfill indexes chunks that are still pending or failed.
//
// It is safe to run repeatedly: chunks that are already ready are skipped and
// Qdrant upserts are keyed by chunk id.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"campusclaw/internal/config"
	"campusclaw/internal/embed"
	"campusclaw/internal/indexer"
	"campusclaw/internal/store"
	"campusclaw/internal/vector"
)

func main() {
	statuses := flag.String("status", "pending,failed", "comma-separated index statuses to index")
	materialID := flag.Int64("material", 0, "limit to one material id (0 = all)")
	flag.Parse()

	cfg, err := gatewayConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	ctx := context.Background()

	st, err := store.Open(ctx, cfg.DBDSN, 60*time.Second)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	defer st.Close()

	emb := embed.New(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel, cfg.EmbeddingDim)
	vec := vector.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.EmbeddingDim)
	if err := vec.EnsureCollection(ctx); err != nil {
		log.Fatalf("ensure qdrant collection: %v", err)
	}
	idx := &indexer.Indexer{Store: st, Embed: emb, Vector: vec, Model: cfg.EmbeddingModel}

	list := strings.Split(*statuses, ",")
	views, err := st.ListChunksByStatus(ctx, list, *materialID)
	if err != nil {
		log.Fatalf("list chunks: %v", err)
	}
	if len(views) == 0 {
		log.Printf("no chunks to index")
		return
	}
	if err := idx.IndexChunkViews(ctx, views); err != nil {
		log.Fatalf("index %d chunks: %v", len(views), err)
	}
	log.Printf("indexed %d chunks", len(views))
}

// gatewayConfig loads only the fields the indexing commands need, without
// requiring the HTTP-only TOKEN_SECRET.
func gatewayConfig() (*config.Config, error) {
	// Reuse the standard loader by supplying a throwaway secret when unset;
	// these commands never serve HTTP.
	if os.Getenv("TOKEN_SECRET") == "" {
		_ = os.Setenv("TOKEN_SECRET", "backfill-command")
	}
	return config.Load()
}
