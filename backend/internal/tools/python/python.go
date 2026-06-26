package python

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

const (
	defaultTimeout = 30 * time.Second
	maxTimeout     = 120 * time.Second
)

// AllTools returns every python tool. opts is optional; pass nil for defaults.
func AllTools(opts ...Option) []core.Tool {
	return []core.Tool{NewPythonTool(opts...)}
}

// PythonTool executes Python code in a subprocess with timeout.
// No embedded interpreter — uses system python3. Captures stdout and stderr.
type PythonTool struct {
	timeout   time.Duration
	workDir   string
	pythonBin string // resolved on first use
}

// Option configures a PythonTool.
type Option func(*PythonTool)

// WithWorkDir sets the working directory for Python execution.
func WithWorkDir(dir string) Option {
	return func(t *PythonTool) { t.workDir = dir }
}

// WithTimeout sets the default timeout.
func WithTimeout(d time.Duration) Option {
	return func(t *PythonTool) { t.timeout = d }
}

// NewPythonTool creates a Python execution tool.
func NewPythonTool(opts ...Option) *PythonTool {
	t := &PythonTool{
		timeout: defaultTimeout,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *PythonTool) Name() string { return "python" }

func (t *PythonTool) Description() string {
	return "Execute Python code in a subprocess. Captures stdout and stderr. " +
		"Provide 'data' parameter (JSON string) to make it available as the 'data' variable. " +
		"Print output is captured. Runs with a 30s timeout (max 120s)."
}

func (t *PythonTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"code": {
				"type": "string",
				"description": "Python code to execute. Print output is captured. The last expression's value is not automatically returned — use print() to output results."
			},
			"data": {
				"description": "Optional input data as a JSON string. Available as the 'data' variable in the script.",
				"type": "string"
			},
			"timeout_ms": {
				"type": "integer",
				"description": "Execution timeout in milliseconds (default 30000, max 120000)."
			}
		},
		"required": ["code"]
	}`)
}

func (t *PythonTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	code, _ := args["code"].(string)
	if code == "" {
		return map[string]any{"error": "no code provided"}, nil
	}

	// Resolve python binary once
	if t.pythonBin == "" {
		t.pythonBin = resolvePython()
	}
	if t.pythonBin == "" {
		return map[string]any{
			"error":   "python3 not found on PATH. Install Python 3 to use this tool.",
			"success": false,
		}, nil
	}

	// Parse timeout
	timeout := t.timeout
	if ms, ok := args["timeout_ms"].(float64); ok && ms > 0 {
		timeout = min(time.Duration(ms)*time.Millisecond, maxTimeout)
	}

	// Build the full script with data preamble
	script := ""
	if dataStr, ok := args["data"].(string); ok && dataStr != "" {
		script = "import os, json as _json\n_data_str = os.environ.get('_PYTHON_TOOL_DATA', '')\nif _data_str:\n    try:\n        data = _json.loads(_data_str)\n    except Exception:\n        data = _data_str\nelse:\n    data = None\n\n"
		_ = dataStr // used via env var
	}
	script += code

	// Run with timeout
	type execResult struct {
		stdout   string
		stderr   string
		exitCode int
		err      error
	}
	ch := make(chan execResult, 1)

	go func() {
		stdout, stderr, exitCode, err := t.runSubprocess(ctx, script, args)
		ch <- execResult{stdout, stderr, exitCode, err}
	}()

	select {
	case <-ctx.Done():
		return map[string]any{"error": "cancelled", "success": false}, nil
	case result := <-ch:
		if result.err != nil {
			return map[string]any{
				"error":   result.err.Error(),
				"success": false,
			}, nil
		}

		output := map[string]any{
			"success": result.exitCode == 0,
		}

		if result.stdout != "" {
			tr := truncate.Tail(result.stdout, truncate.FileMaxLines, truncate.BashMaxChars)
			output["output"] = tr.Content
		}
		if result.stderr != "" {
			// Truncate stderr too
			tr := truncate.Tail(result.stderr, truncate.FileMaxLines, truncate.BashMaxChars)
			output["error"] = tr.Content
		}
		if result.exitCode != 0 {
			output["exit_code"] = result.exitCode
		}
		// Wrap via QuarantineResult — agent-authored Python stdout/stderr can
		// echo injection patterns ("ignore previous instructions", etc).
		// Same contract as scripting.go (System B sibling tool).
		return tools.QuarantineResult(output), nil
	case <-time.After(timeout):
		return map[string]any{
			"error":   fmt.Sprintf("execution timed out after %v", timeout),
			"success": false,
		}, nil
	}
}

func (t *PythonTool) runSubprocess(ctx context.Context, script string, args map[string]any) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, t.pythonBin, "-u", "-")
	if t.workDir != "" {
		cmd.Dir = t.workDir
	}

	// Pass data as env var to avoid stdin conflicts
	if dataStr, ok := args["data"].(string); ok && dataStr != "" {
		cmd.Env = append(os.Environ(), "_PYTHON_TOOL_DATA="+dataStr)
	}

	// Pipe script via stdin
	cmd.Stdin = bytes.NewBufferString(script)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", "", 0, fmt.Errorf("failed to run python: %w", err)
		}
	}

	return stdout.String(), stderr.String(), exitCode, nil
}

// resolvePython finds the python3 binary on PATH.
func resolvePython() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}
