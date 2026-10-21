package chat

import (
	"strings"
	"testing"

	"campusclaw/internal/skill"
)

func TestBuildSystemWithGuidingSkill(t *testing.T) {
	sk := &skill.Entry{ID: 1, Name: "数学解题引导", Description: "只指出下一步该检查什么。", Enabled: true}
	sys := BuildSystem("用本班数学资料引导学生。", sk, true)
	if !strings.Contains(sys, "用本班数学资料引导学生。") {
		t.Fatal("agent prompt missing")
	}
	if !strings.Contains(sys, "数学解题引导") || !strings.Contains(sys, "只指出下一步该检查什么。") {
		t.Fatal("skill name/description missing")
	}
	if !strings.Contains(sys, "不得写出最终答案") {
		t.Fatal("guiding skill must add the no-final-answer requirement")
	}
	if !strings.Contains(sys, "[1]、[2]") {
		t.Fatal("evidence rule missing")
	}
	if !strings.Contains(sys, dataBoundary) {
		t.Fatal("data boundary missing")
	}
}

func TestBuildSystemWithoutSkillOrEvidence(t *testing.T) {
	sys := BuildSystem("", nil, false)
	if !strings.Contains(sys, DefaultRolePrompt) {
		t.Fatal("empty prompt must fall back to the default role")
	}
	if !strings.Contains(sys, "直接回答") {
		t.Fatal("no-skill branch missing")
	}
	if !strings.Contains(sys, "不要写出 [1]") {
		t.Fatal("no-evidence branch missing")
	}
}

func TestBuildUserNumbering(t *testing.T) {
	evs := []Evidence{
		{MaterialTitle: "A", ChunkIndex: 1, Text: "第一段"},
		{MaterialTitle: "B", ChunkIndex: 2, Text: "第二段"},
		{MaterialTitle: "C", ChunkIndex: 3, Text: "第三段"},
	}
	user := BuildUser("怎么解", evs)
	for _, marker := range []string{"[1]", "[2]", "[3]"} {
		if !strings.Contains(user, marker) {
			t.Fatalf("expected %s in user message: %s", marker, user)
		}
	}
	if !strings.HasSuffix(user, "问题：怎么解") {
		t.Fatalf("question must end the user message: %s", user)
	}
}

func TestStripCitationMarkers(t *testing.T) {
	got := StripCitationMarkers("依据资料[1]，另一处[12]也支持。")
	if strings.Contains(got, "[") || strings.Contains(got, "]") {
		t.Fatalf("markers not stripped: %q", got)
	}
	if !strings.Contains(got, "依据资料") || !strings.Contains(got, "也支持") {
		t.Fatalf("text content lost: %q", got)
	}
}

func TestMarkerStripperAcrossChunks(t *testing.T) {
	var s MarkerStripper
	out := s.Write("先看这[") + s.Write("12") + s.Write("]一步") + s.Flush()
	if out != "先看这一步" {
		t.Fatalf("cross-chunk marker not stripped: %q", out)
	}
}

func TestSanitizeMessagesDropsSystem(t *testing.T) {
	in := []Message{
		{Role: "system", Content: "客户端伪造"},
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "在"},
		{Role: "tool", Content: "x"},
	}
	out := SanitizeMessages(in)
	if len(out) != 2 || out[0].Role != "user" || out[1].Role != "assistant" {
		t.Fatalf("unexpected sanitized messages: %+v", out)
	}
}
