package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestOpenAPIDocumentAndSwaggerUI(t *testing.T) {
	env := setupEnv(t)

	// The document is public and parseable.
	resp, data := env.do(t, http.MethodGet, "/openapi.json", "", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("openapi.json status %d body=%s", resp.StatusCode, data)
	}
	var doc struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths map[string]map[string]any `json:"paths"`
		SecurityDefinitions map[string]any `json:"securityDefinitions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if doc.Info.Title == "" {
		t.Fatal("openapi.json missing info.title")
	}
	for _, path := range []string{
		"/api/login", "/api/materials", "/api/agents", "/api/skills",
		"/api/conversations", "/api/conversations/{id}", "/api/conversations/{id}/messages",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("openapi.json missing path %s", path)
		}
	}
	if _, ok := doc.SecurityDefinitions["BearerAuth"]; !ok {
		t.Fatal("openapi.json missing BearerAuth security definition")
	}
	if _, ok := doc.Paths["/api/agents"]["post"]; !ok {
		t.Fatal("agents POST not documented")
	}

	// Swagger UI is served at /swagger/index.html.
	resp, body := env.do(t, http.MethodGet, "/swagger/index.html", "", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("swagger ui status %d", resp.StatusCode)
	}
	if !strings.Contains(strings.ToLower(string(body)), "swagger") {
		t.Fatalf("swagger ui body does not look like Swagger UI: %s", string(body[:min(len(body), 120)]))
	}

	// Mounting the docs must not shadow health or the auth flow.
	resp, _ = env.do(t, http.MethodGet, "/health", "", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status %d", resp.StatusCode)
	}
	token := env.login(t, "teacher_a", "CampusClaw#2026")
	resp, _ = env.do(t, http.MethodGet, "/api/me", token, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status %d after mounting docs", resp.StatusCode)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
