// Package skill parses CampusClaw skill files and routes a question to at most
// one candidate skill using deterministic lexical scoring.
//
// A skill file is plain text: metadata key lines above the first blank line,
// the guidance body below it. Metadata keys are 名称, 适用条件, 资料, 工具 and
// 启用. Lines starting with '#' and blank lines in the metadata section are
// ignored.
package skill

import (
	"fmt"
	"strings"
)

// Limits are the per-field size caps from the specification.
const (
	MaxFileRunes        = 8000
	MaxNameRunes        = 32
	MaxTriggerRunes     = 120
	MaxSubjectRunes     = 32
	MaxDescriptionRunes = 2000

	// ToolKnowledgeSearch is the backward-compatible built-in reference.
	ToolKnowledgeSearch = "knowledge_search"
)

// Parsed is the validated result of parsing a skill file.
type Parsed struct {
	Name        string
	Trigger     string
	Subject     string
	Tools       []string
	Enabled     bool
	Description string
	Source      string
}

var allowedKeys = map[string]bool{
	"名称": true, "适用条件": true, "资料": true, "工具": true, "启用": true,
}

// Parse validates and splits a skill file. Any rule violation returns an error
// whose message is safe to show a teacher.
func Parse(source string) (*Parsed, error) {
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("技能文件为空")
	}
	if runeLen(source) > MaxFileRunes {
		return nil, fmt.Errorf("技能文件超过 %d 字上限", MaxFileRunes)
	}

	meta, body := splitAtFirstBlank(source)
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("空行以下缺少技能正文")
	}

	fields := map[string]string{}
	for _, raw := range strings.Split(meta, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := splitKeyValue(trimmed)
		if !ok {
			return nil, fmt.Errorf("无法解析的字段行：%q", trimmed)
		}
		if !allowedKeys[key] {
			return nil, fmt.Errorf("未定义字段：%q", key)
		}
		if _, dup := fields[key]; dup {
			return nil, fmt.Errorf("字段重复出现：%q", key)
		}
		fields[key] = strings.TrimSpace(value)
	}

	name, ok := fields["名称"]
	if !ok || name == "" {
		return nil, fmt.Errorf("缺少必填字段：名称")
	}
	trigger, ok := fields["适用条件"]
	if !ok || trigger == "" {
		return nil, fmt.Errorf("缺少必填字段：适用条件")
	}
	enabledRaw, ok := fields["启用"]
	if !ok || enabledRaw == "" {
		return nil, fmt.Errorf("缺少必填字段：启用")
	}
	enabled, err := parseEnabled(enabledRaw)
	if err != nil {
		return nil, err
	}

	if runeLen(name) > MaxNameRunes {
		return nil, fmt.Errorf("名称超过 %d 字上限", MaxNameRunes)
	}
	if runeLen(trigger) > MaxTriggerRunes {
		return nil, fmt.Errorf("适用条件超过 %d 字上限", MaxTriggerRunes)
	}
	subject := fields["资料"]
	if runeLen(subject) > MaxSubjectRunes {
		return nil, fmt.Errorf("资料超过 %d 字上限", MaxSubjectRunes)
	}
	body = strings.TrimSpace(body)
	if runeLen(body) > MaxDescriptionRunes {
		return nil, fmt.Errorf("技能正文超过 %d 字上限", MaxDescriptionRunes)
	}
	tools, err := parseTools(fields["工具"])
	if err != nil {
		return nil, err
	}

	return &Parsed{
		Name:        name,
		Trigger:     trigger,
		Subject:     subject,
		Tools:       tools,
		Enabled:     enabled,
		Description: body,
		Source:      source,
	}, nil
}

// splitAtFirstBlank returns the metadata and body either side of the first
// blank line (\n\n or \r\n\r\n).
func splitAtFirstBlank(source string) (string, string) {
	idx := -1
	sepLen := 0
	for _, sep := range []string{"\n\n", "\r\n\r\n"} {
		if i := strings.Index(source, sep); i >= 0 && (idx < 0 || i < idx) {
			idx = i
			sepLen = len(sep)
		}
	}
	if idx < 0 {
		return source, ""
	}
	return source[:idx], source[idx+sepLen:]
}

func splitKeyValue(line string) (string, string, bool) {
	for _, sep := range []string{"：", ":"} {
		if i := strings.Index(line, sep); i >= 0 {
			return strings.TrimSpace(line[:i]), line[i+len(sep):], true
		}
	}
	return "", "", false
}

func parseEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "是", "true", "1", "on", "yes", "y":
		return true, nil
	case "否", "false", "0", "off", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("启用只能为是或否，得到 %q", raw)
	}
}

func parseTools(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}, nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '|'
	}) {
		tool := strings.TrimSpace(part)
		if tool == "" {
			continue
		}
		if _, err := ParseToolReference(tool); err != nil {
			return nil, err
		}
		if seen[tool] {
			continue
		}
		seen[tool] = true
		out = append(out, tool)
		if len(out) > MaxTools {
			return nil, fmt.Errorf("工具去重后不能超过 %d 项", MaxTools)
		}
	}
	return out, nil
}

func runeLen(s string) int { return len([]rune(s)) }
