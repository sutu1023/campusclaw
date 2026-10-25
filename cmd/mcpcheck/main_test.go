package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"campusclaw/internal/mcp"
)

func setEnvironment(t *testing.T, origin string) {
	t.Helper()
	t.Setenv("DB_DSN", "test-only-unused-dsn")
	t.Setenv("TOKEN_SECRET", "test-only-secret")
	t.Setenv("MCP_AUTH_TOKEN", "test-only-token")
	bindings, _ := json.Marshal([]mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: origin, TokenEnv: "MCP_AUTH_TOKEN"}})
	t.Setenv("MCP_CREDENTIAL_BINDINGS", string(bindings))
	for _, key := range []string{"MCP_MAX_ROUNDS", "MCP_MAX_CALLS_PER_TURN", "MCP_MAX_CALLS_PER_CONVERSATION", "MCP_TURN_TIMEOUT", "MCP_REQUEST_TIMEOUT"} {
		t.Setenv(key, "")
	}
}

func TestContainerProbeLifecycleAndSafeOutput(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/health" {
			if r.Header.Get("Authorization") != "" {
				t.Error("credential sent to public health endpoint")
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-only-token" {
			t.Error("missing server-side bearer")
		}
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": mcp.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []mcp.Tool{{Name: "formula_hint", Description: "Formula hints", InputSchema: json.RawMessage(`{"type":"object"}`)}, {Name: "unit_convert", Description: "Unit hints", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
		case "tools/call":
			calls.Add(1)
			result = mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: "test-only-token must never be printed by the probe"}}}
		default:
			t.Error("unexpected method")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()
	setEnvironment(t, srv.URL)
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-base-url", srv.URL}, &output); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !strings.Contains(output.String(), `"status":"ok"`) || strings.Contains(output.String(), "test-only-token") || strings.Contains(output.String(), srv.URL) {
		t.Fatalf("unsafe or incomplete probe output: %s", output.String())
	}
}

func TestProbeUnapprovedTargetHasNoOutbound(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	setEnvironment(t, srv.URL)
	if err := run(context.Background(), []string{"-class-id", "2", "-base-url", srv.URL}, &bytes.Buffer{}); err == nil || calls.Load() != 0 {
		t.Fatal("unapproved probe contacted server")
	}
}

func TestProbeWrongTokenVerification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	setEnvironment(t, srv.URL)
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-base-url", srv.URL, "-expect-unauthorized"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"authentication":"unauthorized"`) {
		t.Fatal("missing authentication evidence")
	}
	if err := run(context.Background(), []string{"-unknown-argument"}, &output); err == nil {
		t.Fatal("invalid argument accepted")
	}
}
