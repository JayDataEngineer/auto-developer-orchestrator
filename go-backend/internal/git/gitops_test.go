package git

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestGitOps_Basic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	g := NewGitOps(logger)
	ctx := context.Background()

	// Create a temp directory for our "remote" repository
	remoteDir, err := ioutil.TempDir("", "git-remote-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(remoteDir)

	// Initialize the remote repo
	if err := g.InitRepository(ctx, remoteDir); err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	// Add a file and commit to the remote repo so it has a HEAD
	testFile := filepath.Join(remoteDir, "README.md")
	if err := ioutil.WriteFile(testFile, []byte("# Test Repo"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := g.runGitCmd(ctx, remoteDir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := g.runGitCmd(ctx, remoteDir, "commit", "-m", "Initial commit"); err != nil {
		t.Fatal(err)
	}

	// Create a temp directory for our local clone
	localDir, err := ioutil.TempDir("", "git-local-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(localDir)

	// Test Clone
	opts := CloneOptions{
		URL: remoteDir,
		Dir: localDir,
	}
	if err := g.Clone(ctx, opts); err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	// Test Status
	status, err := g.Status(ctx, StatusOptions{Dir: localDir})
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if !status.IsClean {
		t.Errorf("Expected repository to be clean, but it was not")
	}

	// Test Commit
	newFile := filepath.Join(localDir, "newfile.txt")
	if err := ioutil.WriteFile(newFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	commitOpts := CommitOptions{
		Dir:     localDir,
		Message: "Add new file",
	}
	if err := g.Commit(ctx, commitOpts); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Test GetLog
	logEntries, err := g.GetLog(ctx, GetLogOptions{Dir: localDir, Count: 10})
	if err != nil {
		t.Fatalf("GetLog failed: %v", err)
	}
	if len(logEntries) < 2 {
		t.Errorf("Expected at least 2 log entries, got %d", len(logEntries))
	}

	// Test SanitizeInput
	input := "git status; rm -rf /"
	sanitized := SanitizeInput(input)
	if sanitized != "git status rm -rf /" {
		t.Errorf("SanitizeInput failed: got %q", sanitized)
	}
}

func TestResolvePath(t *testing.T) {
	baseDir := "/tmp/projects"

	// Valid path
	path, err := ResolvePath(baseDir, "my-project")
	if err != nil {
		t.Errorf("ResolvePath failed on valid project: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("Expected absolute path, got %s", path)
	}

	// Traversal attempt
	_, err = ResolvePath(baseDir, "../outside")
	if err == nil {
		t.Errorf("ResolvePath should have failed on directory traversal")
	}
}
