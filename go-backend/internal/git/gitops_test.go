package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestGitOps_CloneAndCommit(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	g := NewGitOps(logger)
	ctx := context.Background()

	// Create a temp "remote" repo with a file and commit
	remoteDir, err := os.MkdirTemp("", "git-remote-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(remoteDir)

	if err := g.runGitCmd(ctx, remoteDir, "init"); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(remoteDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := g.runGitCmd(ctx, remoteDir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := g.runGitCmd(ctx, remoteDir, "commit", "-m", "Initial commit"); err != nil {
		t.Fatal(err)
	}

	// Clone
	localDir, err := os.MkdirTemp("", "git-local-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(localDir)

	if err := g.Clone(ctx, CloneOptions{URL: remoteDir, Dir: localDir}); err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	// Commit a change
	newFile := filepath.Join(localDir, "newfile.txt")
	if err := os.WriteFile(newFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit(ctx, CommitOptions{Dir: localDir, Message: "Add new file"}); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestGitOps_GetCurrentBranch(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	g := NewGitOps(logger)
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "git-branch-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := g.runGitCmd(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	// Need a commit before we can get HEAD
	if err := g.runGitCmd(ctx, dir, "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Fatal(err)
	}

	// Configure git user for test
	g.runGitCmd(ctx, dir, "config", "user.email", "test@test.com")
	g.runGitCmd(ctx, dir, "config", "user.name", "test")

	branch, err := g.GetCurrentBranch(ctx, dir)
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}
	if branch == "" {
		t.Error("expected non-empty branch name")
	}
	t.Logf("branch: %s", branch)
}
