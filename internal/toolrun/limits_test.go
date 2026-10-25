package toolrun

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/chat"
	"campusclaw/internal/store"
)

func TestRunAttemptBudgetCountsErrorsAndEachBatchItem(t *testing.T) {
	probe := newExecutionProbe(t)
	e, repo, g, turn := fixtureEngine(t, probe.server.URL)
	e.Config.MaxCallsPerTurn = 2
	g.decide = func(_ []chat.ProtocolMessage, _ []chat.ToolDefinition) (chat.ProtocolMessage, error) {
		calls := []chat.ToolCall{}
		for i := 0; i < 4; i++ {
			args := `{"query":"valid"}`
			if i == 0 {
				args = `{"query":3}`
			}
			c := proposed(chat.ToolAlias(7, "formula_hint"), args)
			c.ID = fmt.Sprintf("model_%d", i)
			if i == 3 {
				c.Function.Name = "invented_tool"
			}
			calls = append(calls, c)
		}
		return chat.HostAssistantCalls(calls), nil
	}
	p := e.Run(context.Background(), turn, nil)
	if g.calls != 1 || probe.calls.Load() != 1 || len(p.Calls) != 4 || len(repo.reserved) != 1 || len(repo.local) != 2 || !p.UseTools {
		t.Fatal("batch/errors bypassed per-question attempt budget")
	}
	for i, want := range []string{"error", "ok", "denied", "denied"} {
		if p.Calls[i].Status != want {
			t.Fatal("attempt budget classifications incorrect")
		}
	}
	if len(p.Messages) != 7 || len(p.Messages[2].ToolCalls) != 4 {
		t.Fatal("over-budget calls lack paired results")
	}
}
func TestRunDifferentRoundLimitsStopDecisionsButKeepToolData(t *testing.T) {
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			probe := newExecutionProbe(t)
			e, _, g, turn := fixtureEngine(t, probe.server.URL)
			e.Config.MaxRounds = limit
			g.reply = chat.HostAssistantCalls([]chat.ToolCall{proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"valid"}`)})
			p := e.Run(context.Background(), turn, nil)
			if g.calls != limit || probe.calls.Load() != int64(limit) || len(p.Calls) != limit+1 || p.Calls[limit].Status != "denied" || !strings.Contains(p.Calls[limit].Text, "轮次") || !p.UseTools {
				t.Fatal("round budget did not stop tool phase cleanly")
			}
		})
	}
}
func TestRunConversationQuotaStopsRemainingBatchAndDecisions(t *testing.T) {
	probe := newExecutionProbe(t)
	e, repo, g, turn := fixtureEngine(t, probe.server.URL)
	repo.reserveErr = store.ErrMCPQuota
	first := proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"one"}`)
	second := first
	second.ID = "model_two"
	g.reply = chat.HostAssistantCalls([]chat.ToolCall{first, second})
	p := e.Run(context.Background(), turn, nil)
	if g.calls != 1 || probe.total.Load() != 0 || len(p.Calls) != 2 || !p.UseTools {
		t.Fatal("quota exhaustion did not close decision phase")
	}
	for _, s := range p.Calls {
		if s.Status != "denied" || !strings.Contains(s.Text, "会话") {
			t.Fatal("conversation quota denial missing")
		}
	}
}

type deadlineGateway struct{}

func (deadlineGateway) DecideTools(ctx context.Context, _ []chat.ProtocolMessage, _ []chat.ToolDefinition) (chat.ProtocolMessage, error) {
	<-ctx.Done()
	return chat.ProtocolMessage{}, ctx.Err()
}
func TestRunToolPhaseDeadlineDoesNotCancelFinalParent(t *testing.T) {
	for _, duringDecision := range []bool{false, true} {
		t.Run(fmt.Sprint(duringDecision), func(t *testing.T) {
			probe := newExecutionProbe(t)
			e, _, g, turn := fixtureEngine(t, probe.server.URL)
			e.Config.TurnTimeout = 20 * time.Millisecond
			if duringDecision {
				e.Gateway = deadlineGateway{}
			} else {
				probe.mode.Store(2)
				g.reply = chat.HostAssistantCalls([]chat.ToolCall{proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"valid"}`)})
			}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := time.Now()
			p := e.Run(parent, turn, nil)
			if parent.Err() != nil || time.Since(started) > 500*time.Millisecond || len(p.Calls) == 0 || p.Calls[len(p.Calls)-1].Status != "denied" || !strings.Contains(p.Calls[len(p.Calls)-1].Text, "时间") {
				t.Fatal("phase deadline escaped into final context or did not stop")
			}
			want := int64(1)
			if duringDecision {
				want = 0
			}
			if probe.calls.Load() != want {
				t.Fatal("unexpected post-deadline action")
			}
		})
	}
}
func TestRunClientCancellationStopsRemainingBatch(t *testing.T) {
	probe := newExecutionProbe(t)
	probe.mode.Store(2)
	e, _, g, turn := fixtureEngine(t, probe.server.URL)
	first := proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"first"}`)
	second := first
	second.ID = "second_model"
	g.reply = chat.HostAssistantCalls([]chat.ToolCall{first, second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *Plan, 1)
	go func() { done <- e.Run(ctx, turn, nil) }()
	deadline := time.Now().Add(time.Second)
	for probe.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if probe.calls.Load() != 1 {
		t.Fatal("first action never started")
	}
	cancel()
	select {
	case p := <-done:
		if len(p.Calls) != 1 || len(p.Messages[2].ToolCalls) != 1 {
			t.Fatal("cancelled batch continued or left unpaired messages")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop tool phase")
	}
	if g.calls != 1 || probe.calls.Load() != 1 {
		t.Fatal("new work continued after cancellation")
	}
}
func TestRunOversizedDecisionAndInvalidLimitsNeverExecute(t *testing.T) {
	probe := newExecutionProbe(t)
	e, repo, g, turn := fixtureEngine(t, probe.server.URL)
	calls := []chat.ToolCall{}
	for i := 0; i < chat.MaxDecisionCalls+1; i++ {
		c := proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"x"}`)
		c.ID = fmt.Sprintf("model_%d", i)
		calls = append(calls, c)
	}
	g.reply = chat.HostAssistantCalls(calls)
	p := e.Run(context.Background(), turn, nil)
	if len(p.Calls) != 1 || p.Calls[0].Status != "error" || p.UseTools || len(p.Tools) != 0 || len(p.Messages) != len(turn.Messages) || len(repo.reserved) != 0 || probe.total.Load() != 0 {
		t.Fatal("oversized decision partially executed or did not fall back")
	}
	e.Config.MaxCallsPerTurn = 0
	g.calls = 0
	p = e.Run(context.Background(), turn, nil)
	if g.calls != 0 || probe.total.Load() != 0 || len(p.Calls) != 1 || p.Calls[0].Status != "denied" {
		t.Fatal("nonpositive execution budget enabled actions")
	}
}
