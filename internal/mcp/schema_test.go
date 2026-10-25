package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchemaValidation(t *testing.T) {
	v, err := CompileInputSchema(querySchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []string{`{"query":"公式"}`, `{"query":"` + strings.Repeat("中", 500) + `"}`} {
		if err := v.Validate(json.RawMessage(valid)); err != nil {
			t.Fatalf("valid arguments rejected: %v", err)
		}
	}
	for _, invalid := range []string{
		`{}`, `{"query":1}`, `{"query":null}`, `{"query":""}`, `{"query":"` + strings.Repeat("中", 501) + `"}`,
		`{"query":"x","extra":1}`, `[]`, `null`, `"x"`, `{`, `{} {}`,
		`{"query":"first","query":"second"}`, `{"query":"` + strings.Repeat("x", MaxArgumentBytes) + `"}`,
		string([]byte{'{', '"', 'q', 'u', 'e', 'r', 'y', '"', ':', '"', 0xff, '"', '}'}),
	} {
		if err := v.Validate(json.RawMessage(invalid)); !errors.Is(err, ErrArguments) {
			t.Fatalf("invalid arguments accepted: %v", err)
		}
	}
	permissive, err := CompileInputSchema(json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	deep := strings.Repeat(`{"x":`, MaxJSONDepth) + `1` + strings.Repeat(`}`, MaxJSONDepth)
	if err := permissive.Validate(json.RawMessage(deep)); !errors.Is(err, ErrArguments) {
		t.Fatal("deep arguments accepted")
	}
}

func TestSchemaSupportedNumericEnumAndNestedConstraints(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"steps":{"type":"integer","minimum":1,"maximum":3},"unit":{"type":"string","enum":["m","cm"]},"nested":{"type":"object","properties":{"enabled":{"type":"boolean"}},"required":["enabled"],"additionalProperties":false}},"required":["steps","unit","nested"],"additionalProperties":false}`)
	v, err := CompileInputSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(json.RawMessage(`{"steps":2,"unit":"cm","nested":{"enabled":true}}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"steps":0,"unit":"cm","nested":{"enabled":true}}`, `{"steps":2.5,"unit":"cm","nested":{"enabled":true}}`, `{"steps":2,"unit":"unknown","nested":{"enabled":true}}`, `{"steps":2,"unit":"m","nested":{}}`} {
		if err := v.Validate(json.RawMessage(raw)); err == nil {
			t.Fatal("constraint not enforced")
		}
	}
}

func TestSchemaRejectsUnsupportedAndNeverFetches(t *testing.T) {
	var requests atomic.Int32
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer probe.Close()
	for _, raw := range []string{
		`true`, `[]`, `{}`, `{"type":"string"}`, `{"type":"array"}`, `{"type":["object","null"]}`,
		`{"type":"object","properties":[]}`, `{"type":"object","required":"query"}`,
		`{"type":"object","properties":{"x":{"type":"unknown"}}}`,
		`{"type":"object","properties":{"x":{"type":"string","maxLength":-1}}}`,
		`{"type":"object","properties":{"x":{"type":"string","pattern":".*"}}}`,
		`{"type":"object","additionalProperties":{"type":"string"}}`,
		`{"type":"object","type":"object"}`, `{"type":"object","allOf":[]}`,
		fmt.Sprintf(`{"type":"object","$ref":%q}`, probe.URL),
		fmt.Sprintf(`{"type":"object","$schema":%q}`, probe.URL),
		fmt.Sprintf(`{"type":"object","properties":{"x":{"type":"string","$ref":%q}}}`, probe.URL),
		`{"type":"object","$ref":"file:///etc/passwd"}`,
		`{"type":"object","description":"` + strings.Repeat("x", MaxSchemaBytes) + `"}`,
		strings.Repeat(`{"type":"object","properties":{"x":`, MaxJSONDepth) + `{"type":"string"}` + strings.Repeat(`}}`, MaxJSONDepth),
	} {
		if _, err := CompileInputSchema(json.RawMessage(raw)); !errors.Is(err, ErrSchema) {
			t.Errorf("unsafe schema accepted: %v", err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("schema triggered network loading")
	}
}

func TestToolDefinitionLimits(t *testing.T) {
	valid := Tool{"formula_hint", "Formula conditions", querySchema}
	if err := ValidateTools([]Tool{valid}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Formula_hint", "formula-hint", "1tool", "tool@server", strings.Repeat("x", 65)} {
		bad := valid
		bad.Name = name
		if ValidateTools([]Tool{bad}) == nil {
			t.Errorf("invalid tool name accepted: %q", name)
		}
	}
	if ValidateTools([]Tool{valid, valid}) == nil {
		t.Fatal("duplicate names accepted")
	}
	bad := valid
	bad.Description = strings.Repeat("中", MaxDescription+1)
	if ValidateTools([]Tool{bad}) == nil {
		t.Fatal("long description accepted")
	}
	bad = valid
	bad.InputSchema = json.RawMessage(`{"type":"string"}`)
	if ValidateTools([]Tool{bad}) == nil {
		t.Fatal("non-object schema accepted")
	}
	tooMany := make([]Tool, MaxTools+1)
	for i := range tooMany {
		tooMany[i] = valid
		tooMany[i].Name = fmt.Sprintf("tool_%d", i)
	}
	if ValidateTools(tooMany) == nil {
		t.Fatal("oversized catalog accepted")
	}
}

func TestInvalidParametersFailBeforeOutbound(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer srv.Close()
	c := clientFor(t, srv.URL, time.Second)
	for _, args := range []string{`{}`, `{"query":42}`, `{"query":"x","extra":true}`, `{`, strings.Repeat("x", MaxArgumentBytes+1)} {
		_, err := c.Call(context.Background(), Tool{"formula_hint", "Hint", querySchema}, json.RawMessage(args))
		if !errors.Is(err, ErrArguments) {
			t.Fatalf("arguments not rejected: %v", err)
		}
	}
	_, err := c.Call(context.Background(), Tool{"formula_hint", "Hint", json.RawMessage(`{"type":"object","$ref":"https://example.com"}`)}, json.RawMessage(`{}`))
	if !errors.Is(err, ErrSchema) || requests.Load() != 0 {
		t.Fatalf("invalid schema or arguments triggered %d outbound requests; %v", requests.Load(), err)
	}
}

func TestInvalidDiscoveryHasNoPartialCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondRPC(w, "mcp-1", map[string]any{"tools": []Tool{{"formula_hint", "Hint", querySchema}, {"formula_hint", "Changed", querySchema}}})
	}))
	defer srv.Close()
	tools, err := clientFor(t, srv.URL, time.Second).ListTools(context.Background())
	if !errors.Is(err, ErrDefinitions) || tools != nil {
		t.Fatalf("partial catalog accepted: %v; %v", tools, err)
	}
}
