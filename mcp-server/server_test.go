package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/mcp"
)

const serviceTestToken = "test-only-server-token"

func testService(t *testing.T) *httptest.Server {
	t.Helper()
	h, err := newHandler(serviceTestToken)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func postRPC(t *testing.T, url, auth, body string) (int, response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out response
	if len(raw) > 0 {
		if !json.Valid(raw) || resp.StatusCode == http.StatusOK && json.Unmarshal(raw, &out) != nil {
			t.Fatalf("invalid response JSON: %s", raw)
		}
	}
	return resp.StatusCode, out, string(raw)
}

func TestProtocolHealthInitializeNotificationAndTools(t *testing.T) {
	srv := testService(t)
	if err := checkHealth(context.Background(), srv.URL+"/health"); err != nil {
		t.Fatal(err)
	}
	bindings, _ := json.Marshal([]mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: srv.URL, TokenEnv: "MCP_TEST_TOKEN"}})
	registry, err := mcp.NewRegistry(string(bindings), func(string) string { return serviceTestToken })
	if err != nil {
		t.Fatal(err)
	}
	target, err := registry.Resolve(1, "campus-tools", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mcp.NewClient(target, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := client.Discover(context.Background())
	if err != nil || len(tools) != 2 {
		t.Fatalf("real HTTP discovery failed: %v; %v", tools, err)
	}
	for _, tool := range tools {
		result, err := client.Call(context.Background(), tool, json.RawMessage(`{"query":"勾股定理与长度单位"}`))
		if err != nil || result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" || result.Content[0].Text == "" {
			t.Fatalf("real HTTP call failed: %+v; %v", result, err)
		}
	}
	code, _, raw := postRPC(t, srv.URL, "Bearer "+serviceTestToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if code != http.StatusAccepted || raw != "" {
		t.Fatalf("notification returned fake response: %d %q", code, raw)
	}
}

func TestJSONRPCStandardErrors(t *testing.T) {
	srv := testService(t)
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"parse", `{`, -32700},
		{"version", `{"jsonrpc":"1.0","id":"id-1","method":"tools/list"}`, -32600},
		{"missing-method", `{"jsonrpc":"2.0","id":"id-1"}`, -32600},
		{"bad-id", `{"jsonrpc":"2.0","id":{},"method":"tools/list"}`, -32600},
		{"non-object", `[]`, -32600},
		{"unknown-method", `{"jsonrpc":"2.0","id":"id-1","method":"unknown"}`, -32601},
		{"bad-list-params", `{"jsonrpc":"2.0","id":"id-1","method":"tools/list","params":{"cursor":"unsupported"}}`, -32602},
		{"bad-list-params-type", `{"jsonrpc":"2.0","id":"id-1","method":"tools/list","params":null}`, -32602},
		{"bad-initialize", `{"jsonrpc":"2.0","id":"id-1","method":"initialize","params":{"protocolVersion":"wrong","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, -32602},
		{"missing-query", `{"jsonrpc":"2.0","id":"id-1","method":"tools/call","params":{"name":"formula_hint","arguments":{}}}`, -32602},
		{"unknown-tool", `{"jsonrpc":"2.0","id":"id-1","method":"tools/call","params":{"name":"unknown","arguments":{"query":"test"}}}`, -32602},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, out, raw := postRPC(t, srv.URL, "Bearer "+serviceTestToken, tc.body)
			if status != 200 || out.JSONRPC != "2.0" || out.Error == nil || out.Error.Code != tc.code || out.Result != nil {
				t.Fatalf("wrong protocol error: %d %s", status, raw)
			}
			if strings.Contains(raw, serviceTestToken) {
				t.Fatal("protocol error leaked token")
			}
		})
	}
}

func TestResponseIDs(t *testing.T) {
	srv := testService(t)
	for _, id := range []string{`42`, `"request-identifier"`, `null`} {
		_, out, raw := postRPC(t, srv.URL, "Bearer "+serviceTestToken, `{"jsonrpc":"2.0","id":`+id+`,"method":"tools/list"}`)
		if out.Error != nil || string(out.ID) != id {
			t.Fatalf("id not correlated: %s", raw)
		}
	}
}

func TestHealthFailureAndMethods(t *testing.T) {
	srv := testService(t)
	if err := checkHealth(context.Background(), srv.URL+"/missing"); err == nil {
		t.Fatal("unhealthy endpoint accepted")
	}
	for _, path := range []string{"/mcp", "/missing"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Fatal("unsupported method/path accepted")
		}
	}
}
