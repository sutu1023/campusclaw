package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolFinalStreamRejectsBufferedJSONAndNewToolCalls(t *testing.T) {
	def, err := NewToolDefinition(ToolAlias(1, "hint"), "hint", toolTestSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"json", "new_tool", "stream_error", "http_error"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "json" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"BUFFERED_ANSWER"}}]}`)
					return
				}
				if mode == "http_error" {
					w.WriteHeader(500)
					_, _ = fmt.Fprint(w, "private-upstream-secret")
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == "stream_error" {
					_, _ = fmt.Fprint(w, "data: {\"error\":\"private-upstream-secret\"}\n\n")
					return
				}
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"new_call\"}],\"content\":\"not allowed\"}}]}\n\n")
			}))
			defer srv.Close()
			count := 0
			err := New(srv.URL, "", "stub").CompleteToolsDisabledStream(context.Background(), HostMessages([]Message{{Role: "user", Content: "question"}}), []ToolDefinition{def}, func(string) error { count++; return nil })
			if err == nil || count != 0 || strings.Contains(err.Error(), "private-upstream-secret") {
				t.Fatal("final tool path faked streaming, accepted a new action or leaked diagnostics")
			}
		})
	}
}
