package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SandboxExecutor runs a shell command inside a sandbox. Satisfied by
// adapters.BashExecutor. Kept here (not in core) because python-in-sandbox
// is an MCP-server-specific composition — the broader codebase has its own
// python tool with a different lifecycle.
type SandboxExecutor interface {
	Exec(ctx context.Context, command string) (string, error)
}

// SandboxPythonTool executes Python code inside the sandbox by wrapping it
// in `python3 -c`. This is the simple composition the MVP needs — the model
// gets a dedicated `python` tool with clean ergonomics (just code), and the
// code runs inside the sandbox container so org-installed deps (manim,
// surrealdb, etc.) are visible.
//
// This is intentionally minimal — no data-preamble, no subprocess management.
// For richer ergonomics (data JSON, exit codes), the model can still call
// `bash` directly with `python3 -c "..."` or write a script and run it.
type SandboxPythonTool struct {
	exec    SandboxExecutor
	timeout time.Duration
}

// NewSandboxPythonTool builds a python tool that runs inside the sandbox.
// Default timeout is 60s — generous enough for most scripts, short enough
// to fail-fast on infinite loops.
func NewSandboxPythonTool(exec SandboxExecutor) *SandboxPythonTool {
	return &SandboxPythonTool{exec: exec, timeout: 60 * time.Second}
}

func (t *SandboxPythonTool) Name() string { return "python" }

func (t *SandboxPythonTool) Description() string {
	return "Execute Python code inside the sandbox. Print output is captured. " +
		"Sandbox-installed packages (surrealdb, manim, etc.) are available. " +
		"Runs with a 60s timeout."
}

func (t *SandboxPythonTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"code": {
				"type": "string",
				"description": "Python code to execute. Print output is captured and returned."
			}
		},
		"required": ["code"]
	}`)
}

func (t *SandboxPythonTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	code, _ := args["code"].(string)
	if code == "" {
		return map[string]any{"error": "no code provided", "success": false}, nil
	}

	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Wrap the code in a single-quoted heredoc-style invocation. python3 -c
	// takes the code as argv[1]; we use a heredoc to avoid quoting hell with
	// multi-line code containing its own quotes.
	// Use shQ to safely wrap the python code as a single shell argument.
	wrapped := fmt.Sprintf("python3 -c %s", shQPy(code))
	out, err := t.exec.Exec(execCtx, wrapped)
	if execCtx.Err() == context.DeadlineExceeded {
		return map[string]any{
			"error":   fmt.Sprintf("python timed out after %v", t.timeout),
			"success": false,
		}, nil
	}
	if err != nil {
		// Bash exec returns non-zero exit + stderr in `out` for python failures
		// (syntax error, traceback, etc.). Surface that as a non-fatal error
		// with the output so the model can debug.
		return map[string]any{
			"success": false,
			"error":   err.Error(),
			"output":  out,
		}, nil
	}
	return map[string]any{"success": true, "output": out}, nil
}

// shQPy wraps a string in single quotes for shell-safe passage. Mirrors
// adapters.shQ but kept local — the sandbox-python tool doesn't depend on
// adapters at the package boundary.
func shQPy(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// interface assertion
var _ core.Tool = (*SandboxPythonTool)(nil)
