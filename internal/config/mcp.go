package config

import (
	"fmt"
	"strconv"
	"time"

	"campusclaw/internal/mcp"
)

// MCPConfig contains host-side limits and a deployment-only credential
// allowlist. Missing MCP configuration disables access, not application startup.
type MCPConfig struct {
	Registry                *mcp.Registry
	MaxRounds               int
	MaxCallsPerTurn         int
	MaxCallsPerConversation int
	TurnTimeout             time.Duration
	RequestTimeout          time.Duration
}

func loadMCP(getenv func(string) string) (MCPConfig, error) {
	c := MCPConfig{}
	var err error
	c.Registry, err = mcp.NewRegistry(getenv("MCP_CREDENTIAL_BINDINGS"), getenv)
	if err != nil {
		return c, err
	}
	for _, v := range []struct {
		key      string
		fallback int
		max      int
		dest     *int
	}{
		{"MCP_MAX_ROUNDS", 3, 64, &c.MaxRounds},
		{"MCP_MAX_CALLS_PER_TURN", 8, 64, &c.MaxCallsPerTurn},
		{"MCP_MAX_CALLS_PER_CONVERSATION", 40, 10000, &c.MaxCallsPerConversation},
	} {
		n := v.fallback
		if raw := getenv(v.key); raw != "" {
			n, err = strconv.Atoi(raw)
		}
		if err != nil || n <= 0 || n > v.max {
			return c, fmt.Errorf("%s must be an integer between 1 and %d", v.key, v.max)
		}
		*v.dest = n
	}
	for _, v := range []struct {
		key      string
		fallback time.Duration
		max      time.Duration
		dest     *time.Duration
	}{
		{"MCP_TURN_TIMEOUT", 20 * time.Second, 2 * time.Minute, &c.TurnTimeout},
		{"MCP_REQUEST_TIMEOUT", 5 * time.Second, time.Minute, &c.RequestTimeout},
	} {
		d := v.fallback
		if raw := getenv(v.key); raw != "" {
			d, err = time.ParseDuration(raw)
		}
		if err != nil || d <= 0 || d > v.max {
			return c, fmt.Errorf("%s must be a positive Go duration no greater than %s", v.key, v.max)
		}
		*v.dest = d
	}
	return c, nil
}
