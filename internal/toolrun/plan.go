// Package toolrun owns the transient, least-privilege model/MCP tool phase.
// It never persists intermediate protocol messages as conversation history.
package toolrun

import (
	"context"
	"errors"
	"fmt"

	"campusclaw/internal/chat"
	"campusclaw/internal/config"
	"campusclaw/internal/mcp"
	"campusclaw/internal/skill"
	"campusclaw/internal/store"
)

type Repository interface {
	ResolveMCPTool(context.Context, int64, string, string) (*store.MCPToolCandidate, error)
	RecordMCPAttempt(context.Context, store.MCPCallAttempt, string, string) (int64, error)
	ReserveMCPCall(context.Context, store.MCPCallAttempt, store.MCPToolCandidate, int) (int64, error)
	FinishMCPCall(context.Context, int64, int64, string, string, uint64) error
}
type Gateway interface {
	DecideTools(context.Context, []chat.ProtocolMessage, []chat.ToolDefinition) (chat.ProtocolMessage, error)
}
type Engine struct {
	Store        Repository
	Gateway      Gateway
	Config       config.MCPConfig
	OtherSecrets []string
}
type Turn struct {
	ClassID           int64
	UserID            int64
	ConversationID    int64
	QuestionMessageID int64
	Skill             *skill.Entry
	Messages          []chat.Message
}

// CallSummary is the only tool payload suitable for SSE, replay or audit UI.
// It deliberately has no arguments, raw result, address or credential fields.
type CallSummary struct {
	CallID string `json:"call_id" example:"q41-c1" maxLength:"128"`
	Ref    string `json:"ref" example:"formula_hint@campus-tools" maxLength:"128"`
	Round  int    `json:"round" minimum:"0" maximum:"64"`
	Status string `json:"status" enums:"ok,error,denied"`
	Text   string `json:"text" maxLength:"500"`
}
type EmitFunc func([]CallSummary)
type Plan struct {
	Messages   []chat.ProtocolMessage
	Tools      []chat.ToolDefinition
	Calls      []CallSummary
	UseTools   bool
	stopReason string
	turn       Turn
	candidates map[string]store.MCPToolCandidate
	sequence   int
	emit       EmitFunc
}

func (p *Plan) summary(ref string, round int, status, text string) CallSummary {
	p.sequence++
	return CallSummary{CallID: fmt.Sprintf("q%d-c%d", p.turn.QuestionMessageID, p.sequence), Ref: ref, Round: round, Status: status, Text: text}
}
func (p *Plan) publish(calls []CallSummary) {
	if len(calls) == 0 {
		return
	}
	p.Calls = append(p.Calls, calls...)
	if p.emit != nil {
		p.emit(append([]CallSummary(nil), calls...))
	}
}
func (p *Plan) attempt(c store.MCPToolCandidate, s CallSummary) store.MCPCallAttempt {
	return store.MCPCallAttempt{ClassID: p.turn.ClassID, ServerID: c.Server.ID, ToolName: c.Tool.Name, UserID: p.turn.UserID, ConversationID: p.turn.ConversationID, QuestionMessageID: p.turn.QuestionMessageID, CallID: s.CallID, Round: s.Round}
}

// Prepare only sees references from the single deterministic skill match. It
// never enumerates a class directory and never connects or invokes a server.
func (e *Engine) Prepare(ctx context.Context, turn Turn, emit EmitFunc) *Plan {
	p := &Plan{turn: turn, Messages: chat.HostMessages(turn.Messages), candidates: map[string]store.MCPToolCandidate{}, emit: emit}
	if turn.Skill == nil {
		return p
	}
	seen := map[string]bool{}
	for _, raw := range turn.Skill.Tools {
		if ctx.Err() != nil {
			break
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		ref, err := skill.ParseToolReference(raw)
		if err == nil && ref.Builtin {
			continue
		}
		if err != nil || len(seen) > skill.MaxTools {
			p.publish([]CallSummary{p.summary("invalid_mcp_reference", 0, "denied", "技能工具引用无效或超过上限")})
			continue
		}
		candidate, err := e.Store.ResolveMCPTool(ctx, turn.ClassID, ref.Server, ref.Tool)
		if err != nil {
			s := p.summary(raw, 0, "denied", "MCP 工具缺失、未连接或批准快照不可用")
			if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrMCPDenied) {
				s.Status = "error"
				s.Text = "MCP 本地授权信息不可用"
			}
			if candidate != nil {
				e.recordLocal(ctx, p, *candidate, &s)
			}
			p.publish([]CallSummary{s})
			continue
		}
		target, err := e.Config.Registry.Resolve(turn.ClassID, ref.Server, candidate.Server.BaseURL)
		binding, bindingErr := e.Config.Registry.Reference(turn.ClassID, ref.Server, candidate.Server.BaseURL)
		if err != nil || bindingErr != nil || binding != candidate.Server.CredentialRef || mcp.CheckDefinitions(target, []mcp.Tool{candidate.Tool}, e.OtherSecrets...) != nil {
			s := p.summary(raw, 0, "denied", "MCP 部署批准、凭据或工具定义不可用")
			e.recordLocal(ctx, p, *candidate, &s)
			p.publish([]CallSummary{s})
			continue
		}
		alias := chat.ToolAlias(candidate.Server.ID, candidate.Tool.Name)
		definition, err := chat.NewToolDefinition(alias, candidate.Tool.Description, candidate.Tool.InputSchema)
		if _, collision := p.candidates[alias]; err != nil || collision {
			s := p.summary(raw, 0, "denied", "MCP 工具声明不可用")
			e.recordLocal(ctx, p, *candidate, &s)
			p.publish([]CallSummary{s})
			continue
		}
		p.Tools = append(p.Tools, definition)
		p.candidates[alias] = *candidate
	}
	return p
}
func (e *Engine) recordLocal(ctx context.Context, p *Plan, c store.MCPToolCandidate, s *CallSummary) {
	if _, err := e.Store.RecordMCPAttempt(ctx, p.attempt(c, *s), s.Status, s.Text); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.Status = "error"
		s.Text = "MCP 调用审计不可用，未执行远端动作"
	}
}

// Decide returns without touching the gateway when there are no approved MCP
// candidates. A model's plain answer is not itself a tool invocation.
func (e *Engine) Decide(ctx context.Context, p *Plan) (chat.ProtocolMessage, error) {
	if len(p.Tools) == 0 {
		return chat.ProtocolMessage{Role: "assistant"}, nil
	}
	if e.Gateway == nil {
		return chat.ProtocolMessage{}, chat.ErrToolDecision
	}
	return e.Gateway.DecideTools(ctx, p.Messages, p.Tools)
}

// selectCandidate does not resolve arbitrary model names against a directory.
// Unknown aliases are denied locally; only the exact declaration map is used.
func (p *Plan) selectCandidate(alias string) (store.MCPToolCandidate, bool) {
	candidate, ok := p.candidates[alias]
	return candidate, ok
}
