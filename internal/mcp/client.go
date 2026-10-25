package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	ProtocolVersion  = "2025-03-26"
	MaxResponseBytes = 1 << 20
)

var (
	ErrTransport    = errors.New("MCP request failed")
	ErrUnauthorized = errors.New("MCP authentication failed")
	ErrProtocol     = errors.New("invalid MCP protocol response")
	ErrVersion      = errors.New("incompatible MCP protocol version")
	ErrResponseSize = errors.New("MCP response exceeds size limit")
)

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema" swaggertype:"object"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type CallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}

// Client only accepts a Target resolved by the deployment allowlist. It never
// returns network diagnostics or upstream bodies, which may contain secrets.
type Client struct {
	target Target
	http   *http.Client
	nextID atomic.Uint64
}

func NewClient(target Target, timeout time.Duration) (*Client, error) {
	if target.origin == "" || !ValidBearerToken(target.token) {
		return nil, ErrCredentials
	}
	if timeout <= 0 || timeout > time.Minute {
		return nil, errors.New("MCP request timeout must be positive and at most 1m")
	}
	return &Client{
		target: target,
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func (c *Client) request(ctx context.Context, method string, params any, result any) error {
	id := ""
	if result != nil {
		id = fmt.Sprintf("mcp-%d", c.nextID.Add(1))
	}
	body, err := json.Marshal(rpcRequest{"2.0", id, method, params})
	if err != nil {
		return ErrProtocol
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.target.origin+"/mcp", bytes.NewReader(body))
	if err != nil {
		return ErrTransport
	}
	req.Header.Set("Authorization", "Bearer "+c.target.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrTransport
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrTransport
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrTransport
	}
	if len(data) > MaxResponseBytes {
		return ErrResponseSize
	}
	if result == nil {
		if len(bytes.TrimSpace(data)) != 0 {
			return ErrProtocol
		}
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ErrProtocol
	}
	var envelope rpcResponse
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.JSONRPC != "2.0" {
		return ErrProtocol
	}
	var returnedID string
	if err := json.Unmarshal(envelope.ID, &returnedID); err != nil || returnedID != id {
		return ErrProtocol
	}
	if len(envelope.Error) != 0 {
		return ErrProtocol
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" || json.Unmarshal(envelope.Result, result) != nil {
		return ErrProtocol
	}
	return nil
}

// Initialize includes the required initialized notification. This profile uses
// stateless POST/JSON only; it does not implement server-push or legacy SSE.
func (c *Client) Initialize(ctx context.Context) error {
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools *struct{} `json:"tools"`
		} `json:"capabilities"`
	}
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "campusclaw", "version": "1"},
	}
	if err := c.request(ctx, "initialize", params, &result); err != nil {
		return err
	}
	if result.ProtocolVersion != ProtocolVersion {
		return ErrVersion
	}
	if result.Capabilities.Tools == nil {
		return ErrProtocol
	}
	return c.request(ctx, "notifications/initialized", nil, nil)
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var result struct {
		Tools      *[]Tool `json:"tools"`
		NextCursor string  `json:"nextCursor"`
	}
	if err := c.request(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	// A partial paginated list must never become the approved snapshot.
	if result.Tools == nil || result.NextCursor != "" {
		return nil, ErrProtocol
	}
	if err := ValidateTools(*result.Tools); err != nil {
		return nil, err
	}
	return *result.Tools, nil
}

func (c *Client) Discover(ctx context.Context) ([]Tool, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	return c.ListTools(ctx)
}

func (c *Client) Call(ctx context.Context, tool Tool, arguments json.RawMessage) (CallResult, error) {
	if !ValidToolName(tool.Name) {
		return CallResult{}, ErrDefinitions
	}
	validator, err := CompileInputSchema(tool.InputSchema)
	if err != nil {
		return CallResult{}, err
	}
	if err := validator.Validate(arguments); err != nil {
		return CallResult{}, err
	}
	var result CallResult
	err = c.request(ctx, "tools/call", struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{tool.Name, arguments}, &result)
	return result, err
}
