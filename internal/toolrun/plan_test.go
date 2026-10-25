package toolrun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"campusclaw/internal/chat"
	"campusclaw/internal/config"
	"campusclaw/internal/mcp"
	"campusclaw/internal/skill"
	"campusclaw/internal/store"
)

var querySchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500}},"required":["query"],"additionalProperties":false}`)

type fakeRepository struct {
	mu            sync.Mutex
	candidates    map[string]store.MCPToolCandidate
	resolves      int
	local         []store.MCPCallAttempt
	reserved      []store.MCPCallAttempt
	finished      []string
	reserveErr    error
	localErr      error
	finishErr     error
	beforeReserve func()
}

func (f *fakeRepository) ResolveMCPTool(_ context.Context, classID int64, serverName, toolName string) (*store.MCPToolCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolves++
	c, ok := f.candidates[toolName+"@"+serverName]
	if !ok || c.Server.ClassID != classID {
		return nil, store.ErrNotFound
	}
	if c.Server.State != store.MCPConnected {
		return &c, store.ErrMCPDenied
	}
	return &c, nil
}
func (f *fakeRepository) RecordMCPAttempt(_ context.Context, a store.MCPCallAttempt, _, _ string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.localErr != nil {
		return 0, f.localErr
	}
	f.local = append(f.local, a)
	return int64(len(f.local)), nil
}
func (f *fakeRepository) ReserveMCPCall(_ context.Context, a store.MCPCallAttempt, c store.MCPToolCandidate, _ int) (int64, error) {
	if f.beforeReserve != nil {
		f.beforeReserve()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reserveErr != nil {
		return 0, f.reserveErr
	}
	current, ok := f.candidates[c.Tool.Name+"@"+c.Server.Name]
	if !ok || !sameCandidate(c, current) {
		return 1, store.ErrMCPDenied
	}
	f.reserved = append(f.reserved, a)
	return int64(len(f.reserved)), nil
}
func (f *fakeRepository) FinishMCPCall(_ context.Context, _, _ int64, status, _ string, _ uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finishErr != nil {
		return f.finishErr
	}
	f.finished = append(f.finished, status)
	return nil
}

type fakeGateway struct {
	calls  int
	reply  chat.ProtocolMessage
	err    error
	decide func([]chat.ProtocolMessage, []chat.ToolDefinition) (chat.ProtocolMessage, error)
}

func (f *fakeGateway) DecideTools(_ context.Context, msg []chat.ProtocolMessage, defs []chat.ToolDefinition) (chat.ProtocolMessage, error) {
	f.calls++
	if f.decide != nil {
		return f.decide(msg, defs)
	}
	return f.reply, f.err
}
func fixtureEngine(t *testing.T, origin string) (*Engine, *fakeRepository, *fakeGateway, Turn) {
	t.Helper()
	raw, _ := json.Marshal([]mcp.Binding{{ClassID: 1, ServerName: "campus-tools", BaseURL: origin, TokenEnv: "MCP_REMOTE_TOKEN"}})
	registry, err := mcp.NewRegistry(string(raw), func(string) string { return "mcp-only-token" })
	if err != nil {
		t.Fatal(err)
	}
	binding, err := registry.Reference(1, "campus-tools", origin)
	if err != nil {
		t.Fatal(err)
	}
	server := store.MCPServer{ID: 7, ClassID: 1, Name: "campus-tools", BaseURL: origin, CredentialRef: binding, State: store.MCPConnected, ApprovedHash: strings.Repeat("a", 64), Generation: 1}
	repo := &fakeRepository{candidates: map[string]store.MCPToolCandidate{}}
	for _, name := range []string{"formula_hint", "unit_convert"} {
		repo.candidates[name+"@campus-tools"] = store.MCPToolCandidate{Server: server, Tool: mcp.Tool{Name: name, Description: "教学提示", InputSchema: querySchema}}
	}
	gateway := &fakeGateway{reply: chat.ProtocolMessage{Role: "assistant", Content: "No tools needed"}}
	engine := &Engine{Store: repo, Gateway: gateway, Config: config.MCPConfig{Registry: registry, MaxRounds: 3, MaxCallsPerTurn: 8, MaxCallsPerConversation: 40, TurnTimeout: time.Second, RequestTimeout: time.Second}, OtherSecrets: []string{"gateway-only-key"}}
	turn := Turn{ClassID: 1, UserID: 11, ConversationID: 13, QuestionMessageID: 17, Skill: &skill.Entry{ID: 19, Name: "Math", Enabled: true, Tools: []string{"formula_hint@campus-tools"}}, Messages: []chat.Message{{Role: "system", Content: "Host guidance"}, {Role: "user", Content: "勾股公式怎么用"}}}
	return engine, repo, gateway, turn
}

func TestPrepareNoMCPHasNoDecisionOrOutbound(t *testing.T) {
	e, repo, g, turn := fixtureEngine(t, "http://mcp-demo:8090")
	for _, matched := range []*skill.Entry{nil, {ID: 1, Tools: []string{"knowledge_search"}}} {
		turn.Skill = matched
		p := e.Prepare(context.Background(), turn, nil)
		decision, err := e.Decide(context.Background(), p)
		if err != nil || len(decision.ToolCalls) != 0 || len(p.Tools) != 0 || len(p.Calls) != 0 {
			t.Fatal("ordinary chat changed")
		}
	}
	if repo.resolves != 0 || g.calls != 0 || len(repo.reserved) != 0 {
		t.Fatal("plain chat entered tool phase")
	}
}
func TestPrepareOnlyMatchedCandidateAndModelCanStop(t *testing.T) {
	outbound := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { outbound++ }))
	defer srv.Close()
	e, repo, g, turn := fixtureEngine(t, srv.URL)
	p := e.Prepare(context.Background(), turn, nil)
	if len(p.Tools) != 1 || repo.resolves != 1 {
		t.Fatal("class-wide directory leaked into candidates")
	}
	if _, ok := p.selectCandidate(chat.ToolAlias(7, "unit_convert")); ok {
		t.Fatal("unselected tool was authorized")
	}
	if _, ok := p.selectCandidate("invented_function"); ok {
		t.Fatal("unknown alias was authorized")
	}
	decision, err := e.Decide(context.Background(), p)
	if err != nil || len(decision.ToolCalls) != 0 || g.calls != 1 || outbound != 0 || len(repo.reserved) != 0 {
		t.Fatal("model stop caused a remote call")
	}
	raw, _ := json.Marshal(p.Tools)
	for _, private := range []string{srv.URL, "mcp-only-token", "gateway-only-key", "token_env", "credential"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("declaration leaked deployment data")
		}
	}
}
func TestPrepareStaleMissingAndForeignReferencesAreDenied(t *testing.T) {
	e, repo, g, turn := fixtureEngine(t, "http://mcp-demo:8090")
	c := repo.candidates["formula_hint@campus-tools"]
	c.Server.State = store.MCPDisconnected
	repo.candidates["formula_hint@campus-tools"] = c
	foreign := c
	foreign.Server.ClassID = 2
	foreign.Server.Name = "foreign-tools"
	repo.candidates["formula_hint@foreign-tools"] = foreign
	turn.Skill.Tools = []string{"formula_hint@campus-tools", "formula_hint@foreign-tools", "formula_hint@absent-tools"}
	emissions := 0
	p := e.Prepare(context.Background(), turn, func(c []CallSummary) { emissions += len(c) })
	if len(p.Tools) != 0 || len(p.Calls) != 3 || emissions != 3 || len(repo.local) != 1 {
		t.Fatal("stale/missing reference accounting incorrect")
	}
	for _, call := range p.Calls {
		if call.Status != "denied" || call.Round != 0 {
			t.Fatal("preflight failure was not a denial")
		}
	}
	_, _ = e.Decide(context.Background(), p)
	if g.calls != 0 || len(repo.reserved) != 0 {
		t.Fatal("unavailable candidate reached gateway/action")
	}
	repo.localErr = errors.New("private database diagnostics")
	p = e.Prepare(context.Background(), turn, nil)
	if p.Calls[0].Status != "error" || strings.Contains(p.Calls[0].Text, "diagnostics") {
		t.Fatal("audit error leaked diagnostics")
	}
}
