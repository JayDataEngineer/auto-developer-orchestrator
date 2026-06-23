package safeguard

import (
	"context"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestSafeguardHookDestructiveArgsEmitEvent(t *testing.T) {
	router, _ := NewRouter()
	sub := make(chan core.AgentEvent, 4)
	hook := NewSafeguardHook(router, sub)
	hook.AgentName = "cto"

	called := false
	next := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		called = true
		return "ok", nil
	}
	args := map[string]any{
		"command": "git push --force origin main",
	}
	res, err := hook.WrapToolCall(context.Background(), "bash", args, next)
	if err != nil {
		t.Fatalf("WrapToolCall returned err: %v", err)
	}
	if !called {
		t.Error("next() was not called — safeguard must always pass through")
	}
	if res != "ok" {
		t.Errorf("got result %v, want ok", res)
	}

	select {
	case evt := <-sub:
		if evt.Type != core.EventTypeSafeguardFallback {
			t.Errorf("event type = %q, want safeguard_fallback", evt.Type)
		}
		data, ok := evt.Data.(core.SafeguardFallbackData)
		if !ok {
			t.Fatalf("event data wrong type: %T", evt.Data)
		}
		if data.AgentName != "cto" {
			t.Errorf("AgentName = %q, want cto", data.AgentName)
		}
		if data.ToolName != "bash" {
			t.Errorf("ToolName = %q, want bash", data.ToolName)
		}
		if data.MatchedText == "" {
			t.Error("MatchedText should be set")
		}
	case <-time.After(time.Second):
		t.Error("no event emitted for destructive args")
	}
}

func TestSafeguardHookBenignArgsNoEvent(t *testing.T) {
	router, _ := NewRouter()
	sub := make(chan core.AgentEvent, 1)
	hook := NewSafeguardHook(router, sub)

	next := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return "ok", nil
	}
	args := map[string]any{"command": "ls -la /tmp"}
	_, err := hook.WrapToolCall(context.Background(), "bash", args, next)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	select {
	case evt := <-sub:
		t.Errorf("unexpected event emitted for benign args: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// ok — no event expected
	}
}

func TestSafeguardHookNilSubscriberNoPanic(t *testing.T) {
	router, _ := NewRouter()
	hook := NewSafeguardHook(router, nil)
	next := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return "ok", nil
	}
	args := map[string]any{"command": "rm -rf /"}
	// Should not panic even though subscriber is nil.
	if _, err := hook.WrapToolCall(context.Background(), "bash", args, next); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestSafeguardHookNilRouterPassThrough(t *testing.T) {
	hook := NewSafeguardHook(nil, nil)
	called := false
	next := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		called = true
		return "ok", nil
	}
	args := map[string]any{"command": "rm -rf /"}
	if _, err := hook.WrapToolCall(context.Background(), "bash", args, next); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !called {
		t.Error("next() should be called even with nil router")
	}
}

func TestSafeguardHookOnBeforeModelChecksUserMessage(t *testing.T) {
	router, _ := NewRouter()
	sub := make(chan core.AgentEvent, 1)
	hook := NewSafeguardHook(router, sub)

	msgs := []core.Message{
		{Role: "user", Content: "I need to git push --force to overwrite the history"},
	}
	if _, err := hook.OnBeforeModel(context.Background(), &core.LoopState{}, msgs); err != nil {
		t.Fatalf("OnBeforeModel: %v", err)
	}
	select {
	case evt := <-sub:
		if evt.Type != core.EventTypeSafeguardFallback {
			t.Errorf("event type = %q, want safeguard_fallback", evt.Type)
		}
	case <-time.After(time.Second):
		t.Error("no event emitted for destructive content in user message")
	}
}

func TestSafeguardHookOnBeforeModelNoUserMessage(t *testing.T) {
	router, _ := NewRouter()
	sub := make(chan core.AgentEvent, 1)
	hook := NewSafeguardHook(router, sub)

	msgs := []core.Message{
		{Role: "assistant", Content: "thinking about how to proceed"},
	}
	if _, err := hook.OnBeforeModel(context.Background(), &core.LoopState{}, msgs); err != nil {
		t.Fatalf("OnBeforeModel: %v", err)
	}
	select {
	case evt := <-sub:
		t.Errorf("unexpected event: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// ok — no user message, no check
	}
}
