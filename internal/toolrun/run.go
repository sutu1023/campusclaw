package toolrun

import (
	"context"
	"time"

	"campusclaw/internal/chat"
)

func (e *Engine) validLimits() bool {
	c := e.Config
	return c.MaxRounds > 0 && c.MaxRounds <= 64 && c.MaxCallsPerTurn > 0 && c.MaxCallsPerTurn <= 64 && c.MaxCallsPerConversation > 0 && c.MaxCallsPerConversation <= 10000 && c.TurnTimeout > 0 && c.TurnTimeout <= 2*time.Minute && c.RequestTimeout > 0 && c.RequestTimeout <= time.Minute
}

// Run performs only the bounded tool phase. Final answer generation deliberately
// uses the original parent context rather than this phase's expired deadline.
func (e *Engine) Run(ctx context.Context, turn Turn, emit EmitFunc) *Plan {
	timeout := e.Config.TurnTimeout
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 20 * time.Second
	}
	phase, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p := e.Prepare(phase, turn, emit)
	if len(p.Tools) == 0 || ctx.Err() != nil {
		return p
	}
	if !e.validLimits() {
		p.stage(0, "denied", "MCP 工具执行配额配置不可用")
		return p
	}
	p.UseTools = true
	attempts := 0
	for round := 1; round <= e.Config.MaxRounds; round++ {
		if ctx.Err() != nil {
			return p
		}
		if phase.Err() != nil {
			p.stage(round, "denied", "MCP 工具阶段时间预算已耗尽")
			return p
		}
		decision, err := e.Decide(phase, p)
		if ctx.Err() != nil {
			return p
		}
		if phase.Err() != nil {
			p.stage(round, "denied", "MCP 工具阶段时间预算已耗尽")
			return p
		}
		if err != nil || chat.ValidateToolDecision(decision) != nil {
			p.stage(round, "error", "模型工具协议不可用，已退回普通流式回答")
			p.Messages = chat.HostMessages(turn.Messages)
			p.Tools = nil
			p.UseTools = false
			return p
		}
		if len(decision.ToolCalls) == 0 {
			return p
		} // content is never an answer delta
		calls := make([]chat.ToolCall, 0, len(decision.ToolCalls))
		results := make([]chat.ProtocolMessage, 0, len(decision.ToolCalls))
		summaries := make([]CallSummary, 0, len(decision.ToolCalls))
		budgetDenied := false
		for _, proposed := range decision.ToolCalls {
			if ctx.Err() != nil {
				break
			}
			attempts++
			var summary CallSummary
			var result chat.ProtocolMessage
			if p.stopReason != "" {
				summary, result = e.deny(ctx, p, proposed, round, p.stopReason)
			} else if attempts > e.Config.MaxCallsPerTurn {
				budgetDenied = true
				summary, result = e.deny(ctx, p, proposed, round, "MCP 本次提问调用尝试额度已耗尽")
			} else if phase.Err() != nil {
				budgetDenied = true
				summary, result = e.deny(ctx, p, proposed, round, "MCP 工具阶段时间预算已耗尽")
			} else {
				summary, result = e.Execute(phase, p, proposed, round)
			}
			// Host-generated IDs are unique across rounds even if the model reuses
			// its IDs. The assistant declaration, result, SSE and audit agree.
			proposed.ID = summary.CallID
			calls = append(calls, proposed)
			results = append(results, result)
			summaries = append(summaries, summary)
		}
		if len(calls) > 0 {
			p.Messages = append(p.Messages, chat.HostAssistantCalls(calls))
			p.Messages = append(p.Messages, results...)
		}
		p.publish(summaries)
		if ctx.Err() != nil {
			return p
		}
		if p.stopReason != "" {
			return p
		}
		if attempts >= e.Config.MaxCallsPerTurn {
			if !budgetDenied {
				p.stage(round, "denied", "MCP 本次提问调用尝试额度已耗尽")
			}
			return p
		}
		if phase.Err() != nil {
			if !budgetDenied {
				p.stage(round, "denied", "MCP 工具阶段时间预算已耗尽")
			}
			return p
		}
		if round == e.Config.MaxRounds {
			p.stage(round, "denied", "MCP 工具决策轮次额度已耗尽")
			return p
		}
	}
	return p
}

func (p *Plan) stage(round int, status, text string) {
	ref := "mcp_tool_stage"
	if len(p.Tools) > 0 {
		if c, ok := p.candidates[p.Tools[0].Function.Name]; ok {
			ref = c.Tool.Name + "@" + c.Server.Name
		}
	}
	p.publish([]CallSummary{p.summary(ref, round, status, text)})
}
func (e *Engine) deny(ctx context.Context, p *Plan, call chat.ToolCall, round int, text string) (CallSummary, chat.ProtocolMessage) {
	candidate, ok := p.selectCandidate(call.Function.Name)
	ref := "unapproved_tool"
	if ok {
		ref = candidate.Tool.Name + "@" + candidate.Server.Name
	}
	s := p.summary(ref, round, "denied", text)
	if ok {
		// This is local audit bookkeeping only, never permission to exceed the
		// expired action budget. Keep the write bounded even if the DB stalls.
		auditCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		e.recordLocal(auditCtx, p, candidate, &s)
	}
	return s, resultMessage(s, s.Text)
}
