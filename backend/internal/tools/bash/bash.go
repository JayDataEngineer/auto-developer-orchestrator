package bash

import (
	"context"
	"encoding/json"
	"fmt"

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
	executor       Executor
	validator      *Validator          // nil = no command restriction
	secretResolver func(string) string // optional: resolves <secret>domain.key</secret> before exec
}

// New creates a new bash tool with default command restrictions.
func New(exec Executor) *Tool {
	return &Tool{executor: exec, validator: NewDefaultValidator()}
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
		"Execute a bash command. If output exceeds %d characters, it will be truncated (keeping the end).",
		truncate.BashMaxChars,
	)
}

func (t *Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to execute"}
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

	output, err := t.executor.Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}

	tr := truncate.Tail(output, truncate.FileMaxLines, truncate.BashMaxChars)
	result := tr.Content
	if msg := truncate.FormatBashTruncation(tr); msg != "" {
		result += msg
	}

	// Scrub secrets BEFORE returning — prevents leaks like `cat .env` (if
	// hard-deny missed) → key in stdout → model echoes it into a tool-call
	// arg or follow-up message → exfiltrated to the LLM provider.
	result = sensitive.ScrubText(result)

	return map[string]any{"output": result}, nil
}
