package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

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
