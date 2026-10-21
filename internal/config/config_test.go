package config

import "testing"

// setBase sets the two required values plus clears gateway vars so each test
// controls exactly what Load sees.
func setBase(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DSN", "user:pass@tcp(db:3306)/campusclaw?parseTime=true")
	t.Setenv("TOKEN_SECRET", "test-secret")
	t.Setenv("EMBEDDING_BASE_URL", "")
	t.Setenv("EMBEDDING_API_KEY", "")
	t.Setenv("CHAT_BASE_URL", "")
	t.Setenv("CHAT_API_KEY", "")
	t.Setenv("CHAT_MODEL", "")
}

func TestChatGatewayFallsBackToEmbedding(t *testing.T) {
	setBase(t)
	t.Setenv("EMBEDDING_BASE_URL", "https://gateway.example/v1")
	t.Setenv("EMBEDDING_API_KEY", "embed-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ChatBaseURL != "https://gateway.example/v1" {
		t.Fatalf("expected chat base URL to fall back, got %q", cfg.ChatBaseURL)
	}
	if cfg.ChatAPIKey != "embed-key" {
		t.Fatalf("expected chat API key to fall back, got %q", cfg.ChatAPIKey)
	}
	if cfg.ChatModel != "course-chat" {
		t.Fatalf("expected default chat model course-chat, got %q", cfg.ChatModel)
	}
}

func TestExplicitChatGatewayWins(t *testing.T) {
	setBase(t)
	t.Setenv("EMBEDDING_BASE_URL", "https://gateway.example/v1")
	t.Setenv("EMBEDDING_API_KEY", "embed-key")
	t.Setenv("CHAT_BASE_URL", "https://chat.example/v1")
	t.Setenv("CHAT_API_KEY", "chat-key")
	t.Setenv("CHAT_MODEL", "my-chat-model")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ChatBaseURL != "https://chat.example/v1" {
		t.Fatalf("explicit chat base URL must win, got %q", cfg.ChatBaseURL)
	}
	if cfg.ChatAPIKey != "chat-key" {
		t.Fatalf("explicit chat API key must win, got %q", cfg.ChatAPIKey)
	}
	if cfg.ChatModel != "my-chat-model" {
		t.Fatalf("explicit chat model must win, got %q", cfg.ChatModel)
	}
}

func TestMissingTokenSecretFailsFast(t *testing.T) {
	setBase(t)
	t.Setenv("TOKEN_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected Load to fail without TOKEN_SECRET")
	}
}
