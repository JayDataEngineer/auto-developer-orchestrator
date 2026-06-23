// Package scripting provides tools for the agent to write, run, and iterate on
// ad-hoc Python helpers. The agent gets a self-evolving toolkit: it writes
// small scripts once, calls them by name many times. Scripts persist in
// /sandbox/workspace/scripts/ (project-scoped, survives sandbox restarts).
//
// Why: pre-baked tools cover the 80% case but break when selectors change.
// Bash + file_write is too low-level for fast/cheap models like DeepSeek V4
// Flash to use efficiently. This is the middle layer: structured place for
// ad-hoc Python that the agent writes once and calls many times.
//
// Backing implementation: sandbox/scripts/scripts.py — pure-Python CLI that
// does the actual work. These Go tools wrap subprocess calls to it.
package scripting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

// scriptsPyPath resolves the location of scripts.py at runtime.
//
// In sandbox mode: /sandbox/scripts/scripts.py (mounted from <repo>/sandbox/scripts/)
// In native mode: <PROJECT_ROOT>/sandbox/scripts/scripts.py
//
// Returns empty string if not found — tools will return a helpful error.
func scriptsPyPath() string {
	// Try sandbox path first
	if _, err := os.Stat("/sandbox/scripts/scripts.py"); err == nil {
		return "/sandbox/scripts/scripts.py"
	}
	// Fall back to PROJECT_ROOT-relative
	if root := os.Getenv("PROJECT_ROOT"); root != "" {
		p := filepath.Join(root, "sandbox", "scripts", "scripts.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// runScriptsPy invokes scripts.py with the given args and optional stdin.
// Returns the parsed JSON output. On subprocess failure, returns a map with
// "error" and "stderr" keys. Never returns a Go-level error — callers should
// check the "error" key in the returned map.
func runScriptsPy(args []string, stdin string, timeout time.Duration) map[string]any {
	scriptPath := scriptsPyPath()
	if scriptPath == "" {
		return map[string]any{
			"error": "scripts.py not found. Expected at /sandbox/scripts/scripts.py (sandbox mode) or $PROJECT_ROOT/sandbox/scripts/scripts.py (native mode).",
		}
	}

	// Resolve python binary
	pythonBin := "python3"
	if p, err := exec.LookPath(pythonBin); err == nil {
		pythonBin = p
	} else if p, err := exec.LookPath("python"); err == nil {
		pythonBin = p
	} else {
		return map[string]any{
			"error": "python3 binary not found on PATH.",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdArgs := append([]string{"-u", scriptPath}, args...)
	cmd := exec.CommandContext(ctx, pythonBin, cmdArgs...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	// Ensure /sandbox/ is importable for helper modules (twitter_helpers, session, etc.)
	// Mirrors what scripts.py itself does for run_script.
	env := os.Environ()
	env = append(env, "PYTHONPATH=/sandbox")
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return map[string]any{
				"error":  fmt.Sprintf("failed to invoke scripts.py: %v", err),
				"stderr": stderr.String(),
			}
		}
	}

	// scripts.py always emits JSON; parse it
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		// Not JSON — return raw
		out := map[string]any{
			"exit_code": exitCode,
			"stdout":    truncate.Tail(stdout.String(), truncate.FileMaxLines, truncate.BashMaxChars).Content,
		}
		if stderr.Len() > 0 {
			out["stderr"] = truncate.Tail(stderr.String(), truncate.FileMaxLines, truncate.BashMaxChars).Content
		}
		return out
	}

	// Truncate large stdout fields if present
	if rawStdout, ok := result["stdout"].(string); ok {
		result["stdout"] = truncate.Tail(rawStdout, truncate.FileMaxLines, truncate.BashMaxChars).Content
	}
	if rawStderr, ok := result["stderr"].(string); ok {
		result["stderr"] = truncate.Tail(rawStderr, truncate.FileMaxLines, truncate.BashMaxChars).Content
	}
	return result
}

// AvailableScriptsBlock builds the <available_scripts> XML block injected into
// the CTO system prompt so the model knows what helpers already exist (with
// their hints) without needing to call list_scripts first.
//
// Mirrors skills.FormatAvailableSkills for the discoverable-scripts-as-skills
// pattern. Returns "" (silent no-op) when:
//   - scripts.py is unreachable (timeout, missing binary, etc.)
//   - zero scripts exist in the project dir
//
// Caps output at 20 scripts. Larger catalogs require the model to call
// list_scripts. Each entry is one line: "name: description — hints_first_line"
// truncated to ~120 chars total so the block stays cheap even at the cap.
func AvailableScriptsBlock() string {
	const maxScripts = 20
	const maxLineChars = 120

	res := runScriptsPy([]string{"--list"}, "", 5*time.Second)
	scripts, _ := res["scripts"].([]any)
	if len(scripts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nThe following scripts are reusable Python helpers you authored in past sessions.\n")
	b.WriteString("Call run_script(name) to use one; call read_script(name) to peek at code + hints first.\n\n")
	b.WriteString("<available_scripts>\n")

	count := 0
	for _, s := range scripts {
		if count >= maxScripts {
			remaining := len(scripts) - maxScripts
			if remaining > 0 {
				fmt.Fprintf(&b, "  (and %d more — use list_scripts to see all)\n", remaining)
			}
			break
		}
		rec, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := rec["name"].(string)
		desc, _ := rec["description"].(string)
		hints, _ := rec["hints"].(string)
		if name == "" {
			continue
		}

		// "name: description — first hint line"
		line := name
		if desc != "" {
			line += ": " + desc
		}
		// Take only the first hint (before any ' | ' separator) for compactness.
		if hints != "" {
			firstHint := hints
			if idx := strings.Index(hints, " | "); idx >= 0 {
				firstHint = hints[:idx]
			}
			line += " — " + firstHint
		}
		if len(line) > maxLineChars {
			line = line[:maxLineChars-3] + "..."
		}
		fmt.Fprintf(&b, "  - %s\n", line)
		count++
	}
	b.WriteString("</available_scripts>")
	return b.String()
}

// ─── MakeScriptTool ────────────────────────────────────────────────────────

// MakeScriptTool creates a new named Python script in the project's script dir.
type MakeScriptTool struct{}

func (MakeScriptTool) Name() string { return "make_script" }

func (MakeScriptTool) Description() string {
	return "Create a new Python helper script that persists across runs. " +
		"Use this to build up a toolkit of small scripts (like_tweet, read_mentions, etc.) " +
		"instead of inlining Python in bash each time. Scripts live in /sandbox/workspace/scripts/ " +
		"and can be called later via run_script. Validates Python syntax before saving. " +
		"If a script with the same name exists, returns an error — use edit_script to modify. " +
		"Pass 'hints' to attach AI-authored usage guidance — these are surfaced in list_scripts " +
		"and read_script so future-you can pick the right helper without re-reading the code."
}

func (MakeScriptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Script name in snake_case (e.g. 'like_tweet', 'read_mentions'). Must match [A-Za-z_][A-Za-z0-9_]*.",
				"pattern": "^[A-Za-z_][A-Za-z0-9_]*$"
			},
			"description": {
				"type": "string",
				"description": "One-line description of what the script does. Shown in list_scripts output. Keep it short — this is how you'll find the script later."
			},
			"code": {
				"type": "string",
				"description": "Python source code. Can be multi-line. Will be syntax-validated before saving. To accept CLI args, read sys.argv. Print output is captured as stdout in the result."
			},
			"hints": {
				"type": "string",
				"description": "Optional AI-authored usage guidance. Multi-line — one bullet per line, no leading dashes needed. Example: 'Use when user asks to post a note.\\nReturns message_id on success.\\nPitfall: rate-limited above 5 calls/sec.' Surfaced in list_scripts and read_script so the agent can pick the right helper without re-reading code. Always set this for scripts you'll call again."
			}
		},
		"required": ["name", "description", "code"]
	}`)
}

func (t MakeScriptTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	code, _ := args["code"].(string)
	hints, _ := args["hints"].(string)

	if name == "" || code == "" {
		return map[string]any{"error": "both 'name' and 'code' are required"}, nil
	}

	// Pass code via stdin to avoid shell-escaping nightmares
	cliArgs := []string{"--make", "--name", name, "--desc", description, "--stdin"}
	if hints != "" {
		cliArgs = append(cliArgs, "--hints", hints)
	}
	return runScriptsPy(cliArgs, code, 30*time.Second), nil
}

// ─── RunScriptTool ─────────────────────────────────────────────────────────

// RunScriptTool executes a previously-created script by name.
type RunScriptTool struct{}

func (RunScriptTool) Name() string { return "run_script" }

func (RunScriptTool) Description() string {
	return "Run a previously-created script by name. Use list_scripts to see what's available. " +
		"Output is captured (stdout) and returned. Long output is truncated to keep the last 50k chars. " +
		"Default timeout is 5 minutes (300s); override with timeout_seconds for long-running tasks."
}

func (RunScriptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the script to run (must have been created via make_script)."
			},
			"args": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Optional CLI arguments to pass to the script. Access via sys.argv[1:] inside the script."
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Max execution time in seconds (default 300, max 1800).",
				"default": 300,
				"minimum": 1,
				"maximum": 1800
			}
		},
		"required": ["name"]
	}`)
}

func (t RunScriptTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return map[string]any{"error": "'name' is required"}, nil
	}

	timeout := 300 * time.Second
	if rawTimeout, ok := args["timeout_seconds"].(float64); ok && rawTimeout > 0 {
		timeout = time.Duration(rawTimeout) * time.Second
	}
	if timeout > 1800*time.Second {
		timeout = 1800 * time.Second
	}

	cliArgs := []string{"--run", "--name", name}
	if rawArgs, ok := args["args"].([]any); ok {
		for _, a := range rawArgs {
			if s, ok := a.(string); ok {
				cliArgs = append(cliArgs, s)
			}
		}
	}
	// Cap timeout at the subprocess level slightly below ctx deadline
	deadline := timeout + 5*time.Second
	return runScriptsPy(cliArgs, "", deadline), nil
}

// ─── ListScriptsTool ───────────────────────────────────────────────────────

// ListScriptsTool lists all created scripts with their descriptions.
type ListScriptsTool struct{}

func (ListScriptsTool) Name() string { return "list_scripts" }

func (ListScriptsTool) Description() string {
	return "List all scripts you've created so far in this project. " +
		"Use this at the start of a session to remember what's available before writing new ones. " +
		"Returns each script's name, one-line description, size, and last-modified time."
}

func (ListScriptsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t ListScriptsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return runScriptsPy([]string{"--list"}, "", 10*time.Second), nil
}

// ─── EditScriptTool ────────────────────────────────────────────────────────

// EditScriptTool replaces the code of an existing script.
type EditScriptTool struct{}

func (EditScriptTool) Name() string { return "edit_script" }

func (EditScriptTool) Description() string {
	return "Replace the source code of an existing script. Use this to fix bugs, " +
		"update behavior (e.g. when a website changes its selectors), or add features. " +
		"Validates Python syntax before saving. Description is preserved unless a new one is given. " +
		"Hints are preserved unless new ones are given — pass hints=\"\" to clear."
}

func (EditScriptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the script to edit (must already exist)."
			},
			"code": {
				"type": "string",
				"description": "New Python source code (replaces existing entirely — this is not a patch)."
			},
			"description": {
				"type": "string",
				"description": "Optional new description. If omitted, the existing description is preserved."
			},
			"hints": {
				"type": "string",
				"description": "Optional new AI-authored hints. Omit to preserve existing hints. Pass empty string to clear. Multi-line — one bullet per line."
			}
		},
		"required": ["name", "code"]
	}`)
}

func (t EditScriptTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	code, _ := args["code"].(string)
	description, _ := args["description"].(string)

	if name == "" || code == "" {
		return map[string]any{"error": "both 'name' and 'code' are required"}, nil
	}

	cliArgs := []string{"--edit", "--name", name, "--stdin"}
	if description != "" {
		cliArgs = append(cliArgs, "--desc", description)
	}
	// Distinguish "omitted" (preserve existing) from "empty string" (clear).
	// Go's type assertion to string succeeds only when the key is present as a string.
	if rawHints, present := args["hints"]; present {
		if hintsStr, ok := rawHints.(string); ok {
			cliArgs = append(cliArgs, "--hints", hintsStr)
		}
	}
	return runScriptsPy(cliArgs, code, 30*time.Second), nil
}

// ─── ReadScriptTool ────────────────────────────────────────────────────────

// ReadScriptTool returns the full source + metadata (description, hints) of a
// script. Mirrors read_skill — lets the agent peek before calling.
type ReadScriptTool struct{}

func (ReadScriptTool) Name() string { return "read_script" }

func (ReadScriptTool) Description() string {
	return "Read the full source code AND metadata of a script. Use this to peek at a script " +
		"before running it — read its description, AI-authored hints (usage guidance, pitfalls, " +
		"return shape), and code. Returns {name, description, hints, code, size, modified}. " +
		"Mirrors read_skill for the discoverable-scripts-as-skills pattern."
}

func (ReadScriptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the script to read."
			}
		},
		"required": ["name"]
	}`)
}

func (t ReadScriptTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return map[string]any{"error": "'name' is required"}, nil
	}
	return runScriptsPy([]string{"--show", "--name", name}, "", 10*time.Second), nil
}

// ─── ShowScriptTool (deprecated alias) ──────────────────────────────────────

// ShowScriptTool is a deprecated alias for ReadScriptTool. Kept for one release
// so existing prompts/agents that call show_script keep working.
type ShowScriptTool struct{}

func (ShowScriptTool) Name() string { return "show_script" }

func (ShowScriptTool) Description() string {
	return "(Deprecated — use read_script instead.) Returns the source code of a script. " +
		"Prefer read_script, which returns the same code plus description and hints."
}

func (ShowScriptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the script to show."
			}
		},
		"required": ["name"]
	}`)
}

func (t ShowScriptTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return map[string]any{"error": "'name' is required"}, nil
	}
	return runScriptsPy([]string{"--show", "--name", name}, "", 10*time.Second), nil
}

// ─── RemoveScriptTool ──────────────────────────────────────────────────────

// RemoveScriptTool deletes a script. Useful when a script is no longer needed
// or was created by mistake and would otherwise clutter list_scripts output.
type RemoveScriptTool struct{}

func (RemoveScriptTool) Name() string { return "remove_script" }

func (RemoveScriptTool) Description() string {
	return "Delete a script by name. Use this to clean up scripts that are no longer needed, " +
		"were superseded by a better implementation, or were created by mistake. " +
		"Removal is permanent — there is no undo. If you just want to fix a script, " +
		"prefer edit_script over remove+remake."
}

func (RemoveScriptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the script to delete."
			}
		},
		"required": ["name"]
	}`)
}

func (t RemoveScriptTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return map[string]any{"error": "'name' is required"}, nil
	}
	return runScriptsPy([]string{"--rm", "--name", name}, "", 10*time.Second), nil
}

// ─── Registration helper ───────────────────────────────────────────────────

// AllTools returns all scripting tools for registration in the orchestrator.
// Convenience function — keeps the orchestrator imports tidy.
func AllTools() []core.Tool {
	return []core.Tool{
		MakeScriptTool{},
		RunScriptTool{},
		ListScriptsTool{},
		EditScriptTool{},
		ReadScriptTool{},
		ShowScriptTool{},   // deprecated alias for read_script
		RemoveScriptTool{},
	}
}
