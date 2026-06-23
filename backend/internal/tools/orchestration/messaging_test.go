package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestMessageBusRegisterSendReceive(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("bob")

	if err := bus.Send(Message{From: "alice", To: "bob", Content: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, err := bus.Receive(ctx, "bob", time.Second)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if msg.Content != "hi" {
		t.Errorf("got content %q, want hi", msg.Content)
	}
	if msg.From != "alice" {
		t.Errorf("got from %q, want alice", msg.From)
	}
}

func TestMessageBusUnregisteredRecipient(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	err := bus.Send(Message{From: "alice", To: "charlie", Content: "hi"})
	if err == nil {
		t.Error("Send to unregistered recipient should error")
	}
	if err.Error() == "" {
		t.Error("error message should be set")
	}
}

func TestMessageBusReceiveUnregistered(t *testing.T) {
	bus := NewMessageBus(4, nil)
	_, err := bus.Receive(context.Background(), "charlie", 50*time.Millisecond)
	if err == nil {
		t.Error("Receive on unregistered agent should error")
	}
}

func TestMessageBusTimeout(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	start := time.Now()
	_, err := bus.Receive(context.Background(), "alice", 50*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("Receive with no message should timeout")
	}
	if elapsed < 40*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("timeout took %s, want ~50ms", elapsed)
	}
}

func TestMessageBusPeers(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("bob")
	bus.Register("charlie")
	peers := bus.Peers()
	if len(peers) != 3 {
		t.Errorf("Peers returned %d, want 3", len(peers))
	}
}

func TestMessageBusBufferFullDrops(t *testing.T) {
	bus := NewMessageBus(2, nil)
	bus.Register("alice")
	bus.Register("bob")
	// Fill alice's buffer.
	if err := bus.Send(Message{From: "bob", To: "alice", Content: "1"}); err != nil {
		t.Fatalf("Send 1: %v", err)
	}
	if err := bus.Send(Message{From: "bob", To: "alice", Content: "2"}); err != nil {
		t.Fatalf("Send 2: %v", err)
	}
	// Third should fail (buffer full).
	err := bus.Send(Message{From: "bob", To: "alice", Content: "3"})
	if err == nil {
		t.Error("Send 3 should have failed with buffer full")
	}
}

func TestMessageBusEmitsAgentMessageEvent(t *testing.T) {
	sub := make(chan core.AgentEvent, 4)
	bus := NewMessageBus(4, sub)
	bus.Register("alice")
	bus.Register("bob")

	if err := bus.Send(Message{From: "alice", To: "bob", Content: "hello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case evt := <-sub:
		if evt.Type != core.EventTypeAgentMessage {
			t.Errorf("event type = %q, want agent_message", evt.Type)
		}
		data, ok := evt.Data.(core.AgentMessageData)
		if !ok {
			t.Fatalf("data type %T", evt.Data)
		}
		if data.FromAgent != "alice" || data.ToAgent != "bob" {
			t.Errorf("from=%q to=%q, want alice/bob", data.FromAgent, data.ToAgent)
		}
		if data.Content != "hello" {
			t.Errorf("content = %q, want hello", data.Content)
		}
	case <-time.After(time.Second):
		t.Error("no event emitted")
	}
}

func TestMessageBusRegisterIdempotent(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("alice") // should not panic, should not double-create
	if len(bus.Peers()) != 1 {
		t.Errorf("Peers = %v, want just [alice]", bus.Peers())
	}
}

func TestMessageBusUnregisterDropsPending(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("bob")
	_ = bus.Send(Message{From: "bob", To: "alice", Content: "1"})
	bus.Unregister("alice")
	// alice re-registers; old message should be gone (queue was closed+replaced)
	bus.Register("alice")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := bus.Receive(ctx, "alice", 50*time.Millisecond)
	if err == nil {
		t.Error("should have timed out — old messages dropped on Unregister")
	}
}

// --- Tools ---

func TestSendMessageToolDelivers(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("bob")
	tool := NewSendMessageTool(bus, "alice")
	res, err := tool.Execute(context.Background(), map[string]any{
		"to":      "bob",
		"content": "ping",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := res.(map[string]any)
	if m["delivered"] != true {
		t.Errorf("delivered = %v, want true", m["delivered"])
	}
	// verify delivery
	msg, _ := bus.Receive(context.Background(), "bob", time.Second)
	if msg.Content != "ping" {
		t.Errorf("got %q, want ping", msg.Content)
	}
}

func TestSendMessageToolMissingArgs(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	tool := NewSendMessageTool(bus, "alice")
	_, err := tool.Execute(context.Background(), map[string]any{"to": "bob"})
	if err == nil {
		t.Error("missing content should error")
	}
}

func TestWaitForMessageToolReturnsMessage(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("bob")
	waitTool := NewWaitForMessageTool(bus, "alice")

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = bus.Send(Message{From: "bob", To: "alice", Content: "async-ping"})
	}()

	res, err := waitTool.Execute(context.Background(), map[string]any{"timeout_ms": 1000})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := res.(map[string]any)
	if m["from"] != "bob" {
		t.Errorf("from = %v, want bob", m["from"])
	}
	if m["content"] != "async-ping" {
		t.Errorf("content = %v, want async-ping", m["content"])
	}
}

func TestWaitForMessageToolTimeout(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	tool := NewWaitForMessageTool(bus, "alice")
	_, err := tool.Execute(context.Background(), map[string]any{"timeout_ms": 50})
	if err == nil {
		t.Error("should timeout with no message")
	}
}

func TestListPeersToolReturnsRegistered(t *testing.T) {
	bus := NewMessageBus(4, nil)
	bus.Register("alice")
	bus.Register("bob")
	bus.Register("charlie")
	tool := NewListPeersTool(bus, "alice")
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := res.(map[string]any)
	if m["self"] != "alice" {
		t.Errorf("self = %v, want alice", m["self"])
	}
	if m["count"].(int) != 3 {
		t.Errorf("count = %v, want 3", m["count"])
	}
}

func TestMessagingToolsReturnsTrio(t *testing.T) {
	bus := NewMessageBus(4, nil)
	tools := MessagingTools(bus, "alice")
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3", len(tools))
	}
	names := []string{tools[0].Name(), tools[1].Name(), tools[2].Name()}
	expected := map[string]bool{"send_message": true, "wait_for_message": true, "list_peers": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected tool %q", n)
		}
	}
}

func TestSendMessageToolNilBus(t *testing.T) {
	tool := NewSendMessageTool(nil, "alice")
	_, err := tool.Execute(context.Background(), map[string]any{"to": "bob", "content": "hi"})
	if err == nil {
		t.Error("nil bus should error")
	}
}
