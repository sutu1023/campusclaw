package toolrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"campusclaw/internal/chat"
	"campusclaw/internal/mcp"
	"campusclaw/internal/store"
)

func sameCandidate(a, b store.MCPToolCandidate) bool {
	x, y := a.Server, b.Server
	return x.ID == y.ID && x.ClassID == y.ClassID && x.Name == y.Name && x.BaseURL == y.BaseURL && x.CredentialRef == y.CredentialRef && x.Generation == y.Generation && x.ApprovedHash == y.ApprovedHash && y.State == store.MCPConnected && y.PendingHash == "" && a.Tool.Name == b.Tool.Name && a.Tool.Description == b.Tool.Description && bytes.Equal(a.Tool.InputSchema, b.Tool.InputSchema)
}

// Execute handles one proposed action. Selection is not permission: the current
// snapshot and credential binding are rechecked, and a durable transactional
// reservation must commit before even initialization goes onto the wire.
func (e *Engine) Execute(ctx context.Context, p *Plan, call chat.ToolCall, round int) (CallSummary, chat.ProtocolMessage) {
	candidate, ok := p.selectCandidate(call.Function.Name)
	if !ok {
		s := p.summary("unapproved_tool", round, "denied", "模型提出的工具不在本次技能候选集中")
		return s, resultMessage(s, s.Text)
	}
	s := p.summary(candidate.Tool.Name+"@"+candidate.Server.Name, round, "error", "MCP 工具调用失败")
	local := func(status, text string) (CallSummary, chat.ProtocolMessage) {
		s.Status = status
		s.Text = text
		e.recordLocal(ctx, p, candidate, &s)
		return s, resultMessage(s, s.Text)
	}
	if call.Type != "function" {
		return local("denied", "模型工具调用结构无效")
	}
	if ctx.Err() != nil {
		return local("denied", "MCP 工具阶段已取消或时间预算耗尽")
	}
	current, err := e.Store.ResolveMCPTool(ctx, p.turn.ClassID, candidate.Server.Name, candidate.Tool.Name)
	if err != nil || current == nil || !sameCandidate(candidate, *current) {
		if err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrMCPDenied) {
			return local("error", "MCP 本地授权信息不可用")
		}
		return local("denied", "MCP 工具连接或批准快照已失效")
	}
	target, err := e.Config.Registry.Resolve(p.turn.ClassID, candidate.Server.Name, candidate.Server.BaseURL)
	binding, bindingErr := e.Config.Registry.Reference(p.turn.ClassID, candidate.Server.Name, candidate.Server.BaseURL)
	if err != nil || bindingErr != nil || binding != candidate.Server.CredentialRef {
		return local("denied", "MCP 部署批准或凭据不可用")
	}
	if mcp.CheckDefinitions(target, []mcp.Tool{candidate.Tool}, e.OtherSecrets...) != nil {
		return local("denied", "MCP 工具定义未通过安全检查")
	}
	validator, err := mcp.CompileInputSchema(candidate.Tool.InputSchema)
	if err != nil {
		return local("denied", "MCP 工具输入结构不可用")
	}
	arguments := json.RawMessage(call.Function.Arguments)
	if validator.Validate(arguments) != nil {
		return local("error", "工具参数不符合已批准的输入结构")
	}
	client, err := mcp.NewClient(target, e.Config.RequestTimeout)
	if err != nil {
		return local("denied", "MCP 调用配置不可用")
	}
	auditID, err := e.Store.ReserveMCPCall(ctx, p.attempt(candidate, s), candidate, e.Config.MaxCallsPerConversation)
	if err != nil {
		s.Status = "denied"
		s.Text = "MCP 工具连接或批准快照已失效"
		if errors.Is(err, store.ErrMCPQuota) {
			s.Text = "MCP 会话累计调用额度已耗尽"
			p.stopReason = s.Text
		} else if !errors.Is(err, store.ErrMCPDenied) && !errors.Is(err, store.ErrNotFound) {
			s.Status = "error"
			s.Text = "MCP 调用审计或额度不可用，未执行远端动作"
		}
		return s, resultMessage(s, s.Text)
	}
	started := time.Now()
	var result mcp.CallResult
	err = client.Initialize(ctx)
	if err == nil {
		result, err = client.Call(ctx, candidate.Tool, arguments)
	}
	data := ""
	if err != nil {
		s.Status = "error"
		s.Text = remoteFailure(err)
	} else {
		data, err = filterResult(result, target, e.OtherSecrets...)
		if err != nil {
			s.Status = "error"
			s.Text = "MCP 工具数据未通过安全检查，整条结果未使用"
		} else {
			s.Status = "ok"
			s.Text = limitRunes(data, 500)
		}
	}
	// Only bookkeeping may outlive a cancelled tool request. This bounded cleanup
	// never performs remote actions; a failure leaves the durable interrupted row.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if e.Store.FinishMCPCall(finishCtx, p.turn.ClassID, auditID, s.Status, s.Text, uint64(time.Since(started).Milliseconds())) != nil {
		s.Status = "error"
		s.Text = "MCP 调用审计终结失败，结果未使用"
		data = ""
	}
	if s.Status != "ok" {
		data = s.Text
	}
	return s, resultMessage(s, data)
}

func resultMessage(s CallSummary, data string) chat.ProtocolMessage {
	body, _ := json.Marshal(struct {
		Source string `json:"source"`
		Status string `json:"status"`
		Data   string `json:"data"`
	}{s.Ref, s.Status, data})
	return chat.HostToolResult(s.CallID, string(body))
}
func remoteFailure(err error) string {
	switch {
	case errors.Is(err, mcp.ErrUnauthorized):
		return "MCP 服务认证失败"
	case errors.Is(err, context.DeadlineExceeded):
		return "MCP 工具调用超时"
	case errors.Is(err, context.Canceled):
		return "MCP 工具调用已取消"
	case errors.Is(err, mcp.ErrResponseSize):
		return "MCP 工具响应超过体积上限"
	default:
		return "MCP 服务不可用或响应协议无效"
	}
}
