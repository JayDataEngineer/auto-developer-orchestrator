package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SandboxExecutor runs a shell command inside a sandbox. Satisfied by
// adapters.BashExecutor. Kept here (not in core) because python-in-sandbox
// is a composition specific to the MCP server — we want the model to see a
// dedicated `python` tool with clean ergonomics, not a shell wrapper.
type SandboxExecutor interface {
	Exec(ctx context.Context, command string) (string, error)
}

// SandboxPythonTool executes Python code inside the sandbox by wrapping it
// in `python3 -c`. The model gets a dedicated `python` tool with clean
// ergonomics (just the code), and the code runs inside the sandbox
// container so whatever the sandbox image ships with (Python stdlib + any
// apt/uv-installed deps from sandbox/Dockerfile) is available.
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
		"Whatever the sandbox image ships with is available. " +
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

	// Wrap the code in a single-quoted shell argument. shQ handles any
	// embedded quotes — the model can pass multi-line python with its own
	// quotes/spaces safely.
	wrapped := fmt.Sprintf("python3 -c %s", shQ(code))
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

// interface assertion
var _ core.Tool = (*SandboxPythonTool)(nil)
