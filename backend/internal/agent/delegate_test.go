package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// fakeRoleLookup is a map-backed RoleLookup for tests.
type fakeRoleLookup struct {
	mu    sync.Mutex
	roles map[string]RoleConfig
}

func (l *fakeRoleLookup) Role(name string) (RoleConfig, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.roles[name]
	return r, ok
}

// TestDelegate_HappyPath verifies the basic delegation: parent calls
// delegate_to(role, task), child loop runs to completion, result returned.
func TestDelegate_HappyPath(t *testing.T) {
	// Both the parent AND the child share the same fake provider — but the
	// child sees only round 1's content (its own scope). We script the
	// provider's batches so the first invocation (parent's first round)
	// triggers delegate_to, the second (child) returns text, the third
	// (parent's second round) returns final text.
	childBatch := []core.ChatEvent{
		{Type: core.ChatEventContent, Content: "child did the work"},
		{Type: core.ChatEventDone, Finish: core.FinishStop},
	}
	parentBatch1 := []core.ChatEvent{
		{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{{
			Index: 0, ID: "tu",
			Function: core.FunctionCallDelta{
				Name:      "delegate_to",
				Arguments: `{"role":"researcher","task":"find stuff"}`,
			},
		}}},
		{Type: core.ChatEventDone, Finish: core.FinishToolCalls},
	}
	parentBatch2 := []core.ChatEvent{
		{Type: core.ChatEventContent, Content: "parent done"},
		{Type: core.ChatEventDone, Finish: core.FinishStop},
	}
	prov := newFakeProvider(parentBatch1, childBatch, parentBatch2)

	lookup := &fakeRoleLookup{roles: map[string]RoleConfig{
		"researcher": {Name: "researcher", Prompt: "you are a researcher"},
	}}
	delegate := NewDelegateTool(lookup, prov, &recordingExecutor{})

	// Drive the parent loop manually so we can inspect the delegate result
	// both at the tool-call level and via the parent's final response.
	parentExec := &recordingExecutor{}
	// Override the recording executor: when the parent calls delegate_to,
	// we want to actually invoke the delegate tool, not just record "ok-delegate_to".
	delegateExec := &routingExecutor{
		fallback: parentExec,
		routes: map[string]core.Tool{
			"delegate_to": delegate,
		},
	}

	loop, _ := NewLoop(LoopConfig{
		Provider: prov, Executor: delegateExec, SystemPrompt: "cto",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := loop.Run(ctx, "use the researcher")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "parent done" {
		t.Errorf("output = %q, want %q", out, "parent done")
	}

	// The parent loop's history should include the delegate_to result
	// ("child did the work") in a tool_result block. Inspect messages.
	msgs := loop.messagesForTest()
	var sawChildResult bool
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "tu" {
			if contains(m.Content, "child did the work") {
				sawChildResult = true
			}
		}
	}
	if !sawChildResult {
		t.Errorf("parent history missing delegate_to result; msgs=%v", msgs)
	}
}

// TestDelegate_UnknownRole verifies missing roles return a tool error.
func TestDelegate_UnknownRole(t *testing.T) {
	lookup := &fakeRoleLookup{roles: map[string]RoleConfig{}}
	prov := newFakeProvider()
	delegate := NewDelegateTool(lookup, prov, &recordingExecutor{})

	_, err := delegate.Execute(context.Background(), map[string]any{
		"role": "ghost",
		"task": "boo",
	})
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
	if !contains(err.Error(), "unknown role") {
		t.Errorf("err = %v, want 'unknown role'", err)
	}
}

// TestDelegate_MissingParams verifies required field validation.
func TestDelegate_MissingParams(t *testing.T) {
	delegate := NewDelegateTool(&fakeRoleLookup{}, newFakeProvider(), &recordingExecutor{})

	if _, err := delegate.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("missing role: expected error")
	}
	if _, err := delegate.Execute(context.Background(), map[string]any{"role": "x"}); err == nil {
		t.Error("missing task: expected error")
	}
}

// TestDelegate_ChildLoopFailureSurfacesAsText verifies a failed child
// loop does NOT propagate as a Go error — the parent CTO must be able to
// see the failure and react. Instead, the failure becomes a structured
// tool result with ok=false.
func TestDelegate_ChildLoopFailureSurfacesAsText(t *testing.T) {
	// Child returns ErrMaxRounds: 3 batches all returning tool_calls.
	loopBatch := []core.ChatEvent{
		{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{{
			Index: 0, ID: "tu", Function: core.FunctionCallDelta{Name: "bash"},
		}}},
		{Type: core.ChatEventDone, Finish: core.FinishToolCalls},
	}
	prov := newFakeProvider(loopBatch, loopBatch, loopBatch, loopBatch, loopBatch)

	lookup := &fakeRoleLookup{roles: map[string]RoleConfig{
		"worker": {Name: "worker", Prompt: "worker prompt", MaxRounds: 3},
	}}
	delegate := NewDelegateTool(lookup, prov, &recordingExecutor{})

	res, err := delegate.Execute(context.Background(), map[string]any{
		"role": "worker",
		"task": "loop",
	})
	if err != nil {
		t.Fatalf("delegate returned Go error (should have surfaced as text): %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", res)
	}
	if m["ok"] != false {
		t.Errorf("ok = %v, want false", m["ok"])
	}
	if !contains(m["error"].(string), "max rounds") {
		t.Errorf("error = %q, want max rounds", m["error"])
	}
}

// TestFilterTools_WhitelistEnforced verifies the helper actually filters.
func TestFilterTools_WhitelistEnforced(t *testing.T) {
	all := []core.OpenAITool{
		mkTool("bash"), mkTool("file_read"), mkTool("delegate_to"),
	}
	got := FilterTools(all, []string{"bash", "file_read"})
	if len(got) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(got))
	}
	for _, tool := range got {
		if tool.Function.Name == "delegate_to" {
			t.Errorf("delegate_to leaked through whitelist filter")
		}
	}
}

// TestFilterTools_EmptyWhitelistReturnsAll verifies the open-policy
// shortcut — empty whitelist means "no restriction".
func TestFilterTools_EmptyWhitelistReturnsAll(t *testing.T) {
	all := []core.OpenAITool{mkTool("bash"), mkTool("file_read")}
	got := FilterTools(all, nil)
	if len(got) != 2 {
		t.Fatalf("got len = %d, want 2 (empty whitelist = open policy)", len(got))
	}
}

// TestAssertNoDelegateInWhitelist verifies the recursion guard.
func TestAssertNoDelegateInWhitelist(t *testing.T) {
	if err := AssertNoDelegateInWhitelist("cto", []string{"bash", "file_read"}); err != nil {
		t.Errorf("clean whitelist rejected: %v", err)
	}
	err := AssertNoDelegateInWhitelist("researcher", []string{"bash", "delegate_to"})
	if err == nil {
		t.Fatal("expected error when delegate_to is in whitelist")
	}
	if !contains(err.Error(), "researcher") {
		t.Errorf("error should name the role: %v", err)
	}
}

// routingExecutor dispatches Execute() to a registered tool if one exists
// for the name, otherwise falls through to a recording executor. Used in
// tests where the parent loop needs to actually invoke the delegate tool
// rather than just record "ok-delegate_to".
type routingExecutor struct {
	fallback *recordingExecutor
	routes   map[string]core.Tool
}

func (e *routingExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if tool, ok := e.routes[name]; ok {
		return tool.Execute(ctx, args)
	}
	return e.fallback.Execute(ctx, name, args)
}

func mkTool(name string) core.OpenAITool {
	return core.OpenAITool{
		Type: "function",
		Function: core.FunctionDef{Name: name},
	}
}

// reference the errors package so the import is real even if future edits
// remove the only call site. Avoids fmt-style churn during refactors.
var _ = errors.New
