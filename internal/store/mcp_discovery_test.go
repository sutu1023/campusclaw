package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"campusclaw/internal/mcp"
)

func testMCPTools() []mcp.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500}},"required":["query"],"additionalProperties":false}`)
	return []mcp.Tool{{Name: "formula_hint", Description: "Formula conditions", InputSchema: schema}, {Name: "unit_convert", Description: "Unit directions", InputSchema: schema}}
}

func mcpTeacherID(t *testing.T, st *Store) int64 {
	t.Helper()
	var id int64
	if err := st.DB.QueryRowContext(context.Background(), `SELECT id FROM users WHERE username='teacher_a'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func connectTestMCP(t *testing.T, st *Store, server *MCPServer, tools []mcp.Tool) *MCPServer {
	t.Helper()
	ctx := context.Background()
	operation, err := st.BeginMCPConnect(ctx, server.ClassID, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	connected, err := st.CompleteMCPDiscovery(ctx, server.ClassID, server.ID, operation.Generation, tools, mcpTeacherID(t, st))
	if err != nil {
		t.Fatal(err)
	}
	return connected
}

func TestMCPDiscoveryReplacesAndCanonicalizes(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	server := createTestMCPServer(t, st, 1, "campus-tools")
	connected := connectTestMCP(t, st, server, testMCPTools())
	if connected.State != MCPConnected || connected.ApprovedHash == "" {
		t.Fatal("first discovery not activated")
	}
	tools := testMCPTools()
	tools[0], tools[1] = tools[1], tools[0]
	for i := range tools {
		var v any
		_ = json.Unmarshal(tools[i].InputSchema, &v)
		tools[i].InputSchema, _ = json.MarshalIndent(v, "", "  ")
	}
	again := connectTestMCP(t, st, server, tools)
	if again.State != MCPConnected || again.ApprovedHash != connected.ApprovedHash {
		t.Fatal("order/whitespace caused false change")
	}
	directory, err := st.ReadMCPDirectory(ctx, 1, server.ID)
	if err != nil || len(directory.Tools) != 2 {
		t.Fatalf("repeat discovery duplicated tools: %+v, %v", directory, err)
	}
	reduced := connectTestMCP(t, st, server, testMCPTools()[:1])
	directory, err = st.ReadMCPDirectory(ctx, 1, server.ID)
	if err != nil || reduced.State != MCPConnected || len(directory.Tools) != 1 || directory.Tools[0].Name != "formula_hint" {
		t.Fatal("removed tool remained available")
	}
	if directory.Change == nil || len(directory.Change.ChangedTools) != 1 || directory.Change.ChangedTools[0] != "unit_convert" {
		t.Fatal("removal not recorded")
	}
	if _, err := st.ReadMCPDirectory(ctx, 2, server.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("directory leaked across class")
	}
}

func TestMCPDescriptionAndSchemaChangesRequireConfirmation(t *testing.T) {
	for _, kind := range []string{"description", "schema"} {
		t.Run(kind, func(t *testing.T) {
			st := testStore(t)
			ctx := context.Background()
			server := createTestMCPServer(t, st, 1, "campus-tools")
			approved := connectTestMCP(t, st, server, testMCPTools())
			changed := testMCPTools()
			if kind == "description" {
				changed[0].Description = "New complete teaching description"
			} else {
				changed[0].InputSchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":2,"maxLength":500}},"required":["query"],"additionalProperties":false}`)
			}
			pending := connectTestMCP(t, st, server, changed)
			if pending.State != MCPPending || pending.PendingHash == "" || pending.ApprovedHash != approved.ApprovedHash {
				t.Fatal("changed definition activated without approval")
			}
			directory, err := st.ReadMCPDirectory(ctx, 1, server.ID)
			if err != nil || len(directory.Tools) != 0 || len(directory.PendingTools) != 2 || len(directory.ApprovedTools) != 2 || directory.Change == nil || directory.Change.PreviousHash != approved.ApprovedHash || directory.Change.ApprovedAt != nil {
				t.Fatalf("missing complete comparison data: %+v %v", directory, err)
			}
			if _, err := st.ApproveMCPSnapshot(ctx, 1, server.ID, pending.Generation, approved.ApprovedHash, mcpTeacherID(t, st)); !errors.Is(err, ErrMCPStale) {
				t.Fatal("stale hash approved")
			}
			active, err := st.ApproveMCPSnapshot(ctx, 1, server.ID, pending.Generation, pending.PendingHash, mcpTeacherID(t, st))
			if err != nil || active.State != MCPConnected {
				t.Fatalf("matching approval failed: %v", err)
			}
			directory, err = st.ReadMCPDirectory(ctx, 1, server.ID)
			if err != nil || len(directory.Tools) != 2 || directory.Change.ApprovedAt == nil || directory.Change.ApprovedBy == nil {
				t.Fatal("approval evidence or directory missing")
			}
			if _, err := st.ApproveMCPSnapshot(ctx, 1, server.ID, pending.Generation, pending.PendingHash, mcpTeacherID(t, st)); !errors.Is(err, ErrMCPStale) {
				t.Fatal("duplicate approval accepted")
			}
		})
	}
}

func TestMCPLateDiscoveryCannotReviveDisconnectedServer(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	server := createTestMCPServer(t, st, 1, "campus-tools")
	operation, err := st.BeginMCPConnect(ctx, 1, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DisconnectMCPServer(ctx, 1, server.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompleteMCPDiscovery(ctx, 1, server.ID, operation.Generation, testMCPTools(), mcpTeacherID(t, st)); !errors.Is(err, ErrMCPStale) {
		t.Fatal("late connection revived server")
	}
	older, err := st.BeginMCPConnect(ctx, 1, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := st.BeginMCPConnect(ctx, 1, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompleteMCPDiscovery(ctx, 1, server.ID, older.Generation, testMCPTools(), mcpTeacherID(t, st)); !errors.Is(err, ErrMCPStale) {
		t.Fatal("older connection overwrote newer operation")
	}
	if _, err := st.CompleteMCPDiscovery(ctx, 1, server.ID, newer.Generation, testMCPTools(), mcpTeacherID(t, st)); err != nil {
		t.Fatal(err)
	}
	if err := st.FailMCPDiscovery(ctx, 1, server.ID, older.Generation); !errors.Is(err, ErrMCPStale) {
		t.Fatal("late failure cleared good connection")
	}
}

func TestMCPDisconnectRestartRetainApprovedHistory(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	server := createTestMCPServer(t, st, 1, "campus-tools")
	original := connectTestMCP(t, st, server, testMCPTools())
	changed := testMCPTools()
	changed[0].Description = "Changed after disconnect"
	for _, revoke := range []func() error{
		func() error { return st.DisconnectMCPServer(ctx, 1, server.ID) },
		func() error { return st.InvalidateMCPConnections(ctx) },
	} {
		if err := revoke(); err != nil {
			t.Fatal(err)
		}
		directory, err := st.ReadMCPDirectory(ctx, 1, server.ID)
		if err != nil || len(directory.Tools) != 0 || len(directory.PendingTools) != 0 || directory.Server.ApprovedHash != original.ApprovedHash {
			t.Fatal("revocation erased approved comparison history")
		}
		pending := connectTestMCP(t, st, server, changed)
		if pending.State != MCPPending {
			t.Fatal("disconnect/restart bypassed change confirmation")
		}
	}
	if err := st.DisconnectMCPServer(ctx, 1, server.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DisconnectMCPServer(ctx, 1, server.ID); err != nil {
		t.Fatal("disconnect not idempotent")
	}
	operation, err := st.BeginMCPConnect(ctx, 1, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	bad := testMCPTools()
	bad = append(bad, bad[0])
	if _, err := st.CompleteMCPDiscovery(ctx, 1, server.ID, operation.Generation, bad, mcpTeacherID(t, st)); err == nil {
		t.Fatal("partial invalid catalog accepted")
	}
	if err := st.FailMCPDiscovery(ctx, 1, server.ID, operation.Generation); err != nil {
		t.Fatal(err)
	}
	directory, err := st.ReadMCPDirectory(ctx, 1, server.ID)
	if err != nil || directory.Server.State != MCPDisconnected || directory.Server.LastError == "" || len(directory.Tools) != 0 {
		t.Fatal("failed connection left tools available")
	}
}
