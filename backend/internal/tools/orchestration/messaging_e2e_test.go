package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// stubParentExecutor is a minimal core.ToolExecutor that records the last
// non-messaging tool call it received. Used as the parent inside
// messagingExecutor to prove the fall-through path.
type stubParentExecutor struct {
	lastCalled string
	lastArgs   map[string]any
}

func (s *stubParentExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	s.lastCalled = name
	s.lastArgs = args
	return map[string]any{"ok": true, "tool": name}, nil
}

func (s *stubParentExecutor) ToolTimeoutHint(toolName string) time.Duration { return 0 }

// Compile-time: stubParentExecutor satisfies core.ToolExecutor.
var _ core.ToolExecutor = (*stubParentExecutor)(nil)

// TestMessagingE2ESendMessageFiresAgentMessageEvent proves the full PR6
// composition path:
//
//	sub-agent tool call (send_message)
//	  → messagingExecutor.Execute
//	  → SendMessageTool.Execute
//	  → MessageBus.Send
//	  → MessageBus.emit
//	  → subscriber chan receives AgentMessage event
//
// Bus-level coverage exists in messaging_test.go (TestMessageBusEmitsAgentMessageEvent),
// and the SSE wire format is locked down in handlers/pux_sse_test.go. This test
// is the missing middle: it proves the executor wrapper actually dispatches
// send_message to the bus. If someone unwires newMessagingExecutor in
// parallel_runner.go (line 966), this test fails before the bug reaches prod.
func TestMessagingE2ESendMessageFiresAgentMessageEvent(t *testing.T) {
	subscriber := make(chan core.AgentEvent, 8)
	bus := NewMessageBus(4, subscriber)
	bus.Register("alice")
	bus.Register("bob")

	parent := &stubParentExecutor{}
	wrapped := newMessagingExecutor(parent, bus, "alice")

	args := map[string]any{
		"to":      "bob",
		"content": "e2e body",
	}
	result, err := wrapped.Execute(context.Background(), "send_message", args)
	if err != nil {
		t.Fatalf("send_message returned error: %v", err)
	}
	if result == nil {
		t.Fatal("send_message returned nil result")
	}

	// Parent should NOT have been touched — messaging tools stay in-band.
	if parent.lastCalled != "" {
		t.Errorf("parent executor was invoked for send_message: tool=%q args=%v",
			parent.lastCalled, parent.lastArgs)
	}

	// Event must reach the subscriber.
	select {
	case evt := <-subscriber:
		if evt.Type != core.EventTypeAgentMessage {
			t.Fatalf("expected EventTypeAgentMessage, got %q", evt.Type)
		}
		data, ok := evt.Data.(core.AgentMessageData)
		if !ok {
			t.Fatalf("expected AgentMessageData payload, got %T", evt.Data)
		}
		if data.FromAgent != "alice" {
			t.Errorf("FromAgent: got %q, want %q", data.FromAgent, "alice")
		}
		if data.ToAgent != "bob" {
			t.Errorf("ToAgent: got %q, want %q", data.ToAgent, "bob")
		}
		if data.Content != "e2e body" {
			t.Errorf("Content: got %q, want %q", data.Content, "e2e body")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive AgentMessage event on subscriber within 500ms")
	}

	// And the message must also be deliverable to bob via the bus — proves
	// the bus queue path still works alongside the event emission.
	msg, err := bus.Receive(context.Background(), "bob", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("bob did not receive queued message: %v", err)
	}
	if msg.Content != "e2e body" {
		t.Errorf("queued message content: got %q, want %q", msg.Content, "e2e body")
	}
}

// TestMessagingE2ENonMessagingToolsFallThroughToParent proves that
// messagingExecutor does not intercept tools it doesn't own. A sub-agent
// calling `bash` (or any other employee tool) must reach the real executor.
func TestMessagingE2ENonMessagingToolsFallThroughToParent(t *testing.T) {
	subscriber := make(chan core.AgentEvent, 8)
	bus := NewMessageBus(4, subscriber)
	bus.Register("alice")

	parent := &stubParentExecutor{}
	wrapped := newMessagingExecutor(parent, bus, "alice")

	args := map[string]any{"command": "echo hello"}
	_, err := wrapped.Execute(context.Background(), "bash", args)
	if err != nil {
		t.Fatalf("bash fall-through returned error: %v", err)
	}

	if parent.lastCalled != "bash" {
		t.Errorf("parent executor did not see the bash call: lastCalled=%q",
			parent.lastCalled)
	}
	if parent.lastArgs["command"] != "echo hello" {
		t.Errorf("parent executor args mismatch: %v", parent.lastArgs)
	}

	// No event should have been emitted — bash isn't a messaging tool.
	select {
	case evt := <-subscriber:
		t.Errorf("unexpected event emitted for bash call: %+v", evt)
	default:
		// good — channel empty
	}
}

// TestMessagingE2ENilBusReturnsParent proves the nil-safety contract: when
// a runner has no bus (e.g., SandboxOnly CTO), newMessagingExecutor returns
// the parent unchanged. This is what prevents a nil-pointer panic when a
// sub-agent tries to send a message in a bus-less configuration — the tool
// simply isn't in the registry.
func TestMessagingE2ENilBusReturnsParent(t *testing.T) {
	parent := &stubParentExecutor{}
	wrapped := newMessagingExecutor(parent, nil, "alice")
	if wrapped != parent {
		t.Errorf("newMessagingExecutor with nil bus should return parent unchanged; got %T",
			wrapped)
	}
}

// TestMessagingE2EWaitForMessageRoutesThroughExecutor proves the
// wait_for_message tool also dispatches via messagingExecutor. Mirrors the
// send_message test but for the receive side — catches asymmetric wiring.
func TestMessagingE2EWaitForMessageRoutesThroughExecutor(t *testing.T) {
	subscriber := make(chan core.AgentEvent, 8)
	bus := NewMessageBus(4, subscriber)
	bus.Register("alice")
	bus.Register("bob")

	// Pre-stage a message from bob so wait_for_message returns immediately.
	// bus.Send emits an AgentMessage event for the delivery; drain it before
	// the wait_for_message call so the post-call subscriber check is clean.
	if err := bus.Send(Message{
		From:      "bob",
		To:        "alice",
		Content:   "ping",
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("bus.Send: %v", err)
	}
	select {
	case <-subscriber:
		// drain the AgentMessage event fired by Send
	default:
		t.Fatal("expected AgentMessage event from pre-stage Send to drain")
	}

	parent := &stubParentExecutor{}
	wrapped := newMessagingExecutor(parent, bus, "alice")

	args := map[string]any{"timeout_ms": float64(500)}
	result, err := wrapped.Execute(context.Background(), "wait_for_message", args)
	if err != nil {
		t.Fatalf("wait_for_message returned error: %v", err)
	}

	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if resMap["from"] != "bob" {
		t.Errorf("from: got %v, want %q", resMap["from"], "bob")
	}
	if !strings.Contains(resMap["content"].(string), "ping") {
		t.Errorf("content: got %v, want to contain %q", resMap["content"], "ping")
	}

	// wait_for_message doesn't emit an AgentMessage event — only send_message
	// does. Make sure we didn't accidentally emit one on the receive path.
	select {
	case evt := <-subscriber:
		t.Errorf("wait_for_message should not emit events; got %+v", evt)
	default:
		// good
	}
}

// TestMessagingE2EListPeersRoutesThroughExecutor proves list_peers also
// dispatches via messagingExecutor. The three tools are a package deal —
// if any one is missing from the dispatch map, the contract is broken.
func TestMessagingE2EListPeersRoutesThroughExecutor(t *testing.T) {
	subscriber := make(chan core.AgentEvent, 8)
	bus := NewMessageBus(4, subscriber)
	bus.Register("alice")
	bus.Register("bob")
	bus.Register("carol")

	parent := &stubParentExecutor{}
	wrapped := newMessagingExecutor(parent, bus, "alice")

	result, err := wrapped.Execute(context.Background(), "list_peers", map[string]any{})
	if err != nil {
		t.Fatalf("list_peers returned error: %v", err)
	}

	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	peers, ok := resMap["peers"].([]string)
	if !ok {
		t.Fatalf("expected []string under 'peers', got %T", resMap["peers"])
	}
	if len(peers) != 3 {
		t.Errorf("peers count: got %d, want 3 (alice+bob+carol)", len(peers))
	}
}
