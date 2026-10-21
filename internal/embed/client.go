// Package embed calls an OpenAI-compatible /embeddings gateway.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client talks to the external embedding gateway.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	Dim     int
	HTTP    *http.Client
}

// New builds an embedding client.
func New(baseURL, apiKey, model string, dim int) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		Dim:     dim,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Configured reports whether a gateway URL is set.
func (c *Client) Configured() bool { return c.BaseURL != "" }

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingItem struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingResponse struct {
	Data []embeddingItem `json:"data"`
}

// Embed returns one vector per input, verifying the configured dimension.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("embedding gateway is not configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embeddingRequest{Model: c.Model, Input: texts})
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		vecs, err := c.embedOnce(ctx, body, len(texts))
		if err == nil {
			return vecs, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}
	return nil, lastErr
}

func (c *Client) embedOnce(ctx context.Context, body []byte, want int) ([][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("embedding gateway status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].Index < out.Data[j].Index })
	vecs := make([][]float32, 0, len(out.Data))
	for _, item := range out.Data {
		if c.Dim > 0 && len(item.Embedding) != c.Dim {
			return nil, fmt.Errorf("embedding dimension %d does not match configured %d", len(item.Embedding), c.Dim)
		}
		vecs = append(vecs, item.Embedding)
	}
	if len(vecs) != want {
		return nil, fmt.Errorf("embedding gateway returned %d vectors for %d inputs", len(vecs), want)
	}
	return vecs, nil
}
