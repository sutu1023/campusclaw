package toolrun

import (
	"context"

	"campusclaw/internal/chat"
)

type FinalGateway interface {
	CompleteMessagesStream(context.Context, []chat.Message, chat.DeltaFunc) error
	CompleteToolsDisabledStream(context.Context, []chat.ProtocolMessage, []chat.ToolDefinition, chat.DeltaFunc) error
}

// StreamFinal uses an independent parent context. Protocol fallback uses only
// the original trusted body turns: no tool result is ever promoted to user or
// system just to get an incompatible gateway to accept the request.
func (p *Plan) StreamFinal(ctx context.Context, gateway FinalGateway, onDelta chat.DeltaFunc) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if p.UseTools {
		return gateway.CompleteToolsDisabledStream(ctx, p.Messages, p.Tools, onDelta)
	}
	return gateway.CompleteMessagesStream(ctx, p.turn.Messages, onDelta)
}
