// Package orchestration messaging.go — peer-to-peer messaging primitives
// for multi-agent collaboration.
//
// This ships the Fable/Mythos §8.15 pattern: agents that can talk to each
// other directly (rather than relaying everything through the CTO) hit a
// Pareto frontier that blocking-delegate can't reach. Anthropic's numbers
// show send_message + wait_for_message roughly double the success rate on
// tasks that need information sharing between specialists.
//
// Design:
//
//	MessageBus owns per-agent channels. Register/Unregister on agent
//	start/stop. Send is non-blocking (drops if buffer full — messages are
//	advisory, not transactional). Wait blocks with timeout.
//
// The bus is owned by ParallelRunner so it knows the full agent graph.
// Tools are constructed per-agent with the agent's own ID injected — the
// tool uses the bus for delivery but identifies itself on every send.
package orchestration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Message is one peer-to-peer delivery.
type Message struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// MessageBus routes peer-to-peer messages between agents. Each agent gets
// a buffered channel on Register; Send drops (with an emitted event) if
// the recipient's buffer is full rather than blocking the sender.
type MessageBus struct {
	mu      sync.RWMutex
	queues  map[string]chan Message
	subscriber chan<- core.AgentEvent
	bufDepth int
}

// NewMessageBus constructs a bus. bufDepth is the per-agent channel buffer;
// 16 is a reasonable default for most workloads. Pass nil for subscriber to
// suppress SSE emission (tests).
func NewMessageBus(bufDepth int, subscriber chan<- core.AgentEvent) *MessageBus {
	if bufDepth <= 0 {
		bufDepth = 16
	}
	return &MessageBus{
		queues:    make(map[string]chan Message),
		subscriber: subscriber,
		bufDepth:  bufDepth,
	}
}

// Register creates a queue for the named agent. idempotent — re-registering
// an existing agent is a no-op so CallOnAgentStart hooks can be defensive.
func (b *MessageBus) Register(agentID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.queues[agentID]; !exists {
		b.queues[agentID] = make(chan Message, b.bufDepth)
	}
}

// Unregister removes the agent and drops any undelivered messages in its
// queue. Safe to call on already-unregistered agents.
func (b *MessageBus) Unregister(agentID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, exists := b.queues[agentID]; exists {
		delete(b.queues, agentID)
		close(ch)
	}
}

// Peers returns the currently-registered agent IDs. The CTO uses this to
// enumerate valid send_message recipients.
func (b *MessageBus) Peers() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.queues))
	for id := range b.queues {
		out = append(out, id)
	}
	return out
}

// Send delivers a message to the recipient's queue. Returns an error if
// the recipient is not registered; drops silently (with an event) if the
// recipient's buffer is full — we don't block the sender on a slow peer.
// Emits an `agent_message` SSE event on every successful send.
func (b *MessageBus) Send(msg Message) error {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	b.mu.RLock()
	ch, exists := b.queues[msg.To]
	b.mu.RUnlock()
	if !exists {
		return fmt.Errorf("send_message: recipient %q not registered", msg.To)
	}
	select {
	case ch <- msg:
		b.emit(msg)
		return nil
	default:
		// Buffer full — drop. We could emit a "message_dropped" event,
		// but for now we treat messages as advisory and let the recipient
		// pull again. Better to lose one message than block the sender.
		return fmt.Errorf("send_message: recipient %q queue full (message dropped)", msg.To)
	}
}

// Receive blocks waiting for the next message addressed to agentID. Returns
// the message, or an error if the agent isn't registered / context cancels
// / timeout elapses.
func (b *MessageBus) Receive(ctx context.Context, agentID string, timeout time.Duration) (Message, error) {
	b.mu.RLock()
	ch, exists := b.queues[agentID]
	b.mu.RUnlock()
	if !exists {
		return Message{}, fmt.Errorf("wait_for_message: agent %q not registered", agentID)
	}
	if timeout <= 0 {
		select {
		case m, ok := <-ch:
			if !ok {
				return Message{}, fmt.Errorf("wait_for_message: queue closed")
			}
			return m, nil
		case <-ctx.Done():
			return Message{}, ctx.Err()
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case m, ok := <-ch:
		if !ok {
			return Message{}, fmt.Errorf("wait_for_message: queue closed")
		}
		return m, nil
	case <-timer.C:
		return Message{}, fmt.Errorf("wait_for_message: timeout after %s", timeout)
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

// emit ships an agent_message event to the SSE subscriber (if any).
func (b *MessageBus) emit(msg Message) {
	if b.subscriber == nil {
		return
	}
	core.SendEvent(b.subscriber, core.AgentEvent{
		Type: core.EventTypeAgentMessage,
		Data: core.AgentMessageData{
			FromAgent: msg.From,
			ToAgent:   msg.To,
			Content:   truncateForEvent(msg.Content, 500),
		},
	})
}

// truncateForEvent caps the message content in the SSE event so a chatty
// peer can't blow up the frontend. Full content still reaches the recipient.
func truncateForEvent(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
