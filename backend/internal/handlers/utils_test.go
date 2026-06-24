package handlers

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/auto-developer-orchestrator/backend/internal/util"
	"go.uber.org/zap"
)

// ── Truncate ──────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
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
		got := util.Truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
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

// ── ParseSSHURL ──────────────────────────────────────────────

func TestParseSSHURL(t *testing.T) {
	tests := []struct {
		raw      string
		user     string
		host     string
		port     string
		sshPath  string
		ok       bool
	}{
		{"ssh://ubuntu@192.0.2.1/home/user/project", "ubuntu", "192.0.2.1", "22", "/home/user/project", true},
		{"ssh://admin@my-server:2222/var/www", "admin", "my-server", "2222", "/var/www", true},
		{"ssh://root@192.168.1.1/", "root", "192.168.1.1", "22", "/", true},
		{"ssh://deploy@10.0.0.1", "deploy", "10.0.0.1", "22", "/", true},
		{"http://example.com", "", "", "", "", false},
		{"not-ssh://user@host/path", "", "", "", "", false},
		{"ssh://missing-at-host/path", "", "", "", "", false},
		{"", "", "", "", "", false},
	}

	for _, tt := range tests {
		info, ok := ParseSSHURL(tt.raw)
		if ok != tt.ok {
			t.Errorf("ParseSSHURL(%q) ok = %v, want %v", tt.raw, ok, tt.ok)
			continue
		}
		if !tt.ok {
			continue
		}
		if info.User != tt.user {
			t.Errorf("ParseSSHURL(%q) User = %q, want %q", tt.raw, info.User, tt.user)
		}
		if info.Host != tt.host {
			t.Errorf("ParseSSHURL(%q) Host = %q, want %q", tt.raw, info.Host, tt.host)
		}
		if info.Port != tt.port {
			t.Errorf("ParseSSHURL(%q) Port = %q, want %q", tt.raw, info.Port, tt.port)
		}
		if info.Path != tt.sshPath {
			t.Errorf("ParseSSHURL(%q) Path = %q, want %q", tt.raw, info.Path, tt.sshPath)
		}
	}
}

// ── resolveProjectFS ─────────────────────────────────────────

func TestResolveProjectFSEmptyProject(t *testing.T) {
	_, err := resolveProjectFS("", nil, nil)
	if err == nil {
		t.Error("expected error for empty project")
	}
}

func TestResolveProjectFSLocalPath(t *testing.T) {
	dir := t.TempDir()
	fs, err := resolveProjectFS(dir, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fs.Type() != "local" {
		t.Errorf("expected local FS, got %q", fs.Type())
	}
	if fs.Root() != dir {
		t.Errorf("expected root %q, got %q", dir, fs.Root())
	}
}

func TestResolveProjectFSFromDB(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "my-local-proj")
	os.MkdirAll(projectDir, 0755)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	db.AddCustomProject(ctx, "my-local-proj", projectDir)

	fs, err := resolveProjectFS("my-local-proj", db, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fs.Type() != "local" {
		t.Errorf("expected local FS, got %q", fs.Type())
	}
}

func TestResolveProjectFSSSHFromDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	db.AddCustomProject(ctx, "remote-proj", "ssh://ubuntu@192.0.2.1/home/user/project")

	// Without SSH manager — should get error
	_, err = resolveProjectFS("remote-proj", db, nil)
	if err == nil {
		t.Error("expected error when SSH manager is nil for SSH project")
	}
	if !strings.Contains(err.Error(), "SSH not available") {
		t.Errorf("expected SSH not available error, got: %v", err)
	}
}

func TestResolveProjectFSSSHReturnsSshFS(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	db.AddCustomProject(ctx, "remote-proj", "ssh://admin@my-server:2222/var/www")

	// With SSH manager (no real connections needed for resolution)
	sshMgr := puxssh.NewSessionManager(zap.NewNop())

	fs, err := resolveProjectFS("remote-proj", db, sshMgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fs.Type() != "ssh" {
		t.Errorf("expected ssh FS, got %q", fs.Type())
	}
	ssh := fs.SSHInfo()
	if ssh == nil {
		t.Fatal("expected SSHInfo to be non-nil")
	}
	if ssh.User != "admin" {
		t.Errorf("expected user 'admin', got %q", ssh.User)
	}
	if ssh.Host != "my-server" {
		t.Errorf("expected host 'my-server', got %q", ssh.Host)
	}
	if ssh.Port != "2222" {
		t.Errorf("expected port '2222', got %q", ssh.Port)
	}
}

func TestResolveProjectFSNotFound(t *testing.T) {
	t.Setenv("PROJECT_ROOT", t.TempDir())
	_, err := resolveProjectFS("nonexistent", nil, nil)
	if err == nil {
		t.Error("expected error for nonexistent project")
	}
}

func TestResolveProjectFSInvalidSSHURL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	// URL with :// but not a valid SSH URL
	db.AddCustomProject(ctx, "bad-proj", "jules://some-url")

	_, err = resolveProjectFS("bad-proj", db, puxssh.NewSessionManager(zap.NewNop()))
	if err == nil {
		t.Error("expected error for invalid SSH URL")
	}
}

// ── LocalFS Resolve ─────────────────────────────────────────

func TestLocalFSResolve(t *testing.T) {
	dir := t.TempDir()
	fs := NewLocalFS(dir)

	// Normal path
	p, err := fs.Resolve("src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(dir, "src", "main.go")
	if p != expected {
		t.Errorf("expected %q, got %q", expected, p)
	}

	// Path traversal should fail
	_, err = fs.Resolve("../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
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
