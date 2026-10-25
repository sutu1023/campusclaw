package mcp

import (
	"errors"
	"strings"
)

var ErrUnsafeData = errors.New("MCP data rejected by safety checks")

// CheckDefinitions inspects decoded schema strings as well as names and full
// descriptions, so JSON escaping cannot hide an exact credential echo. It
// never returns the matching value, source text or a diagnostic excerpt.
func CheckDefinitions(target Target, tools []Tool, otherSecrets ...string) error {
	if err := ValidateTools(tools); err != nil {
		return err
	}
	unsafe := func(text string) bool {
		if target.ContainsCredential(text) || (target.BaseURL() != "" && strings.Contains(text, target.BaseURL())) {
			return true
		}
		for _, secret := range otherSecrets {
			if secret != "" && strings.Contains(text, secret) {
				return true
			}
		}
		return false
	}
	var inspect func(any) bool
	inspect = func(value any) bool {
		switch v := value.(type) {
		case string:
			return unsafe(v)
		case map[string]any:
			for key, child := range v {
				if unsafe(key) || inspect(child) {
					return true
				}
			}
		case []any:
			for _, child := range v {
				if inspect(child) {
					return true
				}
			}
		}
		return false
	}
	for _, tool := range tools {
		if unsafe(tool.Name) || unsafe(tool.Description) {
			return ErrUnsafeData
		}
		value, err := decodeBounded(tool.InputSchema, MaxSchemaBytes)
		if err != nil || inspect(value) {
			return ErrUnsafeData
		}
	}
	return nil
}
