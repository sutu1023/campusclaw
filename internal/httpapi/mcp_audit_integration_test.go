package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

func TestMCPAllRoutesRequireTeacherAndAuditIsClassScoped(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()
	remote, _ := newMCPRemote(t)
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: remote.URL, TokenEnv: "MCP_REMOTE_TOKEN"}}, map[string]string{"MCP_REMOTE_TOKEN": "test-only-remote-token"})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	student := env.login(t, "student_a1", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, remote.URL)
	path := fmt.Sprintf("/api/mcp/servers/%d", server.ID)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/mcp/servers"}, {"POST", "/api/mcp/servers"}, {"GET", path}, {"PUT", path}, {"DELETE", path}, {"POST", path + "/connect"}, {"POST", path + "/disconnect"}, {"POST", path + "/approve"}, {"GET", path + "/tools"}, {"GET", "/api/mcp/audit"},
	} {
		for _, token := range []string{"", student} {
			resp, _ := env.do(t, route.method, route.path, token, []byte(`{}`), "application/json")
			want := 401
			if token != "" {
				want = 403
			}
			if resp.StatusCode != want {
				t.Fatalf("permission bypass: %s %s => %d", route.method, route.path, resp.StatusCode)
			}
		}
	}
	resp, _ := env.do(t, "POST", path+"/connect", teacher, nil, "")
	if resp.StatusCode != 200 {
		t.Fatal("connect failed")
	}
	user, err := env.store.GetUserByUsername(ctx, "student_a1")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := env.store.CreateAgent(ctx, store.NewAgent{ClassID: 1, Name: "Audit tutor", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := env.store.CreateConversation(ctx, 1, user.ID, agent.ID, "Audit question")
	if err != nil {
		t.Fatal(err)
	}
	question, err := env.store.AppendMessage(ctx, store.NewMessage{ConversationID: conversation.ID, Role: "user", Content: "请提示公式"})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := env.store.ResolveMCPTool(ctx, 1, "campus-tools", "formula_hint")
	if err != nil {
		t.Fatal(err)
	}
	attempt := store.MCPCallAttempt{ClassID: 1, ServerID: server.ID, ToolName: "formula_hint", UserID: user.ID, ConversationID: conversation.ID, QuestionMessageID: question, CallID: "audit-ok", Round: 1}
	id, err := env.store.ReserveMCPCall(ctx, attempt, *candidate, 40)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.FinishMCPCall(ctx, 1, id, "ok", "请检查公式条件", 5); err != nil {
		t.Fatal(err)
	}
	attempt.CallID = "audit-error"
	if _, err := env.store.RecordMCPAttempt(ctx, attempt, "error", "参数未通过结构校验"); err != nil {
		t.Fatal(err)
	}
	attempt.CallID = "audit-denied"
	attempt.Round = 2
	if _, err := env.store.RecordMCPAttempt(ctx, attempt, "denied", "工具额度已耗尽"); err != nil {
		t.Fatal(err)
	}
	resp, raw := env.do(t, "GET", "/api/mcp/audit?page_size=2&class_id=2", teacher, nil, "")
	var page mcpAuditResponse
	if resp.StatusCode != 200 || json.Unmarshal(raw, &page) != nil || page.Total != 3 || len(page.Calls) != 2 || page.Calls[0].CallID != "audit-denied" || page.Calls[1].CallID != "audit-error" || strings.Contains(string(raw), "test-only-remote-token") {
		t.Fatal("audit order/class scope/summary invalid")
	}
	for _, status := range []string{"ok", "error", "denied"} {
		resp, raw = env.do(t, "GET", "/api/mcp/audit?status="+status, teacher, nil, "")
		_ = json.Unmarshal(raw, &page)
		if resp.StatusCode != 200 || page.Total != 1 || page.Calls[0].Status != status || page.Calls[0].QuestionMessageID != question || page.Calls[0].UserID != user.ID {
			t.Fatal("audit status/source not traceable")
		}
	}
	foreign, err := env.store.CreateMCPServer(ctx, 2, "foreign-tools", remote.URL, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{foreign.ID, 999999} {
		resp, _ = env.do(t, "GET", fmt.Sprintf("/api/mcp/audit?server_id=%d", id), teacher, nil, "")
		if resp.StatusCode != 404 {
			t.Fatal("foreign/nonexistent audit filter not 404")
		}
	}
	for _, query := range []string{"page=0", "page_size=101", "status=unknown", "server_id=-1"} {
		resp, _ = env.do(t, "GET", "/api/mcp/audit?"+query, teacher, nil, "")
		if resp.StatusCode != 400 {
			t.Fatal("invalid audit filter accepted")
		}
	}
}
