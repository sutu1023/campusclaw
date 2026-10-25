// mcpcheck verifies the actual MCP service from the distroless API container.
// It uses deployment-approved credentials and prints only safe status summaries.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"campusclaw/internal/config"
	"campusclaw/internal/mcp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("mcpcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	classID := flags.Int64("class-id", 1, "approved class ID")
	serverName := flags.String("server", "campus-tools", "approved server name")
	baseURL := flags.String("base-url", "http://mcp-demo:8090", "approved origin")
	unauthorized := flags.Bool("expect-unauthorized", false, "verify a deliberately incorrect runtime credential")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return errors.New("usage: mcpcheck [-class-id ID] [-server NAME] [-base-url ORIGIN] [-expect-unauthorized]")
	}
	cfg, err := config.Load()
	if err != nil {
		return errors.New("mcpcheck: invalid server configuration")
	}
	target, err := cfg.MCP.Registry.Resolve(*classID, *serverName, *baseURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.MCP.TurnTimeout)
	defer cancel()
	if err := health(ctx, target.BaseURL(), cfg.MCP.RequestTimeout); err != nil {
		return err
	}
	client, err := mcp.NewClient(target, cfg.MCP.RequestTimeout)
	if err != nil {
		return err
	}
	if *unauthorized {
		if err := client.Initialize(ctx); !errors.Is(err, mcp.ErrUnauthorized) {
			return errors.New("mcpcheck: expected MCP HTTP 401")
		}
		return json.NewEncoder(out).Encode(map[string]string{"health": "ok", "authentication": "unauthorized"})
	}
	tools, err := client.Discover(ctx)
	if err != nil {
		return err
	}
	byName := map[string]mcp.Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	type callSummary struct {
		Tool       string `json:"tool"`
		Status     string `json:"status"`
		Characters int    `json:"characters"`
	}
	calls := []callSummary{}
	for _, item := range []struct{ name, query string }{
		{"formula_hint", "如何检查勾股定理的适用条件？"},
		{"unit_convert", "长度单位换算应如何确定方向？"},
	} {
		tool, ok := byName[item.name]
		if !ok {
			return errors.New("mcpcheck: teaching tool missing from discovery")
		}
		arguments, _ := json.Marshal(map[string]string{"query": item.query})
		result, err := client.Call(ctx, tool, arguments)
		if err != nil {
			return err
		}
		if result.IsError || len(result.Content) == 0 {
			return errors.New("mcpcheck: teaching tool failed")
		}
		characters := 0
		for _, content := range result.Content {
			if content.Type != "text" || strings.TrimSpace(content.Text) == "" {
				return errors.New("mcpcheck: expected non-empty text")
			}
			characters += utf8.RuneCountInString(content.Text)
		}
		calls = append(calls, callSummary{item.name, "ok", characters})
	}
	return json.NewEncoder(out).Encode(map[string]any{"health": "ok", "protocol": mcp.ProtocolVersion, "discovered": len(tools), "calls": calls})
}

func health(ctx context.Context, origin string, timeout time.Duration) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/health", nil)
	if err != nil {
		return errors.New("mcpcheck: health check failed")
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("mcpcheck: health check failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
	var result struct {
		Status string `json:"status"`
	}
	if err != nil || len(raw) > 1024 || resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &result) != nil || result.Status != "ok" {
		return errors.New("mcpcheck: health check failed")
	}
	return nil
}
