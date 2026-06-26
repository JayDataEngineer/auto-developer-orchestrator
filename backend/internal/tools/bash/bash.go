package bash

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

// Executor executes a bash command and returns stdout.
type Executor interface {
	Exec(ctx context.Context, command string) (string, error)
}

// Tool implements core.Tool for bash execution.
type Tool struct {
	executor        Executor
	taskMgr         *core.TaskManager
	workDir         string
	validator       *Validator // nil = no command restriction
	secretResolver  func(string) string // optional: resolves <secret>domain.key</secret> before exec
}

// New creates a new bash tool with default command restrictions.
func New(exec Executor) *Tool {
	return &Tool{executor: exec, validator: NewDefaultValidator()}
}

// NewWithTaskManager creates a bash tool with background task support and default restrictions.
func NewWithTaskManager(exec Executor, taskMgr *core.TaskManager, workDir string) *Tool {
	return &Tool{executor: exec, taskMgr: taskMgr, workDir: workDir, validator: NewDefaultValidator()}
}

// WithSecretResolver attaches a secret-resolver function. When set, the tool
// resolves <secret>domain.key</secret> placeholders in commands BEFORE exec,
// so the real value never reaches the model's context. The resolver receives
// the raw command and returns the resolved command.
func (t *Tool) WithSecretResolver(r func(string) string) *Tool {
	t.secretResolver = r
	return t
}

func (t *Tool) Name() string { return "bash" }
func (t *Tool) Description() string {
	return fmt.Sprintf(
		"Execute a bash command. If output exceeds %d characters, it will be truncated (keeping the end). Use run_in_background=true for long-running commands like builds, tests, or dev servers.",
		truncate.BashMaxChars,
	)
}

func (t *Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to execute"},
			"run_in_background": {
				"type": "boolean",
				"default": false,
				"description": "Run command in background. Returns task_id immediately. Use task_output to get results later. Use for long-running commands (builds, tests, dev servers)."
			}
		},
		"required": ["command"]
	}`)
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	cmd, _ := args["command"].(string)
	if cmd == "" {
		cmd, _ = args["code"].(string)
	}
	if cmd == "" {
		cmd, _ = args["cmd"].(string)
	}
	if cmd == "" {
		return nil, core.NewToolError("bash", "missing required parameter 'command'")
	}

	// Resolve <secret>domain.key</secret> placeholders BEFORE validation/exec.
	// The real value is substituted in the command string, runs in the shell,
	// and never enters model context (placeholders aren't scrubbed because they
	// don't match secret patterns; real values in stdout still get scrubbed).
	if t.secretResolver != nil {
		cmd = t.secretResolver(cmd)
	}

	// Validate command against restriction rules (defense-in-depth)
	if t.validator != nil {
		if err := t.validator.Validate(cmd); err != nil {
			return map[string]any{
				"output":  "",
				"error":   err.Error(),
				"blocked": true,
			}, nil
		}
	}

	runInBackground, _ := args["run_in_background"].(bool)

	// If we have a TaskManager AND the executor is local (not SSH), use the
	// background task system. TaskManager runs via local exec.Command which
	// doesn't work for SSH-backed executors.
	if t.taskMgr != nil && !isRemoteExecutor(t.executor) {
		return t.executeWithTaskManager(ctx, cmd, runInBackground)
	}

	// Fallback: synchronous execution (no TaskManager available)
	return t.executeSync(ctx, cmd)
}

// executeWithTaskManager handles all three execution paths:
// A) Explicit background (run_in_background=true)
// B) Foreground with background escape (default, via Ctrl+B or auto-bg)
// C) Normal completion
func (t *Tool) executeWithTaskManager(ctx context.Context, cmd string, runInBackground bool) (any, error) {
	task, err := t.taskMgr.Start(ctx, cmd, "", runInBackground, t.workDir)
	if err != nil {
		return nil, err
	}

	// Path A: Explicit background — return immediately
	if runInBackground {
		return map[string]any{
			"task_id": task.ID,
			"status":  "running",
			"message": fmt.Sprintf("Command running in background. Use task_output with task_id '%s' to check results.", task.ID),
		}, nil
	}

	// Path B+C: Foreground with background escape
	// Set up auto-background timer (15s for main agent)
	autoBgTimer := time.AfterFunc(core.AutoBackgroundMs*time.Millisecond, func() {
		_ = t.taskMgr.Background(task.ID)
	})
	defer autoBgTimer.Stop()

	// Wait for completion or background signal
	select {
	case <-task.Done:
		// Path C: Normal completion
		return t.formatTaskResult(task)
	case <-task.BackgroundReq:
		// Path B: Backgrounded (Ctrl+B or auto-bg timer)
		autoBgTimer.Stop()
		return map[string]any{
			"task_id":           task.ID,
			"status":            "backgrounded",
			"output_so_far":     task.GetOutput(),
			"message":           fmt.Sprintf("Command sent to background (task_id: %s). It will continue running. Use task_output to get results.", task.ID),
			"auto_backgrounded": true,
		}, nil
	case <-ctx.Done():
		// Context cancelled — try to background the task
		_ = t.taskMgr.Background(task.ID)
		return map[string]any{
			"task_id":       task.ID,
			"status":        "backgrounded",
			"output_so_far": task.GetOutput(),
			"message":       fmt.Sprintf("Command sent to background (task_id: %s) due to context cancellation.", task.ID),
		}, nil
	}
}

// formatTaskResult returns the task output in the standard format.
func (t *Tool) formatTaskResult(task *core.BackgroundTask) (any, error) {
	output := task.GetOutput()

	// Apply tail-truncation
	tr := truncate.Tail(output, truncate.FileMaxLines, truncate.BashMaxChars)
	result := tr.Content
	if msg := truncate.FormatBashTruncation(tr); msg != "" {
		result += msg
	}

	// Scrub secrets from output BEFORE returning to model — defense-in-depth.
	// Prevents leaks like `cat .env` (if hard-deny missed) → key in stdout →
	// model echoes it into a tool-call arg or follow-up message → exfiltrated
	// to the LLM provider.
	result = sensitive.ScrubText(result)

	if task.Status == core.TaskFailed {
		return map[string]any{
			"output":   result,
			"exitCode": task.ExitCode,
			"error":    sensitive.ScrubText(task.Error),
		}, nil
	}

	return map[string]any{"output": result}, nil
}

// executeSync is the fallback for when no TaskManager is available.
func (t *Tool) executeSync(ctx context.Context, cmd string) (any, error) {
	output, err := t.executor.Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}

	tr := truncate.Tail(output, truncate.FileMaxLines, truncate.BashMaxChars)
	result := tr.Content
	if msg := truncate.FormatBashTruncation(tr); msg != "" {
		result += msg
	}

	// Scrub secrets BEFORE returning — see formatTaskResult for rationale.
	result = sensitive.ScrubText(result)

	return map[string]any{"output": result}, nil
}

// isRemoteExecutor returns true if the executor runs commands on a remote
// machine (e.g., SSH). Remote executors cannot use the local TaskManager
// which spawns processes via os/exec.
func isRemoteExecutor(exec Executor) bool {
	if exec == nil {
		return false
	}
	name := fmt.Sprintf("%T", exec)
	return name == "*adapters.SSHExecutor"
}
