package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// testStore opens the MySQL pointed at by TEST_DB_DSN, resets it and seeds it.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; skipping MySQL integration test")
	}
	ctx := context.Background()
	st, err := Open(ctx, dsn, 60*time.Second)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Child-first order preserves FK checks on every pooled connection.
	if _, err := st.DB.ExecContext(ctx, `DROP TABLE IF EXISTS mcp_tool_calls, mcp_discovery_changes, mcp_discovery_snapshots, mcp_tools, mcp_servers, mcp_conversation_usage, messages, conversations, tutor_agent_materials, tutor_agent_skills, skills, tutor_agents, knowledge_chunks, knowledge_entries, materials, sessions, users, classes`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := st.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return st
}

func TestListMaterialsIsClassScoped(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	materials, total, err := st.ListMaterials(ctx, 1, 1, 20, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total < 1 || len(materials) == 0 {
		t.Fatalf("expected at least one class-A material, got total=%d", total)
	}
	for _, m := range materials {
		if m.ClassID != 1 {
			t.Fatalf("class leak: material %d has class %d", m.ID, m.ClassID)
		}
		if strings.Contains(m.Title, "B 班") {
			t.Fatalf("class B material leaked into class A list: %q", m.Title)
		}
	}
}

func TestCrossClassMaterialIsNotFound(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	classB, _, err := st.ListMaterials(ctx, 2, 1, 20, "")
	if err != nil || len(classB) == 0 {
		t.Fatalf("expected class B material: %v", err)
	}
	if _, err := st.GetMaterial(ctx, 1, classB[0].ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-class access, got %v", err)
	}
}

func TestSessionExpiryAndHashOnlyStorage(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	user, err := st.GetUserByUsername(ctx, "teacher_a")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	// Expired sessions never resolve.
	expiredHash := "aa" + strings.Repeat("0", 62)
	if err := st.CreateSession(ctx, expiredHash, user.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	if _, err := st.SessionUser(ctx, expiredHash); err != ErrNotFound {
		t.Fatalf("expired session must not resolve, got %v", err)
	}

	// The raw token is never stored: only its hash exists.
	rawToken := "super-secret-raw-token"
	hash := "bb" + strings.Repeat("0", 62)
	if err := st.CreateSession(ctx, hash, user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	var count int
	if err := st.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE token_hash=?`, rawToken).Scan(&count); err != nil {
		t.Fatalf("query raw token: %v", err)
	}
	if count != 0 {
		t.Fatal("raw token must not be stored in sessions")
	}
	resolved, err := st.SessionUser(ctx, hash)
	if err != nil || resolved.ID != user.ID {
		t.Fatalf("valid session must resolve, got user=%v err=%v", resolved, err)
	}
}

func TestKeywordSearchIsReadyOnlyAndClassScoped(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	entry, err := st.GetEntryByMaterial(ctx, 1, 1)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	ids, err := st.InsertChunks(ctx, []NewChunk{{
		KnowledgeEntryID: entry.ID, ClassID: 1, ChunkIndex: 0,
		StartOffset: 0, EndOffset: 2, ChunkText: "勾股定理",
	}})
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	// Pending chunks are not searchable.
	if hits, _ := st.KeywordSearch(ctx, 1, "勾股", 10); len(hits) != 0 {
		t.Fatalf("pending chunk must not match, got %d", len(hits))
	}
	if err := st.SetChunkStatus(ctx, ids[0], "ready", ""); err != nil {
		t.Fatalf("set status: %v", err)
	}
	hits, err := st.KeywordSearch(ctx, 1, "勾股", 10)
	if err != nil {
		t.Fatalf("keyword search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 keyword hit, got %d", len(hits))
	}
	// A class-A session cannot see a class-B-only term.
	if hits, _ := st.KeywordSearch(ctx, 1, "红叶", 10); len(hits) != 0 {
		t.Fatalf("cross-class keyword leak: %d hits", len(hits))
	}
}
