package adapters

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
)

// fileOpsShim wraps file.SimpleSandboxOps for use in adapter tests.
type fileOpsShim struct {
	dir string
}

func (f *fileOpsShim) WriteFile(ctx context.Context, path, content string, overwrite bool) (string, error) {
	ops := &file.SimpleSandboxOps{BasePath: f.dir}
	return ops.WriteFile(ctx, path, content, overwrite)
}

func (f *fileOpsShim) ReadFile(ctx context.Context, path string) (string, error) {
	ops := &file.SimpleSandboxOps{BasePath: f.dir}
	return ops.ReadFile(ctx, path)
}

func TestShQ(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "simple", want: "'simple'"},
		{input: "with space", want: "'with space'"},
		{input: "it's", want: "'it'\\''s'"},
		{input: "path/file.txt", want: "'path/file.txt'"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := shQ(tc.input)
			if got != tc.want {
				t.Errorf("shQ(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestGitExecutor_Commit_Nil(t *testing.T) {
	g := &GitExecutor{Git: nil}
	err := g.Commit(context.Background(), "message")
	if err != nil {
		t.Fatalf("expected nil error for nil Git, got %v", err)
	}
}

func TestBashExecutor_Exec_NilManager(t *testing.T) {
	b := &BashExecutor{Mgr: nil}
	_, err := b.Exec(context.Background(), "echo hi")
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
}

func TestFileOps_ReadFile_NilManager(t *testing.T) {
	f := &FileOps{Mgr: nil}
	_, err := f.ReadFile(context.Background(), "file.txt")
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
}

func TestFileOps_WriteFile_NilManager(t *testing.T) {
	f := &FileOps{Mgr: nil}
	_, err := f.WriteFile(context.Background(), "file.txt", "content", false)
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
}

func TestFileOps_EditFile_NilManager(t *testing.T) {
	f := &FileOps{Mgr: nil}
	_, err := f.EditFile(context.Background(), "file.txt", "old", "new", false)
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
}

func TestFileOps_Grep_NilManager(t *testing.T) {
	f := &FileOps{Mgr: nil}
	_, err := f.Grep(context.Background(), ".", "pattern")
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
}

func TestFileOps_Glob_NilManager(t *testing.T) {
	f := &FileOps{Mgr: nil}
	_, err := f.Glob(context.Background(), ".", "*.go")
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
}

func TestApprovalHandler_RequestApproval_CtxCancelled(t *testing.T) {
	h := &ApprovalHandler{Registry: core.GlobalDecisions}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.RequestApproval(ctx, "req-1", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestHostExecutor_PathRemap(t *testing.T) {
	dir := t.TempDir()
	h := &HostExecutor{WorkDir: dir}
	ctx := context.Background()

	// /sandbox/workspace/ paths get remapped to WorkDir
	out, err := h.Exec(ctx, "echo /sandbox/workspace/src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	want := dir + "/src/main.go"
	if strings.TrimSpace(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}

	// /sandbox/workspace without trailing slash
	out, err = h.Exec(ctx, "echo /sandbox/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != dir {
		t.Errorf("got %q, want %q", out, dir)
	}
}

func TestHostExecutor_NoWorkDir(t *testing.T) {
	h := &HostExecutor{}
	ctx := context.Background()

	// No WorkDir: no remapping, command runs as-is
	out, err := h.Exec(ctx, "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("got %q, want hello", out)
	}
}

// TestSubAgentWriteThenRead simulates the exact sub-agent pipeline:
// file_write with /sandbox/workspace/ path → HostExecutor bash ls → file_read back.
// This catches the class of bugs where file tools and bash tools resolve paths differently.
func TestSubAgentWriteThenRead(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Same setup as execFactory("native") in orchestrator.go
	hostExec := &HostExecutor{WorkDir: dir}

	// Use the file package directly — import it
	fileOps := &fileOpsShim{dir: dir}

	sandboxPath := "/sandbox/workspace/go-backend/internal/mypkg/mypkg.go"
	content := "package mypkg\n\nfunc Hello() string { return \"hello\" }\n"

	// Step 1: file_write via SimpleSandboxOps (sub-agent uses /sandbox/workspace/ path)
	_, err := fileOps.WriteFile(ctx, sandboxPath, content, false)
	if err != nil {
		t.Fatalf("file_write failed: %v", err)
	}

	// Step 2: bash ls via HostExecutor — should see the file
	out, err := hostExec.Exec(ctx, "ls /sandbox/workspace/go-backend/internal/mypkg/")
	if err != nil {
		t.Fatalf("bash ls failed: %v", err)
	}
	if !strings.Contains(out, "mypkg.go") {
		t.Errorf("bash ls did not find mypkg.go — file_write may not be persisting. ls output: %q", out)
	}

	// Step 3: file_read via SimpleSandboxOps — should read back what was written
	got, err := fileOps.ReadFile(ctx, sandboxPath)
	if err != nil {
		t.Fatalf("file_read failed: %v", err)
	}
	if got != content {
		t.Errorf("file_read content mismatch.\ngot:  %q\nwant: %q", got, content)
	}

	// Step 4: Verify the file exists at the actual host path (not just in sandbox namespace)
	hostPath := dir + "/go-backend/internal/mypkg/mypkg.go"
	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("os.ReadFile at host path failed: %v (file_write did not persist to disk)", err)
	}
	if string(data) != content {
		t.Errorf("host file content mismatch.\ngot:  %q\nwant: %q", string(data), content)
	}
}

// TestDoubleNestingFix verifies that /sandbox/workspace/<projectBasename>/... paths
// get corrected to avoid double-nesting. When the project dir is .../go-backend,
// /sandbox/workspace/go-backend/internal/... should resolve to .../go-backend/internal/...
// NOT .../go-backend/go-backend/internal/...
func TestDoubleNestingFix(t *testing.T) {
	// Create a temp dir that ends in "go-backend" to simulate the real project structure
	parent := t.TempDir()
	dir := parent + "/go-backend"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	hostExec := &HostExecutor{WorkDir: dir}
	fileOps := &fileOpsShim{dir: dir}

	// The sub-agent's task references /sandbox/workspace/go-backend/... (double-nesting)
	doubleNestedPath := "/sandbox/workspace/go-backend/internal/mypkg/mypkg.go"
	content := "package mypkg\n\nfunc Hello() string { return \"hello\" }\n"

	// file_write should write to dir/internal/mypkg/mypkg.go (NOT dir/go-backend/...)
	_, err := fileOps.WriteFile(ctx, doubleNestedPath, content, false)
	if err != nil {
		t.Fatalf("file_write failed: %v", err)
	}

	// Verify it wrote to the correct location (no double go-backend)
	hostPath := dir + "/internal/mypkg/mypkg.go"
	data, err := os.ReadFile(hostPath)
	if err != nil {
		// Check if it went to the wrong place
		wrongPath := dir + "/go-backend/internal/mypkg/mypkg.go"
		if _, statErr := os.Stat(wrongPath); statErr == nil {
			t.Fatalf("double-nesting bug: file written to %q instead of %q", wrongPath, hostPath)
		}
		t.Fatalf("file not found at expected path %q: %v", hostPath, err)
	}
	if string(data) != content {
		t.Errorf("content mismatch.\ngot:  %q\nwant: %q", string(data), content)
	}

	// bash with double-nested path should also work
	out, err := hostExec.Exec(ctx, "cat /sandbox/workspace/go-backend/internal/mypkg/mypkg.go")
	if err != nil {
		t.Fatalf("bash cat failed: %v", err)
	}
	if !strings.Contains(out, "package mypkg") {
		t.Errorf("bash cat did not find file content. output: %q", out)
	}

	// file_read should also work
	got, err := fileOps.ReadFile(ctx, doubleNestedPath)
	if err != nil {
		t.Fatalf("file_read failed: %v", err)
	}
	if got != content {
		t.Errorf("file_read content mismatch.\ngot:  %q\nwant: %q", got, content)
	}
}
