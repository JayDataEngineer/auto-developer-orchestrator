package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// stubExecutor is a minimal ToolExecutor that records the calls it sees.
type stubExecutor struct {
	seen []string
}

func (s *stubExecutor) Execute(_ context.Context, name string, _ map[string]any) (any, error) {
	s.seen = append(s.seen, name)
	return "stub:" + name, nil
}

func TestMessagingExecutorRoutesMessagingTools(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("bob")
	parent := &stubExecutor{}
	exec := newMessagingExecutor(parent, bus, "alice")

	// send_message should hit the bus, not the parent.
	_, err := exec.Execute(context.Background(), "send_message", map[string]any{
		"to": "bob", "content": "hi",
	})
	if err != nil {
		t.Fatalf("send_message: %v", err)
	}
	for _, n := range parent.seen {
		if n == "send_message" {
			t.Error("send_message leaked to parent executor")
		}
	}

	// Non-messaging tool should fall through to parent.
	_, err = exec.Execute(context.Background(), "bash", map[string]any{"cmd": "ls"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if len(parent.seen) != 1 || parent.seen[0] != "bash" {
		t.Errorf("parent saw %v, want [bash]", parent.seen)
	}
}

func TestMessagingExecutorNilBusReturnsParent(t *testing.T) {
	parent := &stubExecutor{}
	exec := newMessagingExecutor(parent, nil, "alice")
	if exec != parent {
		t.Error("nil bus should return parent unchanged")
	}
}

func TestMessagingExecutorToolTimeoutHintDelegates(t *testing.T) {
	// ToolRegistry implements ToolTimeoutHint; verify the wrapper delegates.
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	reg := core.NewToolRegistry(nil)
	exec := newMessagingExecutor(reg, bus, "alice")

	// Hint for a non-existent tool should be 0 (from parent registry).
	if h := exec.(interface{ ToolTimeoutHint(string) time.Duration }).ToolTimeoutHint("nonexistent"); h != 0 {
		t.Errorf("ToolTimeoutHint = %s, want 0", h)
	}
}

func TestMessagingToolSpecsHasTrio(t *testing.T) {
	specs := messagingToolSpecs()
	if len(specs) != 3 {
		t.Fatalf("got %d specs, want 3", len(specs))
	}
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Function.Name] = true
	}
	for _, want := range []string{"send_message", "wait_for_message", "list_peers"} {
		if !names[want] {
			t.Errorf("missing spec %q", want)
		}
	}
}
