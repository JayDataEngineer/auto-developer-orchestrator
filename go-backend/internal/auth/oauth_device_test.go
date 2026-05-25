package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestToken_IsExpired(t *testing.T) {
	tests := []struct {
		name   string
		token  Token
		buffer time.Duration
		want   bool
	}{
		{
			name:   "empty token",
			token:  Token{},
			buffer: 0,
			want:   true,
		},
		{
			name:   "future expiry",
			token:  Token{AccessToken: "abc", Expiry: time.Now().Add(1 * time.Hour)},
			buffer: 5 * time.Minute,
			want:   false,
		},
		{
			name:   "past expiry",
			token:  Token{AccessToken: "abc", Expiry: time.Now().Add(-1 * time.Hour)},
			buffer: 5 * time.Minute,
			want:   true,
		},
		{
			name:   "expiring within buffer",
			token:  Token{AccessToken: "abc", Expiry: time.Now().Add(3 * time.Minute)},
			buffer: 5 * time.Minute,
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsExpired(tt.buffer); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	store := NewTokenStore(path)

	token := &Token{
		AccessToken:  "ya29.test-access-token",
		RefreshToken: "1//test-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
	}

	if err := store.Set("google", token); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Load into a new store
	store2 := NewTokenStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	got := store2.Get("google")
	if got == nil {
		t.Fatal("expected token, got nil")
	}
	if got.AccessToken != token.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, token.AccessToken)
	}
	if got.RefreshToken != token.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, token.RefreshToken)
	}
}

func TestTokenStore_Permissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	store := NewTokenStore(path)
	store.Set("test", &Token{AccessToken: "secret"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	// File should be 0600 (owner read/write only)
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestTokenStore_MissingFile(t *testing.T) {
	store := NewTokenStore("/nonexistent/path/tokens.json")
	// Load should not error on missing file
	if err := store.Load(); err != nil {
		t.Errorf("Load on missing file should not error: %v", err)
	}
}

func TestTokenStore_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	store := NewTokenStore(path)
	token := &Token{
		AccessToken:  "abc123",
		RefreshToken: "refresh456",
		TokenType:    "Bearer",
		Expiry:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	store.Set("test-provider", token)

	// Read raw file and verify it's human-readable JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]*Token
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("file is not valid JSON: %v", err)
	}

	if _, ok := parsed["test-provider"]; !ok {
		t.Error("expected test-provider key in JSON")
	}
}

func TestGetOrRefreshToken_NoToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(filepath.Join(dir, "tokens.json"))

	provider := OAuthProvider{
		Name:         "test",
		TokenURL:     "https://example.com/token",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Scope:        "read",
	}

	token, err := GetOrRefreshToken(context.Background(), provider, store)
	if err != nil {
		t.Errorf("expected no error for missing token, got: %v", err)
	}
	if token != nil {
		t.Error("expected nil token when no token exists")
	}
}

func TestGetOrRefreshToken_ValidToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(filepath.Join(dir, "tokens.json"))

	store.Set("test", &Token{
		AccessToken:  "valid-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
	})

	provider := OAuthProvider{Name: "test"}

	token, err := GetOrRefreshToken(context.Background(), provider, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.AccessToken != "valid-token" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "valid-token")
	}
}

func TestDefaultTokenPath(t *testing.T) {
	path := DefaultTokenPath()
	if path == "" {
		t.Error("expected non-empty path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
}
