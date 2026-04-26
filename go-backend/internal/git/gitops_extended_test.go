package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func setupGitRepo(t *testing.T) (*GitOps, string) {
	t.Helper()
	logger := zap.NewNop()
	g := NewGitOps(logger)
	ctx := context.Background()

	dir := t.TempDir()
	if err := g.InitRepository(ctx, dir); err != nil {
		t.Fatalf("InitRepository: %v", err)
	}

	// Add a file and commit so HEAD exists
	testFile := filepath.Join(dir, "README.md")
	os.WriteFile(testFile, []byte("# Test"), 0644)
	g.runGitCmd(ctx, dir, "add", ".")
	g.runGitCmd(ctx, dir, "commit", "-m", "Initial commit")
	g.runGitCmd(ctx, dir, "remote", "add", "origin", "https://github.com/test/repo.git")

	return g, dir
}

// ── Push ──────────────────────────────────────────────────────

func TestPushNoRemote(t *testing.T) {
	g, dir := setupGitRepo(t)
	// Remove the remote to test error handling
	g.runGitCmd(context.Background(), dir, "remote", "remove", "origin")

	err := g.Push(context.Background(), PushOptions{Dir: dir})
	if err == nil {
		t.Error("expected error pushing without remote")
	}
}

func TestPushOptions(t *testing.T) {
	g, dir := setupGitRepo(t)
	ctx := context.Background()

	// Push with Force and custom Remote/Branch should attempt the push
	err := g.Push(ctx, PushOptions{
		Dir:    dir,
		Remote: "origin",
		Branch: "main",
		Force:  true,
	})
	// Will fail since there's no actual remote, but should not panic
	if err == nil {
		t.Log("push succeeded (unexpected but ok)")
	}
}

// ── Pull ──────────────────────────────────────────────────────

func TestPullNoRemote(t *testing.T) {
	g, dir := setupGitRepo(t)
	g.runGitCmd(context.Background(), dir, "remote", "remove", "origin")

	err := g.Pull(context.Background(), PullOptions{Dir: dir})
	if err == nil {
		t.Error("expected error pulling without remote")
	}
}

func TestPullWithOptions(t *testing.T) {
	g, dir := setupGitRepo(t)
	ctx := context.Background()

	err := g.Pull(ctx, PullOptions{
		Dir:    dir,
		Remote: "origin",
		Branch: "main",
		Rebase: true,
	})
	if err == nil {
		t.Log("pull succeeded (unexpected but ok)")
	}
}

// ── Checkout ──────────────────────────────────────────────────

func TestCheckoutNewBranch(t *testing.T) {
	g, dir := setupGitRepo(t)
	ctx := context.Background()

	err := g.Checkout(ctx, CheckoutOptions{
		Dir:       dir,
		Branch:    "feature-test",
		CreateNew: true,
	})
	if err != nil {
		t.Fatalf("Checkout new branch: %v", err)
	}

	branch, err := g.GetCurrentBranch(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature-test" {
		t.Errorf("expected 'feature-test', got %q", branch)
	}
}

func TestCheckoutExistingBranch(t *testing.T) {
	g, dir := setupGitRepo(t)
	ctx := context.Background()

	// Create a branch first
	g.Checkout(ctx, CheckoutOptions{Dir: dir, Branch: "feature-1", CreateNew: true})

	// Go back to master
	err := g.Checkout(ctx, CheckoutOptions{Dir: dir, Branch: "master"})
	if err != nil {
		t.Fatalf("Checkout existing: %v", err)
	}

	branch, _ := g.GetCurrentBranch(ctx, dir)
	if branch != "master" {
		t.Errorf("expected 'master', got %q", branch)
	}
}

// ── GetCurrentBranch ──────────────────────────────────────────

func TestGetCurrentBranch(t *testing.T) {
	g, dir := setupGitRepo(t)
	branch, err := g.GetCurrentBranch(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "master" {
		t.Errorf("expected 'master', got %q", branch)
	}
}

func TestGetCurrentBranchInvalidDir(t *testing.T) {
	g := NewGitOps(zap.NewNop())
	_, err := g.GetCurrentBranch(context.Background(), "/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

// ── IsGitRepository ───────────────────────────────────────────

func TestIsGitRepository(t *testing.T) {
	_, dir := setupGitRepo(t)
	if !IsGitRepository(dir) {
		t.Error("expected dir to be a git repository")
	}

	tmpDir := t.TempDir()
	if IsGitRepository(tmpDir) {
		t.Error("expected temp dir to not be a git repository")
	}
}

// ── GetRemoteURL ──────────────────────────────────────────────

func TestGetRemoteURL(t *testing.T) {
	g, dir := setupGitRepo(t)
	url, err := g.GetRemoteURL(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/test/repo.git" {
		t.Errorf("expected remote URL, got %q", url)
	}
}

func TestGetRemoteURLNoRemote(t *testing.T) {
	g := NewGitOps(zap.NewNop())
	tmpDir := t.TempDir()
	_, err := g.GetRemoteURL(context.Background(), tmpDir)
	if err == nil {
		t.Error("expected error for dir without remote")
	}
}

// ── ResolvePath ───────────────────────────────────────────────

func TestResolvePathValid(t *testing.T) {
	path, err := ResolvePath("/tmp/projects", "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
}

func TestResolvePathTraversal(t *testing.T) {
	_, err := ResolvePath("/tmp/projects", "../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestResolvePathEmpty(t *testing.T) {
	_, err := ResolvePath("/tmp/projects", "")
	if err == nil {
		t.Error("expected error for empty project name")
	}
}

// ── SanitizeInput ─────────────────────────────────────────────

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git status; rm -rf /", "git status rm -rf /"},
		{"echo $(whoami)", "echo whoami"},
		{"ls | grep test", "ls  grep test"},
		{"normal input", "normal input"},
		{"  trimmed  ", "trimmed"},
	}

	for _, tt := range tests {
		got := SanitizeInput(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeInput(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeInputTruncation(t *testing.T) {
	longInput := ""
	for i := 0; i < 300; i++ {
		longInput += "a"
	}
	got := SanitizeInput(longInput)
	if len(got) > 256 {
		t.Errorf("expected truncation to 256, got %d", len(got))
	}
}
