package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func classAMaterials(t *testing.T, ctx context.Context, st *Store) []Material {
	t.Helper()
	materials, _, err := st.ListMaterials(ctx, 1, 1, 100, "")
	if err != nil || len(materials) == 0 {
		t.Fatalf("expected class A materials: %v", err)
	}
	return materials
}

func TestSeedSkillsIdempotent(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	skills, err := st.ListSkills(ctx, 1)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 seeded skills, got %d", len(skills))
	}
	for _, sk := range skills {
		if sk.SeedKey == "" {
			t.Fatalf("seeded skill %q must carry a seed_key", sk.Name)
		}
		if sk.ClassID != 1 {
			t.Fatalf("seed skill leaked into class %d", sk.ClassID)
		}
	}

	if err := st.Seed(ctx); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	again, _ := st.ListSkills(ctx, 1)
	if len(again) != len(skills) {
		t.Fatalf("seed not idempotent: %d -> %d", len(skills), len(again))
	}
}

func TestAgentCreateAndUpdateBindings(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	skills, _ := st.ListSkills(ctx, 1)
	materials := classAMaterials(t, ctx, st)

	agent, err := st.CreateAgent(ctx, NewAgent{
		ClassID: 1, Name: "数学助手", Prompt: "用本班数学资料引导学生。", Enabled: true,
		SkillIDs: []int64{skills[0].ID}, MaterialIDs: []int64{materials[0].ID},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if len(agent.SkillIDs) != 1 || agent.SkillIDs[0] != skills[0].ID {
		t.Fatalf("skill binding did not round-trip: %v", agent.SkillIDs)
	}
	if len(agent.MaterialIDs) != 1 || agent.MaterialIDs[0] != materials[0].ID {
		t.Fatalf("material binding did not round-trip: %v", agent.MaterialIDs)
	}

	// Full replacement: unbind the skill, add no material.
	updated, err := st.UpdateAgent(ctx, 1, agent.ID, NewAgent{
		ClassID: 1, Name: "数学助手", Prompt: "改过的提示词", Enabled: true,
		SkillIDs: []int64{}, MaterialIDs: []int64{},
	})
	if err != nil {
		t.Fatalf("update agent: %v", err)
	}
	if len(updated.SkillIDs) != 0 || len(updated.MaterialIDs) != 0 {
		t.Fatalf("bindings were not replaced: %+v", updated)
	}
	if updated.Prompt != "改过的提示词" {
		t.Fatalf("prompt not updated: %q", updated.Prompt)
	}
}

func TestAgentCandidatesOnlyBoundAndEnabled(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	skills, _ := st.ListSkills(ctx, 1)
	agent, err := st.CreateAgent(ctx, NewAgent{
		ClassID: 1, Name: "候选集助手", Enabled: true,
		SkillIDs: []int64{skills[0].ID, skills[1].ID},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	candidates, err := st.ListAgentCandidateSkills(ctx, 1, agent.ID)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// Disabling a bound skill removes it from the candidate set immediately.
	if _, err := st.UpdateSkill(ctx, 1, skills[0].ID, NewSkill{
		ClassID: 1, Name: skills[0].Name, Trigger: skills[0].Trigger,
		Description: skills[0].Description, Tools: skills[0].Tools,
		Source: skills[0].Source, Subject: skills[0].Subject, Enabled: false,
	}); err != nil {
		t.Fatalf("disable skill: %v", err)
	}
	candidates, _ = st.ListAgentCandidateSkills(ctx, 1, agent.ID)
	if len(candidates) != 1 || candidates[0].ID != skills[1].ID {
		t.Fatalf("disabled skill still routable: %+v", candidates)
	}
}

func TestSkillSourceRoundTrip(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	source := "名称：方程解题引导\r\n适用条件：方程,移项\r\n工具：knowledge_search\r\n资料：数学\r\n启用：是\r\n\r\n先让学生说出等号两边要保持相等。\r\n"
	sk, err := st.CreateSkill(ctx, NewSkill{
		ClassID: 1, Name: "方程解题引导", Trigger: "方程,移项", Subject: "数学",
		Description: "先让学生说出等号两边要保持相等。", Tools: []string{"knowledge_search"},
		Source: source, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	got, err := st.GetSkill(ctx, 1, sk.ID)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if got.Source != source {
		t.Fatalf("source not preserved:\nwant %q\ngot  %q", source, got.Source)
	}
}

func TestDeleteSkillClearsBindings(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	skills, _ := st.ListSkills(ctx, 1)
	agent, err := st.CreateAgent(ctx, NewAgent{
		ClassID: 1, Name: "删除绑定助手", Enabled: true, SkillIDs: []int64{skills[0].ID},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := st.DeleteSkill(ctx, 1, skills[0].ID); err != nil {
		t.Fatalf("delete skill: %v", err)
	}
	var n int
	if err := st.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tutor_agent_skills WHERE skill_id=?`, skills[0].ID).Scan(&n); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bindings after delete, got %d", n)
	}
	got, err := st.GetAgent(ctx, 1, agent.ID, false)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if len(got.SkillIDs) != 0 {
		t.Fatalf("agent still lists deleted skill: %v", got.SkillIDs)
	}
}

func TestBindingRejectsCrossClassResources(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	classB, _, err := st.ListMaterials(ctx, 2, 1, 10, "")
	if err != nil || len(classB) == 0 {
		t.Fatalf("expected class B material: %v", err)
	}
	_, err = st.CreateAgent(ctx, NewAgent{
		ClassID: 1, Name: "越界助手", Enabled: true, MaterialIDs: []int64{classB[0].ID},
	})
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-class binding, got %v", err)
	}
}

func TestConversationAndWindow(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	user, err := st.GetUserByUsername(ctx, "student_a1")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	agent, err := st.CreateAgent(ctx, NewAgent{ClassID: 1, Name: "会话助手", Enabled: true})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	conv, err := st.CreateConversation(ctx, 1, user.ID, agent.ID, "新的提问")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if conv.Title != "新的提问" || conv.AgentID != agent.ID {
		t.Fatalf("unexpected conversation: %+v", conv)
	}

	for i := 0; i < 8; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		content := strings.Repeat("字", 10)
		if i == 7 {
			content = strings.Repeat("长", 600)
		}
		if _, err := st.AppendMessage(ctx, NewMessage{ConversationID: conv.ID, Role: role, Content: content}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	window, err := st.RecentWindowMessages(ctx, conv.ID, 6, 500)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if len(window) != 6 {
		t.Fatalf("expected 6 window messages, got %d", len(window))
	}
	last := window[len(window)-1]
	if len([]rune(last.Content)) != 500 {
		t.Fatalf("expected last message truncated to 500 runes, got %d", len([]rune(last.Content)))
	}
	// Skipped invalid rows are not part of this test's data; assert order.
	for i := 1; i < len(window); i++ {
		if window[i-1].ID >= window[i].ID {
			t.Fatalf("window not in write order: %d then %d", window[i-1].ID, window[i].ID)
		}
	}
}

func TestConversationDeleteCascadesMessages(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	user, _ := st.GetUserByUsername(ctx, "student_a1")
	agent, _ := st.CreateAgent(ctx, NewAgent{ClassID: 1, Name: "级联助手", Enabled: true})
	conv, _ := st.CreateConversation(ctx, 1, user.ID, agent.ID, "新的提问")
	if _, err := st.AppendMessage(ctx, NewMessage{ConversationID: conv.ID, Role: "user", Content: "你好"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if _, err := st.DB.ExecContext(ctx, `DELETE FROM conversations WHERE id=?`, conv.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	var n int
	if err := st.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE conversation_id=?`, conv.ID).Scan(&n); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 messages after cascade, got %d", n)
	}
}

func TestConversationOwnershipIsolation(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	owner, _ := st.GetUserByUsername(ctx, "student_a1")
	other, _ := st.GetUserByUsername(ctx, "teacher_a")
	agent, _ := st.CreateAgent(ctx, NewAgent{ClassID: 1, Name: "归属助手", Enabled: true})
	conv, _ := st.CreateConversation(ctx, 1, owner.ID, agent.ID, "新的提问")

	if _, err := st.GetConversation(ctx, 1, other.ID, conv.ID); err != ErrNotFound {
		t.Fatalf("other user must get ErrNotFound, got %v", err)
	}
	if _, err := st.GetConversation(ctx, 2, owner.ID, conv.ID); err != ErrNotFound {
		t.Fatalf("cross-class must get ErrNotFound, got %v", err)
	}
	if _, err := st.GetConversation(ctx, 1, owner.ID, conv.ID); err != nil {
		t.Fatalf("owner read failed: %v", err)
	}
}

func TestCrossClassAgentSkillNotFound(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	agent, _ := st.CreateAgent(ctx, NewAgent{ClassID: 1, Name: "隔离助手", Enabled: true})
	if _, err := st.GetAgent(ctx, 2, agent.ID, false); err != ErrNotFound {
		t.Fatalf("cross-class agent must be ErrNotFound, got %v", err)
	}
	skills, _ := st.ListSkills(ctx, 1)
	if _, err := st.GetSkill(ctx, 2, skills[0].ID); err != ErrNotFound {
		t.Fatalf("cross-class skill must be ErrNotFound, got %v", err)
	}
	// A disabled agent is invisible to the student view.
	if _, err := st.UpdateAgent(ctx, 1, agent.ID, NewAgent{ClassID: 1, Name: "隔离助手", Enabled: false}); err != nil {
		t.Fatalf("disable agent: %v", err)
	}
	if _, err := st.GetAgent(ctx, 1, agent.ID, true); err != ErrNotFound {
		t.Fatalf("disabled agent must be ErrNotFound for students, got %v", err)
	}
	if _, err := st.GetAgent(ctx, 1, agent.ID, false); err != nil {
		t.Fatalf("teacher must still read a disabled agent: %v", err)
	}
}

func TestMessageCitationsDefaultEmptyArray(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	user, _ := st.GetUserByUsername(ctx, "student_a1")
	agent, _ := st.CreateAgent(ctx, NewAgent{ClassID: 1, Name: "引用助手", Enabled: true})
	conv, _ := st.CreateConversation(ctx, 1, user.ID, agent.ID, "新的提问")

	if _, err := st.AppendMessage(ctx, NewMessage{ConversationID: conv.ID, Role: "assistant", Content: "回答"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, _ := st.ListMessages(ctx, conv.ID)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if string(msgs[0].Citations) != "[]" {
		t.Fatalf("expected empty-array citations, got %q", msgs[0].Citations)
	}
	// Confirm the stored JSON is valid.
	var arr []any
	if err := json.Unmarshal(msgs[0].Citations, &arr); err != nil {
		t.Fatalf("citations not valid JSON: %v", err)
	}
}
