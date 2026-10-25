package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"campusclaw/docs"
)

func TestGeneratedMCPContractSchemasAndFailures(t *testing.T) {
	raw := docs.SwaggerInfo.ReadDoc()
	var doc struct {
		Paths map[string]map[string]struct {
			Summary     string                `json:"summary"`
			Description string                `json:"description"`
			Security    []map[string][]string `json:"security"`
			Responses   map[string]any        `json:"responses"`
		} `json:"paths"`
		Definitions map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"definitions"`
		SecurityDefinitions map[string]struct {
			Description string `json:"description"`
		} `json:"securityDefinitions"`
	}
	if json.Unmarshal([]byte(raw), &doc) != nil {
		t.Fatal("generated OpenAPI is not parseable")
	}
	expected := map[string]map[string][]string{
		"/api/mcp/servers":                 {"get": {"200", "401", "403"}, "post": {"201", "400", "401", "403"}},
		"/api/mcp/servers/{id}":            {"get": {"200", "401", "403", "404"}, "put": {"200", "400", "401", "403", "404"}, "delete": {"204", "401", "403", "404"}},
		"/api/mcp/servers/{id}/connect":    {"post": {"200", "400", "401", "403", "404", "409", "503"}},
		"/api/mcp/servers/{id}/tools":      {"get": {"200", "401", "403", "404", "503"}},
		"/api/mcp/servers/{id}/approve":    {"post": {"200", "400", "401", "403", "404", "409", "503"}},
		"/api/mcp/servers/{id}/disconnect": {"post": {"204", "401", "403", "404"}},
		"/api/mcp/audit":                   {"get": {"200", "400", "401", "403", "404"}},
	}
	for path, methods := range expected {
		for method, statuses := range methods {
			op, ok := doc.Paths[path][method]
			if !ok {
				t.Fatalf("missing documented method %s %s", method, path)
			}
			if !strings.Contains(op.Summary, "仅教师") || len(op.Security) != 1 || op.Security[0]["BearerAuth"] == nil {
				t.Fatalf("teacher/application Bearer missing at %s %s", method, path)
			}
			for _, status := range statuses {
				if _, ok := op.Responses[status]; !ok {
					t.Fatalf("missing %s at %s %s", status, method, path)
				}
			}
		}
	}
	server := doc.Definitions["httpapi.mcpServerDTO"].Properties
	if server["has_token"] == nil || server["state"] == nil {
		t.Fatal("safe server configuration fields missing")
	}
	for _, private := range []string{"token", "credential_ref", "token_env", "generation", "api_key"} {
		if server[private] != nil {
			t.Fatal("private persistence/credential field documented as public")
		}
	}
	var schema struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(doc.Definitions["mcp.Tool"].Properties["inputSchema"], &schema)
	if schema.Type != "object" {
		t.Fatal("tool inputSchema incorrectly exposed as raw byte array")
	}
	var summaries struct {
		Type  string `json:"type"`
		Items struct {
			Ref string `json:"$ref"`
		} `json:"items"`
	}
	_ = json.Unmarshal(doc.Definitions["httpapi.messageView"].Properties["tool_calls"], &summaries)
	if summaries.Type != "array" || summaries.Items.Ref != "#/definitions/toolrun.CallSummary" {
		t.Fatal("replay summary schema missing")
	}
	var status struct {
		Enum []string `json:"enum"`
	}
	_ = json.Unmarshal(doc.Definitions["toolrun.CallSummary"].Properties["status"], &status)
	if strings.Join(status.Enum, ",") != "ok,error,denied" {
		t.Fatal("tool outcome enum inaccurate")
	}
	if !strings.Contains(doc.SecurityDefinitions["BearerAuth"].Description, "不是 MCP Bearer") {
		t.Fatal("Swagger confuses application and service credentials")
	}
	stream := doc.Paths["/api/conversations/{id}/messages"]["post"].Description
	for _, term := range []string{"call_id", "tool_calls", "status=ok", "首个 delta", "部分生成"} {
		if !strings.Contains(stream, term) {
			t.Fatal("incremental SSE/partial-answer contract missing")
		}
	}
	if _, ok := doc.Paths["/api/skills"]["post"].Responses["404"]; !ok {
		t.Fatal("skill MCP ownership failure missing")
	}
	for _, key := range []string{"TEST_MCP_TOKEN", "MCP_AUTH_TOKEN", "CHAT_API_KEY"} {
		if value := os.Getenv(key); value != "" && strings.Contains(raw, value) {
			t.Fatal("configured credential appeared in generated OpenAPI")
		}
	}
}
