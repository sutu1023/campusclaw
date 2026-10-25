package skill

import (
	"errors"
	"strings"

	"campusclaw/internal/mcp"
)

const MaxTools = 8

var ErrToolReference = errors.New("工具只接受 knowledge_search 或合法的 工具名@服务器名")

type ToolReference struct {
	Tool    string
	Server  string
	Builtin bool
}

// ParseToolReference is purely syntactic. It never reads configuration or
// databases and cannot grant runtime permission or implicitly connect a server.
func ParseToolReference(raw string) (ToolReference, error) {
	if raw == ToolKnowledgeSearch {
		return ToolReference{Tool: raw, Builtin: true}, nil
	}
	if strings.Count(raw, "@") != 1 {
		return ToolReference{}, ErrToolReference
	}
	tool, server, _ := strings.Cut(raw, "@")
	if !mcp.ValidToolName(tool) || !mcp.ValidServerName(server) {
		return ToolReference{}, ErrToolReference
	}
	return ToolReference{Tool: tool, Server: server}, nil
}

func (ref ToolReference) String() string {
	if ref.Builtin {
		return ref.Tool
	}
	return ref.Tool + "@" + ref.Server
}
