// Package adapters provides shared wiring adapters that bridge low-level
// infrastructure (sandbox manager, git, approval) to new-architecture
// interfaces (tools.Tool, hooks.GitExecutor, hooks.ApprovalHandler).
//
// Both the HTTP handlers and the scheduler use these adapters, avoiding
// duplicate implementations across wiring layers.
package adapters

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/approval"
	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/hooks"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
)

// ── Sandbox adapters ─────────────────────────────────────────────────

// BashExecutor executes shell commands inside a Docker sandbox.
// Implements tools/bash.Executor.
type BashExecutor struct {
	Mgr       *sandbox.Manager
	SandboxID string
}

func (b *BashExecutor) Exec(ctx context.Context, command string) (string, error) {
	if b.Mgr == nil {
		return "", fmt.Errorf("sandbox manager not available")
	}
	output, err := b.Mgr.ExecInSandbox(ctx, b.SandboxID, []string{"sh", "-c", command})
	return output, err
}

// FileOps provides read/write/edit/grep/glob inside a Docker sandbox.
// Implements tools/file.SandboxFileOps.
type FileOps struct {
	Mgr       *sandbox.Manager
	SandboxID string
}

func (f *FileOps) exec(ctx context.Context, cmd string) (string, error) {
	if f.Mgr == nil {
		return "", fmt.Errorf("sandbox manager not available")
	}
	return f.Mgr.ExecInSandbox(ctx, f.SandboxID, []string{"sh", "-c", cmd})
}

func (f *FileOps) ReadFile(ctx context.Context, path string) (string, error) {
	return f.exec(ctx, fmt.Sprintf("cat %s", shQ(path)))
}

func (f *FileOps) WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error) {
	redirect := ">"
	if !overwrite {
		redirect = ">>"
	}
	cmd := fmt.Sprintf("cat <<'OPENCODE_EOF' %s %s\n%s\nOPENCODE_EOF", redirect, shQ(path), content)
	output, err := f.exec(ctx, cmd)
	if err != nil {
		return output, err
	}
	return fmt.Sprintf("Wrote %s (%d bytes)", path, len(content)), nil
}

func (f *FileOps) EditFile(ctx context.Context, path string, oldStr, newStr string) (string, error) {
	sedOld := strings.ReplaceAll(oldStr, "/", "\\/")
	sedNew := strings.ReplaceAll(newStr, "/", "\\/")
	_, err := f.exec(ctx, fmt.Sprintf("sed -i 's/%s/%s/' %s", sedOld, sedNew, shQ(path)))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited %s", path), nil
}

func (f *FileOps) UndoEdit(ctx context.Context, path string) (string, error) {
	return "", fmt.Errorf("undo not implemented in sandbox adapter")
}

func (f *FileOps) Grep(ctx context.Context, path string, pattern string) (string, error) {
	return f.exec(ctx, fmt.Sprintf("grep -rn %s %s 2>&1 || true", shQ(pattern), shQ(path)))
}

func (f *FileOps) Glob(ctx context.Context, path string, pattern string) (string, error) {
	return f.exec(ctx, fmt.Sprintf("find %s -name %s -type f -maxdepth 50", shQ(path), shQ(pattern)))
}

// ── Git adapter ──────────────────────────────────────────────────────

// GitExecutor wraps *git.GitOps to implement hooks.GitExecutor.
// A nil Git field makes Commit a no-op (for scheduler and other contexts
// where git checkpoints are not needed).
type GitExecutor struct {
	Git     *git.GitOps
	RepoDir string
}

func (g *GitExecutor) Commit(ctx context.Context, message string) error {
	if g.Git == nil {
		return nil // no-op
	}
	return g.Git.Commit(ctx, git.CommitOptions{
		Dir:     g.RepoDir,
		Message: message,
	})
}

// ── Approval adapter ─────────────────────────────────────────────────

// ApprovalHandler wraps *approval.Manager to implement hooks.ApprovalHandler.
// This bridges the central approval.Manager (HTTP Respond endpoint) to the
// agent's approval hook.
type ApprovalHandler struct {
	Mgr *approval.Manager
}

func (a *ApprovalHandler) RequestApproval(ctx context.Context, requestID string, data map[string]any) (hooks.ApprovalResponse, error) {
	ch := a.Mgr.Register(requestID)
	defer a.Mgr.Cleanup(requestID)

	select {
	case <-ctx.Done():
		return hooks.ApprovalResponse{Approved: false, Feedback: "timeout"}, ctx.Err()
	case resp := <-ch:
		return hooks.ApprovalResponse{
			Approved: resp.Action == "approve",
			Feedback: resp.Message,
		}, nil
	}
}

// ── Compile-time interface checks ────────────────────────────────────
//
// These assertions ensure the adapters satisfy their target interfaces.
// The actual interface types are imported where needed (tools/bash, tools/file,
// hooks).
// To verify, build the consuming packages — the compiler checks the
// assignment compatibility at their use sites.

// ── Helpers ──────────────────────────────────────────────────────────

func shQ(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
