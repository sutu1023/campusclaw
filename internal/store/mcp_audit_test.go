package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func auditFixture(t *testing.T) (*Store, *MCPToolCandidate, MCPCallAttempt) {
	t.Helper()
	ctx := context.Background()
	st := testStore(t)
	server := createTestMCPServer(t, st, 1, "campus-tools")
	connectTestMCP(t, st, server, testMCPTools())
	conversation, question := mcpTestQuestion(t, st)
	candidate, err := st.ResolveMCPTool(ctx, 1, "campus-tools", "formula_hint")
	if err != nil {
		t.Fatal(err)
	}
	return st, candidate, MCPCallAttempt{ClassID: 1, ServerID: server.ID, ToolName: "formula_hint", UserID: conversation.UserID, ConversationID: conversation.ID, QuestionMessageID: question, CallID: "test-call", Round: 1}
}

func TestMCPConcurrentReservationAndPersistentUsage(t *testing.T) {
	st, candidate, attempt := auditFixture(t)
	ctx := context.Background()
	type result struct {
		id  int64
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, callID := range []string{"question-1-call-1", "question-2-call-1"} {
		workers.Add(1)
		go func(id string) {
			defer workers.Done()
			a := attempt
			a.CallID = id
			<-start
			auditID, err := st.ReserveMCPCall(ctx, a, *candidate, 1)
			results <- result{auditID, err}
		}(callID)
	}
	close(start)
	workers.Wait()
	close(results)
	allowed, denied := 0, 0
	for r := range results {
		if r.id <= 0 {
			t.Fatalf("missing audit evidence: %v", r.err)
		}
		if r.err == nil {
			allowed++
		} else if errors.Is(r.err, ErrMCPQuota) {
			denied++
		} else {
			t.Fatal(r.err)
		}
	}
	if allowed != 1 || denied != 1 {
		t.Fatalf("last credit bypassed: allowed=%d denied=%d", allowed, denied)
	}
	if count, err := st.MCPConversationUsage(ctx, 1, attempt.UserID, attempt.ConversationID); err != nil || count != 1 {
		t.Fatalf("usage not reserved: %d %v", count, err)
	}
	// A new pool/process plus startup invalidation must not reset durable usage.
	restarted, err := Open(ctx, os.Getenv("TEST_DB_DSN"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.InvalidateMCPConnections(ctx); err != nil {
		t.Fatal(err)
	}
	if count, err := restarted.MCPConversationUsage(ctx, 1, attempt.UserID, attempt.ConversationID); err != nil || count != 1 {
		t.Fatal("restart reset conversation quota")
	}
	connectTestMCP(t, restarted, &candidate.Server, testMCPTools())
	fresh, err := restarted.ResolveMCPTool(ctx, 1, "campus-tools", "formula_hint")
	if err != nil {
		t.Fatal(err)
	}
	attempt.CallID = "after-restart"
	if id, err := restarted.ReserveMCPCall(ctx, attempt, *fresh, 1); id <= 0 || !errors.Is(err, ErrMCPQuota) {
		t.Fatalf("restart bypassed quota: %d %v", id, err)
	}
}

func TestMCPAuditPrewriteAndFinalizeFailuresFailClosed(t *testing.T) {
	st, candidate, attempt := auditFixture(t)
	ctx := context.Background()
	if _, err := st.DB.ExecContext(ctx, `ALTER TABLE mcp_tool_calls ADD CONSTRAINT mcp_test_block_insert CHECK (call_id='intentionally-unreachable-call')`); err != nil {
		t.Fatal(err)
	}
	id, err := st.ReserveMCPCall(ctx, attempt, *candidate, 40)
	if err == nil || id != 0 {
		t.Fatal("failed audit prewrite granted a remote permit")
	}
	if count, err := st.MCPConversationUsage(ctx, 1, attempt.UserID, attempt.ConversationID); err != nil || count != 0 {
		t.Fatal("failed transaction consumed credit")
	}
	if _, err := st.DB.ExecContext(ctx, `ALTER TABLE mcp_tool_calls DROP CHECK mcp_test_block_insert`); err != nil {
		t.Fatal(err)
	}
	id, err = st.ReserveMCPCall(ctx, attempt, *candidate, 40)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `ALTER TABLE mcp_tool_calls ADD CONSTRAINT mcp_test_block_update CHECK (completed_at IS NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishMCPCall(ctx, 1, id, "ok", "工具已返回安全提示", 12); err == nil {
		t.Fatal("failed finalization reported success")
	}
	calls, total, err := st.ListMCPAudit(ctx, 1, 1, 20, 0, "")
	if err != nil || total != 1 || calls[0].Status != "error" || calls[0].CompletedAt != nil || !strings.Contains(calls[0].Text, "interrupted") {
		t.Fatalf("interrupted evidence lost: %+v %v", calls, err)
	}
	if _, err := st.DB.ExecContext(ctx, `ALTER TABLE mcp_tool_calls DROP CHECK mcp_test_block_update`); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishMCPCall(ctx, 1, id, "error", "审计终结失败，结果未采用", 12); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishMCPCall(ctx, 1, id, "ok", "不应重写历史", 13); !errors.Is(err, ErrMCPStale) {
		t.Fatal("final audit could be overwritten")
	}
	if count, _ := st.MCPConversationUsage(ctx, 1, attempt.UserID, attempt.ConversationID); count != 1 {
		t.Fatal("failure refunded reserved action")
	}
}

func TestMCPAuthorizationRecheckedAndAuditHistoryRetained(t *testing.T) {
	st, candidate, attempt := auditFixture(t)
	ctx := context.Background()
	id, err := st.ReserveMCPCall(ctx, attempt, *candidate, 40)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishMCPCall(ctx, 1, id, "ok", "请先检查公式条件", 8); err != nil {
		t.Fatal(err)
	}
	attempt.CallID = "local-parameter-error"
	if _, err := st.RecordMCPAttempt(ctx, attempt, "error", "工具参数不符合批准结构"); err != nil {
		t.Fatal(err)
	}
	if err := st.DisconnectMCPServer(ctx, 1, attempt.ServerID); err != nil {
		t.Fatal(err)
	}
	attempt.CallID = "disconnected-call"
	if id, err := st.ReserveMCPCall(ctx, attempt, *candidate, 40); id <= 0 || !errors.Is(err, ErrMCPDenied) {
		t.Fatal("stale candidate granted remote permission")
	}
	if count, _ := st.MCPConversationUsage(ctx, 1, attempt.UserID, attempt.ConversationID); count != 1 {
		t.Fatal("local failure consumed remote quota")
	}
	calls, total, err := st.ListMCPAudit(ctx, 1, 1, 2, 0, "")
	if err != nil || total != 3 || len(calls) != 2 || calls[0].Status != "denied" || calls[1].Status != "error" {
		t.Fatalf("audit ordering/status wrong: %+v %v", calls, err)
	}
	if _, total, err := st.ListMCPAudit(ctx, 2, 1, 20, 0, ""); err != nil || total != 0 {
		t.Fatal("audit leaked across class")
	}
	if _, _, err := st.ListMCPAudit(ctx, 2, 1, 20, attempt.ServerID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign audit filter leaked existence")
	}
	if err := st.DeleteMCPServer(ctx, 1, attempt.ServerID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `DELETE FROM conversations WHERE id=?`, attempt.ConversationID); err != nil {
		t.Fatal(err)
	}
	calls, total, err = st.ListMCPAudit(ctx, 1, 1, 20, attempt.ServerID, "")
	if err != nil || total != 3 || calls[0].ServerName != "campus-tools" || calls[0].Ref != "formula_hint@campus-tools" {
		t.Fatal("deletion erased historical source")
	}
}

func TestMCPUnattributableAttemptsNeverWriteForeignAudit(t *testing.T) {
	st, _, attempt := auditFixture(t)
	ctx := context.Background()
	foreign := createTestMCPServer(t, st, 2, "foreign-tools")
	for _, serverID := range []int64{foreign.ID, 999999} {
		attempt.ServerID = serverID
		if id, err := st.RecordMCPAttempt(ctx, attempt, "denied", "本次工具不可用"); id != 0 || !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign/missing target logged as local server")
		}
	}
	if _, total, err := st.ListMCPAudit(ctx, 1, 1, 20, 0, ""); err != nil || total != 0 {
		t.Fatal("unattributable attempt created an audit row")
	}
}
