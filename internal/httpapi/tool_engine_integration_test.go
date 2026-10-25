package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/chat"
	"campusclaw/internal/mcp"
	"campusclaw/internal/skill"
	"campusclaw/internal/store"
	"campusclaw/internal/toolrun"
)

type oneActionGateway struct{ alias string }

func (g oneActionGateway) DecideTools(_ context.Context, messages []chat.ProtocolMessage, _ []chat.ToolDefinition) (chat.ProtocolMessage, error) {
	for _, msg := range messages {
		if msg.Role == "tool" {
			return chat.ProtocolMessage{Role: "assistant", Content: "stop"}, nil
		}
	}
	return chat.HostAssistantCalls([]chat.ToolCall{{ID: "model_call", Type: "function", Function: chat.FunctionCall{Name: g.alias, Arguments: `{"query":"只检查公式适用条件"}`}}}), nil
}

func TestToolEngineConcurrentFinalCreditAndRestartRemainBounded(t *testing.T) {
	origin, token := os.Getenv("TEST_MCP_URL"), os.Getenv("TEST_MCP_TOKEN")
	if origin == "" || token == "" {
		t.Skip("real MCP environment required")
	}
	env := setupEnv(t)
	ctx := context.Background()
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: origin, TokenEnv: "MCP_ENGINE_TOKEN"}}, map[string]string{"MCP_ENGINE_TOKEN": token})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, origin)
	connect := func() {
		t.Helper()
		resp, _ := env.do(t, "POST", fmt.Sprintf("/api/mcp/servers/%d/connect", server.ID), teacher, nil, "")
		if resp.StatusCode != 200 {
			t.Fatal("real connection failed")
		}
	}
	connect()
	student, err := env.store.GetUserByUsername(ctx, "student_a1")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := env.store.CreateAgent(ctx, store.NewAgent{ClassID: 1, Name: "Concurrent engine", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := env.store.CreateConversation(ctx, 1, student.ID, agent.ID, "Quota test")
	if err != nil {
		t.Fatal(err)
	}
	newTurn := func() toolrun.Turn {
		t.Helper()
		id, err := env.store.AppendMessage(ctx, store.NewMessage{ConversationID: conv.ID, Role: "user", Content: "公式条件"})
		if err != nil {
			t.Fatal(err)
		}
		return toolrun.Turn{ClassID: 1, UserID: student.ID, ConversationID: conv.ID, QuestionMessageID: id, Skill: &skill.Entry{Tools: []string{"formula_hint@campus-tools"}}, Messages: []chat.Message{{Role: "user", Content: "公式条件"}}}
	}
	env.api.MCP.MaxCallsPerConversation = 1
	engine := &toolrun.Engine{Store: env.store, Config: env.api.MCP, Gateway: oneActionGateway{alias: chat.ToolAlias(server.ID, "formula_hint")}}
	first, second := newTurn(), newTurn()
	start := make(chan struct{})
	done := make(chan *toolrun.Plan, 2)
	for _, turn := range []toolrun.Turn{first, second} {
		go func(turn toolrun.Turn) { <-start; done <- engine.Run(ctx, turn, nil) }(turn)
	}
	close(start)
	statuses := map[string]int{}
	for i := 0; i < 2; i++ {
		select {
		case p := <-done:
			if len(p.Calls) != 1 {
				t.Fatal("unexpected extra decisions after cumulative quota")
			}
			statuses[p.Calls[0].Status]++
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent quota deadlocked")
		}
	}
	if statuses["ok"] != 1 || statuses["denied"] != 1 {
		t.Fatal("last conversation credit granted more than once")
	}
	fresh, err := store.Open(ctx, os.Getenv("TEST_DB_DSN"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := fresh.InvalidateMCPConnections(ctx); err != nil {
		t.Fatal(err)
	}
	connect() // startup requires a new connection; it must not reset usage
	engine.Store = fresh
	p := engine.Run(ctx, newTurn(), nil)
	if len(p.Calls) != 1 || p.Calls[0].Status != "denied" || !strings.Contains(p.Calls[0].Text, "累计") {
		t.Fatal("fresh pool/startup invalidation reset conversation usage")
	}
	used, err := fresh.MCPConversationUsage(ctx, 1, student.ID, conv.ID)
	if err != nil || used != 1 {
		t.Fatal("persistent quota changed after restart path")
	}
	_, total, err := fresh.ListMCPAudit(ctx, 1, 1, 20, server.ID, "")
	if err != nil || total != 3 {
		t.Fatal("concurrent/restarted attempts lost audit evidence")
	}
}

func TestToolEngineRealMCPAndTransactionalAuditBoundary(t *testing.T) {
	origin, token := os.Getenv("TEST_MCP_URL"), os.Getenv("TEST_MCP_TOKEN")
	if origin == "" || token == "" {
		t.Skip("TEST_MCP_URL / TEST_MCP_TOKEN required for real MCP execution")
	}
	env := setupEnv(t)
	ctx := context.Background()
	configureTestMCP(t, env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: origin, TokenEnv: "MCP_ENGINE_TOKEN"}}, map[string]string{"MCP_ENGINE_TOKEN": token})
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	server := registerMCPHTTP(t, env, teacher, origin)
	target, err := env.api.MCP.Registry.Resolve(1, "campus-tools", origin)
	if err != nil {
		t.Fatal("target unavailable")
	}
	client, err := mcp.NewClient(target, env.api.MCP.RequestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := client.Discover(ctx)
	if err != nil {
		t.Fatal("real MCP discovery failed")
	}
	connect := func() {
		t.Helper()
		op, err := env.store.BeginMCPConnect(ctx, 1, server.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.store.CompleteMCPDiscovery(ctx, 1, server.ID, op.Generation, tools, 1); err != nil {
			t.Fatal(err)
		}
	}
	connect()
	student, err := env.store.GetUserByUsername(ctx, "student_a1")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := env.store.CreateAgent(ctx, store.NewAgent{ClassID: 1, Name: "Engine test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := env.store.CreateConversation(ctx, 1, student.ID, agent.ID, "Engine test")
	if err != nil {
		t.Fatal(err)
	}
	engine := &toolrun.Engine{Store: env.store, Config: env.api.MCP}
	newTurn := func() toolrun.Turn {
		t.Helper()
		question, err := env.store.AppendMessage(ctx, store.NewMessage{ConversationID: conv.ID, Role: "user", Content: "勾股定理适用条件"})
		if err != nil {
			t.Fatal(err)
		}
		return toolrun.Turn{ClassID: 1, UserID: student.ID, ConversationID: conv.ID, QuestionMessageID: question, Skill: &skill.Entry{Tools: []string{"formula_hint@campus-tools"}}, Messages: []chat.Message{{Role: "user", Content: "勾股定理适用条件"}}}
	}
	call := func(p *toolrun.Plan, args string) (toolrun.CallSummary, chat.ProtocolMessage) {
		t.Helper()
		if len(p.Tools) != 1 {
			t.Fatal("real directory unavailable")
		}
		return engine.Execute(ctx, p, chat.ToolCall{ID: "model-call", Type: "function", Function: chat.FunctionCall{Name: p.Tools[0].Function.Name, Arguments: args}}, 1)
	}
	p := engine.Prepare(ctx, newTurn(), nil)
	success, msg := call(p, `{"query":"先核对直角条件与边的角色"}`)
	if success.Status != "ok" || msg.Role != "tool" || strings.Contains(msg.Content, token) {
		t.Fatal("real MCP did not produce safe successful result")
	}
	p = engine.Prepare(ctx, newTurn(), nil)
	bad, _ := call(p, `{"query":3}`)
	if bad.Status != "error" {
		t.Fatal("bad arguments were not a local error")
	}
	p = engine.Prepare(ctx, newTurn(), nil)
	if err := env.store.DisconnectMCPServer(ctx, 1, server.ID); err != nil {
		t.Fatal(err)
	}
	denied, _ := call(p, `{"query":"valid"}`)
	if denied.Status != "denied" {
		t.Fatal("stale prepared snapshot executed after disconnect")
	}
	usage, err := env.store.MCPConversationUsage(ctx, 1, student.ID, conv.ID)
	if err != nil || usage != 1 {
		t.Fatal("local failure consumed quota or success was not reserved")
	}
	for _, status := range []string{"ok", "error", "denied"} {
		rows, total, err := env.store.ListMCPAudit(ctx, 1, 1, 20, server.ID, status)
		if err != nil || total != 1 || len(rows) != 1 || rows[0].ConversationID != conv.ID || rows[0].UserID != student.ID || rows[0].Round != 1 || strings.Contains(rows[0].Text, token) {
			t.Fatal("audit outcome/source incorrect")
		}
	}
	connect()
	p = engine.Prepare(ctx, newTurn(), nil)
	if _, err := env.store.DB.ExecContext(ctx, `ALTER TABLE mcp_tool_calls ADD CONSTRAINT mcp_engine_no_interrupted CHECK (safe_text NOT LIKE 'interrupted:%')`); err != nil {
		t.Fatal(err)
	}
	defer env.store.DB.ExecContext(ctx, `ALTER TABLE mcp_tool_calls DROP CHECK mcp_engine_no_interrupted`)
	failed, _ := call(p, `{"query":"valid"}`)
	if failed.Status != "error" || !strings.Contains(failed.Text, "未执行") {
		t.Fatal("audit prewrite failure allowed execution")
	}
	usage, err = env.store.MCPConversationUsage(ctx, 1, student.ID, conv.ID)
	if err != nil || usage != 1 {
		t.Fatal("failed audit transaction consumed quota")
	}
	_, total, err := env.store.ListMCPAudit(ctx, 1, 1, 20, server.ID, "")
	if err != nil || total != 3 {
		t.Fatal("prewrite failure fabricated an audit success")
	}
}
