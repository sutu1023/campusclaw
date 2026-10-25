// Package chat calls an OpenAI-compatible /chat/completions gateway.
package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the external chat gateway.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// New builds a chat client.
func New(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Configured reports whether a gateway URL is set.
func (c *Client) Configured() bool { return c.BaseURL != "" }

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Complete sends a server-authored system prompt plus the user turn.
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	return c.CompleteMessages(ctx, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
}

// CompleteMessages sends an explicit, server-validated message list.
func (c *Client) CompleteMessages(ctx context.Context, messages []Message) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("chat gateway is not configured")
	}
	body, err := json.Marshal(chatRequest{Model: c.Model, Messages: messages})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("chat gateway status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("chat gateway returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// DeltaFunc receives one incremental text fragment. Returning an error aborts
// the stream (used when the client disconnects).
type DeltaFunc func(text string) error

type streamRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string            `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// CompleteMessagesStream consumes the gateway's streaming chat completion and
// invokes onDelta for every new text fragment. When the gateway answers with a
// non-SSE body (streaming unsupported), the whole answer is delivered as a
// single delta so the caller's contract still holds.
func (c *Client) CompleteMessagesStream(ctx context.Context, messages []Message, onDelta DeltaFunc) error {
	return c.completeStream(ctx, streamRequest{Model: c.Model, Messages: messages, Stream: true}, onDelta, false)
}

func (c *Client) completeStream(ctx context.Context, payload any, onDelta DeltaFunc, strict bool) error {
	if !c.Configured() {
		return fmt.Errorf("chat gateway is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if strict {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrToolDecision
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if strict {
			return ErrToolDecision
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("chat gateway status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		if strict {
			return ErrToolDecision
		}
		// Non-streaming fallback: one delta with the complete answer.
		var out chatResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return fmt.Errorf("decode chat response: %w", err)
		}
		if len(out.Choices) == 0 {
			return fmt.Errorf("chat gateway returned no choices")
		}
		return onDelta(out.Choices[0].Message.Content)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	wrote := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &probe); err == nil {
			if raw, ok := probe["error"]; ok && string(raw) != "null" {
				if strict {
					return ErrToolDecision
				}
				return fmt.Errorf("chat gateway stream error: %s", string(raw))
			}
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if strict && len(chunk.Choices[0].Delta.ToolCalls) > 0 {
			return ErrToolDecision
		}
		if chunk.Choices[0].Delta.Content == "" {
			continue
		}
		if err := onDelta(chunk.Choices[0].Delta.Content); err != nil {
			return err
		}
		wrote = true
	}
	if err := scanner.Err(); err != nil {
		if strict {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrToolDecision
		}
		return err
	}
	if !wrote {
		return fmt.Errorf("chat gateway returned no streaming content")
	}
	return nil
}
