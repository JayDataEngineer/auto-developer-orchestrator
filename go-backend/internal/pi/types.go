package pi

import (
	"encoding/json"
	"fmt"
)

// AgentEvent represents a streaming event from the agent.
// Converted from llama.AgentEvent via event_convert.go, then mapped to SSE in pi_sse.go.
type AgentEvent struct {
	Type string `json:"type"`
	// Nested event for text/thinking deltas (from message_update)
	AssistantMessageEvent *AssistantMessageEvent `json:"assistantMessageEvent,omitempty"`
	Message               json.RawMessage        `json:"message,omitempty"`
	Messages              json.RawMessage        `json:"messages,omitempty"`
	ToolResults           json.RawMessage        `json:"toolResults,omitempty"`
	// Tool fields at top level (from event_convert.go)
	ToolName string                 `json:"toolName,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolId   string                 `json:"toolId,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	// Data payload for state/error events
	Data AgentEventData `json:"data,omitempty"`
}

// AssistantMessageEvent is the nested event inside message_update.
type AssistantMessageEvent struct {
	Type         string          `json:"type"`
	ContentIndex int             `json:"contentIndex"`
	Delta        string          `json:"delta,omitempty"`
	Content      string          `json:"content,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

// AgentEventData holds the payload of an agent event.
type AgentEventData struct {
	Text string `json:"text,omitempty"`

	// Tool execution events
	ToolName string                 `json:"toolName,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolId   string                 `json:"toolId,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`

	// Session state
	Model   string      `json:"model,omitempty"`
	Input   interface{} `json:"input,omitempty"`
	Output  float64     `json:"output,omitempty"`
	Cache   float64     `json:"cache,omitempty"`
	Streaming bool      `json:"streaming,omitempty"`

	// Compaction
	CompactedMessages int `json:"compactedMessages,omitempty"`
	KeptMessages      int `json:"keptMessages,omitempty"`
}

// Event types sent to the frontend via SSE
const (
	EventTextDelta       = "text_delta"
	EventThinkingDelta   = "thinking_delta"
	EventToolStart       = "tool_execution_start"
	EventToolEnd         = "tool_execution_end"
	EventAgentStart      = "agent_start"
	EventAgentEnd        = "agent_end"
	EventAgentSpawned    = "agent_spawned"
	EventCompactionStart = "compaction_start"
	EventCompactionEnd   = "compaction_end"
	EventError           = "error"
	EventStateUpdate     = "state_update"
	EventCommitCreated   = "commit_created"
	EventPushComplete    = "push_complete"
	EventPRCreated       = "pr_created"

	// Human-in-the-loop events
	EventApprovalRequest = "approval_request"
)

// Event type constants used by event_convert.go and pi_sse.go
const (
	RpcEventToolStart       = "tool_execution_start"
	RpcEventToolEnd         = "tool_execution_end"
	RpcEventAgentStart      = "agent_start"
	RpcEventAgentEnd        = "agent_end"
	RpcEventCompactionStart = "compaction_start"
	RpcEventCompactionEnd   = "compaction_end"
	RpcEventError           = "error"
	RpcEventResponse        = "response"

	// Orchestrator events
	EventArtifactCreated = "artifact_created"
	EventArtifactUpdated = "artifact_updated"
	EventPlanCreated     = "plan_created"
	EventPlanUpdated     = "plan_updated"
	EventSubAgentStart   = "subagent_start"
	EventSubAgentEnd     = "subagent_end"
)

// ApprovalRequestData is sent to the frontend when the agent needs approval.
type ApprovalRequestData struct {
	RequestID string                 `json:"requestId"`
	Type      string                 `json:"type"` // "tool_confirm", "plan", "question"
	ToolName  string                 `json:"toolName,omitempty"`
	ToolArgs  map[string]interface{} `json:"toolArgs,omitempty"`
	Message   string                 `json:"message"`
	Risk      string                 `json:"risk"` // "low", "medium", "high"
}

// ApprovalResponse is sent from the frontend when the user responds to an approval.
type ApprovalResponse struct {
	Action  string `json:"action"` // "approve", "deny", "answer"
	Message string `json:"message,omitempty"`
}

// ToFloat64 converts an interface{} to float64.
func ToFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

// ModelConfigProvider resolves the provider for a given model ID.
var ModelConfigProvider func(modelId string) string = func(_ string) string { return "llamacpp" }
