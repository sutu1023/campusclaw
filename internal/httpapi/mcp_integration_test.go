package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/config"
	"campusclaw/internal/mcp"
)

func configureTestMCP(t *testing.T, env *testEnv, bindings []mcp.Binding, secrets map[string]string) {
	t.Helper()
	raw, _ := json.Marshal(bindings)
	registry, err := mcp.NewRegistry(string(raw), func(key string) string { return secrets[key] })
	if err != nil {
		t.Fatal(err)
	}
	env.api.MCP = config.MCPConfig{Registry: registry, MaxRounds: 3, MaxCallsPerTurn: 8, MaxCallsPerConversation: 40, TurnTimeout: 20 * time.Second, RequestTimeout: time.Second}
}

func TestMCPManagementCRUDPermissionsAndNoSecrets(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()
	configureTestMCP(t, env, []mcp.Binding{
		{ClassID: 1, ServerName: "campus-tools", BaseURL: "http://mcp-demo:8090", TokenEnv: "MCP_AUTH_TOKEN"},
		{ClassID: 1, ServerName: "no-token", BaseURL: "http://mcp-demo:8090", TokenEnv: "MCP_MISSING_TOKEN"},
		{ClassID: 1, ServerName: "campus-tools", BaseURL: "http://replacement:8090", TokenEnv: "MCP_REPLACEMENT_TOKEN"},
	}, map[string]string{"MCP_AUTH_TOKEN": "test-only-server-secret", "MCP_REPLACEMENT_TOKEN": "test-only-replacement-secret"})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	student := env.login(t, "student_a1", "CampusClaw#2026")
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/mcp/servers"}, {"POST", "/api/mcp/servers"}, {"GET", "/api/mcp/servers/1"}, {"PUT", "/api/mcp/servers/1"}, {"DELETE", "/api/mcp/servers/1"},
	} {
		for _, token := range []string{"", student} {
			resp, _ := env.do(t, tc.method, tc.path, token, []byte(`{}`), "application/json")
			want := 401
			if token != "" {
				want = 403
			}
			if resp.StatusCode != want {
				t.Fatalf("%s %s: got %d want %d", tc.method, tc.path, resp.StatusCode, want)
			}
		}
	}
	if servers, err := env.store.ListMCPServers(ctx, 1); err != nil || len(servers) != 0 {
		t.Fatal("forbidden requests created data")
	}
	checkSecrets := func(raw []byte) {
		t.Helper()
		for _, secret := range []string{"test-only-server-secret", "test-only-replacement-secret", "browser-raw-secret", "credential_ref", "token_env", "MCP_AUTH_TOKEN"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("MCP response leaked private credential information")
			}
		}
	}
	body := []byte(`{"class_id":2,"name":"campus-tools","base_url":"http://mcp-demo:8090/","token":"browser-raw-secret","token_env":"DB_DSN"}`)
	resp, raw := env.do(t, "POST", "/api/mcp/servers", teacher, body, "application/json")
	checkSecrets(raw)
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, raw)
	}
	var created mcpServerDTO
	if json.Unmarshal(raw, &created) != nil || created.ClassID != 1 || !created.HasToken || created.State != "disconnected" {
		t.Fatalf("unsafe create DTO: %s", raw)
	}
	path := fmt.Sprintf("/api/mcp/servers/%d", created.ID)
	for _, method := range []string{"GET", "PUT"} {
		resp, raw = env.do(t, method, path, teacher, []byte(`{"base_url":"http://replacement:8090"}`), "application/json")
		checkSecrets(raw)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", method, resp.StatusCode, raw)
		}
	}
	resp, raw = env.do(t, "PUT", path, teacher, []byte(`{"name":"renamed","base_url":"http://replacement:8090"}`), "application/json")
	checkSecrets(raw)
	if resp.StatusCode != 400 {
		t.Fatal("server renamed")
	}
	resp, raw = env.do(t, "POST", "/api/mcp/servers", teacher, body, "application/json")
	checkSecrets(raw)
	if resp.StatusCode != 400 {
		t.Fatal("duplicate accepted")
	}
	resp, raw = env.do(t, "POST", "/api/mcp/servers", teacher, []byte(`{"name":"no-token","base_url":"http://mcp-demo:8090"}`), "application/json")
	checkSecrets(raw)
	var missing mcpServerDTO
	_ = json.Unmarshal(raw, &missing)
	if resp.StatusCode != 201 || missing.HasToken {
		t.Fatal("missing credential blocked registration or reported configured")
	}
	resp, raw = env.do(t, "GET", "/api/mcp/servers", teacher, nil, "")
	checkSecrets(raw)
	if resp.StatusCode != 200 {
		t.Fatal("list failed")
	}
	for _, bad := range []string{`{"name":"campus-tools","base_url":"http://unapproved:8090"}`, `{"name":"campus-tools","base_url":"http://user:secret@mcp-demo:8090"}`} {
		resp, raw = env.do(t, "POST", "/api/mcp/servers", teacher, []byte(bad), "application/json")
		checkSecrets(raw)
		if resp.StatusCode != 400 {
			t.Fatal("unapproved address accepted")
		}
	}
	foreign, err := env.store.CreateMCPServer(ctx, 2, "foreign-tools", "http://foreign:8090", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		var previous string
		for _, id := range []int64{foreign.ID, 999999} {
			resp, raw = env.do(t, method, fmt.Sprintf("/api/mcp/servers/%d", id), teacher, []byte(`{"bad":"input"}`), "application/json")
			checkSecrets(raw)
			if resp.StatusCode != 404 || previous != "" && string(raw) != previous {
				t.Fatal("foreign/missing distinction leaked")
			}
			previous = string(raw)
		}
	}
	resp, raw = env.do(t, "DELETE", path, teacher, nil, "")
	checkSecrets(raw)
	if resp.StatusCode != 204 {
		t.Fatal("delete failed")
	}
	resp, _ = env.do(t, "GET", path, teacher, nil, "")
	if resp.StatusCode != 404 {
		t.Fatal("deleted server still visible")
	}
	resp, _ = env.do(t, "POST", "/api/mcp/servers", teacher, body, "application/json")
	if resp.StatusCode != 400 {
		t.Fatal("deleted name reused")
	}
}
