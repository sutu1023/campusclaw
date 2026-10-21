package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"campusclaw/internal/store"
)

type conversationJSON struct {
	ID      int64  `json:"id"`
	ClassID int64  `json:"class_id"`
	UserID  int64  `json:"user_id"`
	AgentID int64  `json:"agent_id"`
	Title   string `json:"title"`
}

func (e *testEnv) createAgent(t *testing.T, token, name string, enabled bool) agentJSON {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "enabled": enabled, "prompt": ""})
	resp, data := e.do(t, http.MethodPost, "/api/agents", token, body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent status %d body=%s", resp.StatusCode, data)
	}
	var a agentJSON
	_ = json.Unmarshal(data, &a)
	return a
}

func TestCreateConversationValidatesAgent(t *testing.T) {
	env := setupEnv(t)
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	student := env.login(t, "student_a1", "CampusClaw#2026")
	studentB := env.login(t, "student_b1", "CampusClaw#2026")

	agent := env.createAgent(t, teacher, "会话助手", true)

	// Valid creation.
	body, _ := json.Marshal(map[string]any{"agent_id": agent.ID})
	resp, data := env.do(t, http.MethodPost, "/api/conversations", student, body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create conversation status %d body=%s", resp.StatusCode, data)
	}
	var conv conversationJSON
	_ = json.Unmarshal(data, &conv)
	if conv.Title != placeholderTitle || conv.AgentID != agent.ID {
		t.Fatalf("unexpected conversation: %+v", conv)
	}

	// Unknown agent id -> 404.
	unknown, _ := json.Marshal(map[string]any{"agent_id": 999999})
	resp, unknownBody := env.do(t, http.MethodPost, "/api/conversations", student, unknown, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown agent must be 404, got %d", resp.StatusCode)
	}

	// Cross-class agent id -> 404 with an identical response.
	bAgent, err := env.store.CreateAgent(context.Background(), store.NewAgent{ClassID: 2, Name: "B 班助手", Enabled: true})
	if err != nil {
		t.Fatalf("seed class B agent: %v", err)
	}
	cross, _ := json.Marshal(map[string]any{"agent_id": bAgent.ID})
	resp, crossBody := env.do(t, http.MethodPost, "/api/conversations", student, cross, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-class agent must be 404, got %d", resp.StatusCode)
	}
	if !bytes.Equal(unknownBody, crossBody) {
		t.Fatalf("cross-class and unknown responses must be identical: %s vs %s", unknownBody, crossBody)
	}

	// Disabled agent -> 404.
	disabled := env.createAgent(t, teacher, "停用助手", false)
	disBody, _ := json.Marshal(map[string]any{"agent_id": disabled.ID})
	resp, _ = env.do(t, http.MethodPost, "/api/conversations", student, disBody, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled agent must be 404, got %d", resp.StatusCode)
	}

	// A class with no enabled agent -> 400. Disable the class B agent first so
	// the class genuinely has none.
	if _, err := env.store.UpdateAgent(context.Background(), 2, bAgent.ID,
		store.NewAgent{ClassID: 2, Name: "B 班助手", Enabled: false}); err != nil {
		t.Fatalf("disable class B agent: %v", err)
	}
	resp, data = env.do(t, http.MethodPost, "/api/conversations", studentB, body, "application/json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("class with no enabled agent must be 400, got %d body=%s", resp.StatusCode, data)
	}
}

func TestConversationOwnershipAndIsolation(t *testing.T) {
	env := setupEnv(t)
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	student := env.login(t, "student_a1", "CampusClaw#2026")
	studentB := env.login(t, "student_b1", "CampusClaw#2026")

	agent := env.createAgent(t, teacher, "归属助手", true)
	body, _ := json.Marshal(map[string]any{"agent_id": agent.ID})
	resp, data := env.do(t, http.MethodPost, "/api/conversations", student, body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status %d", resp.StatusCode)
	}
	var conv conversationJSON
	_ = json.Unmarshal(data, &conv)

	// Owner sees it; others do not.
	resp, data = env.do(t, http.MethodGet, "/api/conversations", student, nil, "")
	if resp.StatusCode != http.StatusOK || !bytes.Contains(data, []byte(fmt.Sprintf(`"id":%d`, conv.ID))) {
		t.Fatalf("owner list missing conversation: %d %s", resp.StatusCode, data)
	}
	for _, tok := range []string{teacher, studentB} {
		resp, data = env.do(t, http.MethodGet, "/api/conversations", tok, nil, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list status %d", resp.StatusCode)
		}
		if bytes.Contains(data, []byte(fmt.Sprintf(`"id":%d`, conv.ID))) {
			t.Fatalf("conversation leaked into another user's list: %s", data)
		}
	}

	// Owner can read it; cross-user and cross-class are 404.
	resp, _ = env.do(t, http.MethodGet, fmt.Sprintf("/api/conversations/%d", conv.ID), student, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner read must be 200, got %d", resp.StatusCode)
	}
	_, crossUser := env.do(t, http.MethodGet, fmt.Sprintf("/api/conversations/%d", conv.ID), teacher, nil, "")
	resp, crossUser2 := env.do(t, http.MethodGet, fmt.Sprintf("/api/conversations/%d", conv.ID), studentB, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-class read must be 404, got %d", resp.StatusCode)
	}
	_, missing := env.do(t, http.MethodGet, "/api/conversations/999999", student, nil, "")
	if !bytes.Equal(crossUser, missing) || !bytes.Equal(crossUser2, missing) {
		t.Fatalf("cross-user/cross-class/missing bodies must be identical: %s | %s | %s", crossUser, crossUser2, missing)
	}

	// Unauthenticated access is 401.
	resp, _ = env.do(t, http.MethodGet, "/api/conversations", "", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated must be 401, got %d", resp.StatusCode)
	}
}

func TestConversationTitleAndOrder(t *testing.T) {
	env := setupEnv(t)
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	student := env.login(t, "student_a1", "CampusClaw#2026")

	agent := env.createAgent(t, teacher, "标题助手", true)
	conv1 := env.newConversation(t, student, agent.ID)
	env.newConversation(t, student, agent.ID)
	if conv1.Title != placeholderTitle {
		t.Fatalf("new conversation must use the placeholder, got %q", conv1.Title)
	}

	question := "这是一个超过二十四个字的首轮提问用于验证标题截断逻辑正确"
	resp, raw := env.ask(t, student, conv1.ID, question)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ask status %d body=%s", resp.StatusCode, raw)
	}

	resp, data := env.do(t, http.MethodGet, "/api/conversations", student, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status %d", resp.StatusCode)
	}
	var out struct {
		Conversations []conversationJSON `json:"conversations"`
	}
	_ = json.Unmarshal(data, &out)
	if len(out.Conversations) < 2 {
		t.Fatalf("expected at least 2 conversations, got %d", len(out.Conversations))
	}
	// The conversation just asked in must sort first (most recent update).
	if out.Conversations[0].ID != conv1.ID {
		t.Fatalf("recently asked conversation must sort first, got order %+v", out.Conversations)
	}
	if got := out.Conversations[0].Title; len([]rune(got)) != 24 {
		t.Fatalf("title must be the first 24 runes, got %q (%d)", got, len([]rune(got)))
	}
}

func TestTitleFromQuestion(t *testing.T) {
	if got := titleFromQuestion("   "); got != placeholderTitle {
		t.Fatalf("blank question must keep placeholder, got %q", got)
	}
	if got := titleFromQuestion("移项怎么做"); got != "移项怎么做" {
		t.Fatalf("short question title mismatch: %q", got)
	}
	long := ""
	for i := 0; i < 30; i++ {
		long += "字"
	}
	got := titleFromQuestion(long)
	if len([]rune(got)) != 24 {
		t.Fatalf("expected 24-rune title, got %d (%q)", len([]rune(got)), got)
	}
}
