package bash

import (
	"context"
	"errors"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

type mockExecutor struct {
	fn func(ctx context.Context, command string) (string, error)
}

func (m *mockExecutor) Exec(ctx context.Context, command string) (string, error) {
	return m.fn(ctx, command)
}

func TestTool_Name(t *testing.T) {
	tool := New(nil)
	if tool.Name() != "bash" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "bash")
	}
}

func TestTool_Description(t *testing.T) {
	tool := New(nil)
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, New(nil))
}

func TestTool_Execute_Success(t *testing.T) {
	exec := &mockExecutor{
		fn: func(ctx context.Context, command string) (string, error) {
			return "hello world", nil
		},
	}
	tool := New(exec)

	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello world",
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "output", "hello world")
}

func TestTool_Execute_EmptyCommand(t *testing.T) {
	tool := New(nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestTool_Execute_ExecutorError(t *testing.T) {
	exec := &mockExecutor{
		fn: func(ctx context.Context, command string) (string, error) {
			return "", errors.New("execution failed")
		},
	}
	tool := New(exec)

	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "failing-command",
	})
	if err == nil {
		t.Fatal("expected error from executor")
	}
}

func TestTool_Execute_CodeFallback(t *testing.T) {
	exec := &mockExecutor{
		fn: func(ctx context.Context, command string) (string, error) {
			return "ok", nil
		},
	}
	tool := New(exec)

	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "some code",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "output", "ok")
}

func TestTool_Execute_CmdFallback(t *testing.T) {
	exec := &mockExecutor{
		fn: func(ctx context.Context, command string) (string, error) {
			return "ok", nil
		},
	}
	tool := New(exec)

	result, err := tool.Execute(context.Background(), map[string]any{
		"cmd": "run this",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "output", "ok")
}
