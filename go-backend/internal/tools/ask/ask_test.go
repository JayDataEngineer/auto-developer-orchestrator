package ask

import (
	"context"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

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

func TestAskUserTool_Execute_EmptyOptions(t *testing.T) {
	tool := NewAskUserTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "Pick one?",
	})
	if err == nil {
		t.Fatal("expected error for missing options")
	}
}

func TestAskUserTool_Execute_SingleOption(t *testing.T) {
	tool := NewAskUserTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "Pick one?",
		"options":  []interface{}{"Only choice"},
	})
	if err == nil {
		t.Fatal("expected error for single option (need at least 2)")
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
	ctx = context.WithValue(ctx, core.SubscriberKey{}, subscriber)

	go func() {
		val, err := tool.Execute(ctx, map[string]any{
			"question": "What is your name?",
			"options":  []interface{}{"Alice", "Bob"},
		})
		done <- result{val, err}
	}()

	// Wait for the decision_request event
	var decisionID string
	select {
	case evt := <-subscriber:
		if evt.Type != core.EventTypeDecisionRequest {
			t.Fatalf("expected decision_request event, got %q", evt.Type)
		}
		d := evt.Data.(core.DecisionRequestData)
		if d.Type != "question" {
			t.Fatalf("expected question type, got %q", d.Type)
		}
		decisionID = d.ID
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Resolve via the unified decision registry
	ok := core.GlobalDecisions.Resolve(decisionID, core.DecisionResponse{
		Action: "answer",
		Value:  "Alice",
	})
	if !ok {
		t.Fatal("failed to resolve decision")
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
			"options":  []interface{}{"Option A", "Option B"},
		})
		done <- result{val, err}
	}()

	// Wait for registration
	var decisionID string
	select {
	case evt := <-subscriber:
		decisionID = evt.Data.(core.DecisionRequestData).ID
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
		// Verify cleanup removed the pending decision
		ok := core.GlobalDecisions.Resolve(decisionID, core.DecisionResponse{Action: "answer", Value: ""})
		if ok {
			t.Fatal("decision should have been cleaned up after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tool to complete")
	}
}
