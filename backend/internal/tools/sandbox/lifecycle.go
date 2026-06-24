// Package sandboxtools exposes lifecycle tools that let the CTO (or a
// human) explicitly tear down the sandbox container at the end of a task.
//
// The watchdog (backend/internal/sandbox/watchdog.go) handles idle
// teardown; this package handles the explicit "I'm done" case where the
// agent knows work is complete and there's no reason to keep the
// container alive.
//
// Why a tool, not just an HTTP endpoint:
//   - The agent decides when its work is complete, not a human.
//   - A tool participates in the diligence substrate (reason string is
//     quarantined via tools.QuarantineResult before being persisted).
//   - The audit trail lives in the same event stream as every other
//     tool call — frontends and downstream consumers don't need a new
//     subscription channel.
//
// CTO-only. Sub-agents don't see this tool (gated by the orchestrator's
// role-level tool composition) so they can't accidentally kill the
// sandbox mid-task while the CTO is waiting on a result.
package sandboxtools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools"
)

// ContainerShutdown is the subset of *sandbox.Manager this package needs.
// Defining it as an interface keeps the tool testable without spinning
// up Docker — tests pass a fake that records the calls.
type ContainerShutdown interface {
	ShutdownByProjectLabel(ctx context.Context, projectPath string) ([]string, error)
}

// ShutdownContainerTool tears down the sandbox container carrying the
// openshell.project-path label for ProjectPath. Terminal — after this
// returns, any further tool call against the same sandbox will fail
// with "container shutting down" or similar.
//
// The orchestrator treats this as a normal tool call: the result is
// returned to the model, the model emits its final text response, and
// the prompt completes. The next prompt on the same project re-creates
// the sandbox from scratch (init_files copy, chmod 0444, etc).
type ShutdownContainerTool struct {
	manager     ContainerShutdown
	projectPath string
}

// NewShutdownContainerTool wires the tool. projectPath is the path the
// orchestrator resolves for the current project (the same one passed
// to Manager.CreateSandbox). Empty projectPath = tool returns an error
// when called (defensive — the orchestrator should always set this).
func NewShutdownContainerTool(manager ContainerShutdown, projectPath string) *ShutdownContainerTool {
	return &ShutdownContainerTool{
		manager:     manager,
		projectPath: projectPath,
	}
}

func (t *ShutdownContainerTool) Name() string { return "shutdown_container" }

func (t *ShutdownContainerTool) Description() string {
	return "Tear down the sandbox container for this project. Call this AFTER yielding your final response when the task is complete and no further tool calls are needed. " +
		"The next prompt will re-create the sandbox from scratch. Optional 'reason' is logged + shown to the user. " +
		"Terminal: any tool call after this one will fail because the container is gone."
}

func (t *ShutdownContainerTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"reason": {
				"type": "string",
				"description": "Why the sandbox is being shut down (e.g. 'task complete', 'switching context'). Logged for audit + surfaced to user. Optional but recommended."
			}
		}
	}`)
}

func (t *ShutdownContainerTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	reason, _ := args["reason"].(string)
	if reason == "" {
		reason = "agent shutdown_container tool call"
	}

	if t.projectPath == "" {
		return map[string]any{
			"error": "shutdown_container cannot run without a resolved project path — the orchestrator must wire this at tool construction.",
		}, nil
	}
	if t.manager == nil {
		return map[string]any{
			"error": "shutdown_container cannot run without a sandbox manager — the orchestrator must wire this at tool construction.",
		}, nil
	}

	removed, err := t.manager.ShutdownByProjectLabel(ctx, t.projectPath)
	if err != nil {
		return map[string]any{
			"error":        fmt.Sprintf("shutdown_container failed: %v", err),
			"project_path": t.projectPath,
		}, nil
	}

	// Build the result. The reason string is agent-authored text and
	// could contain injection patterns — wrap it through QuarantineResult
	// before returning so downstream consumers see the boundary.
	result := map[string]any{
		"shutdown":        true,
		"containers":      removed,
		"count":           len(removed),
		"project_path":    t.projectPath,
		"reason":          reason,
		"shutdown_at":     time.Now().UTC().Format(time.RFC3339),
		"next_prompt":     "will re-create sandbox from scratch",
	}
	return tools.QuarantineResult(result), nil
}

// AllTools returns every tool in this package. Mirrors the contract
// enforced by backend/internal/tools/tool_audit_test.go — every tool
// package exposes AllTools() so orchestrator wiring has one callsite.
func AllTools(manager ContainerShutdown, projectPath string) []core.Tool {
	return []core.Tool{
		NewShutdownContainerTool(manager, projectPath),
	}
}
