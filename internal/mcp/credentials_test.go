package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNormalizeOrigin(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"http://mcp-demo:8090/", "http://mcp-demo:8090"},
		{"HTTPS://EXAMPLE.COM:443", "https://example.com"},
		{"http://example.com:80", "http://example.com"},
		{"http://[::1]:8090/", "http://[::1]:8090"},
	} {
		got, err := NormalizeOrigin(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeOrigin(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, raw := range []string{
		"", "//mcp-demo", "ftp://mcp-demo", "http://user:secret@mcp-demo",
		"http://mcp-demo/mcp", "http://mcp-demo//", "http://mcp-demo/%2f",
		"http://mcp-demo?x=1", "http://mcp-demo?", "http://mcp-demo#", "http://mcp-demo#x",
		"http://mcp-demo:", "http://mcp-demo:0", "http://mcp-demo:65536",
		"http://mcp-demo:not-a-port", "http://", " http://mcp-demo", "http://mcp-demo\n",
		"http://mcp-demo\\evil", "http://[fe80::1%25eth0]",
	} {
		if _, err := NormalizeOrigin(raw); err == nil {
			t.Errorf("accepted unsafe origin %q", raw)
		}
	}
}

func testRegistry(t *testing.T, origin string, getenv func(string) string) *Registry {
	t.Helper()
	raw, _ := json.Marshal([]Binding{{1, "campus-tools", origin, "MCP_TEST_TOKEN"}})
	r, err := NewRegistry(string(raw), getenv)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegistryExactTargetAndNoUnapprovedOutbound(t *testing.T) {
	var requests, lookups atomic.Int32
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer probe.Close()
	r := testRegistry(t, "http://mcp-demo:8090", func(key string) string {
		lookups.Add(1)
		if key != "MCP_TEST_TOKEN" {
			t.Fatalf("non-allowlisted environment read: %q", key)
		}
		return "test-only-token"
	})
	for _, tc := range []struct {
		class int64
		name  string
		url   string
	}{
		{2, "campus-tools", "http://mcp-demo:8090"},
		{1, "other-server", "http://mcp-demo:8090"},
		{1, "campus-tools", probe.URL},
		{1, "campus-tools", "http://mcp-demo:8090/mcp"},
		{1, "campus-tools", "http://secret@mcp-demo:8090"},
	} {
		target, err := r.Resolve(tc.class, tc.name, tc.url)
		if !errors.Is(err, ErrUnapproved) || target != (Target{}) {
			t.Fatalf("unapproved target resolved: %v", err)
		}
	}
	if requests.Load() != 0 || lookups.Load() != 0 {
		t.Fatal("unapproved target triggered network or environment access")
	}
	target, err := r.Resolve(1, "campus-tools", "http://MCP-DEMO:8090/")
	if err != nil || target.BaseURL() != "http://mcp-demo:8090" || !r.HasToken(1, "campus-tools", target.BaseURL()) {
		t.Fatalf("approved target unavailable: %v", err)
	}
	encoded, _ := json.Marshal(target)
	for _, printable := range []string{string(encoded), fmt.Sprint(target), fmt.Sprintf("%+v %#v", target, target)} {
		if strings.Contains(printable, "test-only-token") {
			t.Fatal("target formatting leaked credential")
		}
	}
	ref, err := r.Reference(1, "campus-tools", "http://mcp-demo:8090/")
	if err != nil || len(ref) != 64 {
		t.Fatalf("invalid non-secret binding identity: %v", err)
	}
}

func TestRegistryInvalidBindingsNeverReadEnvironment(t *testing.T) {
	valid := `{"class_id":1,"server_name":"campus-tools","base_url":"http://mcp-demo:8090","token_env":"MCP_AUTH_TOKEN"}`
	for _, raw := range []string{
		`{}`, `null`, `[`, `[] {}`, `[{"token":"a-secret"}]`,
		"[" + valid + "," + valid + "]",
		strings.Replace("["+valid+"]", `"class_id":1`, `"class_id":0`, 1),
		strings.Replace("["+valid+"]", "campus-tools", "BadName", 1),
		strings.Replace("["+valid+"]", "MCP_AUTH_TOKEN", "CHAT_API_KEY", 1),
		strings.Replace("["+valid+"]", "MCP_AUTH_TOKEN", "DB_DSN", 1),
		strings.Replace("["+valid+"]", "http://mcp-demo:8090", "http://user:secret@mcp-demo", 1),
		"[" + valid + "," + strings.Replace(valid, "campus-tools", "other", 1) + "]",
	} {
		if _, err := NewRegistry(raw, func(string) string { t.Fatal("unexpected environment read"); return "" }); err == nil {
			t.Errorf("accepted invalid binding %s", raw)
		} else if strings.Contains(err.Error(), "a-secret") || strings.Contains(err.Error(), "user:secret") {
			t.Fatal("configuration error leaked input")
		}
	}
}

func TestRegistryMissingOrUnsafeCredential(t *testing.T) {
	for _, token := range []string{"", " leading-space", "header\r\ninjection", "x y", strings.Repeat("x", 4097)} {
		r := testRegistry(t, "http://mcp-demo:8090", func(string) string { return token })
		if _, err := r.Resolve(1, "campus-tools", "http://mcp-demo:8090"); !errors.Is(err, ErrCredentials) {
			t.Fatalf("unsafe credential accepted: %v", err)
		}
		if r.HasToken(1, "campus-tools", "http://mcp-demo:8090") {
			t.Fatal("unsafe credential reported available")
		}
	}
	for _, raw := range []string{"", "[]"} {
		r, err := NewRegistry(raw, func(string) string { t.Fatal("unexpected environment read"); return "" })
		if err != nil || r.HasToken(1, "campus-tools", "http://mcp-demo:8090") {
			t.Fatalf("empty registry should disable MCP: %v", err)
		}
	}
}
