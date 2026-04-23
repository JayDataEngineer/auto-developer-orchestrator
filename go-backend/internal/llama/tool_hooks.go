package llama

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// ToolHook intercepts tool execution before and/or after.
// Hooks compose: all Before hooks run first (any can block), then the tool
// executes, then all After hooks run (any can modify the result).
type ToolHook interface {
	// Name returns a unique identifier for logging and debugging.
	Name() string

	// BeforeToolCall is called before tool execution.
	//   - proceed=false blocks execution (err is returned to the model)
	//   - modifiedArgs replaces the original args (return nil to keep originals)
	//   - return (true, nil, nil) to proceed without modification
	BeforeToolCall(ctx context.Context, toolName string, args map[string]interface{}) (proceed bool, modifiedArgs map[string]interface{}, err error)

	// AfterToolCall is called after tool execution.
	//   - Can modify the result (return nil to keep original)
	//   - Receives the original err — can transform or suppress errors
	AfterToolCall(ctx context.Context, toolName string, args map[string]interface{}, result interface{}, err error) (modifiedResult interface{}, modifiedErr error)
}

// HookedExecutor wraps a ToolExecutor with before/after hooks.
// It implements ToolExecutor so it can be used anywhere a ToolExecutor is expected.
type HookedExecutor struct {
	inner  ToolExecutor
	hooks  []ToolHook
	logger *zap.Logger
}

// NewHookedExecutor creates a HookedExecutor wrapping inner with the given hooks.
func NewHookedExecutor(inner ToolExecutor, hooks []ToolHook, logger *zap.Logger) *HookedExecutor {
	return &HookedExecutor{
		inner:  inner,
		hooks:  hooks,
		logger: logger,
	}
}

// Execute runs all Before hooks, then the inner executor, then all After hooks.
func (e *HookedExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	// Phase 1: Before hooks
	for _, hook := range e.hooks {
		proceed, modifiedArgs, err := hook.BeforeToolCall(ctx, toolName, args)
		if err != nil {
			e.logger.Debug("Hook blocked tool call",
				zap.String("hook", hook.Name()),
				zap.String("tool", toolName),
				zap.Error(err),
			)
			return nil, fmt.Errorf("[hook:%s blocked] %w", hook.Name(), err)
		}
		if modifiedArgs != nil {
			args = modifiedArgs
		}
		if !proceed {
			e.logger.Debug("Hook cancelled tool call",
				zap.String("hook", hook.Name()),
				zap.String("tool", toolName),
			)
			return nil, fmt.Errorf("[hook:%s cancelled execution of %s]", hook.Name(), toolName)
		}
	}

	// Phase 2: Execute the tool
	result, err := e.inner.Execute(ctx, toolName, args)

	// Phase 3: After hooks
	for _, hook := range e.hooks {
		modifiedResult, modifiedErr := hook.AfterToolCall(ctx, toolName, args, result, err)
		if modifiedResult != nil {
			result = modifiedResult
		}
		if modifiedErr != nil {
			err = modifiedErr
		}
	}

	return result, err
}
