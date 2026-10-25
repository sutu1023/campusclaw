// mcp-server is a stateless HTTP/JSON MCP teaching service in the root Go module.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "--healthcheck" {
		return checkHealth(context.Background(), "http://127.0.0.1:8090/health")
	}
	if len(args) != 0 {
		return errors.New("usage: mcp-server [--healthcheck]")
	}
	handler, err := newHandler(os.Getenv("MCP_AUTH_TOKEN"))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr: ":8090", Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 * 1024,
	}
	return srv.ListenAndServe()
}

// The executable probes itself without depending on curl in the runtime image.
func checkHealth(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.New("MCP health check failed")
	}
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("MCP health check failed")
	}
	defer resp.Body.Close()
	var status struct {
		Status string `json:"status"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1024)).Decode(&status) != nil || status.Status != "ok" {
		return errors.New("MCP health check failed")
	}
	return nil
}
