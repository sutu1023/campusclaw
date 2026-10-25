package config

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMCPDefaultsAndAbsentConfiguration(t *testing.T) {
	c, err := loadMCP(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxRounds != 3 || c.MaxCallsPerTurn != 8 || c.MaxCallsPerConversation != 40 || c.TurnTimeout != 20*time.Second || c.RequestTimeout != 5*time.Second {
		t.Fatalf("unexpected MCP defaults: %+v", c)
	}
	if c.Registry.HasToken(1, "campus-tools", "http://mcp-demo:8090") {
		t.Fatal("missing MCP configuration must not grant access")
	}
	setBase(t)
	for _, key := range []string{"MCP_CREDENTIAL_BINDINGS", "MCP_MAX_ROUNDS", "MCP_MAX_CALLS_PER_TURN", "MCP_MAX_CALLS_PER_CONVERSATION", "MCP_TURN_TIMEOUT", "MCP_REQUEST_TIMEOUT"} {
		t.Setenv(key, "")
	}
	if _, err := Load(); err != nil {
		t.Fatalf("absent MCP configuration blocked application startup: %v", err)
	}
}

func TestMCPLimitValidation(t *testing.T) {
	for _, key := range []string{"MCP_MAX_ROUNDS", "MCP_MAX_CALLS_PER_TURN", "MCP_MAX_CALLS_PER_CONVERSATION"} {
		for _, bad := range []string{"0", "-1", "oops", "1.5", "99999999999999999999999999", "10001"} {
			t.Run(key+"/"+bad, func(t *testing.T) {
				_, err := loadMCP(func(k string) string {
					if k == key {
						return bad
					}
					return ""
				})
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("invalid limit not rejected: %v", err)
				}
			})
		}
	}
	for _, key := range []string{"MCP_TURN_TIMEOUT", "MCP_REQUEST_TIMEOUT"} {
		for _, bad := range []string{"0s", "-1s", "oops", "999h", "5"} {
			_, err := loadMCP(func(k string) string {
				if k == key {
					return bad
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("invalid timeout not rejected: %v", err)
			}
		}
	}
}

func TestMCPEnvironmentExampleMatchesDefaults(t *testing.T) {
	raw, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok && strings.HasPrefix(key, "MCP_") {
			vars[key] = strings.Trim(value, "'")
		}
	}
	c, err := loadMCP(func(key string) string { return vars[key] })
	if err != nil {
		t.Fatalf("example MCP configuration is invalid: %v", err)
	}
	want := map[string]string{
		"MCP_MAX_ROUNDS":                 strconv.Itoa(c.MaxRounds),
		"MCP_MAX_CALLS_PER_TURN":         strconv.Itoa(c.MaxCallsPerTurn),
		"MCP_MAX_CALLS_PER_CONVERSATION": strconv.Itoa(c.MaxCallsPerConversation),
		"MCP_TURN_TIMEOUT":               c.TurnTimeout.String(),
		"MCP_REQUEST_TIMEOUT":            c.RequestTimeout.String(),
	}
	for key, value := range want {
		if vars[key] != value {
			t.Errorf("example %s is %q, want %q", key, vars[key], value)
		}
	}
	if token, exists := vars["MCP_AUTH_TOKEN"]; !exists || token != "" {
		t.Fatal("example must contain an empty MCP secret")
	}
	if c.Registry.HasToken(1, "campus-tools", "http://mcp-demo:8090") {
		t.Fatal("example must not provide an actual credential")
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for key := range vars {
		if !strings.Contains(string(readme), "`"+key+"`") {
			t.Errorf("MCP variable %s missing from README", key)
		}
	}
}

func TestMCPOverridesAndBindingValidation(t *testing.T) {
	vars := map[string]string{
		"MCP_MAX_ROUNDS": "2", "MCP_MAX_CALLS_PER_TURN": "4", "MCP_MAX_CALLS_PER_CONVERSATION": "10",
		"MCP_TURN_TIMEOUT": "10s", "MCP_REQUEST_TIMEOUT": "2s",
		"MCP_CREDENTIAL_BINDINGS": `[{"class_id":1,"server_name":"campus-tools","base_url":"http://mcp-demo:8090/","token_env":"MCP_AUTH_TOKEN"}]`,
		"MCP_AUTH_TOKEN":          "",
	}
	c, err := loadMCP(func(k string) string { return vars[k] })
	if err != nil || c.MaxRounds != 2 || c.MaxCallsPerTurn != 4 || c.MaxCallsPerConversation != 10 || c.TurnTimeout != 10*time.Second || c.RequestTimeout != 2*time.Second {
		t.Fatalf("overrides not applied: %+v, %v", c, err)
	}
	if c.Registry.HasToken(1, "campus-tools", "http://mcp-demo:8090") {
		t.Fatal("empty deployment token must remain unavailable")
	}
	vars["MCP_AUTH_TOKEN"] = "test-only-token"
	if !c.Registry.HasToken(1, "campus-tools", "http://mcp-demo:8090") {
		t.Fatal("server-side credential lookup failed")
	}
	vars["MCP_CREDENTIAL_BINDINGS"] = `[{"class_id":1,"server_name":"campus-tools","base_url":"http://mcp-demo:8090","token_env":"DB_DSN"}]`
	if _, err := loadMCP(func(k string) string { return vars[k] }); err == nil {
		t.Fatal("invalid binding must fail configuration loading")
	}
}
