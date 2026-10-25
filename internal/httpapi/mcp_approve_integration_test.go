package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

func TestMCPSnapshotApprovalAndHistoryCannotBeBypassed(t *testing.T) {
	for _, changeMode := range []int64{6, 7} {
		t.Run(fmt.Sprint(changeMode), func(t *testing.T) {
			env := setupEnv(t)
			remote, mode := newMCPRemote(t)
			ctx := context.Background()
			configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"}}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token"})
			teacher := env.login(t, "teacher_a", "CampusClaw#2026")
			server := registerMCPHTTP(t, env, teacher, remote.URL)
			path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
			resp, raw := env.do(t, "POST", path+"/connect", teacher, nil, "")
			if resp.StatusCode != 200 {
				t.Fatal("initial connection failed")
			}
			var initial mcpServerDTO
			_ = json.Unmarshal(raw, &initial)
			mode.Store(changeMode)
			resp, raw = env.do(t, "POST", path+"/connect", teacher, nil, "")
			var pending mcpServerDTO
			_ = json.Unmarshal(raw, &pending)
			if resp.StatusCode != 200 || pending.State != "pending_approval" || pending.PendingHash == "" {
				t.Fatalf("change activated without confirmation: %s", raw)
			}
			if _, err := env.store.ResolveMCPTool(ctx, 1, "campus-tools", "formula_hint"); !errors.Is(err, store.ErrMCPDenied) {
				t.Fatal("pending tool became callable")
			}
			resp, raw = env.do(t, "GET", path+"/tools", teacher, nil, "")
			var directory mcpToolsResponse
			_ = json.Unmarshal(raw, &directory)
			if resp.StatusCode != 200 || len(directory.Tools) != 0 || len(directory.PendingTools) != 2 || len(directory.ApprovedTools) != 2 || directory.Change == nil || directory.Change.PreviousHash != initial.ApprovedHash {
				t.Fatal("full change comparison not visible")
			}
			stale, _ := json.Marshal(mcpApprovalRequest{SnapshotHash: initial.ApprovedHash})
			resp, _ = env.do(t, "POST", path+"/approve", teacher, stale, "application/json")
			if resp.StatusCode != 409 {
				t.Fatal("old hash approved")
			}
			// Both revocation paths must preserve the old approved comparison.
			for _, revoke := range []func() error{
				func() error { return env.store.DisconnectMCPServer(ctx, 1, server.ID) },
				func() error { return env.store.InvalidateMCPConnections(ctx) },
			} {
				if err := revoke(); err != nil {
					t.Fatal(err)
				}
				body, _ := json.Marshal(mcpApprovalRequest{SnapshotHash: pending.PendingHash})
				resp, _ = env.do(t, "POST", path+"/approve", teacher, body, "application/json")
				if resp.StatusCode != 409 {
					t.Fatal("revoked pending snapshot approved")
				}
				resp, raw = env.do(t, "POST", path+"/connect", teacher, nil, "")
				_ = json.Unmarshal(raw, &pending)
				if resp.StatusCode != 200 || pending.State != "pending_approval" {
					t.Fatal("revocation bypassed history comparison")
				}
			}
			// Two confirmations race on the same hash: exactly one succeeds.
			body, _ := json.Marshal(mcpApprovalRequest{SnapshotHash: pending.PendingHash})
			codes := make(chan int, 2)
			var workers sync.WaitGroup
			for i := 0; i < 2; i++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					resp, _ := env.do(t, "POST", path+"/approve", teacher, body, "application/json")
					codes <- resp.StatusCode
				}()
			}
			workers.Wait()
			close(codes)
			ok, conflict := 0, 0
			for code := range codes {
				if code == 200 {
					ok++
				} else if code == 409 {
					conflict++
				} else {
					t.Fatalf("unexpected approval status %d", code)
				}
			}
			if ok != 1 || conflict != 1 {
				t.Fatal("concurrent approval was not exclusive")
			}
			candidate, err := env.store.ResolveMCPTool(ctx, 1, "campus-tools", "formula_hint")
			if err != nil || candidate.Server.ApprovedHash != pending.PendingHash {
				t.Fatal("confirmed snapshot not active")
			}
		})
	}
}
