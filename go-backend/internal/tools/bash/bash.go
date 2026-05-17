package bash

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

// Executor executes a bash command and returns stdout.
type Executor interface {
	Exec(ctx context.Context, command string) (string, error)
}

// Tool implements core.Tool for bash execution.
type Tool struct {
	executor Executor
}

// New creates a new bash tool.
func New(exec Executor) *Tool {
	return &Tool{executor: exec}
}

func (t *Tool) Name() string        { return "bash" }
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
	output, err := t.executor.Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}

	// Apply tail-truncation for bash output (keep errors/results at the end)
	tr := truncate.Tail(output, truncate.FileMaxLines, truncate.BashMaxChars)
	result := tr.Content
	if msg := truncate.FormatBashTruncation(tr); msg != "" {
		result += msg
	}

	return map[string]any{"output": result}, nil
}
