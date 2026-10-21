package skill

import (
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	source := "名称：数学解题引导\n" +
		"# 这一行注释会被忽略\n" +
		"适用条件：方程,去括号,分数|通分\n" +
		"资料：数学\n" +
		"工具：knowledge_search\n" +
		"启用：是\n" +
		"\n" +
		"先看懂学生卡在哪一步，只指出下一步该检查什么。\n"

	p, err := Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Name != "数学解题引导" || p.Trigger != "方程,去括号,分数|通分" || p.Subject != "数学" {
		t.Fatalf("unexpected metadata: %+v", p)
	}
	if !p.Enabled {
		t.Fatal("启用：是 must parse to enabled")
	}
	if len(p.Tools) != 1 || p.Tools[0] != ToolKnowledgeSearch {
		t.Fatalf("unexpected tools: %v", p.Tools)
	}
	if !strings.Contains(p.Description, "只指出下一步") {
		t.Fatalf("unexpected description: %q", p.Description)
	}
	if p.Source != source {
		t.Fatal("source must be preserved verbatim")
	}
}

func TestParseValidMultipleTools(t *testing.T) {
	source := "名称：多工具\n适用条件：方程\n工具：knowledge_search，knowledge_search\n启用：否\n\n正文。\n"
	p, err := Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %v", p.Tools)
	}
	if p.Enabled {
		t.Fatal("启用：否 must parse to disabled")
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		wantSubstr string
	}{
		{"missing name", "适用条件：方程\n启用：是\n\n正文", "名称"},
		{"missing trigger", "名称：技能\n启用：是\n\n正文", "适用条件"},
		{"missing enabled", "名称：技能\n适用条件：方程\n\n正文", "启用"},
		{"bad enabled", "名称：技能\n适用条件：方程\n启用：也许\n\n正文", "启用"},
		{"duplicate field", "名称：技能\n名称：另一个\n适用条件：方程\n启用：是\n\n正文", "重复"},
		{"undefined field", "名称：技能\n适用条件：方程\n启用：是\n颜色：红\n\n正文", "未定义"},
		{"illegal tool", "名称：技能\n适用条件：方程\n工具：shell\n启用：是\n\n正文", "knowledge_search"},
		{"no body", "名称：技能\n适用条件：方程\n启用：是", "正文"},
		{"empty source", "", "为空"},
		{"oversize file", "名称：技能\n适用条件：方程\n启用：是\n\n" + strings.Repeat("字", MaxFileRunes), "上限"},
		{"name too long", "名称：" + strings.Repeat("名", MaxNameRunes+1) + "\n适用条件：方程\n启用：是\n\n正文", "名称"},
		{"trigger too long", "名称：技能\n适用条件：" + strings.Repeat("词", MaxTriggerRunes+1) + "\n启用：是\n\n正文", "适用条件"},
		{"subject too long", "名称：技能\n适用条件：方程\n资料：" + strings.Repeat("料", MaxSubjectRunes+1) + "\n启用：是\n\n正文", "资料"},
		{"description too long", "名称：技能\n适用条件：方程\n启用：是\n\n" + strings.Repeat("文", MaxDescriptionRunes+1), "正文"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.source)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstr)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}
