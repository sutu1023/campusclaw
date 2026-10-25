package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"campusclaw/internal/mcp"
)

func TestBearerAuthenticationBeforeParsing(t *testing.T) {
	srv := testService(t)
	for _, auth := range []string{"", "Bearer wrong-token", "Basic " + serviceTestToken, "Bearer", "Bearer ", "Bearer  " + serviceTestToken, "Bearer " + serviceTestToken + " extra"} {
		for _, body := range []string{`{`, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"formula_hint","arguments":{"query":"test"}}}`} {
			status, out, raw := postRPC(t, srv.URL, auth, body)
			if status != http.StatusUnauthorized || out.Result != nil || strings.Contains(raw, serviceTestToken) || strings.Contains(raw, "wrong-token") {
				t.Fatalf("bad authentication response: %d %s", status, raw)
			}
		}
	}
}

func TestMissingCredentialRefusesStartup(t *testing.T) {
	for _, token := range []string{"", " ", "invalid token", "header\r\ninjection"} {
		t.Setenv("MCP_AUTH_TOKEN", token)
		if handler, err := newHandler(token); err == nil || handler != nil {
			t.Fatal("insecure handler created")
		}
		if err := run(nil); err == nil || !strings.Contains(err.Error(), "MCP_AUTH_TOKEN") {
			t.Fatalf("missing or unsafe token did not fail before listening: %v", err)
		}
	}
}

func TestTeachingQueriesAndNoFinalAnswers(t *testing.T) {
	for _, tc := range []struct{ name, query, contains, notContains string }{
		{"formula_hint", "去括号 -(x+2)", "符号", "-x-2"},
		{"formula_hint", "直角边为3和4，勾股定理算斜边", "斜边", "5"},
		{"formula_hint", "求未知量，答案是123456789", "适用条件", "123456789"},
		{"unit_convert", "137厘米换算成米", "100 厘米", "1.37"},
		{"unit_convert", "271千克换成克", "1000 克", "271000"},
		{"unit_convert", "37分钟换成秒", "60 秒", "2220"},
	} {
		args, _ := json.Marshal(map[string]string{"query": tc.query})
		result, err := callTeachingTool(tc.name, args)
		if err != nil || result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" {
			t.Fatalf("tool call failed: %+v, %v", result, err)
		}
		text := result.Content[0].Text
		if !strings.Contains(text, tc.contains) || strings.Contains(text, tc.notContains) {
			t.Fatalf("expected method hint, not exercise answer: %s", text)
		}
	}
	if err := mcp.ValidateTools(teachingTools()); err != nil {
		t.Fatalf("service definitions violate host profile: %v", err)
	}
}

func TestTeachingQueryValidation(t *testing.T) {
	for _, name := range []string{"formula_hint", "unit_convert"} {
		for _, raw := range []string{
			`{}`, `null`, `[]`, `{"query":null}`, `{"query":42}`, `{"query":""}`, `{"query":"  \n\t"}`,
			`{"query":"test","extra":"not allowed"}`, `{"query":"x","query":"y"}`,
			`{"query":"` + strings.Repeat("中", 501) + `"}`,
		} {
			if _, err := callTeachingTool(name, json.RawMessage(raw)); err == nil {
				t.Fatalf("%s accepted invalid query", name)
			}
		}
		args, _ := json.Marshal(map[string]string{"query": strings.Repeat("中", 500)})
		if _, err := callTeachingTool(name, args); err != nil {
			t.Fatalf("500-character query rejected: %v", err)
		}
	}
}

func TestInvalidToolArgumentsAreRPCFailuresNotSuccessText(t *testing.T) {
	srv := testService(t)
	for _, args := range []string{`{}`, `{"query":1}`, `{"query":"x","extra":true}`, `{"query":""}`} {
		status, out, raw := postRPC(t, srv.URL, "Bearer "+serviceTestToken, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"formula_hint","arguments":%s}}`, args))
		if status != 200 || out.Error == nil || out.Error.Code != -32602 || out.Result != nil {
			t.Fatalf("argument error disguised as tool success: %s", raw)
		}
	}
}
