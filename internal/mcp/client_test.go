package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var querySchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500}},"required":["query"],"additionalProperties":false}`)

func clientFor(t *testing.T, url string, timeout time.Duration) *Client {
	t.Helper()
	r := testRegistry(t, url, func(string) string { return "test-only-token" })
	target, err := r.Resolve(1, "campus-tools", url)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(target, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func respondRPC(w http.ResponseWriter, id string, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func TestClientDiscoverAndCall(t *testing.T) {
	methods := []string{}
	ids := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/mcp" || r.Header.Get("Authorization") != "Bearer test-only-token" || r.Header.Get("MCP-Protocol-Version") != ProtocolVersion {
			t.Error("incorrect MCP HTTP request")
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      string          `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JSONRPC != "2.0" {
			t.Error("invalid JSON-RPC request")
		}
		methods = append(methods, req.Method)
		if req.Method == "notifications/initialized" {
			if req.ID != "" {
				t.Error("notification has an id")
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.ID == "" || ids[req.ID] {
			t.Error("missing or repeated request id")
		}
		ids[req.ID] = true
		switch req.Method {
		case "initialize":
			respondRPC(w, req.ID, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}})
		case "tools/list":
			respondRPC(w, req.ID, map[string]any{"tools": []Tool{{"formula_hint", "A teaching hint", querySchema}}})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &params) != nil || params.Name != "formula_hint" || params.Arguments["query"] != "model-written-query" {
				t.Error("model arguments not preserved")
			}
			respondRPC(w, req.ID, CallResult{Content: []Content{{"text", "Check the conditions first."}}})
		}
	}))
	defer srv.Close()
	c := clientFor(t, srv.URL, time.Second)
	tools, err := c.Discover(context.Background())
	if err != nil || len(tools) != 1 {
		t.Fatalf("discover: %v, %v", tools, err)
	}
	result, err := c.Call(context.Background(), tools[0], json.RawMessage(`{"query":"model-written-query"}`))
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("call: %+v, %v", result, err)
	}
	if !reflect.DeepEqual(methods, []string{"initialize", "notifications/initialized", "tools/list", "tools/call"}) {
		t.Fatalf("wrong lifecycle: %v", methods)
	}
}

func TestClientRejectsBadResponsesWithoutEcho(t *testing.T) {
	for _, tc := range []struct {
		name              string
		code              int
		contentType, body string
		want              error
	}{
		{"auth", 401, "application/json", `test-only-token`, ErrUnauthorized},
		{"http-error", 500, "text/plain", `Bearer test-only-token`, ErrTransport},
		{"invalid-json", 200, "application/json", `{test-only-token`, ErrProtocol},
		{"wrong-id", 200, "application/json", `{"jsonrpc":"2.0","id":"wrong","result":{"tools":[]}}`, ErrProtocol},
		{"numeric-id", 200, "application/json", `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`, ErrProtocol},
		{"wrong-rpc-version", 200, "application/json", `{"jsonrpc":"1.0","id":"mcp-1","result":{"tools":[]}}`, ErrProtocol},
		{"remote-error", 200, "application/json", `{"jsonrpc":"2.0","id":"mcp-1","error":{"code":-32602,"message":"test-only-token"}}`, ErrProtocol},
		{"missing-tools", 200, "application/json", `{"jsonrpc":"2.0","id":"mcp-1","result":{}}`, ErrProtocol},
		{"partial-list", 200, "application/json", `{"jsonrpc":"2.0","id":"mcp-1","result":{"tools":[],"nextCursor":"more"}}`, ErrProtocol},
		{"wrong-content-type", 200, "text/html", `{"jsonrpc":"2.0","id":"mcp-1","result":{"tools":[]}}`, ErrProtocol},
		{"oversize", 200, "application/json", strings.Repeat("x", MaxResponseBytes+1), ErrResponseSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := clientFor(t, srv.URL, time.Second).ListTools(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v; want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "test-only-token") {
				t.Fatal("upstream secret leaked")
			}
		})
	}
}

func TestClientInitializeVersionAndCapabilities(t *testing.T) {
	for _, tc := range []struct {
		result any
		want   error
	}{
		{map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}}, ErrVersion},
		{map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}}, ErrProtocol},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { respondRPC(w, "mcp-1", tc.result) }))
		if err := clientFor(t, srv.URL, time.Second).Initialize(context.Background()); !errors.Is(err, tc.want) {
			t.Fatalf("got %v; want %v", err, tc.want)
		}
		srv.Close()
	}
}

func TestClientNoRedirect(t *testing.T) {
	var outbound atomic.Int32
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { outbound.Add(1) }))
	defer probe.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, probe.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	_, err := clientFor(t, srv.URL, time.Second).ListTools(context.Background())
	if !errors.Is(err, ErrTransport) || outbound.Load() != 0 {
		t.Fatalf("redirect followed: %d; %v", outbound.Load(), err)
	}
}

func TestClientTimeoutAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()
	if _, err := clientFor(t, srv.URL, 10*time.Millisecond).ListTools(context.Background()); !errors.Is(err, ErrTransport) {
		t.Fatalf("missing request timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := clientFor(t, srv.URL, time.Second).ListTools(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel not preserved: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := clientFor(t, srv.URL, time.Second).ListTools(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline not preserved: %v", err)
	}
	if _, err := NewClient(Target{}, time.Second); !errors.Is(err, ErrCredentials) {
		t.Fatal("zero target allowed")
	}
}
