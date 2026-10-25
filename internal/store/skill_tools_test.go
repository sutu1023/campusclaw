package store

import (
	"context"
	"errors"
	"testing"
)

func TestSkillMCPReferencesRequireCurrentClassApprovedDirectory(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	server := createTestMCPServer(t, st, 1, "campus-tools")
	connectTestMCP(t, st, server, testMCPTools())
	original := NewSkill{ClassID: 1, Name: "MCP skill", Trigger: "公式", Description: "原说明", Tools: []string{"knowledge_search", "formula_hint@campus-tools"}, Source: "original source", Enabled: true}
	created, err := st.CreateSkill(ctx, original)
	if err != nil || len(created.Tools) != 2 {
		t.Fatalf("approved reference rejected: %v", err)
	}
	invalid := original
	invalid.Name = "should not be saved"
	invalid.Source = "changed source"
	invalid.Tools = []string{"formula_hint@campus-tools", "unknown@campus-tools"}
	if _, err := st.UpdateSkill(ctx, 1, created.ID, invalid); !errors.Is(err, ErrMCPDenied) {
		t.Fatalf("unknown tool not rejected: %v", err)
	}
	unchanged, err := st.GetSkill(ctx, 1, created.ID)
	if err != nil || unchanged.Name != original.Name || unchanged.Source != original.Source || len(unchanged.Tools) != 2 {
		t.Fatal("failed update wrote partial fields")
	}
	foreign := createTestMCPServer(t, st, 2, "foreign-tools")
	_ = foreign
	for _, ref := range []string{"formula_hint@foreign-tools", "formula_hint@absent-tools"} {
		invalid.Tools = []string{ref}
		if _, err := st.CreateSkill(ctx, invalid); !errors.Is(err, ErrNotFound) {
			t.Fatal("missing/foreign server distinguishable")
		}
	}
	if err := st.DisconnectMCPServer(ctx, 1, server.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateSkill(ctx, 1, created.ID, original); !errors.Is(err, ErrMCPDenied) {
		t.Fatal("disconnected reference saved")
	}
	changed := testMCPTools()
	changed[0].Description = "changed description"
	pending := connectTestMCP(t, st, server, changed)
	if pending.State != MCPPending {
		t.Fatal("fixture is not pending")
	}
	if _, err := st.CreateSkill(ctx, original); !errors.Is(err, ErrMCPDenied) {
		t.Fatal("pending reference saved")
	}
	invalid.Tools = []string{"not-a-valid-reference"}
	if _, err := st.UpdateSkill(ctx, 2, created.ID, invalid); !errors.Is(err, ErrNotFound) {
		t.Fatal("update validated references before ownership")
	}
	builtin := original
	builtin.Name = "Builtin only"
	builtin.Tools = []string{"knowledge_search"}
	if _, err := st.CreateSkill(ctx, builtin); err != nil {
		t.Fatal("builtin skill depends on MCP")
	}
	builtin.Name = "No tools"
	builtin.Tools = nil
	if _, err := st.CreateSkill(ctx, builtin); err != nil {
		t.Fatal("empty tool skill depends on MCP")
	}
}
