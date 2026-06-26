package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SendMessageTool lets an agent deliver a message to a peer via the
// MessageBus. The tool is constructed per-agent with the sender's ID
// injected — the model only supplies the recipient and content.
type SendMessageTool struct {
	bus     *MessageBus
	selfID  string
	timeout time.Duration
}

// NewSendMessageTool constructs the tool. selfID is the agent calling the
// tool; the bus uses it to set Message.From.
func NewSendMessageTool(bus *MessageBus, selfID string) *SendMessageTool {
	return &SendMessageTool{bus: bus, selfID: selfID, timeout: 5 * time.Second}
}

func (t *SendMessageTool) Name() string { return "send_message" }

func (t *SendMessageTool) Description() string {
	return "Send a peer-to-peer message to another agent. Use for clarifications, status handoffs, or sharing findings. The recipient receives the message on its next wait_for_message call. Messages are advisory — dropped if the recipient's buffer is full."
}

func (t *SendMessageTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"to": {
				"type": "string",
				"description": "Recipient agent ID. Must be currently registered (use list_peers to enumerate)."
			},
			"content": {
				"type": "string",
				"description": "Message body. Be terse — peers are working in parallel and don't want a wall of text."
			}
		},
		"required": ["to", "content"]
	}`)
}

func (t *SendMessageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.bus == nil {
		return nil, core.NewToolError("send_message", "messaging not configured")
	}
	to, _ := args["to"].(string)
	content, _ := args["content"].(string)
	if to == "" {
		return nil, core.NewToolError("send_message", "missing required arg 'to'")
	}
	if content == "" {
		return nil, core.NewToolError("send_message", "missing required arg 'content'")
	}
	msg := Message{From: t.selfID, To: to, Content: content}
	if err := t.bus.Send(msg); err != nil {
		return nil, core.NewToolError("send_message", err.Error())
	}
	return map[string]any{
		"delivered": true,
		"to":        to,
		"hint":      "recipient will see this on its next wait_for_message call",
	}, nil
}

// WaitForMessageTool blocks until a peer sends a message to the calling
// agent, or the timeout elapses. Default timeout is 30s; the model can
// override via the timeout_ms arg.
type WaitForMessageTool struct {
	bus    *MessageBus
	selfID string
}

// NewWaitForMessageTool constructs the tool. selfID is the agent calling
// the tool; the bus uses it to find the right queue.
func NewWaitForMessageTool(bus *MessageBus, selfID string) *WaitForMessageTool {
	return &WaitForMessageTool{bus: bus, selfID: selfID}
}

func (t *WaitForMessageTool) Name() string { return "wait_for_message" }

func (t *WaitForMessageTool) Description() string {
	return "Block until a peer sends you a message. Use after delegating work that needs to come back with a clarification. Returns the message content and sender."
}

func (t *WaitForMessageTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"timeout_ms": {
				"type": "integer",
				"description": "How long to wait in milliseconds. Default 30000 (30s). Use 0 to wait forever (not recommended — you will hang if no peer messages)."
			}
		}
	}`)
}

func (t *WaitForMessageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.bus == nil {
		return nil, core.NewToolError("wait_for_message", "messaging not configured")
	}
	timeoutMs := parseDurationMs(args["timeout_ms"])
	if timeoutMs == 0 {
		timeoutMs = 30000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	msg, err := t.bus.Receive(ctx, t.selfID, timeout)
	if err != nil {
		return nil, core.NewToolError("wait_for_message", err.Error())
	}
	return map[string]any{
		"from":    msg.From,
		"content": msg.Content,
		"sent_at": msg.Timestamp.Format(time.RFC3339),
	}, nil
}

// parseDurationMs pulls an integer-like value from args, tolerating the
// float64 that JSON unmarshal produces AND the int that unit tests pass.
// Returns 0 for missing/unparseable values.
func parseDurationMs(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

// ListPeersTool enumerates the currently-registered agents on the bus.
// Useful for the CTO or any agent that needs to discover valid recipients
// before calling send_message.
type ListPeersTool struct {
	bus    *MessageBus
	selfID string
}

// NewListPeersTool constructs the tool.
func NewListPeersTool(bus *MessageBus, selfID string) *ListPeersTool {
	return &ListPeersTool{bus: bus, selfID: selfID}
}

func (t *ListPeersTool) Name() string { return "list_peers" }

func (t *ListPeersTool) Description() string {
	return "List agent IDs currently registered on the message bus. Use before send_message to discover valid recipients. Includes yourself."
}

func (t *ListPeersTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t *ListPeersTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	if t.bus == nil {
		return nil, core.NewToolError("list_peers", "messaging not configured")
	}
	peers := t.bus.Peers()
	return map[string]any{
		"self":  t.selfID,
		"peers": peers,
		"count": len(peers),
	}, nil
}

// MessagingTools returns the full trio (send, wait, list) for an agent.
// Pass the same bus to all agents that should be able to talk to each
// other; pass the same selfID for an agent's three tools.
func MessagingTools(bus *MessageBus, selfID string) []core.Tool {
	return []core.Tool{
		NewSendMessageTool(bus, selfID),
		NewWaitForMessageTool(bus, selfID),
		NewListPeersTool(bus, selfID),
	}
}

// AllTools returns the subset of orchestration tools that don't need the
// DelegateRunner / MCPResolver / RoleMap plumbing — i.e. messaging + the
// stateless Synthesize tool. Delegate* and CollectResults tools require
// per-orchestrator wiring; compose them individually at the call site.
func AllTools(bus *MessageBus, selfID string) []core.Tool {
	return append(
		MessagingTools(bus, selfID),
		NewSynthesizeTool(),
	)
}

// formatPeersHelper is a convenience used by tool-result formatters that
// want to inline the peer list into another tool's output. Kept for future
// use; suppresses unused-import churn in tests.
func formatPeersHelper(peers []string) string {
	if len(peers) == 0 {
		return "(no peers registered)"
	}
	out := ""
	for i, p := range peers {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

var _ = formatPeersHelper

// errSelfSend is returned when an agent tries to message itself. We allow
// it at the bus level (some patterns use self-messaging for state) but
// surface it in the tool result so the model knows.
func errSelfSend(self string) error {
	return fmt.Errorf("send_message: sender %q == recipient; self-messaging allowed but suspicious", self)
}

var _ = errSelfSend
