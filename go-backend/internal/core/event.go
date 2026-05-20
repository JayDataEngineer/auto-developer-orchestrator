package core

import "encoding/json"

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
	EventTypeStepStart       AgentEventType = "step_start"
	EventTypeStepEnd         AgentEventType = "step_end"
	EventTypeHookRequest     AgentEventType = "hook_request"
	EventTypeDecisionRequest AgentEventType = "decision_request" // unified HITL
	EventTypeUserQuestion    AgentEventType = "user_question"    // legacy, replaced by decision_request
	EventTypeSource          AgentEventType = "source"            // citation/reference link
)

// AgentEvent is an event emitted by the agent loop.
type AgentEvent struct {
	Type AgentEventType  `json:"type"`
	Data AgentEventData  `json:"data"`
	Raw  json.RawMessage `json:"-"`
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
	Cache             float64        `json:"cache,omitempty"`
	Model             string         `json:"model,omitempty"`
	ContextWindow     int            `json:"contextWindow,omitempty"`
	Streaming         bool           `json:"streaming,omitempty"`
	CompactedMessages int            `json:"compactedMessages,omitempty"`
	KeptMessages      int            `json:"keptMessages,omitempty"`

	// Context management metrics
	ContextTokens    int     `json:"contextTokens,omitempty"`
	ContextSize      int     `json:"contextSize,omitempty"`
	ContextUtil      float64 `json:"contextUtil,omitempty"`
	CompactionType   string  `json:"compactionType,omitempty"` // "micro" or "full"

	// Step-level context
	Round    int    `json:"round,omitempty"`    // current tool round
	Decision string `json:"decision,omitempty"` // step end decision: "respond", "delegate", "ask", "error"

	// Sub-agent context — set when events come from delegated sub-agents.
	AgentName    string `json:"agentName,omitempty"`    // e.g. "sarah", "jake"
	ParentToolID string `json:"parentToolId,omitempty"` // ID of the delegate_to call
	Task         string `json:"task,omitempty"`         // task description
	Status       string `json:"status,omitempty"`       // e.g. "running", "completed", "error"
	TranscriptID string `json:"transcriptId,omitempty"` // composite DB agent_id for transcript retrieval

	// Hook interception — set for hook_request events.
	HookPoint string `json:"hookPoint,omitempty"` // "tool_call", "tool_result", "context"
	HookID    string `json:"hookId,omitempty"`    // unique ID to match request → response

	// Rich tool result fields — set on tool_execution_end events
	Artifact     any    `json:"artifact,omitempty"`     // structured data for rich rendering (diffs, HTML, etc.)
	ModelContent string `json:"modelContent,omitempty"` // content fed back to the model, separate from display result

	// Source/citation fields — set on source events
	SourceType string `json:"sourceType,omitempty"` // "url" or "document"
	SourceURL  string `json:"sourceUrl,omitempty"`  // URL for "url" sources
	SourceID   string `json:"sourceId,omitempty"`   // unique ID for the source
}

// SubscriberKey is a context key for injecting the SSE subscriber channel.
type SubscriberKey struct{}

// TokenUsageKey is a context key for injecting real API token usage data
// from the agent loop into the context manager for accurate token estimation.
type TokenUsageKey struct{}

// TokenUsage holds real token counts from the last API call, injected via
// context so the context manager can use real usage instead of char heuristics.
type TokenUsage struct {
	PromptTokens     int // tokens in the prompt (from API response)
	CompletionTokens int // tokens in the completion
	ContextSize      int // model's context window size
	TrailingMessages int // number of messages appended since last API call
}

// SendEvent sends an event to the subscriber channel without blocking.
func SendEvent(ch chan<- AgentEvent, evt AgentEvent) {
	defer func() { recover() }()
	select {
	case ch <- evt:
	default:
	}
}

// ApprovalResponse is the user's response to an approval/question request.
type ApprovalResponse struct {
	Action  string
	Message string
}
