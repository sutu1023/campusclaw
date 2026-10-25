package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"campusclaw/internal/mcp"
)

func remoteMCPTools() []mcp.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500}},"required":["query"],"additionalProperties":false}`)
	return []mcp.Tool{{Name: "formula_hint", Description: "Full formula description", InputSchema: schema}, {Name: "unit_convert", Description: "Full unit description", InputSchema: schema}}
}

func newMCPRemote(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	mode := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-only-remote-token" {
			t.Error("wrong server-side bearer")
		}
		current := mode.Load()
		if current == 1 {
			w.WriteHeader(401)
			_, _ = w.Write([]byte("test-only-remote-token"))
			return
		}
		if current == 2 {
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
			return
		}
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			version := mcp.ProtocolVersion
			if current == 3 {
				version = "incompatible"
			}
			result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			tools := remoteMCPTools()
			switch current {
			case 4:
				tools = append(tools, tools[0])
			case 5:
				tools = tools[:1]
			case 6:
				tools[0].Description = "Changed full formula description"
			case 7:
				tools[0].InputSchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":2}},"required":["query"],"additionalProperties":false}`)
			case 8:
				tools[0].Description = "test-only-remote-token"
			}
			result = map[string]any{"tools": tools}
		default:
			t.Error("unexpected MCP method")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv, mode
}

func registerMCPHTTP(t *testing.T, env *testEnv, teacher, origin string) mcpServerDTO {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": "campus-tools", "base_url": origin})
	resp, raw := env.do(t, "POST", "/api/mcp/servers", teacher, body, "application/json")
	var server mcpServerDTO
	if resp.StatusCode != 201 || json.Unmarshal(raw, &server) != nil {
		t.Fatalf("register: %d %s", resp.StatusCode, raw)
	}
	return server
}

func TestMCPConnectionFailuresClearCatalogAndRemoveTools(t *testing.T) {
	env := setupEnv(t)
	remote, mode := newMCPRemote(t)
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"}}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token"})
	env.api.MCP.RequestTimeout = 20 * time.Millisecond
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, remote.URL)
	path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
	for _, failureMode := range []int64{1, 2, 3, 4, 8} {
		mode.Store(0)
		resp, raw := env.do(t, "POST", path+"/connect", teacher, nil, "")
		if resp.StatusCode != 200 {
			t.Fatalf("initial connect: %d %s", resp.StatusCode, raw)
		}
		mode.Store(failureMode)
		resp, raw = env.do(t, "POST", path+"/connect", teacher, nil, "")
		if resp.StatusCode != 503 || strings.Contains(string(raw), "test-only-remote-token") {
			t.Fatalf("failure not local/deidentified: mode %d status %d", failureMode, resp.StatusCode)
		}
		resp, raw = env.do(t, "GET", path+"/tools", teacher, nil, "")
		var directory mcpToolsResponse
		if resp.StatusCode != 200 || json.Unmarshal(raw, &directory) != nil || len(directory.Tools) != 0 || directory.Server.State != "disconnected" {
			t.Fatal("failed discovery retained old/partial tools")
		}
		resp, _ = env.do(t, "GET", "/api/me", teacher, nil, "")
		if resp.StatusCode != 200 {
			t.Fatal("remote 401 invalidated application login")
		}
	}
	for _, current := range []int64{0, 0, 5} {
		mode.Store(current)
		resp, raw := env.do(t, "POST", path+"/connect", teacher, nil, "")
		if resp.StatusCode != 200 {
			t.Fatalf("reconnect: %d %s", resp.StatusCode, raw)
		}
	}
	resp, raw := env.do(t, "GET", path+"/tools", teacher, nil, "")
	var directory mcpToolsResponse
	if resp.StatusCode != 200 || json.Unmarshal(raw, &directory) != nil || len(directory.Tools) != 1 || directory.Tools[0].Name != "formula_hint" || directory.Tools[0].Description != "Full formula description" {
		t.Fatal("directory did not replace removed tool / full definition")
	}
}

func TestMCPManagementConnectsRealTeachingService(t *testing.T) {
	origin, token := os.Getenv("TEST_MCP_URL"), os.Getenv("TEST_MCP_TOKEN")
	if origin == "" || token == "" {
		t.Skip("TEST_MCP_URL / TEST_MCP_TOKEN not set; skipping real MCP service test")
	}
	env := setupEnv(t)
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: origin, TokenEnv: "MCP_LIVE_TOKEN"}}, map[string]string{"MCP_LIVE_TOKEN": token})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, origin)
	path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
	resp, raw := env.do(t, "POST", path+"/connect", teacher, nil, "")
	if resp.StatusCode != 200 || strings.Contains(string(raw), token) {
		t.Fatal("real MCP connect failed or leaked token")
	}
	resp, raw = env.do(t, "GET", path+"/tools", teacher, nil, "")
	var directory mcpToolsResponse
	if resp.StatusCode != 200 || strings.Contains(string(raw), token) || json.Unmarshal(raw, &directory) != nil || directory.Server.State != "connected" || len(directory.Tools) != 2 {
		t.Fatal("real MCP tools unavailable")
	}
}
