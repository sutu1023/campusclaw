package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Chunk is one persisted slice of a knowledge entry.
type Chunk struct {
	ID               int64  `json:"id"`
	KnowledgeEntryID int64  `json:"knowledge_entry_id"`
	ClassID          int64  `json:"class_id"`
	ChunkIndex       int    `json:"chunk_index"`
	StartOffset      int    `json:"start_offset"`
	EndOffset        int    `json:"end_offset"`
	ChunkText        string `json:"-"`
	IndexStatus      string `json:"index_status"`
	EmbeddingModel   string `json:"embedding_model,omitempty"`
}

// ChunkView is a chunk joined with its material title, used in API responses.
type ChunkView struct {
	Chunk
	MaterialID    int64  `json:"material_id"`
	MaterialTitle string `json:"material_title"`
}

// NewChunk is the input for InsertChunks.
type NewChunk struct {
	KnowledgeEntryID int64
	ClassID          int64
	ChunkIndex       int
	StartOffset      int
	EndOffset        int
	ChunkText        string
}

// InsertChunks persists slices with index_status='pending' and returns ids.
func (s *Store) InsertChunks(ctx context.Context, chunks []NewChunk) ([]int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin chunk tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ids := make([]int64, 0, len(chunks))
	for _, c := range chunks {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO knowledge_chunks
			   (knowledge_entry_id, class_id, chunk_index, start_offset, end_offset, chunk_text, index_status)
			 VALUES (?,?,?,?,?,?,'pending')`,
			c.KnowledgeEntryID, c.ClassID, c.ChunkIndex, c.StartOffset, c.EndOffset, c.ChunkText)
		if err != nil {
			return nil, fmt.Errorf("insert chunk: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit chunks: %w", err)
	}
	return ids, nil
}

const chunkViewSelect = `SELECT c.id, c.knowledge_entry_id, c.class_id, c.chunk_index,
	c.start_offset, c.end_offset, c.chunk_text, c.index_status, c.embedding_model,
	m.id, m.title
	FROM knowledge_chunks c
	JOIN knowledge_entries e ON e.id = c.knowledge_entry_id
	JOIN materials m ON m.id = e.material_id`

func scanChunkView(rows *sql.Rows) (*ChunkView, error) {
	var cv ChunkView
	var model sql.NullString
	if err := rows.Scan(&cv.ID, &cv.KnowledgeEntryID, &cv.ClassID, &cv.ChunkIndex,
		&cv.StartOffset, &cv.EndOffset, &cv.ChunkText, &cv.IndexStatus, &model,
		&cv.MaterialID, &cv.MaterialTitle); err != nil {
		return nil, err
	}
	cv.EmbeddingModel = model.String
	return &cv, nil
}

// ListChunksByMaterial returns a material's chunks scoped to the caller's class.
func (s *Store) ListChunksByMaterial(ctx context.Context, classID, materialID int64) ([]ChunkView, error) {
	rows, err := s.DB.QueryContext(ctx,
		chunkViewSelect+` WHERE c.class_id=? AND m.id=? ORDER BY c.chunk_index`, classID, materialID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	defer rows.Close()
	return collectChunkViews(rows)
}

// GetChunkForClass re-reads a chunk with its material, verifying the class a
// second time after a vector hit.
func (s *Store) GetChunkForClass(ctx context.Context, classID, chunkID int64) (*ChunkView, error) {
	rows, err := s.DB.QueryContext(ctx, chunkViewSelect+` WHERE c.class_id=? AND c.id=?`, classID, chunkID)
	if err != nil {
		return nil, fmt.Errorf("get chunk: %w", err)
	}
	defer rows.Close()
	views, err := collectChunkViews(rows)
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, ErrNotFound
	}
	return &views[0], nil
}

// ListChunksByStatus returns chunks in the given statuses, optionally limited
// to one material. Used by the backfill command.
func (s *Store) ListChunksByStatus(ctx context.Context, statuses []string, materialID int64) ([]ChunkView, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := make([]any, 0, len(statuses)+1)
	for _, st := range statuses {
		args = append(args, st)
	}
	q := chunkViewSelect + ` WHERE c.index_status IN (` + placeholders + `)`
	if materialID > 0 {
		q += ` AND m.id=?`
		args = append(args, materialID)
	}
	q += ` ORDER BY c.id`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list chunks by status: %w", err)
	}
	defer rows.Close()
	return collectChunkViews(rows)
}

func collectChunkViews(rows *sql.Rows) ([]ChunkView, error) {
	var out []ChunkView
	for rows.Next() {
		cv, err := scanChunkView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *cv)
	}
	return out, rows.Err()
}

// SetChunkStatus updates the index status and (optionally) embedding model.
func (s *Store) SetChunkStatus(ctx context.Context, chunkID int64, status, model string) error {
	if model == "" {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE knowledge_chunks SET index_status=? WHERE id=?`, status, chunkID); err != nil {
			return fmt.Errorf("set chunk status: %w", err)
		}
		return nil
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE knowledge_chunks SET index_status=?, embedding_model=? WHERE id=?`,
		status, model, chunkID); err != nil {
		return fmt.Errorf("set chunk status: %w", err)
	}
	return nil
}

// DeleteChunksByMaterial removes a material's chunks and returns their ids so
// the caller can delete the matching Qdrant points.
func (s *Store) DeleteChunksByMaterial(ctx context.Context, materialID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT c.id FROM knowledge_chunks c
		 JOIN knowledge_entries e ON e.id = c.knowledge_entry_id
		 WHERE e.material_id=?`, materialID)
	if err != nil {
		return nil, fmt.Errorf("select chunks to delete: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.DB.ExecContext(ctx,
		`DELETE c FROM knowledge_chunks c
		 JOIN knowledge_entries e ON e.id = c.knowledge_entry_id
		 WHERE e.material_id=?`, materialID); err != nil {
		return nil, fmt.Errorf("delete chunks: %w", err)
	}
	return ids, nil
}

// KeywordHit is one full-text match.
type KeywordHit struct {
	ChunkID int64
	Score   float64
}

// KeywordSearch runs the ngram full-text query scoped to a class and to ready
// chunks. Only rows with a positive natural-language score count as evidence.
func (s *Store) KeywordSearch(ctx context.Context, classID int64, query string, limit int) ([]KeywordHit, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, MATCH(chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS score
		 FROM knowledge_chunks
		 WHERE class_id=? AND index_status='ready'
		   AND MATCH(chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) > 0
		 ORDER BY score DESC
		 LIMIT ?`, query, classID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	defer rows.Close()
	var out []KeywordHit
	for rows.Next() {
		var h KeywordHit
		if err := rows.Scan(&h.ChunkID, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ChunkIDsForClass returns the set of chunk ids belonging to a class, used to
// filter Qdrant candidates before fetching text.
func (s *Store) ChunkIDsForClass(ctx context.Context, classID int64, ids []int64) (map[int64]bool, error) {
	if len(ids) == 0 {
		return map[int64]bool{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{classID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM knowledge_chunks WHERE class_id=? AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("filter chunk ids: %w", err)
	}
	defer rows.Close()
	found := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		found[id] = true
	}
	return found, rows.Err()
}

// KeywordSearchInMaterials runs the ngram full-text query restricted to a set of
// bound material ids, scoped to a class and to ready chunks.
func (s *Store) KeywordSearchInMaterials(ctx context.Context, classID int64, materialIDs []int64, query string, limit int) ([]KeywordHit, error) {
	if len(materialIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(materialIDs)), ",")
	args := []any{query, classID}
	for _, id := range materialIDs {
		args = append(args, id)
	}
	args = append(args, query, limit)
	rows, err := s.DB.QueryContext(ctx,
		`SELECT c.id, MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS score
		 FROM knowledge_chunks c
		 JOIN knowledge_entries e ON e.id = c.knowledge_entry_id
		 WHERE c.class_id=? AND c.index_status='ready' AND e.material_id IN (`+placeholders+`)
		   AND MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) > 0
		 ORDER BY score DESC
		 LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("keyword search in materials: %w", err)
	}
	defer rows.Close()
	var out []KeywordHit
	for rows.Next() {
		var h KeywordHit
		if err := rows.Scan(&h.ChunkID, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
