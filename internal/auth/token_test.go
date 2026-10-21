package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHashTokenIsDeterministicAndKeyed(t *testing.T) {
	a := HashToken("secret", "token-1")
	b := HashToken("secret", "token-1")
	if a != b {
		t.Fatalf("hash must be deterministic: %s vs %s", a, b)
	}
	if a == HashToken("other-secret", "token-1") {
		t.Fatal("hash must depend on the secret")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(a))
	}
}

func TestNewTokenIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := NewToken()
		if err != nil {
			t.Fatalf("new token: %v", err)
		}
		if tok == "" || seen[tok] {
			t.Fatalf("token not unique or empty: %q", tok)
		}
		seen[tok] = true
	}
}

func TestMiddlewareRejectsMissingTokenWithoutStore(t *testing.T) {
	reached := false
	h := Middleware(nil, "secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/materials", nil)
	h.ServeHTTP(rec, req)

	if reached {
		t.Fatal("handler must not run without a token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestBearerTokenParsing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := BearerToken(req); ok {
		t.Fatal("expected no token")
	}
	req.Header.Set("Authorization", "Bearer abc123")
	tok, ok := BearerToken(req)
	if !ok || tok != "abc123" {
		t.Fatalf("expected abc123, got %q ok=%v", tok, ok)
	}
	req.Header.Set("Authorization", "bearer xyz")
	if tok, ok := BearerToken(req); !ok || tok != "xyz" {
		t.Fatalf("scheme must be case-insensitive, got %q ok=%v", tok, ok)
	}
}
