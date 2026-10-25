package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	MaxTools         = 64
	MaxSchemaBytes   = 64 * 1024
	MaxDescription   = 2000
	MaxArgumentBytes = 16 * 1024
	MaxJSONDepth     = 16
)

var (
	ErrDefinitions = errors.New("invalid or oversized MCP tool definitions")
	ErrSchema      = errors.New("MCP input schema is unsupported or invalid")
	ErrArguments   = errors.New("MCP arguments do not match the approved input schema or limits")
)

// decodeBounded rejects duplicate keys, invalid UTF-8, trailing values and deep
// JSON before it can reach the Schema library or be sent to another process.
func decodeBounded(raw []byte, maxBytes int) (any, error) {
	if len(raw) == 0 || len(raw) > maxBytes || !utf8.Valid(raw) {
		return nil, ErrArguments
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := readJSONValue(d, 1)
	if err != nil {
		return nil, ErrArguments
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrArguments
	}
	return v, nil
}

func readJSONValue(d *json.Decoder, depth int) (any, error) {
	if depth > MaxJSONDepth {
		return nil, ErrArguments
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			keyToken, err := d.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return nil, ErrArguments
			}
			if _, exists := m[key]; exists {
				return nil, ErrArguments
			}
			v, err := readJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, ErrArguments
		}
		return m, nil
	case '[':
		a := []any{}
		for d.More() {
			v, err := readJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, ErrArguments
		}
		return a, nil
	default:
		return nil, ErrArguments
	}
}

// checkSchemaSubset is an allowlist, not a second schema validator. Validation
// of keyword types and constraints belongs to the pinned offline compiler.
// References, custom vocabularies, executable formats and combinators are not
// part of this teaching profile and cannot trigger network/file loading.
func checkSchemaSubset(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return ErrSchema
	}
	typ, ok := m["type"].(string)
	if !ok {
		return ErrSchema
	}
	switch typ {
	case "object", "string", "number", "integer", "boolean", "null":
	default:
		return ErrSchema
	}
	for key, value := range m {
		switch key {
		case "type", "required", "minLength", "maxLength", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
		case "description":
			text, ok := value.(string)
			if !ok || utf8.RuneCountInString(text) > MaxDescription {
				return ErrSchema
			}
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok || typ != "object" || len(properties) > 64 {
				return ErrSchema
			}
			for name, child := range properties {
				if name == "" || utf8.RuneCountInString(name) > 128 || checkSchemaSubset(child) != nil {
					return ErrSchema
				}
			}
		case "additionalProperties":
			if _, ok := value.(bool); !ok || typ != "object" {
				return ErrSchema
			}
		case "enum":
			items, ok := value.([]any)
			if !ok || len(items) == 0 || len(items) > 64 {
				return ErrSchema
			}
			for _, item := range items {
				switch item.(type) {
				case nil, string, bool, json.Number:
				default:
					return ErrSchema
				}
			}
		default:
			return ErrSchema
		}
	}
	return nil
}

type InputValidator struct{ compiled *jsonschema.Schema }

func CompileInputSchema(raw json.RawMessage) (*InputValidator, error) {
	v, err := decodeBounded(raw, MaxSchemaBytes)
	if err != nil || checkSchemaSubset(v) != nil {
		return nil, ErrSchema
	}
	if v.(map[string]any)["type"] != "object" {
		return nil, ErrSchema
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	compiler.LoadURL = func(string) (io.ReadCloser, error) { return nil, ErrSchema }
	const resource = "https://campusclaw.invalid/mcp-input.json"
	if compiler.AddResource(resource, bytes.NewReader(raw)) != nil {
		return nil, ErrSchema
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		return nil, ErrSchema
	}
	return &InputValidator{compiled: compiled}, nil
}

func (v *InputValidator) Validate(arguments json.RawMessage) error {
	value, err := decodeBounded(arguments, MaxArgumentBytes)
	if err != nil || v == nil || v.compiled == nil {
		return ErrArguments
	}
	if _, ok := value.(map[string]any); !ok || v.compiled.Validate(value) != nil {
		return ErrArguments
	}
	return nil
}

func ValidateTools(tools []Tool) error {
	if len(tools) > MaxTools {
		return ErrDefinitions
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if !ValidToolName(tool.Name) || seen[tool.Name] || !utf8.ValidString(tool.Description) || utf8.RuneCountInString(tool.Description) > MaxDescription {
			return ErrDefinitions
		}
		if _, err := CompileInputSchema(tool.InputSchema); err != nil {
			return ErrDefinitions
		}
		seen[tool.Name] = true
	}
	return nil
}
