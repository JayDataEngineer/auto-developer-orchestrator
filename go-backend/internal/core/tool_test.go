package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestToolRegistry_New(t *testing.T) {
	reg := NewToolRegistry([]Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "write"},
	})
	if reg.Get("bash") == nil {
		t.Error("expected 'bash' tool in registry")
	}
	if reg.Get("write") == nil {
		t.Error("expected 'write' tool in registry")
	}
	if reg.Get("nonexistent") != nil {
		t.Error("expected nil for nonexistent tool")
	}
}

func TestToolRegistry_All(t *testing.T) {
	reg := NewToolRegistry([]Tool{
		&stubTool{name: "a"},
		&stubTool{name: "b"},
	})
	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(all))
	}
}

func TestToolRegistry_Names(t *testing.T) {
	reg := NewToolRegistry([]Tool{
		&stubTool{name: "a"},
		&stubTool{name: "b"},
	})
	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
}

func TestToolRegistry_Alias(t *testing.T) {
	reg := NewToolRegistry([]Tool{
		&stubTool{name: "bash"},
	})
	reg.RegisterAlias("execute_bash", "bash")

	if reg.Get("execute_bash") == nil {
		t.Error("expected alias 'execute_bash' to resolve to 'bash'")
	}
	if reg.NormalizeToolName("EXECUTE_BASH") != "bash" {
		t.Errorf("NormalizeToolName should lowercase, got %q", reg.NormalizeToolName("EXECUTE_BASH"))
	}
}

func TestToolRegistry_Alias_SelfReference(t *testing.T) {
	reg := NewToolRegistry(nil)
	reg.RegisterAlias("foo", "foo") // Should not register

	if reg.Get("foo") != nil {
		t.Error("self-referencing alias should not register")
	}
}

func TestToolRegistry_RegisterCommonAliases(t *testing.T) {
	reg := NewToolRegistry([]Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "file_read"},
		&stubTool{name: "file_write"},
		&stubTool{name: "file_edit"},
		&stubTool{name: "file_grep"},
		&stubTool{name: "file_glob"},
		&stubTool{name: "click_element"},
		&stubTool{name: "type"},
		&stubTool{name: "search_web"},
	})
	reg.RegisterCommonAliases()

	aliases := []struct {
		alias     string
		canonical string
	}{
		{"bash_execute", "bash"},
		{"execute_bash", "bash"},
		{"execute_command", "bash"},
		{"run_command", "bash"},
		{"read_file", "file_read"},
		{"write_file", "file_write"},
		{"edit_file", "file_edit"},
		{"grep", "file_grep"},
		{"glob", "file_glob"},
		{"click", "click_element"},
		{"type_text", "type"},
		{"search", "search_web"},
	}
	for _, a := range aliases {
		if reg.Get(a.alias) == nil {
			t.Errorf("alias %q should resolve to %q", a.alias, a.canonical)
		}
	}
	// Also verify direct canonical names still work
	for _, name := range []string{"bash", "file_read", "file_write", "file_edit", "file_grep", "file_glob"} {
		if reg.Get(name) == nil {
			t.Errorf("canonical tool %q should be accessible directly", name)
		}
	}
}

func TestToolRegistry_Execute(t *testing.T) {
	reg := NewToolRegistry([]Tool{
		&stubTool{name: "echo", execute: func(ctx context.Context, args map[string]any) (any, error) {
			return "hello", nil
		}},
	})

	result, err := reg.Execute(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}
}

func TestToolRegistry_Execute_NotFound(t *testing.T) {
	reg := NewToolRegistry(nil)
	_, err := reg.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
	var notFound *ToolNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *ToolNotFoundError, got %T", err)
	}
}

func TestToolRegistry_Execute_WithAlias(t *testing.T) {
	reg := NewToolRegistry([]Tool{
		&stubTool{name: "bash", execute: func(ctx context.Context, args map[string]any) (any, error) {
			return "result", nil
		}},
	})
	reg.RegisterAlias("execute_bash", "bash")

	result, err := reg.Execute(context.Background(), "EXECUTE_BASH", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("expected 'result', got %v", result)
	}
}

func TestToolError(t *testing.T) {
	err := NewToolError("bash", "command failed")
	if err.Error() != "[bash] command failed" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestToolNotFoundError(t *testing.T) {
	err := &ToolNotFoundError{ToolName: "foo"}
	if err.Error() != "tool not found: foo" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "nil error", err: nil, want: ErrorUnknown},
		{name: "timeout", err: errors.New("timeout"), want: ErrorTransient},
		{name: "connection refused", err: errors.New("connection refused"), want: ErrorTransient},
		{name: "context canceled", err: errors.New("context canceled"), want: ErrorTransient},
		{name: "deadline exceeded", err: errors.New("deadline exceeded"), want: ErrorTransient},
		{name: "rate limit exceeded", err: errors.New("rate limit exceeded"), want: ErrorTransient},
		{name: "502 bad gateway", err: errors.New("502 bad gateway"), want: ErrorTransient},
		{name: "503 service unavailable", err: errors.New("503 service unavailable"), want: ErrorTransient},
		{name: "504 gateway timeout", err: errors.New("504 gateway timeout"), want: ErrorTransient},
		{name: "stream error", err: errors.New("stream error"), want: ErrorTransient},
		{name: "eof", err: errors.New("eof"), want: ErrorTransient},
		{name: "not found", err: errors.New("not found"), want: ErrorPermanent},
		{name: "permission denied", err: errors.New("permission denied"), want: ErrorPermanent},
		{name: "invalid parameter", err: errors.New("invalid parameter"), want: ErrorPermanent},
		{name: "unknown command", err: errors.New("unknown command"), want: ErrorPermanent},
		{name: "missing required field", err: errors.New("missing required field"), want: ErrorPermanent},
		{name: "not available", err: errors.New("not available"), want: ErrorPermanent},
		{name: "generic error", err: errors.New("generic error"), want: ErrorUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyError(tc.err)
			if got != tc.want {
				t.Errorf("ClassifyError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSendEvent(t *testing.T) {
	ch := make(chan AgentEvent, 1)
	evt := AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: "hello"}}
	SendEvent(ch, evt)

	select {
	case received := <-ch:
		if received.Type != EventTypeTextDelta {
			t.Errorf("expected EventTypeTextDelta, got %v", received.Type)
		}
		if received.Data.Text != "hello" {
			t.Errorf("expected text 'hello', got %q", received.Data.Text)
		}
	default:
		t.Fatal("expected event on channel")
	}
}

func TestSendEvent_Blocking(t *testing.T) {
	// Unbuffered channel should not block due to SendEvent's non-blocking select
	ch := make(chan AgentEvent)
	evt := AgentEvent{Type: EventTypeTextDelta}
	SendEvent(ch, evt) // Should not block (default case)

	select {
	case <-ch:
		// Should not reach here since the channel is unbuffered and default case should fire
	default:
		// OK - SendEvent used the default case
	}
}

// stubTool for testing
type stubTool struct {
	name    string
	execute func(ctx context.Context, args map[string]any) (any, error)
}

func (s *stubTool) Name() string { return s.name }
func (s *stubTool) Description() string { return "stub " + s.name }
func (s *stubTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *stubTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if s.execute != nil {
		return s.execute(ctx, args)
	}
	return nil, nil
}
