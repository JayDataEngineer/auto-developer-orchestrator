package handlers

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

// ── truncateStr ───────────────────────────────────────────────

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello"},
		{"short", 5, "short"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateStr(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

// ── resolveProjectPath ────────────────────────────────────────

func TestResolveProjectPathEmpty(t *testing.T) {
	result := resolveProjectPath("", nil)
	if result != "" {
		t.Errorf("expected empty for empty project, got %q", result)
	}
}

func TestResolveProjectPathFromEnv(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "my-proj")
	os.MkdirAll(projectDir, 0755)
	t.Setenv("PROJECT_ROOT", dir)

	result := resolveProjectPath("my-proj", nil)
	if result != projectDir {
		t.Errorf("expected %q, got %q", projectDir, result)
	}
}

func TestResolveProjectPathNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROJECT_ROOT", dir)

	result := resolveProjectPath("nonexistent", nil)
	if result != "" {
		t.Errorf("expected empty for missing project, got %q", result)
	}
}

func TestResolveProjectPathFromDB(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "db-proj")
	os.MkdirAll(projectDir, 0755)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	db.AddCustomProject(ctx, "db-proj", projectDir)

	result := resolveProjectPath("db-proj", db)
	if result != projectDir {
		t.Errorf("expected %q, got %q", projectDir, result)
	}
}

func TestResolveProjectPathDBSkipsURLSchemes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROJECT_ROOT", dir)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, _ := storage.NewDatabase(dbPath)
	defer db.Close()

	ctx := context.Background()
	db.AddCustomProject(ctx, "remote-proj", "jules://some-url")

	result := resolveProjectPath("remote-proj", db)
	if result != "" {
		t.Errorf("expected empty for URL-only project with no dir, got %q", result)
	}
}

// ── GitHubTokenStore ──────────────────────────────────────────

func TestGitHubTokenStoreEmpty(t *testing.T) {
	os.Unsetenv("GITHUB_TOKEN")
	store := NewGitHubTokenStore()
	if store.Get() != "" {
		t.Error("expected empty token")
	}
}

func TestGitHubTokenStoreSetGet(t *testing.T) {
	store := NewGitHubTokenStore()
	store.Set("my-token")
	if got := store.Get(); got != "my-token" {
		t.Errorf("expected 'my-token', got %q", got)
	}
}

func TestGitHubTokenStoreFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	store := NewGitHubTokenStore()
	if got := store.Get(); got != "env-token" {
		t.Errorf("expected 'env-token', got %q", got)
	}
}

// ── parseOwnerRepo ────────────────────────────────────────────

func TestParseOwnerRepo(t *testing.T) {
	tests := []struct {
		url      string
		owner    string
		repo     string
		hasError bool
	}{
		{"https://github.com/owner/repo.git", "owner", "repo", false},
		{"https://github.com/owner/repo", "owner", "repo", false},
		{"git@github.com:owner/repo.git", "owner", "repo", false},
		{"git@github.com:my-org/my-project.git", "my-org", "my-project", false},
		{"https://gitlab.com/owner/repo.git", "", "", true},
		{"not-a-url", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		owner, repo, err := parseOwnerRepo(tt.url)
		if tt.hasError {
			if err == nil {
				t.Errorf("parseOwnerRepo(%q) expected error", tt.url)
			}
		} else {
			if err != nil {
				t.Errorf("parseOwnerRepo(%q) unexpected error: %v", tt.url, err)
			}
			if owner != tt.owner || repo != tt.repo {
				t.Errorf("parseOwnerRepo(%q) = (%q, %q), want (%q, %q)", tt.url, owner, repo, tt.owner, tt.repo)
			}
		}
	}
}

// ── setSSEHeaders ─────────────────────────────────────────────

func TestSetSSEHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	setSSEHeaders(w)

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected no-cache, got %q", cc)
	}
	if conn := w.Header().Get("Connection"); conn != "keep-alive" {
		t.Errorf("expected keep-alive, got %q", conn)
	}
	if xb := w.Header().Get("X-Accel-Buffering"); xb != "no" {
		t.Errorf("expected no, got %q", xb)
	}
}
