// Package vector is a minimal Qdrant REST client.
//
// Only identifiers live in Qdrant payloads; chunk text always stays in MySQL.
package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Payload is the identifier-only payload stored per point.
type Payload struct {
	ClassID          int64 `json:"class_id"`
	MaterialID       int64 `json:"material_id"`
	KnowledgeEntryID int64 `json:"knowledge_entry_id"`
	ChunkID          int64 `json:"chunk_id"`
	ChunkIndex       int   `json:"chunk_index"`
}

// Point is one vector to upsert.
type Point struct {
	ID      int64   `json:"id"`
	Vector  []float32 `json:"vector"`
	Payload Payload `json:"payload"`
}

// ScoredPoint is one search result.
type ScoredPoint struct {
	ID      int64   `json:"id"`
	Score   float64 `json:"score"`
	Payload Payload `json:"payload"`
}

// ErrDimensionMismatch signals an existing collection whose dimension does not
// match configuration. Startup treats it as fatal.
var ErrDimensionMismatch = errors.New("qdrant collection dimension mismatch")

// Client talks to Qdrant over its REST API.
type Client struct {
	BaseURL    string
	Collection string
	Dim        int
	HTTP       *http.Client
}

// New builds a Qdrant client.
func New(baseURL, collection string, dim int) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Collection: collection,
		Dim:        dim,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
	}
}

// Available does a lightweight health probe.
func (c *Client) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/collections", nil)
	if err != nil {
		return false
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

// EnsureCollection creates the collection when missing and verifies the
// existing dimension matches configuration.
func (c *Client) EnsureCollection(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/collections/"+c.Collection, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var out struct {
			Result struct {
				Config struct {
					Params struct {
						Vectors struct {
							Size int `json:"size"`
						} `json:"vectors"`
					} `json:"params"`
				} `json:"config"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return fmt.Errorf("decode collection info: %w", err)
		}
		if c.Dim > 0 && out.Result.Config.Params.Vectors.Size != c.Dim {
			return fmt.Errorf("%w: collection %d, configured %d",
				ErrDimensionMismatch, out.Result.Config.Params.Vectors.Size, c.Dim)
		}
		return nil
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("qdrant get collection status %d", resp.StatusCode)
	}

	body := map[string]any{"vectors": map[string]any{"size": c.Dim, "distance": "Cosine"}}
	resp, err = c.do(ctx, http.MethodPut, "/collections/"+c.Collection, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("qdrant create collection status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Upsert writes points idempotently by primary key.
func (c *Client) Upsert(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	body := map[string]any{"points": points}
	resp, err := c.do(ctx, http.MethodPut, "/collections/"+c.Collection+"/points?wait=true", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("qdrant upsert status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Delete removes points by id.
func (c *Client) Delete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	body := map[string]any{"points": ids}
	resp, err := c.do(ctx, http.MethodPost, "/collections/"+c.Collection+"/points/delete?wait=true", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("qdrant delete status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Search runs a class-filtered cosine search.
func (c *Client) Search(ctx context.Context, vector []float32, classID int64, limit int) ([]ScoredPoint, error) {
	body := map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "class_id", "match": map[string]any{"value": classID}},
			},
		},
	}
	return c.search(ctx, body)
}

// SearchInMaterials runs a class-filtered cosine search restricted to a set of
// bound material ids.
func (c *Client) SearchInMaterials(ctx context.Context, vector []float32, classID int64, materialIDs []int64, limit int) ([]ScoredPoint, error) {
	if len(materialIDs) == 0 {
		return nil, nil
	}
	idsAny := make([]any, 0, len(materialIDs))
	for _, id := range materialIDs {
		idsAny = append(idsAny, id)
	}
	body := map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "class_id", "match": map[string]any{"value": classID}},
				{"key": "material_id", "match": map[string]any{"any": idsAny}},
			},
		},
	}
	return c.search(ctx, body)
}

func (c *Client) search(ctx context.Context, body map[string]any) ([]ScoredPoint, error) {
	resp, err := c.do(ctx, http.MethodPost, "/collections/"+c.Collection+"/points/search", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("qdrant search status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Result []ScoredPoint `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return out.Result, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.HTTP.Do(req)
}
