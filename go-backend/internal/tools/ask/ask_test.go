package ask

import (
	"context"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestPendingRegistry_Register(t *testing.T) {
	ch := PendingQuestions.Register("q-1")
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestPendingRegistry_Resolve(t *testing.T) {
	ch := PendingQuestions.Register("q-1")
	ok := PendingQuestions.Resolve("q-1", "yes")
	if !ok {
		t.Fatal("expected Resolve to return true")
	}

	select {
	case resp := <-ch:
		if resp != "yes" {
			t.Errorf("expected 'yes', got %q", resp)
		}
	default:
		t.Fatal("expected response on channel")
	}
}

func TestPendingRegistry_Resolve_NotFound(t *testing.T) {
	ok := PendingQuestions.Resolve("nonexistent", "")
	if ok {
		t.Fatal("expected Resolve to return false for unknown ID")
	}
}

func TestPendingRegistry_Resolve_Twice(t *testing.T) {
	PendingQuestions.Register("q-1")
	if !PendingQuestions.Resolve("q-1", "first") {
		t.Fatal("first resolve should succeed")
	}
	if PendingQuestions.Resolve("q-1", "second") {
		t.Fatal("second resolve should fail (already resolved)")
	}
}

func TestAskUserTool_Name(t *testing.T) {
	tool := NewAskUserTool()
	if tool.Name() != "ask_user" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "ask_user")
	}
}

func TestAskUserTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewAskUserTool())
}

func TestAskUserTool_Execute_EmptyQuestion(t *testing.T) {
	tool := NewAskUserTool()
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestAskUserTool_Execute_WithResponse(t *testing.T) {
	subscriber := make(chan core.AgentEvent, 10)
	tool := NewAskUserTool()

	type result struct {
		val any
		err error
	}
	done := make(chan result, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Contract 3: subscriber injected via context (like the agent loop does)
	ctx = context.WithValue(ctx, core.SubscriberKey{}, subscriber)

	go func() {
		val, err := tool.Execute(ctx, map[string]any{
			"question": "What is your name?",
			"options":  []interface{}{"Alice", "Bob"},
		})
		done <- result{val, err}
	}()

	// Wait for the question to be registered and event emitted
	var questionID string
	select {
	case evt := <-subscriber:
		if evt.Data.ToolName != "ask_user" {
			t.Fatalf("expected ask_user event, got %q", evt.Data.ToolName)
		}
		questionID = evt.Data.ToolID
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Resolve the question
	ok := PendingQuestions.Resolve(questionID, "Alice")
	if !ok {
		t.Fatal("failed to resolve question")
	}

	select {
	case r := <-done:
		testutil.AssertNoError(t, r.err)
		m := r.val.(map[string]any)
		testutil.AssertStringField(t, m, "response", "Alice")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tool to complete")
	}
}

func TestAskUserTool_Execute_ContextCancel(t *testing.T) {
	subscriber := make(chan core.AgentEvent, 10)
	tool := NewAskUserTool()

	type result struct {
		val any
		err error
	}
	done := make(chan result, 1)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, core.SubscriberKey{}, subscriber)

	go func() {
		val, err := tool.Execute(ctx, map[string]any{
			"question": "Quick question?",
		})
		done <- result{val, err}
	}()

	// Wait for registration
	var questionID string
	select {
	case evt := <-subscriber:
		questionID = evt.Data.ToolID
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Cancel the context
	cancel()

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("expected error after context cancel")
		}
		// Verify cleanup removed the pending question
		ok := PendingQuestions.Resolve(questionID, "")
		if ok {
			t.Fatal("question should have been cleaned up after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tool to complete")
	}
}
