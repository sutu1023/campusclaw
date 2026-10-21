package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompleteMessagesStreamSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req streamRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("expected stream:true in the request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, frag := range []string{"先看", "这一步", "。"} {
			payload, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]string{"content": frag}}},
			})
			_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	c := New(srv.URL, "", "stub")
	var got strings.Builder
	calls := 0
	err := c.CompleteMessagesStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(text string) error {
		calls++
		got.WriteString(text)
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 deltas, got %d", calls)
	}
	if got.String() != "先看这一步。" {
		t.Fatalf("unexpected concatenation: %q", got.String())
	}
}

func TestCompleteMessagesStreamNonStreamingFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "完整回答"}}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "", "stub")
	var got strings.Builder
	calls := 0
	err := c.CompleteMessagesStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(text string) error {
		calls++
		got.WriteString(text)
		return nil
	})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if calls != 1 || got.String() != "完整回答" {
		t.Fatalf("expected a single full delta, got calls=%d text=%q", calls, got.String())
	}
}

func TestCompleteMessagesStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		payload, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": "半"}}}})
		_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"error\":\"boom\"}\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	c := New(srv.URL, "", "stub")
	err := c.CompleteMessagesStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected a stream error")
	}
}
