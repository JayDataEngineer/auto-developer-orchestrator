package core

// ChatEventType identifies the type of a ChatEvent from the LLM stream.
type ChatEventType int

const (
	ChatEventContent   ChatEventType = iota // text content delta
	ChatEventThinking                       // reasoning/thinking content
	ChatEventToolChunk                      // tool call fragment
	ChatEventDone                           // generation complete
	ChatEventError                          // error occurred
)

// ChatEvent is a streaming event from chat completion.
type ChatEvent struct {
	Type    ChatEventType
	Content string
	Deltas  []ToolCallDelta // tool call chunks in this event
	Finish  FinishReason
	Usage   *StreamUsage // non-nil on final event with token counts
	Err     error
}

// AgentEventType identifies the type of an agent event emitted to subscribers.
type AgentEventType string

const (
	EventTypeTextDelta       AgentEventType = "text_delta"
	EventTypeThinkingDelta   AgentEventType = "thinking_delta"
	EventTypeToolStart       AgentEventType = "tool_execution_start"
	EventTypeToolEnd         AgentEventType = "tool_execution_end"
	EventTypeAgentStart      AgentEventType = "agent_start"
	EventTypeAgentEnd        AgentEventType = "agent_end"
	EventTypeError           AgentEventType = "error"
	EventTypeArtifactCreated AgentEventType = "artifact_created"
	EventTypeArtifactUpdated AgentEventType = "artifact_updated"
	EventTypePlanCreated     AgentEventType = "plan_created"
	EventTypePlanUpdated     AgentEventType = "plan_updated"
	EventTypeSubAgentStart   AgentEventType = "subagent_start"
	EventTypeSubAgentEnd     AgentEventType = "subagent_end"
	EventTypeApprovalRequest AgentEventType = "approval_request"
	EventTypeCompactionStart AgentEventType = "compaction_start"
	EventTypeCompactionEnd   AgentEventType = "compaction_end"
	EventTypeToolUpdate      AgentEventType = "tool_update"
	EventTypeAgentSpawned    AgentEventType = "agent_spawned"
	EventTypeStateUpdate     AgentEventType = "state_update"
	EventTypeGrindAttempt    AgentEventType = "grind_attempt"
	EventTypeGrindVerify     AgentEventType = "grind_verify"
	EventTypeGrindEnd        AgentEventType = "grind_end"
)

// AgentEvent is an event emitted by the agent loop.
type AgentEvent struct {
	Type AgentEventType `json:"type"`
	Data AgentEventData `json:"data"`
}

// AgentEventData holds the payload of an agent event.
type AgentEventData struct {
	Text              string         `json:"text,omitempty"`
	ToolName          string         `json:"toolName,omitempty"`
	ToolArgs          map[string]any `json:"args,omitempty"`
	ToolID            string         `json:"toolId,omitempty"`
	Result            any            `json:"result,omitempty"`
	Error             string         `json:"error,omitempty"`
	Input             float64        `json:"input,omitempty"`
	Output            float64        `json:"output,omitempty"`
	Model             string         `json:"model,omitempty"`
	ContextWindow     int            `json:"contextWindow,omitempty"`
	CompactedMessages int            `json:"compactedMessages,omitempty"`
	KeptMessages      int            `json:"keptMessages,omitempty"`

	// Sub-agent context — set when events come from delegated sub-agents.
	AgentName    string `json:"agentName,omitempty"`    // e.g. "sarah", "jake"
	ParentToolID string `json:"parentToolId,omitempty"` // ID of the delegate_to call
	Task         string `json:"task,omitempty"`         // task description
	Status       string `json:"status,omitempty"`       // e.g. "running", "completed", "error"
}

// SubscriberKey is a context key for injecting the SSE subscriber channel.
type SubscriberKey struct{}

// SendEvent sends an event to the subscriber channel without blocking.
func SendEvent(ch chan<- AgentEvent, evt AgentEvent) {
	defer func() { recover() }()
	select {
	case ch <- evt:
	default:
	}
}
