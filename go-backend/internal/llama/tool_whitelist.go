package llama

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// ── ToolWhitelistExecutor ────────────────────────────────────────────

// ToolWhitelistExecutor wraps a ToolExecutor and enforces a dynamic tool whitelist.
// Used by sub-agents created via delegate_to — the orchestrator picks the tool set.
type ToolWhitelistExecutor struct {
	toolWhitelist []string
	baseExecutor  ToolExecutor
	logger        *zap.Logger
}

// Execute checks the tool whitelist before delegating to the base executor.
func (e *ToolWhitelistExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	// Normalize tool name (handle common aliases)
	normalized := normalizeToolName(toolName, args)

	// yield_artifact is handled by the agent loop (terminal signal)
	// but we need to accept it here so the whitelist check passes.
	if normalized == "yield_artifact" {
		output, _ := args["output"].(string)
		return map[string]interface{}{
			"yielded": true,
			"output":  output,
		}, nil
	}

	// Check whitelist
	for _, allowed := range e.toolWhitelist {
		if normalized == allowed {
			return e.baseExecutor.Execute(ctx, normalized, args)
		}
	}

	e.logger.Error("Tool whitelist rejection",
		zap.String("original", toolName),
		zap.String("normalized", normalized),
		zap.Int("whitelistLen", len(e.toolWhitelist)),
	)
	return nil, fmt.Errorf(
		"[SYSTEM: Tool %q is not available. Available tools: %v. Use only the tools listed above.]",
		toolName, e.toolWhitelist,
	)
}
