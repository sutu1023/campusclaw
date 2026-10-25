package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/mcp"
	"campusclaw/internal/toolrun"
)

func toolSummaries(t *testing.T, raw []byte) ([]toolrun.CallSummary, string, bool, bool) {
	t.Helper()
	calls := []toolrun.CallSummary{}
	var answer strings.Builder
	done, failed := false, false
	for _, ev := range parseSSE(t, raw) {
		switch ev.Event {
		case "meta":
			var meta struct {
				Status    string                `json:"status"`
				ToolCalls []toolrun.CallSummary `json:"tool_calls"`
			}
			_ = json.Unmarshal(ev.Data, &meta)
			if meta.Status != "ok" {
				t.Fatal("tool failure changed outer meta status")
			}
			calls = append(calls, meta.ToolCalls...)
		case "delta":
			var chunk struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(ev.Data, &chunk)
			answer.WriteString(chunk.Text)
		case "done":
			done = true
		case "error":
			failed = true
		}
	}
	return calls, answer.String(), done, failed
}

func TestMCPStreamDisconnectDeniesOriginalSkillAndPreservesIsolation(t *testing.T) {
	f := prepareMCPChat(t, false)
	stats := installToolChatGateway(t, f, "two_rounds")
	resp, _ := f.env.ask(t, f.student, f.conv.ID, "勾股公式与单位怎么检查")
	if resp.StatusCode != 200 {
		t.Fatal("initial ask failed")
	}
	resp, _ = f.env.do(t, "POST", fmt.Sprintf("/api/mcp/servers/%d/disconnect", f.serverID), f.teacher, nil, "")
	if resp.StatusCode != 204 {
		t.Fatal("disconnect failed")
	}
	resp, raw := f.env.ask(t, f.student, f.conv.ID, "勾股公式与单位再检查")
	calls, answer, done, failed := toolSummaries(t, raw)
	if resp.StatusCode != 200 || len(calls) != 2 || answer == "" || !done || failed || strings.Contains(answer, "[1]") || stats.decisions.Load() != 3 || stats.finals.Load() != 2 {
		t.Fatal("disconnect broke main path or entered another decision phase")
	}
	for _, s := range calls {
		if s.Status != "denied" || s.Round != 0 {
			t.Fatal("stale references were not explicit preflight denials")
		}
	}
	messages := f.env.messages(t, f.student, f.conv.ID)
	if len(messages) != 4 || len(messages[3].ToolCalls) != 2 || messages[3].ToolCalls[0].Status != "denied" {
		t.Fatal("denials not persisted for replay")
	}
	_, total, err := f.env.store.ListMCPAudit(context.Background(), 1, 1, 20, f.serverID, "denied")
	if err != nil || total != 2 {
		t.Fatal("disconnect denial audit missing")
	}
	sk, err := f.env.store.GetSkill(context.Background(), 1, f.skill.ID)
	if err != nil || sk.Source != f.skill.Source {
		t.Fatal("disconnect changed skill source")
	}
	foreign := f.env.login(t, "student_b1", "CampusClaw#2026")
	for _, id := range []int64{f.conv.ID, 999999} {
		resp, _ = f.env.ask(t, foreign, id, "勾股公式怎么用")
		if resp.StatusCode != 404 {
			t.Fatal("foreign/nonexistent conversation distinguishable")
		}
	}
	if stats.decisions.Load() != 3 || stats.finals.Load() != 2 {
		t.Fatal("cross-class request reached gateway")
	}
}
func TestMCPStreamToolFailuresAndAuditOutageStillAnswer(t *testing.T) {
	for _, mode := range []string{"unauthorized", "timeout", "audit_outage", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			f := prepareMCPChat(t, false)
			gatewayMode := "two_rounds"
			if mode == "unsupported" {
				gatewayMode = mode
			}
			stats := installToolChatGateway(t, f, gatewayMode)
			switch mode {
			case "unauthorized":
				configureTestMCP(t, f.env, []mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: f.origin, TokenEnv: "MCP_STREAM_TOKEN"}}, map[string]string{"MCP_STREAM_TOKEN": "deliberately-invalid-test-token"})
			case "timeout":
				f.env.api.MCP.RequestTimeout = time.Nanosecond
			case "audit_outage":
				if _, err := f.env.store.DB.ExecContext(context.Background(), `ALTER TABLE mcp_tool_calls ADD CONSTRAINT mcp_stream_no_interrupted CHECK (safe_text NOT LIKE 'interrupted:%')`); err != nil {
					t.Fatal(err)
				}
				defer f.env.store.DB.ExecContext(context.Background(), `ALTER TABLE mcp_tool_calls DROP CHECK mcp_stream_no_interrupted`)
			}
			resp, raw := f.env.ask(t, f.student, f.conv.ID, "勾股公式与单位怎么检查")
			calls, answer, done, failed := toolSummaries(t, raw)
			if resp.StatusCode != 200 || len(calls) == 0 || answer == "" || !done || failed || strings.Contains(answer, "[1]") || stats.finals.Load() != 1 {
				t.Fatal("local failure prevented final answer")
			}
			for _, s := range calls {
				if s.Status != "error" || strings.Contains(s.Text, f.token) || strings.Contains(s.Text, f.origin) || strings.Contains(s.Text, "private") {
					t.Fatal("failure not a sanitized error")
				}
			}
			messages := f.env.messages(t, f.student, f.conv.ID)
			if len(messages) != 2 || len(messages[1].ToolCalls) != len(calls) || messages[1].Content != answer {
				t.Fatal("error summaries or answer missing from replay")
			}
			_, total, err := f.env.store.ListMCPAudit(context.Background(), 1, 1, 20, f.serverID, "")
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if mode == "audit_outage" || mode == "unsupported" {
				want = 0
			}
			if total != want {
				t.Fatal("failure audit count fabricated or lost")
			}
			if mode == "unauthorized" {
				resp, _ := f.env.do(t, "GET", "/api/me", f.student, nil, "")
				if resp.StatusCode != 200 {
					t.Fatal("remote 401 invalidated app identity")
				}
			}
		})
	}
}
func TestMCPStreamFinalErrorsKeepExistingPartialGenerationContract(t *testing.T) {
	for _, mode := range []string{"final_fail", "partial_fail"} {
		t.Run(mode, func(t *testing.T) {
			f := prepareMCPChat(t, false)
			installToolChatGateway(t, f, mode)
			resp, raw := f.env.ask(t, f.student, f.conv.ID, "勾股公式与单位怎么检查")
			calls, answer, done, failed := toolSummaries(t, raw)
			if resp.StatusCode != 200 || len(calls) != 2 || strings.Contains(string(raw), "private final failure") {
				t.Fatal("final error changed HTTP status or leaked diagnostics")
			}
			messages := f.env.messages(t, f.student, f.conv.ID)
			if mode == "final_fail" {
				if answer != "" || done || !failed || len(messages) != 1 {
					t.Fatal("empty final failure should end in error without assistant row")
				}
			} else {
				if answer != "已生成的局部提示" || !done || failed || len(messages) != 2 || messages[1].Content != answer || len(messages[1].ToolCalls) != 2 {
					t.Fatal("partial generation text/summary was not persisted")
				}
			}
		})
	}
}
