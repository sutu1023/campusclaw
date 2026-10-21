// Package indexer runs the chunk -> embed -> Qdrant pipeline.
//
// MySQL knowledge_chunks is the source of truth. Embedding failures mark chunks
// failed without writing partial vectors; the backfill/reindex paths retry.
package indexer

import (
	"context"
	"fmt"

	"campusclaw/internal/chunk"
	"campusclaw/internal/embed"
	"campusclaw/internal/store"
	"campusclaw/internal/vector"
)

// Indexer coordinates chunking, embedding and vector writes.
type Indexer struct {
	Store  *store.Store
	Embed  *embed.Client
	Vector *vector.Client
	Model  string
}

// IndexMaterial re-splits a material's original text and indexes the new
// chunks. The caller is responsible for deleting old chunks/vectors first.
func (ix *Indexer) IndexMaterial(ctx context.Context, classID, materialID int64, opts chunk.Options) (int, error) {
	entry, err := ix.Store.GetEntryByMaterial(ctx, classID, materialID)
	if err != nil {
		return 0, err
	}
	parts, err := chunk.Split(entry.BodyText, opts)
	if err != nil {
		return 0, err
	}
	if len(parts) == 0 {
		return 0, nil
	}
	newChunks := make([]store.NewChunk, 0, len(parts))
	for _, p := range parts {
		newChunks = append(newChunks, store.NewChunk{
			KnowledgeEntryID: entry.ID,
			ClassID:          classID,
			ChunkIndex:       p.Index,
			StartOffset:      p.StartOffset,
			EndOffset:        p.EndOffset,
			ChunkText:        p.Text,
		})
	}
	if _, err := ix.Store.InsertChunks(ctx, newChunks); err != nil {
		return 0, err
	}

	views, err := ix.Store.ListChunksByMaterial(ctx, classID, materialID)
	if err != nil {
		return 0, err
	}
	pending := views[:0]
	for _, v := range views {
		if v.IndexStatus != "ready" {
			pending = append(pending, v)
		}
	}
	if err := ix.IndexChunkViews(ctx, pending); err != nil {
		return len(pending), err
	}
	return len(pending), nil
}

// EnsureMaterial indexes a material without duplicating chunks: if chunks
// already exist it retries the non-ready ones, otherwise it splits and inserts.
// It is safe to run on every startup.
func (ix *Indexer) EnsureMaterial(ctx context.Context, classID, materialID int64, opts chunk.Options) (int, error) {
	views, err := ix.Store.ListChunksByMaterial(ctx, classID, materialID)
	if err != nil {
		return 0, err
	}
	if len(views) == 0 {
		return ix.IndexMaterial(ctx, classID, materialID, opts)
	}
	pending := make([]store.ChunkView, 0, len(views))
	for _, v := range views {
		if v.IndexStatus != "ready" {
			pending = append(pending, v)
		}
	}
	if err := ix.IndexChunkViews(ctx, pending); err != nil {
		return len(pending), err
	}
	return len(pending), nil
}

// IndexChunkViews embeds and upserts already-persisted chunks, marking each
// ready on success or failed on any error.
func (ix *Indexer) IndexChunkViews(ctx context.Context, views []store.ChunkView) error {
	if len(views) == 0 {
		return nil
	}
	texts := make([]string, len(views))
	for i, v := range views {
		texts[i] = v.ChunkText
	}
	vecs, err := ix.Embed.Embed(ctx, texts)
	if err != nil {
		ix.markAll(ctx, views, "failed")
		return fmt.Errorf("embed chunks: %w", err)
	}
	points := make([]vector.Point, 0, len(views))
	for i, v := range views {
		points = append(points, vector.Point{
			ID:     v.ID,
			Vector: vecs[i],
			Payload: vector.Payload{
				ClassID:          v.ClassID,
				MaterialID:       v.MaterialID,
				KnowledgeEntryID: v.KnowledgeEntryID,
				ChunkID:          v.ID,
				ChunkIndex:       v.ChunkIndex,
			},
		})
	}
	if err := ix.Vector.Upsert(ctx, points); err != nil {
		ix.markAll(ctx, views, "failed")
		return fmt.Errorf("upsert vectors: %w", err)
	}
	for _, v := range views {
		if err := ix.Store.SetChunkStatus(ctx, v.ID, "ready", ix.Model); err != nil {
			return err
		}
	}
	return nil
}

func (ix *Indexer) markAll(ctx context.Context, views []store.ChunkView, status string) {
	for _, v := range views {
		_ = ix.Store.SetChunkStatus(ctx, v.ID, status, ix.Model)
	}
}
