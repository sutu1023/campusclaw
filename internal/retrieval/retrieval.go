package retrieval

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"campusclaw/internal/embed"
	"campusclaw/internal/store"
	"campusclaw/internal/vector"
)

// ErrVectorUnavailable signals a vector/hybrid search that cannot run because
// the embedding gateway or Qdrant is unavailable. Handlers turn this into 503.
var ErrVectorUnavailable = errors.New("vector search unavailable")

// Hit is one traceable search result.
type Hit struct {
	ChunkID       int64   `json:"chunk_id"`
	MaterialID    int64   `json:"material_id"`
	MaterialTitle string  `json:"material_title"`
	ChunkIndex    int     `json:"chunk_index"`
	StartOffset   int     `json:"start_offset"`
	EndOffset     int     `json:"end_offset"`
	Snippet       string  `json:"snippet"`
	Score         float64 `json:"score,omitempty"`
	// ChunkText is the full slice body, used internally to build evidence for
	// the streaming path. It is never serialized.
	ChunkText string `json:"-"`
}

// Result is the outcome of a search.
type Result struct {
	Hits        []Hit     `json:"hits"`
	Status      string    `json:"status"`
	Message     string    `json:"message,omitempty"`
	QueryVector []float32 `json:"query_vector,omitempty"`
}

// Service performs class-scoped retrieval.
type Service struct {
	Store    *store.Store
	Embed    *embed.Client
	Vector   *vector.Client
	MinScore float64
	RRFK     int
}

// NoEvidenceMessage is the fixed answer when nothing relevant is found.
const NoEvidenceMessage = "资料中未找到相关内容"

const snippetRunes = 160

// Search runs the requested mode. The class id is always server-derived by the
// caller; any class_id in the request has already been discarded.
func (s *Service) Search(ctx context.Context, classID int64, query, mode string, limit, offset int) (*Result, error) {
	if mode == "" {
		mode = "hybrid"
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	switch mode {
	case "keyword":
		hits, err := s.keywordHits(ctx, classID, query, limit+offset)
		if err != nil {
			return nil, err
		}
		return finalize(hits, offset, limit), nil
	case "vector":
		ids, err := s.vectorIDs(ctx, classID, query, limit+offset)
		if err != nil {
			return nil, err
		}
		hits, err := s.hydrate(ctx, classID, ids)
		if err != nil {
			return nil, err
		}
		return finalize(hits, offset, limit), nil
	case "hybrid":
		keywordIDs, err := s.keywordIDs(ctx, classID, query, limit+offset)
		if err != nil {
			return nil, err
		}
		vectorIDs, err := s.vectorIDs(ctx, classID, query, limit+offset)
		if err != nil {
			return nil, err
		}
		fused := RankFuse(s.RRFK, keywordIDs, vectorIDs)
		hits, err := s.hydrate(ctx, classID, fused)
		if err != nil {
			return nil, err
		}
		return finalize(hits, offset, limit), nil
	default:
		return nil, fmt.Errorf("unknown search mode %q", mode)
	}
}

func (s *Service) keywordIDs(ctx context.Context, classID int64, query string, limit int) ([]int64, error) {
	raw, err := s.Store.KeywordSearch(ctx, classID, query, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(raw))
	for _, h := range raw {
		ids = append(ids, h.ChunkID)
	}
	return ids, nil
}

func (s *Service) keywordHits(ctx context.Context, classID int64, query string, limit int) ([]Hit, error) {
	raw, err := s.Store.KeywordSearch(ctx, classID, query, limit)
	if err != nil {
		return nil, err
	}
	scoreByID := make(map[int64]float64, len(raw))
	ids := make([]int64, 0, len(raw))
	for _, h := range raw {
		ids = append(ids, h.ChunkID)
		scoreByID[h.ChunkID] = h.Score
	}
	hits, err := s.hydrate(ctx, classID, ids)
	if err != nil {
		return nil, err
	}
	for i := range hits {
		hits[i].Score = scoreByID[hits[i].ChunkID]
	}
	return hits, nil
}

func (s *Service) vectorIDs(ctx context.Context, classID int64, query string, limit int) ([]int64, error) {
	if s.Embed == nil || !s.Embed.Configured() {
		return nil, ErrVectorUnavailable
	}
	vecs, err := s.Embed.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVectorUnavailable, err)
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	points, err := s.Vector.Search(ctx, vecs[0], classID, limit*3)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVectorUnavailable, err)
	}
	ids := make([]int64, 0, len(points))
	for _, p := range points {
		if p.Score < s.MinScore {
			continue // filter low-relevance candidates before fusion
		}
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// hydrate loads MySQL text for candidate ids, dropping any row that is not in
// the caller's class (a second class check after the vector hit).
func (s *Service) hydrate(ctx context.Context, classID int64, ids []int64) ([]Hit, error) {
	hits := make([]Hit, 0, len(ids))
	for _, id := range ids {
		cv, err := s.Store.GetChunkForClass(ctx, classID, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		hits = append(hits, Hit{
			ChunkID:       cv.ID,
			MaterialID:    cv.MaterialID,
			MaterialTitle: cv.MaterialTitle,
			ChunkIndex:    cv.ChunkIndex,
			StartOffset:   cv.StartOffset,
			EndOffset:     cv.EndOffset,
			Snippet:       snippet(cv.ChunkText),
			ChunkText:     cv.ChunkText,
		})
	}
	return hits, nil
}

func snippet(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= snippetRunes {
		return string(runes)
	}
	return string(runes[:snippetRunes]) + "…"
}

func finalize(hits []Hit, offset, limit int) *Result {
	if offset > len(hits) {
		offset = len(hits)
	}
	hits = hits[offset:]
	if limit < len(hits) {
		hits = hits[:limit]
	}
	if len(hits) == 0 {
		return &Result{Hits: []Hit{}, Status: "no_evidence", Message: NoEvidenceMessage}
	}
	return &Result{Hits: hits, Status: "ok"}
}

// FilterBySubject narrows hits to materials whose title contains the skill's
// 资料 keyword. An empty subject means no filtering.
func FilterBySubject(hits []Hit, subject string) []Hit {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return hits
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if strings.Contains(h.MaterialTitle, subject) {
			out = append(out, h)
		}
	}
	return out
}

// RetrieveInMaterials runs a hybrid search restricted to a set of bound
// material ids. It returns traceable hits, capped at limit. An empty material
// set yields no hits without touching any backend.
func (s *Service) RetrieveInMaterials(ctx context.Context, classID int64, materialIDs []int64, query string, limit int) ([]Hit, error) {
	if len(materialIDs) == 0 {
		return []Hit{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 4
	}
	keywordIDs, err := s.keywordIDsInMaterials(ctx, classID, materialIDs, query, limit)
	if err != nil {
		return nil, err
	}
	vectorIDs, err := s.vectorIDsInMaterials(ctx, classID, materialIDs, query, limit)
	if err != nil {
		return nil, err
	}
	fused := RankFuse(s.RRFK, keywordIDs, vectorIDs)
	hits, err := s.hydrate(ctx, classID, fused)
	if err != nil {
		return nil, err
	}
	if limit < len(hits) {
		hits = hits[:limit]
	}
	return hits, nil
}

func (s *Service) keywordIDsInMaterials(ctx context.Context, classID int64, materialIDs []int64, query string, limit int) ([]int64, error) {
	raw, err := s.Store.KeywordSearchInMaterials(ctx, classID, materialIDs, query, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(raw))
	for _, h := range raw {
		ids = append(ids, h.ChunkID)
	}
	return ids, nil
}

func (s *Service) vectorIDsInMaterials(ctx context.Context, classID int64, materialIDs []int64, query string, limit int) ([]int64, error) {
	if s.Embed == nil || !s.Embed.Configured() {
		return nil, ErrVectorUnavailable
	}
	vecs, err := s.Embed.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVectorUnavailable, err)
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	points, err := s.Vector.SearchInMaterials(ctx, vecs[0], classID, materialIDs, limit*3)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVectorUnavailable, err)
	}
	ids := make([]int64, 0, len(points))
	for _, p := range points {
		if p.Score < s.MinScore {
			continue
		}
		ids = append(ids, p.ID)
	}
	return ids, nil
}
