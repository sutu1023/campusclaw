package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"campusclaw/internal/mcp"
)

const MaxDecisionCalls = 16

var ErrToolDecision = errors.New("tool decision unavailable or invalid")

// ProtocolMessage is deliberately separate from Message: client/history turns
// cannot carry tool authority. Only the host builds this ephemeral transcript.
type ProtocolMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}
type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters" swaggertype:"object"`
}

// ToolAlias is stable but reveals neither the approved origin nor a credential.
// Callers retain the exact alias-to-reference map and reject any collision.
func ToolAlias(serverID int64, toolName string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", serverID, toolName)))
	return "mcp_" + hex.EncodeToString(hash[:16])
}

func NewToolDefinition(alias, description string, schema json.RawMessage) (ToolDefinition, error) {
	if !mcp.ValidToolName(alias) || !utf8.ValidString(description) || utf8.RuneCountInString(description) > mcp.MaxDescription {
		return ToolDefinition{}, ErrToolDecision
	}
	if _, err := mcp.CompileInputSchema(schema); err != nil {
		return ToolDefinition{}, ErrToolDecision
	}
	return ToolDefinition{Type: "function", Function: FunctionDefinition{Name: alias, Description: description, Parameters: append(json.RawMessage(nil), schema...)}}, nil
}

func HostMessages(messages []Message) []ProtocolMessage {
	out := make([]ProtocolMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, ProtocolMessage{Role: msg.Role, Content: msg.Content})
	}
	return out
}
func HostAssistantCalls(calls []ToolCall) ProtocolMessage {
	return ProtocolMessage{Role: "assistant", ToolCalls: append([]ToolCall(nil), calls...)}
}
func HostToolResult(id, content string) ProtocolMessage {
	return ProtocolMessage{Role: "tool", ToolCallID: id, Content: content}
}

type decisionRequest struct {
	Model      string            `json:"model"`
	Messages   []ProtocolMessage `json:"messages"`
	Tools      []ToolDefinition  `json:"tools"`
	ToolChoice string            `json:"tool_choice"`
	Stream     bool              `json:"stream"`
}

// DecideTools is a bounded, nonstreaming structured decision request. A malformed
// response never becomes an action; the orchestrator can fall back to plain chat.
func (c *Client) DecideTools(ctx context.Context, messages []ProtocolMessage, tools []ToolDefinition) (ProtocolMessage, error) {
	if !c.Configured() || len(tools) == 0 || len(tools) > 8 {
		return ProtocolMessage{}, ErrToolDecision
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Type != "function" || seen[tool.Function.Name] {
			return ProtocolMessage{}, ErrToolDecision
		}
		if _, err := NewToolDefinition(tool.Function.Name, tool.Function.Description, tool.Function.Parameters); err != nil {
			return ProtocolMessage{}, err
		}
		seen[tool.Function.Name] = true
	}
	body, err := json.Marshal(decisionRequest{Model: c.Model, Messages: messages, Tools: tools, ToolChoice: "auto", Stream: false})
	if err != nil {
		return ProtocolMessage{}, ErrToolDecision
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ProtocolMessage{}, ErrToolDecision
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ProtocolMessage{}, ctx.Err()
		}
		return ProtocolMessage{}, ErrToolDecision
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		return ProtocolMessage{}, ErrToolDecision
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 || !utf8.Valid(raw) {
		return ProtocolMessage{}, ErrToolDecision
	}
	var out struct {
		Choices []struct {
			Message ProtocolMessage `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &out) != nil || len(out.Choices) != 1 {
		return ProtocolMessage{}, ErrToolDecision
	}
	message := out.Choices[0].Message
	if err := ValidateToolDecision(message); err != nil {
		return ProtocolMessage{}, err
	}
	return message, nil
}

// ValidateToolDecision also protects callers using a Gateway implementation
// other than Client. Argument Schema/size failures belong to the individual
// attempt, not the whole decision; the HTTP response itself is bounded to 1 MiB.
func ValidateToolDecision(message ProtocolMessage) error {
	if message.Role != "assistant" || message.ToolCallID != "" || len(message.ToolCalls) > MaxDecisionCalls {
		return ErrToolDecision
	}
	ids := map[string]bool{}
	for _, call := range message.ToolCalls {
		if !validCallID(call.ID) || ids[call.ID] || call.Type != "function" || !mcp.ValidToolName(call.Function.Name) || len(call.Function.Arguments) > 1024*1024 {
			return ErrToolDecision
		}
		ids[call.ID] = true
	}
	if len(message.ToolCalls) == 0 && strings.TrimSpace(message.Content) == "" {
		return ErrToolDecision
	}
	return nil
}

// CompleteToolsDisabledStream requests new answer deltas, never the buffered
// decision content. A tool-enabled final response must genuinely be SSE; it
// cannot silently turn a JSON answer into simulated progress.
func (c *Client) CompleteToolsDisabledStream(ctx context.Context, messages []ProtocolMessage, tools []ToolDefinition, onDelta DeltaFunc) error {
	if len(tools) == 0 || len(tools) > 8 {
		return ErrToolDecision
	}
	for _, tool := range tools {
		if tool.Type != "function" {
			return ErrToolDecision
		}
		if _, err := NewToolDefinition(tool.Function.Name, tool.Function.Description, tool.Function.Parameters); err != nil {
			return err
		}
	}
	return c.completeStream(ctx, decisionRequest{Model: c.Model, Messages: messages, Tools: tools, ToolChoice: "none", Stream: true}, onDelta, true)
}

func validCallID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
