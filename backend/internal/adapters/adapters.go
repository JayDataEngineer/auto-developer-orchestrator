// Package adapters bridges the sandbox manager to the tool implementations.
//
// BashExecutor and FileOps are the two adapters the MCP server uses —
// tools/bash and tools/file talk to these interfaces, the adapters dispatch
// into sandbox.Manager. Keeping the boundary here means the tools don't
// import the sandbox package directly.
package adapters

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

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
	// Auto-source /sandbox/.env if the sandbox image ships one. `set -a`
	// exports every subsequently-set var so child processes (python3, etc.)
	// inherit them — without it, sourcing only creates shell vars which
	// subprocesses can't see.
	// IMPORTANT: dash (Ubuntu's /bin/sh) exits the entire shell when `. file`
	// fails — even without `set -e`. Guard with [ -f ] so commands still run
	// when /sandbox/.env doesn't exist.
	wrapped := "if [ -f /sandbox/.env ]; then set -a; . /sandbox/.env 2>/dev/null; set +a; fi; " + command
	output, err := b.Mgr.ExecInSandbox(ctx, b.SandboxID, []string{"sh", "-c", wrapped})
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

// AbsPath resolves a sandbox-relative path. Inside the sandbox container,
// paths are already absolute (no host-side translation needed) — this method
// exists to satisfy the SandboxFileOps interface contract alongside the
// host-backed SimpleSandboxOps implementation.
func (f *FileOps) AbsPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + strings.TrimPrefix(p, "/")
}

func (f *FileOps) ReadFile(ctx context.Context, path string) (string, error) {
	return f.exec(ctx, fmt.Sprintf("cat %s", shQ(path)))
}

func (f *FileOps) WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error) {
	redirect := ">"
	if !overwrite {
		redirect = ">>"
	}
	// Use base64 pipe instead of heredoc — heredocs break with Docker exec TTY mode
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf("echo '%s' | base64 -d %s %s", encoded, redirect, shQ(path))
	output, err := f.exec(ctx, cmd)
	if err != nil {
		return output, err
	}
	return fmt.Sprintf("Wrote %s (%d bytes)", path, len(content)), nil
}

func (f *FileOps) EditFile(ctx context.Context, path string, oldStr, newStr string, replaceAll bool) (string, error) {
	sedOld := strings.ReplaceAll(oldStr, "/", "\\/")
	sedNew := strings.ReplaceAll(newStr, "/", "\\/")
	flag := ""
	if replaceAll {
		flag = "g"
	}
	_, err := f.exec(ctx, fmt.Sprintf("sed -i 's/%s/%s/%s' %s", sedOld, sedNew, flag, shQ(path)))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited %s", path), nil
}

// rgExcludes is the set of directories always excluded from file search.
// Sourced from OpenCode's SkipHidden + common noise dirs. Passed as
// --glob '!<dir>' flags to ripgrep so junk never gets listed.
var rgExcludes = []string{
	"node_modules", "vendor", "dist", "build", "target",
	".git", ".idea", ".vscode", "__pycache__",
	"bin", "obj", "out", "coverage",
	"tmp", "temp", "logs", "generated",
	"bower_components", "jspm_packages",
	".cache", ".next", ".nuxt", ".turbo",
	".venv", "venv", "env",
	".tox", ".mypy_cache", ".pytest_cache",
	".DS_Store", "Thumbs.db",
}

// rgExcludeFlags builds the --glob '!<dir>' flags for ripgrep.
func rgExcludeFlags() string {
	var b strings.Builder
	for _, d := range rgExcludes {
		b.WriteString(" --glob ")
		b.WriteString(shQ("!"+d))
	}
	return b.String()
}

func (f *FileOps) Grep(ctx context.Context, path string, pattern string) (string, error) {
	// Prefer ripgrep: respects .gitignore, skips hidden files, and
	// excludes common junk dirs via --glob negation flags.
	if out, err := f.exec(ctx, fmt.Sprintf(
		"rg --max-count=200 --max-depth=6 %s %s %s 2>/dev/null",
		rgExcludeFlags(), shQ(pattern), shQ(path),
	)); err == nil && out != "" {
		return out, nil
	}
	// Fallback to grep
	return f.exec(ctx, fmt.Sprintf("grep -rn %s %s 2>/dev/null | head -200 || true", shQ(pattern), shQ(path)))
}

func (f *FileOps) Glob(ctx context.Context, path string, pattern string) (string, error) {
	// Prefer ripgrep: respects .gitignore, skips binary/hidden files,
	// and excludes common junk dirs via --glob negation flags.
	if out, err := f.exec(ctx, fmt.Sprintf(
		"rg --files --glob %s %s --max-depth 6 --sort=path %s 2>/dev/null | head -500",
		shQ(pattern), rgExcludeFlags(), shQ(path),
	)); err == nil && out != "" {
		return out, nil
	}
	// Fallback: capped find (no gitignore awareness)
	return f.exec(ctx, fmt.Sprintf("find %s -name %s -type f -maxdepth 6 2>/dev/null | head -500", shQ(path), shQ(pattern)))
}

// ── Compile-time interface checks ────────────────────────────────────
//
// These assertions ensure the adapters satisfy their target interfaces.
// The actual interface types are imported where needed (tools/bash, tools/file).
// To verify, build the consuming packages — the compiler checks the
// assignment compatibility at their use sites.

// ── Helpers ──────────────────────────────────────────────────────────

func shQ(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
