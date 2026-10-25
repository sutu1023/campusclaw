package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func mcpTestQuestion(t *testing.T, st *Store) (*Conversation, int64) {
	t.Helper()
	ctx := context.Background()
	var userID int64
	if err := st.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE username='student_a1'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	agent, err := st.CreateAgent(ctx, NewAgent{ClassID: 1, Name: "MCP test tutor", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := st.CreateConversation(ctx, 1, userID, agent.ID, "MCP test question")
	if err != nil {
		t.Fatal(err)
	}
	question, err := st.AppendMessage(ctx, NewMessage{ConversationID: conversation.ID, Role: "user", Content: "如何检查公式条件？"})
	if err != nil {
		t.Fatal(err)
	}
	return conversation, question
}

func TestMCPLegacyColumnMigrationPreservesData(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	conversation, question := mcpTestQuestion(t, st)
	skill, err := st.CreateSkill(ctx, NewSkill{ClassID: 1, Name: "旧技能", Trigger: "旧规则", Description: "原说明", Tools: []string{"knowledge_search"}, Source: "原文件\n\n原说明", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Recreate exactly the lesson-5 column shape, without resetting any rows.
	for _, ddl := range []string{`ALTER TABLE skills MODIFY COLUMN tools_json VARCHAR(255) NOT NULL DEFAULT ''`, `ALTER TABLE messages DROP COLUMN tool_calls_json`} {
		if _, err := st.DB.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := st.Migrate(ctx); err != nil {
			t.Fatalf("migration %d: %v", i, err)
		}
	}
	var toolsType, summaryType, nullable, roleType string
	if err := st.DB.QueryRowContext(ctx, `SELECT DATA_TYPE FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='skills' AND column_name='tools_json'`).Scan(&toolsType); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRowContext(ctx, `SELECT DATA_TYPE, IS_NULLABLE FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='messages' AND column_name='tool_calls_json'`).Scan(&summaryType, &nullable); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRowContext(ctx, `SELECT COLUMN_TYPE FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='messages' AND column_name='role'`).Scan(&roleType); err != nil {
		t.Fatal(err)
	}
	if toolsType != "text" || summaryType != "mediumtext" || nullable != "YES" || roleType != "enum('user','assistant')" {
		t.Fatalf("wrong migrated types: %s %s %s %s", toolsType, summaryType, nullable, roleType)
	}
	var oldSummaryNull bool
	if err := st.DB.QueryRowContext(ctx, `SELECT tool_calls_json IS NULL FROM messages WHERE id=?`, question).Scan(&oldSummaryNull); err != nil || !oldSummaryNull {
		t.Fatal("old message was backfilled with fake data")
	}
	messages, err := st.ListMessages(ctx, conversation.ID)
	if err != nil || len(messages) != 1 || string(messages[0].ToolCalls) != "[]" || messages[0].Content != "如何检查公式条件？" {
		t.Fatalf("old message incompatible: %+v, %v", messages, err)
	}
	oldSkill, err := st.GetSkill(ctx, 1, skill.ID)
	if err != nil || oldSkill.Source != skill.Source || len(oldSkill.Tools) != 1 {
		t.Fatal("migration changed old skill")
	}
	refs := []string{}
	for i := 0; i < 8; i++ {
		refs = append(refs, fmt.Sprintf("%s%d@%s", strings.Repeat("t", 63), i, strings.Repeat("s", 32)))
	}
	raw, _ := json.Marshal(refs)
	if len(raw) <= 255 {
		t.Fatal("fixture does not exceed legacy size")
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE skills SET tools_json=? WHERE id=?`, string(raw), skill.ID); err != nil {
		t.Fatalf("long references truncated/rejected: %v", err)
	}
	upgraded, err := st.GetSkill(ctx, 1, skill.ID)
	if err != nil || encodeTools(upgraded.Tools) != string(raw) {
		t.Fatal("long references not fully readable")
	}
	if _, err := st.GetConversation(ctx, 1, conversation.UserID, conversation.ID); err != nil {
		t.Fatal("conversation lost during upgrade")
	}
	materials, _, err := st.ListMaterials(ctx, 1, 1, 20, "")
	if err != nil || len(materials) != 3 {
		t.Fatal("old materials lost during upgrade")
	}
}

func TestMCPMessageSummaryRoundTripAndFreshSchema(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	conversation, _ := mcpTestQuestion(t, st)
	summary := json.RawMessage(`[{"call_id":"q1-call1","ref":"formula_hint@campus-tools","round":1,"status":"ok","text":"检查公式适用条件"}]`)
	if _, err := st.AppendMessage(ctx, NewMessage{ConversationID: conversation.ID, Role: "assistant", Content: "请先检查条件。", ToolCalls: summary}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := st.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := st.ListMessages(ctx, conversation.ID)
	if err != nil || len(messages) != 2 || string(messages[0].ToolCalls) != "[]" || string(messages[1].ToolCalls) != string(summary) {
		t.Fatalf("summary round trip failed: %+v %v", messages, err)
	}
	if _, err := st.AppendMessage(ctx, NewMessage{ConversationID: conversation.ID, Role: "tool", Content: "must not persist"}); err == nil {
		t.Fatal("migration widened trusted persisted roles")
	}
	window, err := st.RecentWindowMessages(ctx, conversation.ID, 6, 500)
	if err != nil || len(window) != 2 || window[1].Role != "assistant" {
		t.Fatalf("history contract changed: %v", err)
	}
}
