// Package config loads runtime configuration from environment variables.
//
// Secrets (TOKEN_SECRET, DB_DSN, gateway API keys) are read only from the
// environment. The application refuses to start when a required value is
// missing, so a deployment can never silently fall back to an insecure default.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration.
type Config struct {
	Addr           string
	DBDSN          string
	UploadDir      string
	TokenSecret    string
	SessionTTL     time.Duration
	MaxUploadBytes int64

	QdrantURL        string
	QdrantCollection string

	EmbeddingBaseURL string
	EmbeddingAPIKey  string
	EmbeddingModel   string
	EmbeddingDim     int

	ChatBaseURL string
	ChatAPIKey  string
	ChatModel   string

	VectorMinScore float64
	RRFK           int

	MCP MCPConfig
}

// Load reads configuration from the environment and validates required values.
func Load() (*Config, error) {
	c := &Config{
		Addr:             env("ADDR", ":8080"),
		DBDSN:            os.Getenv("DB_DSN"),
		UploadDir:        env("UPLOAD_DIR", "/app/uploads"),
		TokenSecret:      os.Getenv("TOKEN_SECRET"),
		SessionTTL:       time.Hour * 24,
		MaxUploadBytes:   envInt64("MAX_UPLOAD_BYTES", 16*1024*1024),
		QdrantURL:        env("QDRANT_URL", "http://qdrant:6333"),
		QdrantCollection: env("QDRANT_COLLECTION", "campusclaw_chunks"),
		EmbeddingBaseURL: env("EMBEDDING_BASE_URL", ""),
		EmbeddingAPIKey:  env("EMBEDDING_API_KEY", ""),
		EmbeddingModel:   env("EMBEDDING_MODEL", "text-embedding-3-small"),
		EmbeddingDim:     envInt("EMBEDDING_DIM", 1536),
		ChatBaseURL:      env("CHAT_BASE_URL", ""),
		ChatAPIKey:       env("CHAT_API_KEY", ""),
		ChatModel:        env("CHAT_MODEL", "course-chat"),
		VectorMinScore:   envFloat("VECTOR_MIN_SCORE", 0.35),
		RRFK:             envInt("RRF_K", 60),
	}

	if raw := os.Getenv("SESSION_TTL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid SESSION_TTL %q: %w", raw, err)
		}
		c.SessionTTL = d
	}

	if c.DBDSN == "" {
		return nil, fmt.Errorf("DB_DSN is required (MySQL DSN, e.g. user:pass@tcp(db:3306)/campusclaw?parseTime=true)")
	}
	if c.TokenSecret == "" {
		return nil, fmt.Errorf("TOKEN_SECRET is required and must not be hardcoded")
	}

	var err error
	c.MCP, err = loadMCP(os.Getenv)
	if err != nil {
		return nil, err
	}

	// The chat gateway reuses the embedding gateway's API unless chat-specific
	// values are provided explicitly. Explicit values always win.
	if c.ChatBaseURL == "" {
		c.ChatBaseURL = c.EmbeddingBaseURL
	}
	if c.ChatAPIKey == "" {
		c.ChatAPIKey = c.EmbeddingAPIKey
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
