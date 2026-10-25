package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDocumentedMCPMethodsReachActualTeacherMiddleware(t *testing.T) {
	env := setupEnv(t)
	resp, raw := env.do(t, http.MethodGet, "/openapi.json", "", nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("document not available")
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(raw, &document) != nil {
		t.Fatal("invalid document")
	}
	student := env.login(t, "student_a1", "CampusClaw#2026")
	methods := 0
	for path, operations := range document.Paths {
		if !strings.HasPrefix(path, "/api/mcp/") {
			continue
		}
		for method := range operations {
			method = strings.ToUpper(method)
			if method != "GET" && method != "POST" && method != "PUT" && method != "DELETE" {
				continue
			}
			methods++
			target := strings.ReplaceAll(path, "{id}", "999999")
			resp, _ := env.do(t, method, target, "", []byte(`{}`), "application/json")
			if resp.StatusCode != 401 {
				t.Fatalf("documented route did not reach auth: %s %s: %d", method, path, resp.StatusCode)
			}
			resp, _ = env.do(t, method, target, student, []byte(`{}`), "application/json")
			if resp.StatusCode != 403 {
				t.Fatalf("documented route did not reach teacher gate: %s %s: %d", method, path, resp.StatusCode)
			}
		}
	}
	if methods != 10 {
		t.Fatalf("MCP method coverage changed unexpectedly: %d", methods)
	}
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	resp, _ = env.do(t, http.MethodGet, "/api/mcp/servers", teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("Swagger mount shadows MCP business route")
	}
}
