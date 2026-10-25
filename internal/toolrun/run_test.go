package toolrun

import (
	"context"
	"strings"
	"testing"

	"campusclaw/internal/chat"
)

func TestRunTwoDependentRoundsHaveCorrelatedMessagesAndEvents(t *testing.T) {
	probe := newExecutionProbe(t)
	e, repo, g, turn := fixtureEngine(t, probe.server.URL)
	turn.Skill.Tools = append(turn.Skill.Tools, "unit_convert@campus-tools")
	g.decide = func(messages []chat.ProtocolMessage, tools []chat.ToolDefinition) (chat.ProtocolMessage, error) {
		if len(tools) != 2 {
			t.Fatal("candidate set changed")
		}
		switch g.calls {
		case 1:
			return chat.HostAssistantCalls([]chat.ToolCall{proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"先检查公式条件"}`)}), nil
		case 2:
			if len(messages) != 4 || messages[3].Role != "tool" || !strings.Contains(messages[3].Content, "SAFE_HINT formula_hint") || messages[2].ToolCalls[0].ID != messages[3].ToolCallID {
				t.Fatal("second round did not read first correlated result")
			}
			// Deliberately reuse the model ID: the host gives each action a stable
			// per-question ID without breaking the assistant/tool association.
			return chat.HostAssistantCalls([]chat.ToolCall{proposed(chat.ToolAlias(7, "unit_convert"), `{"query":"依据上一步再检查单位"}`)}), nil
		default:
			if len(messages) != 6 || !strings.Contains(messages[5].Content, "SAFE_HINT unit_convert") || messages[4].ToolCalls[0].ID != messages[5].ToolCallID {
				t.Fatal("third decision lost second result")
			}
			return chat.ProtocolMessage{Role: "assistant", Content: "BUFFERED_DECISION_NOT_AN_ANSWER"}, nil
		}
	}
	events := [][]CallSummary{}
	p := e.Run(context.Background(), turn, func(calls []CallSummary) { events = append(events, calls) })
	if probe.calls.Load() != 2 || g.calls != 3 || len(p.Calls) != 2 || len(events) != 2 || len(repo.reserved) != 2 || !p.UseTools {
		t.Fatal("two-round lifecycle incomplete")
	}
	for i, s := range p.Calls {
		if s.Round != i+1 || s.Status != "ok" || s.CallID != p.Messages[2+i*2].ToolCalls[0].ID || s.CallID != p.Messages[3+i*2].ToolCallID || repo.reserved[i].CallID != s.CallID || repo.reserved[i].Round != s.Round {
			t.Fatal("SSE/message/audit correlation differs")
		}
	}
	if p.Calls[0].CallID == p.Calls[1].CallID || p.Messages[0].Content != turn.Messages[0].Content {
		t.Fatal("unstable IDs or tool data elevated into system")
	}
	for _, msg := range p.Messages {
		if strings.Contains(msg.Content, "BUFFERED_DECISION") {
			t.Fatal("buffered decision content retained as answer")
		}
	}
}

func TestRunBatchProducesOneAssistantAndOneResultPerCall(t *testing.T) {
	probe := newExecutionProbe(t)
	e, _, g, turn := fixtureEngine(t, probe.server.URL)
	turn.Skill.Tools = append(turn.Skill.Tools, "unit_convert@campus-tools")
	g.decide = func(messages []chat.ProtocolMessage, tools []chat.ToolDefinition) (chat.ProtocolMessage, error) {
		if g.calls > 1 {
			return chat.ProtocolMessage{Role: "assistant", Content: "stop"}, nil
		}
		first := proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"formula"}`)
		second := proposed(chat.ToolAlias(7, "unit_convert"), `{"query":"units"}`)
		second.ID = "model_call_two"
		return chat.HostAssistantCalls([]chat.ToolCall{first, second}), nil
	}
	events := 0
	p := e.Run(context.Background(), turn, func(calls []CallSummary) {
		events++
		if len(calls) != 2 {
			t.Fatal("batch not emitted as one round")
		}
	})
	if probe.calls.Load() != 2 || len(p.Messages) != 5 || len(p.Messages[2].ToolCalls) != 2 || events != 1 || len(p.Calls) != 2 {
		t.Fatal("batch message shape incorrect")
	}
	for i := 0; i < 2; i++ {
		if p.Messages[2].ToolCalls[i].ID != p.Messages[3+i].ToolCallID || p.Calls[i].Round != 1 {
			t.Fatal("batch results unpaired")
		}
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.names[0] != "formula_hint" || probe.names[1] != "unit_convert" {
		t.Fatal("batch actions not executed in order")
	}
}
