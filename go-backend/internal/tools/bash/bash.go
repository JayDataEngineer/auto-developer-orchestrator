package bash

import (
	"context"
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/core"
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
func (t *Tool) Description() string { return "Execute a bash command" }

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
	return map[string]any{"output": output}, nil
}
