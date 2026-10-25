package mcp

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDefinitionCredentialEchoRejectedIncludingEscapedSchema(t *testing.T) {
	registry := testRegistry(t, "http://mcp-demo:8090", func(string) string { return "test-only-token" })
	target, err := registry.Resolve(1, "campus-tools", "http://mcp-demo:8090")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []Tool{
		{Name: "hint", Description: "test-only-token", InputSchema: querySchema},
		{Name: "hint", Description: "model-key", InputSchema: querySchema},
		{Name: "hint", Description: "http://mcp-demo:8090", InputSchema: querySchema},
		{Name: "hint", Description: "Teaching data", InputSchema: json.RawMessage(`{"type":"object","description":"\u0074est-only-token"}`)},
		{Name: "hint", Description: "Teaching data", InputSchema: json.RawMessage(`{"type":"object","properties":{"model-key":{"type":"string"}}}`)},
	} {
		if err := CheckDefinitions(target, []Tool{tool}, "model-key"); !errors.Is(err, ErrUnsafeData) {
			t.Fatalf("credential echo was accepted: %v", err)
		}
	}
	if err := CheckDefinitions(target, []Tool{{Name: "hint", Description: "Safe explanation", InputSchema: querySchema}}, "model-key"); err != nil {
		t.Fatal(err)
	}
}
