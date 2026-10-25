package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"campusclaw/internal/mcp"
)

func TestMCPDisconnectAddressChangeAndDeleteRevoke(t *testing.T) {
	env := setupEnv(t)
	remote, _ := newMCPRemote(t)
	var calls atomic.Int64
	replacement := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer replacement.Close()
	configureTestMCP(t, env, []mcp.Binding{
		{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"},
		{ClassID: 1, ServerName: "campus-tools", BaseURL: replacement.URL, TokenEnv: "MCP_REPLACEMENT_TOKEN"},
	}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token", "MCP_REPLACEMENT_TOKEN": "test-only-replacement-token"})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, remote.URL)
	path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
	resp, _ := env.do(t, "POST", path+"/connect", teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("connect failed")
	}
	before, err := env.store.GetMCPServer(context.Background(), 1, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		resp, _ = env.do(t, "POST", path+"/disconnect", teacher, nil, "")
		if resp.StatusCode != 204 {
			t.Fatal("disconnect not idempotent")
		}
	}
	resp, raw := env.do(t, "GET", path+"/tools", teacher, nil, "")
	var directory mcpToolsResponse
	_ = json.Unmarshal(raw, &directory)
	if resp.StatusCode != 200 || len(directory.Tools) != 0 || len(directory.PendingTools) != 0 || directory.Server.ApprovedHash != before.ApprovedHash {
		t.Fatal("disconnect retained live tools or erased history")
	}
	resp, _ = env.do(t, "POST", path+"/connect", teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("unchanged reconnect failed")
	}
	body, _ := json.Marshal(map[string]string{"base_url": replacement.URL})
	resp, raw = env.do(t, "PUT", path, teacher, body, "application/json")
	var updated mcpServerDTO
	_ = json.Unmarshal(raw, &updated)
	if resp.StatusCode != 200 || updated.State != "disconnected" || updated.ApprovedHash != before.ApprovedHash || calls.Load() != 0 {
		t.Fatal("address update auto-connected or retained live permission")
	}
	resp, _ = env.do(t, "DELETE", path, teacher, nil, "")
	if resp.StatusCode != 204 {
		t.Fatal("delete failed")
	}
	for _, suffix := range []string{"/connect", "/disconnect"} {
		resp, _ = env.do(t, "POST", path+suffix, teacher, nil, "")
		if resp.StatusCode != 404 {
			t.Fatal("deleted server remained actionable")
		}
	}
}

func TestMCPCredentialBindingChangeRevokesWithoutNetwork(t *testing.T) {
	env := setupEnv(t)
	remote, _ := newMCPRemote(t)
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"}}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token"})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, remote.URL)
	path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
	resp, _ := env.do(t, "POST", path+"/connect", teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("initial connection failed")
	}
	before, err := env.store.GetMCPServer(context.Background(), 1, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REBOUND_TOKEN"}}, map[string]string{"MCP_REBOUND_TOKEN": "test-only-remote-token"})
	body, _ := json.Marshal(map[string]string{"base_url": remote.URL})
	resp, _ = env.do(t, "PUT", path, teacher, body, "application/json")
	after, err := env.store.GetMCPServer(context.Background(), 1, server.ID)
	if err != nil || resp.StatusCode != 200 || after.State != "disconnected" || after.Generation <= before.Generation || after.CredentialRef == before.CredentialRef || after.ApprovedHash != before.ApprovedHash {
		t.Fatal("binding update retained live authority or erased history")
	}
	directory, err := env.store.ReadMCPDirectory(context.Background(), 1, server.ID)
	if err != nil || len(directory.Tools) != 0 {
		t.Fatal("binding update retained tools")
	}
}

func TestMCPLateHTTPConnectDoesNotUndoDisconnect(t *testing.T) {
	env := setupEnv(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "initialize":
			if once.CompareAndSwap(false, true) {
				close(started)
				<-release
			}
			result = map[string]any{"protocolVersion": mcp.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			w.WriteHeader(202)
			return
		case "tools/list":
			result = map[string]any{"tools": remoteMCPTools()}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer remote.Close()
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"}}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token"})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, remote.URL)
	path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
	result := make(chan int, 1)
	go func() { resp, _ := env.do(t, "POST", path+"/connect", teacher, nil, ""); result <- resp.StatusCode }()
	<-started
	resp, _ := env.do(t, "POST", path+"/disconnect", teacher, nil, "")
	close(release)
	if resp.StatusCode != 204 || <-result != 409 {
		t.Fatal("late discovery revived disconnected server")
	}
	resp, raw := env.do(t, "GET", path+"/tools", teacher, nil, "")
	var directory mcpToolsResponse
	_ = json.Unmarshal(raw, &directory)
	if resp.StatusCode != 200 || directory.Server.State != "disconnected" || len(directory.Tools) != 0 {
		t.Fatal("late request repopulated directory")
	}
}
