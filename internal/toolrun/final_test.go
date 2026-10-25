package toolrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"campusclaw/internal/chat"
)

func writeGatewayDelta(w http.ResponseWriter, text string) {
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	w.(http.Flusher).Flush()
}
func TestStreamFinalDisablesToolsAndForwardsTrueDeltas(t *testing.T) {
	probe := newExecutionProbe(t)
	e, _, _, turn := fixtureEngine(t, probe.server.URL)
	requests := atomic.Int64{}
	release := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		for _, private := range []string{probe.server.URL, "mcp-only-token", "gateway-only-key"} {
			if strings.Contains(string(raw), private) {
				t.Error("model body leaked target or credential")
			}
		}
		var req struct {
			Stream   bool                   `json:"stream"`
			Choice   string                 `json:"tool_choice"`
			Tools    []chat.ToolDefinition  `json:"tools"`
			Messages []chat.ProtocolMessage `json:"messages"`
		}
		_ = json.Unmarshal(raw, &req)
		n := requests.Add(1)
		if !req.Stream {
			if req.Choice != "auto" {
				t.Error("decision choice missing")
			}
			w.Header().Set("Content-Type", "application/json")
			message := chat.ProtocolMessage{Role: "assistant", Content: "BUFFERED_DECISION_ANSWER"}
			if n == 1 {
				message = chat.HostAssistantCalls([]chat.ToolCall{proposed(req.Tools[0].Function.Name, `{"query":"model chosen query"}`)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}})
			return
		}
		if req.Choice != "none" || len(req.Tools) != 1 || len(req.Messages) != 4 || req.Messages[3].Role != "tool" || !strings.Contains(req.Messages[3].Content, "SAFE_HINT") {
			t.Error("final request lost paired data or did not disable tools")
		}
		if strings.Contains(req.Messages[0].Content, "SAFE_HINT") || strings.Contains(req.Messages[1].Content, "SAFE_HINT") {
			t.Error("tool result elevated to body/system role")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeGatewayDelta(w, "先核对")
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeGatewayDelta(w, "适用条件。")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer gateway.Close()
	client := chat.New(gateway.URL, "gateway-only-key", "stub")
	e.Gateway = client
	p := e.Run(context.Background(), turn, nil)
	if requests.Load() != 2 || len(p.Calls) != 1 || !p.UseTools {
		t.Fatal("decision phase did not finish independently")
	}
	deltas := make(chan string, 2)
	done := make(chan error, 1)
	go func() {
		done <- p.StreamFinal(context.Background(), client, func(text string) error { deltas <- text; return nil })
	}()
	select {
	case first := <-deltas:
		if first != "先核对" {
			close(release)
			t.Fatal("buffered decision leaked into final answer")
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("first true delta was buffered")
	}
	select {
	case <-done:
		close(release)
		t.Fatal("final response completed before upstream second delta")
	default:
	}
	close(release)
	select {
	case second := <-deltas:
		if second != "适用条件。" {
			t.Fatal("delta was cumulative rather than incremental")
		}
	case <-time.After(time.Second):
		t.Fatal("missing second delta")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStreamFinalPlainAndProtocolFallbackUseOriginalBodyOnly(t *testing.T) {
	for _, mode := range []string{"plain", "unsupported", "failure_after_success"} {
		t.Run(mode, func(t *testing.T) {
			probe := newExecutionProbe(t)
			e, repo, _, turn := fixtureEngine(t, probe.server.URL)
			if mode == "plain" {
				turn.Skill = nil
			}
			decisions := atomic.Int64{}
			finals := atomic.Int64{}
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var req map[string]json.RawMessage
				_ = json.Unmarshal(raw, &req)
				if string(req["stream"]) == "false" {
					n := decisions.Add(1)
					w.Header().Set("Content-Type", "application/json")
					if mode == "failure_after_success" && n == 1 {
						_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": chat.HostAssistantCalls([]chat.ToolCall{proposed(chat.ToolAlias(7, "formula_hint"), `{"query":"valid"}`)})}}})
						return
					}
					w.WriteHeader(400)
					_, _ = fmt.Fprint(w, "private gateway body mcp-only-token")
					return
				}
				finals.Add(1)
				if len(req) != 3 || req["tools"] != nil || req["tool_choice"] != nil || strings.Contains(string(raw), "tool_call") || strings.Contains(string(raw), "SAFE_HINT") {
					t.Error("fallback retained intermediate protocol or changed old request shape")
				}
				var messages []chat.Message
				_ = json.Unmarshal(req["messages"], &messages)
				if len(messages) != len(turn.Messages) || messages[0].Content != turn.Messages[0].Content || messages[1].Content != turn.Messages[1].Content {
					t.Error("fallback promoted data rather than restoring original trusted body")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				writeGatewayDelta(w, "普通")
				writeGatewayDelta(w, "回答")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer gateway.Close()
			client := chat.New(gateway.URL, "gateway-only-key", "stub")
			e.Gateway = client
			p := e.Run(context.Background(), turn, nil)
			var text strings.Builder
			if err := p.StreamFinal(context.Background(), client, func(delta string) error { text.WriteString(delta); return nil }); err != nil {
				t.Fatal(err)
			}
			if p.UseTools || finals.Load() != 1 || text.String() != "普通回答" {
				t.Fatal("plain final did not succeed")
			}
			if mode == "plain" {
				if decisions.Load() != 0 || len(p.Calls) != 0 {
					t.Fatal("plain chat added tool latency")
				}
			} else if p.Calls[len(p.Calls)-1].Status != "error" {
				t.Fatal("protocol failure was not explicit")
			}
			want := 0
			if mode == "failure_after_success" {
				want = 1
			}
			if len(repo.reserved) != want || len(repo.finished) != want || probe.calls.Load() != int64(want) {
				t.Fatal("fallback fabricated successful audit/action")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if p.StreamFinal(ctx, client, func(string) error { return nil }) == nil || finals.Load() != 1 {
				t.Fatal("cancelled client started final generation")
			}
		})
	}
}
