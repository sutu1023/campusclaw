package toolrun

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"campusclaw/internal/mcp"
)

var instructionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+(instructions|rules|prompts)`),
	regexp.MustCompile(`(?i)(reveal|send|exfiltrate|print|expose)[\s\S]{0,80}(api[ _-]?key|token|credential|secret)`),
	regexp.MustCompile(`(忽略|无视).{0,12}(之前|以前|上述|先前|系统).{0,8}(指令|规则|要求|提示)`),
	regexp.MustCompile(`(获取|索取|外发|泄露|泄漏|发送|输出).{0,16}(密钥|凭据|令牌)`),
}

func limitRunes(text string, max int) string {
	runes := []rune(text)
	if len(runes) > max {
		return string(runes[:max])
	}
	return text
}

// filterResult inspects the complete response before truncation. A bad block
// rejects the entire result, not a selectively edited successful remainder.
func filterResult(result mcp.CallResult, target mcp.Target, secrets ...string) (string, error) {
	if result.IsError || len(result.Content) == 0 {
		return "", mcp.ErrUnsafeData
	}
	parts := make([]string, 0, len(result.Content))
	for _, part := range result.Content {
		if part.Type != "text" || !utf8.ValidString(part.Text) || strings.TrimSpace(part.Text) == "" {
			return "", mcp.ErrUnsafeData
		}
		parts = append(parts, part.Text)
	}
	text := strings.Join(parts, "\n")
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\ufeff' {
			return -1
		}
		return r
	}, text)
	if target.ContainsCredential(text) || target.ContainsCredential(compact) || (target.BaseURL() != "" && strings.Contains(compact, target.BaseURL())) {
		return "", mcp.ErrUnsafeData
	}
	for _, secret := range secrets {
		if secret != "" && (strings.Contains(text, secret) || strings.Contains(compact, secret)) {
			return "", mcp.ErrUnsafeData
		}
	}
	for _, pattern := range instructionPatterns {
		if pattern.MatchString(text) || pattern.MatchString(compact) {
			return "", mcp.ErrUnsafeData
		}
	}
	return limitRunes(strings.TrimSpace(text), 2000), nil
}
