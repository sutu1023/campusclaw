package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func createTestMCPServer(t *testing.T, st *Store, classID int64, name string) *MCPServer {
	t.Helper()
	server, err := st.CreateMCPServer(context.Background(), classID, name, "http://mcp-demo:8090/", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestMCPRegistrationIsolationAndTombstones(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	a := createTestMCPServer(t, st, 1, "campus-tools")
	b := createTestMCPServer(t, st, 2, "campus-tools")
	if a.BaseURL != "http://mcp-demo:8090" || a.State != MCPDisconnected || a.Generation != 0 || a.ApprovedHash != "" {
		t.Fatalf("unsafe initial state: %+v", a)
	}
	if _, err := st.CreateMCPServer(ctx, 1, a.Name, a.BaseURL, strings.Repeat("a", 64)); !errors.Is(err, ErrDuplicate) {
		t.Fatal("same-class duplicate accepted")
	}
	for _, id := range []int64{b.ID, 999999} {
		if _, err := st.GetMCPServer(ctx, 1, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-class/missing server distinguishable: %v", err)
		}
		if err := st.DeleteMCPServer(ctx, 1, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-class delete permitted")
		}
		if err := st.DisconnectMCPServer(ctx, 1, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-class disconnect permitted")
		}
		if _, err := st.UpdateMCPServer(ctx, 1, id, "bad-url", "bad-ref"); !errors.Is(err, ErrNotFound) {
			t.Fatal("update parsed foreign resource before ownership")
		}
	}
	list, err := st.ListMCPServers(ctx, 1)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("class-filtered list: %v, %v", list, err)
	}
	if err := st.DeleteMCPServer(ctx, 1, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetMCPServer(ctx, 1, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted server is still visible")
	}
	list, err = st.ListMCPServers(ctx, 1)
	if err != nil || len(list) != 0 {
		t.Fatal("deleted server still listed")
	}
	if _, err := st.CreateMCPServer(ctx, 1, a.Name, a.BaseURL, strings.Repeat("b", 64)); !errors.Is(err, ErrDuplicate) {
		t.Fatal("deleted name taken over")
	}
	var deleted bool
	if err := st.DB.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL FROM mcp_servers WHERE id=?`, a.ID).Scan(&deleted); err != nil || !deleted {
		t.Fatalf("missing tombstone: %v", err)
	}
	if _, err := st.GetMCPServer(ctx, 2, b.ID); err != nil {
		t.Fatal("other class changed by delete")
	}
}

func TestMCPSchemaTablesAndRegistrationValidation(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	for _, table := range []string{"mcp_servers", "mcp_tools", "mcp_discovery_snapshots", "mcp_discovery_changes", "mcp_tool_calls", "mcp_conversation_usage"} {
		var count int
		if err := st.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
	for _, tc := range []struct{ name, url, ref string }{
		{"BadName", "http://mcp-demo:8090", strings.Repeat("a", 64)},
		{"valid", "http://user:secret@mcp-demo:8090", strings.Repeat("a", 64)},
		{"valid", "http://mcp-demo:8090/mcp", strings.Repeat("a", 64)},
		{"valid", "http://mcp-demo:8090", "raw-token-not-a-binding-reference"},
	} {
		if _, err := st.CreateMCPServer(ctx, 1, tc.name, tc.url, tc.ref); !errors.Is(err, ErrInvalidMCP) {
			t.Fatal("invalid registration accepted")
		}
	}
	server := createTestMCPServer(t, st, 1, "campus-tools")
	encoded, _ := json.Marshal(server)
	if strings.Contains(string(encoded), "CredentialRef") || strings.Contains(string(encoded), strings.Repeat("a", 64)) {
		t.Fatal("internal credential reference serialized")
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("repeat DDL not idempotent: %v", err)
	}
}
