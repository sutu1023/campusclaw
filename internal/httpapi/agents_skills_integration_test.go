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

type agentJSON struct {
	ID          int64   `json:"id"`
	ClassID     int64   `json:"class_id"`
	Name        string  `json:"name"`
	Prompt      string  `json:"prompt"`
	Enabled     bool    `json:"enabled"`
	SkillIDs    []int64 `json:"skill_ids"`
	MaterialIDs []int64 `json:"material_ids"`
}

type skillJSON struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Trigger string   `json:"trigger"`
	Tools   []string `json:"tools"`
	Source  string   `json:"source"`
	Subject string   `json:"subject"`
	Enabled bool     `json:"enabled"`
}

func (e *testEnv) classSkills(t *testing.T, token string) []skillJSON {
	t.Helper()
	resp, data := e.do(t, http.MethodGet, "/api/skills", token, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list skills status %d body=%s", resp.StatusCode, data)
	}
	var out struct {
		Skills []skillJSON `json:"skills"`
	}
	_ = json.Unmarshal(data, &out)
	return out.Skills
}

func (e *testEnv) classMaterials(t *testing.T, token string) []struct {
	ID int64 `json:"id"`
} {
	t.Helper()
	resp, data := e.do(t, http.MethodGet, "/api/materials?page=1&page_size=100", token, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list materials status %d body=%s", resp.StatusCode, data)
	}
	var out struct {
		Materials []struct {
			ID int64 `json:"id"`
		} `json:"materials"`
	}
	_ = json.Unmarshal(data, &out)
	return out.Materials
}

func TestAgentEndpointsRBACAndIsolation(t *testing.T) {
	env := setupEnv(t)
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	student := env.login(t, "student_a1", "CampusClaw#2026")

	skills := env.classSkills(t, teacher)
	materials := env.classMaterials(t, teacher)
	if len(skills) == 0 || len(materials) == 0 {
		t.Fatal("expected seeded skills and materials")
	}

	body, _ := json.Marshal(map[string]any{
		"name": "数学助手", "prompt": "用本班数学资料引导学生。", "enabled": true,
		"skill_ids": []int64{skills[0].ID}, "material_ids": []int64{materials[0].ID},
	})
	resp, data := env.do(t, http.MethodPost, "/api/agents", teacher, body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("teacher create status %d body=%s", resp.StatusCode, data)
	}
	var created agentJSON
	_ = json.Unmarshal(data, &created)
	if created.ID == 0 || len(created.SkillIDs) != 1 || len(created.MaterialIDs) != 1 {
		t.Fatalf("unexpected created agent: %+v", created)
	}

	// Student write is forbidden and writes nothing.
	before := len(env.classAgents(t, student))
	studentBody, _ := json.Marshal(map[string]any{"name": "学生助手", "enabled": true})
	resp, _ = env.do(t, http.MethodPost, "/api/agents", student, studentBody, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("student create must be 403, got %d", resp.StatusCode)
	}
	if len(env.classAgents(t, student)) != before {
		t.Fatal("forbidden create changed agent count")
	}

	// Cross-class material binding is reported as not found.
	bToken := env.login(t, "student_b1", "CampusClaw#2026")
	bMaterials := env.classMaterials(t, bToken)
	if len(bMaterials) == 0 {
		t.Fatal("expected class B materials")
	}
	cross, _ := json.Marshal(map[string]any{
		"name": "越界助手", "enabled": true, "material_ids": []int64{bMaterials[0].ID},
	})
	resp, respBody := env.do(t, http.MethodPost, "/api/agents", teacher, cross, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-class binding must be 404, got %d body=%s", resp.StatusCode, respBody)
	}

	// Disabling hides the agent from students but not from the teacher.
	disabled := false
	update, _ := json.Marshal(map[string]any{
		"name": created.Name, "prompt": created.Prompt, "enabled": disabled,
		"skill_ids": created.SkillIDs, "material_ids": created.MaterialIDs,
	})
	resp, data = env.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", created.ID), teacher, update, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status %d body=%s", resp.StatusCode, data)
	}
	for _, a := range env.classAgents(t, student) {
		if a.ID == created.ID {
			t.Fatal("disabled agent must not appear in student list")
		}
	}
	resp, _ = env.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", created.ID), student, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("student read of disabled agent must be 404, got %d", resp.StatusCode)
	}
	resp, _ = env.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", created.ID), teacher, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("teacher read of disabled agent must be 200, got %d", resp.StatusCode)
	}

	// Cross-class read is 404 without leaking the name.
	resp, data = env.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", created.ID), bToken, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-class agent read must be 404, got %d", resp.StatusCode)
	}
	if bytes.Contains(data, []byte("数学助手")) {
		t.Fatalf("404 leaked agent name: %s", data)
	}
}

func (e *testEnv) classAgents(t *testing.T, token string) []agentJSON {
	t.Helper()
	resp, data := e.do(t, http.MethodGet, "/api/agents", token, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list agents status %d body=%s", resp.StatusCode, data)
	}
	var out struct {
		Agents []agentJSON `json:"agents"`
	}
	_ = json.Unmarshal(data, &out)
	return out.Agents
}

func TestSkillEndpointsParsingAndRBAC(t *testing.T) {
	env := setupEnv(t)
	teacher := env.login(t, "teacher_a", "CampusClaw#2026")
	student := env.login(t, "student_a1", "CampusClaw#2026")

	source := "名称：几何解题引导\n适用条件：三角形,全等\n资料：数学\n工具：knowledge_search\n启用：是\n\n只提示该用哪个判定，不写证明过程。\n"
	body, _ := json.Marshal(map[string]string{"source": source})
	resp, data := env.do(t, http.MethodPost, "/api/skills", teacher, body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create skill status %d body=%s", resp.StatusCode, data)
	}
	var created skillJSON
	_ = json.Unmarshal(data, &created)
	if created.Source != source || len(created.Tools) != 1 {
		t.Fatalf("unexpected created skill: %+v", created)
	}

	// Student write is forbidden.
	resp, _ = env.do(t, http.MethodPost, "/api/skills", student, body, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("student skill write must be 403, got %d", resp.StatusCode)
	}

	// Invalid file returns a readable 400.
	bad, _ := json.Marshal(map[string]string{"source": "适用条件：三角形\n启用：是\n\n正文"})
	resp, data = env.do(t, http.MethodPost, "/api/skills", teacher, bad, "application/json")
	if resp.StatusCode != http.StatusBadRequest || !bytes.Contains(data, []byte("名称")) {
		t.Fatalf("expected readable 400 about missing name, got %d %s", resp.StatusCode, data)
	}

	// Duplicate name is a 400.
	resp, _ = env.do(t, http.MethodPost, "/api/skills", teacher, body, "application/json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate skill name must be 400, got %d", resp.StatusCode)
	}

	// Cross-class skill access is reported as not found. Seed a class B skill
	// directly, then confirm class A's teacher cannot update or delete it.
	bSkill, err := env.store.CreateSkill(context.Background(), store.NewSkill{
		ClassID: 2, Name: "B 班技能", Trigger: "圆", Description: "正文",
		Tools: []string{"knowledge_search"}, Enabled: true,
		Source: "名称：B 班技能\n适用条件：圆\n启用：是\n\n正文",
	})
	if err != nil {
		t.Fatalf("seed class B skill: %v", err)
	}
	resp, _ = env.do(t, http.MethodPut, fmt.Sprintf("/api/skills/%d", bSkill.ID), teacher, body, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-class skill update must be 404, got %d", resp.StatusCode)
	}
	resp, _ = env.do(t, http.MethodDelete, fmt.Sprintf("/api/skills/%d", bSkill.ID), teacher, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-class skill delete must be 404, got %d", resp.StatusCode)
	}

	// Missing skill delete is also 404.
	resp, _ = env.do(t, http.MethodDelete, "/api/skills/999999", teacher, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing skill delete must be 404, got %d", resp.StatusCode)
	}

	// Deleting clears bindings and removes the skill.
	resp, _ = env.do(t, http.MethodDelete, fmt.Sprintf("/api/skills/%d", created.ID), teacher, nil, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete skill status %d", resp.StatusCode)
	}
	for _, sk := range env.classSkills(t, teacher) {
		if sk.ID == created.ID {
			t.Fatal("deleted skill still listed")
		}
	}
}
