// Package env provides a unified execution abstraction for all backends
// (local, Docker sandbox, SSH). Modeled on Hermes-agent's BaseEnvironment.
//
// Every command goes through Execute(), which handles session snapshot
// sourcing, CWD tracking, and security guards. The agent loop does not
// know whether commands run locally, in Docker, or over SSH.
package env

import (
	"context"
	"fmt"
	"time"
)

// ExecuteResult is the unified return type for all command execution.
type ExecuteResult struct {
	Output      string // stdout (and merged stderr)
	ExitCode    int    // process exit code
	Interrupted bool   // true if the command was interrupted
}

// ExecuteOptions controls per-call execution behavior.
type ExecuteOptions struct {
	// CWD overrides the working directory for this call.
	// Empty = use the session-tracked CWD.
	CWD string
	// Timeout is the per-command timeout. 0 = use the environment default.
	Timeout time.Duration
	// StdinData is optional data to pipe to the command's stdin.
	StdinData string
}

// Environment is the unified, backend-agnostic execution interface.
// Every command goes through Execute(), which handles session snapshot
// sourcing, CWD tracking, and security guards.
type Environment interface {
	// Execute runs a command in the environment with full wrapping.
	Execute(ctx context.Context, command string, opts ExecuteOptions) (*ExecuteResult, error)

	// InitSession captures login shell environment variables into a
	// snapshot file. Called once after construction. Subsequent
	// commands source this snapshot to preserve env state.
	InitSession(ctx context.Context) error

	// CWD returns the current working directory tracked by the session.
	CWD() string

	// SetEnv sets an environment variable for subsequent commands.
	SetEnv(key, value string)

	// Close releases backend resources.
	Close() error
}

// envExecutorAdapter wraps an Environment to implement bash.Executor.
// Allows gradual migration — existing code that expects bash.Executor
// can use an Environment transparently.
type envExecutorAdapter struct {
	env Environment
}

// AsExecutor wraps an Environment to implement the bash.Executor interface.
func AsExecutor(env Environment) *envExecutorAdapter {
	return &envExecutorAdapter{env: env}
}

// Exec implements bash.Executor. It delegates to Environment.Execute
// and translates the result to the bash.Executor contract:
//   - exit code 0 → return output, nil
//   - non-zero exit → return output + error
func (a *envExecutorAdapter) Exec(ctx context.Context, command string) (string, error) {
	result, err := a.env.Execute(ctx, command, ExecuteOptions{})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return result.Output, fmt.Errorf("exit code %d", result.ExitCode)
	}
	return result.Output, nil
}
