package llama

import "encoding/json"

// AgentEventType identifies the type of agent event.
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
)

// AgentEvent is an event emitted by the agent loop.
type AgentEvent struct {
	Type AgentEventType  `json:"type"`
	Data AgentEventData  `json:"data"`
	Raw  json.RawMessage `json:"-"`
}

// AgentEventData holds the payload of an agent event.
type AgentEventData struct {
	Text              string                 `json:"text,omitempty"`
	ToolName          string                 `json:"toolName,omitempty"`
	ToolArgs          map[string]interface{} `json:"args,omitempty"`
	ToolID            string                 `json:"toolId,omitempty"`
	Result            interface{}            `json:"result,omitempty"`
	Error             string                 `json:"error,omitempty"`
	Input             float64                `json:"input,omitempty"`
	Output            float64                `json:"output,omitempty"`
	Cache             float64                `json:"cache,omitempty"`
	Model             string                 `json:"model,omitempty"`
	Streaming         bool                   `json:"streaming,omitempty"`
	CompactedMessages int                    `json:"compactedMessages,omitempty"`
	KeptMessages      int                    `json:"keptMessages,omitempty"`
}

// ApprovalResponse is the user's response to an approval/question request.
type ApprovalResponse struct {
	Action  string
	Message string
}
