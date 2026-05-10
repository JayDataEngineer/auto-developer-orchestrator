package llama

import "encoding/json"

// ── Types for /v1/chat/completions native tool calling ──────────────

// Message represents a single message in the OpenAI chat completions format.
// Replaces Turn for all new code.
type Message struct {
	Role             string             `json:"role"`                              // "system", "user", "assistant", "tool"
	Content          string             `json:"content"`                           // text content (empty for tool_calls-only)
	ToolCalls        []ToolCallResponse `json:"tool_calls,omitempty"`              // assistant messages with tool calls
	ToolCallID       string             `json:"tool_call_id,omitempty"`            // tool result messages
	Name             string             `json:"name,omitempty"`                    // tool name for tool messages
	ReasoningContent string             `json:"reasoning_content,omitempty"`       // DeepSeek V4 requires this in multi-turn messages
}

// ToolCallResponse is a structured tool call returned by the server.
type ToolCallResponse struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // always "function"
	Function FunctionCallData `json:"function"`
}

// FunctionCallData holds the function name and arguments.
type FunctionCallData struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // valid JSON string
}

// ToToolCall converts a ToolCallResponse to a ToolCall for the executor.
func (tcr *ToolCallResponse) ToToolCall() ToolCall {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tcr.Function.Arguments), &args); err != nil {
		args = map[string]interface{}{"raw": tcr.Function.Arguments}
	}
	return ToolCall{
		ID:   tcr.ID,
		Name: tcr.Function.Name,
		Args: args,
	}
}

// StreamDelta is a streaming chunk from chat completions SSE.
type StreamDelta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"`        // DeepSeek V4 uses "reasoning" (not "reasoning_content") in streaming deltas
	ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta is a partial tool call in a streaming chunk.
type ToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function FunctionCallDelta `json:"function,omitempty"`
}

// FunctionCallDelta holds partial function call data in streaming.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// StreamUsage holds token usage data returned in streaming SSE chunks.
// llama-server includes this on the final chunk (with finish_reason set).
type StreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// FinishReason indicates why the model stopped generating.
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishToolCalls FinishReason = "tool_calls"
)

// ChatEventType identifies the type of a ChatEvent.
type ChatEventType int

const (
	ChatEventContent   ChatEventType = iota // text content delta
	ChatEventThinking                       // reasoning/thinking content
	ChatEventToolChunk                      // tool call fragment
	ChatEventDone                           // generation complete
	ChatEventError                          // error occurred
)

// ChatEvent represents a streaming event from chat completion.
// Replaces TokenEvent for the new streaming path.
type ChatEvent struct {
	Type    ChatEventType
	Content string         // text or thinking content
	Delta   *ToolCallDelta  // tool call chunk (for ChatEventToolChunk)
	Finish  FinishReason    // set on ChatEventDone
	Usage   *StreamUsage    // token usage (set on ChatEventDone)
	Err     error
}

// ToolResult holds a tool execution result for feeding back to the model.
type ToolResult struct {
	ToolCallID string
	ToolName   string
	Content    string
}

// ── Shared types ───────────────────────────────────────────────────

// ToolCall represents a parsed tool call. Used by the executor interface.
type ToolCall struct {
	ID   string                 `json:"id"`
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
	Raw  string                 `json:"raw"`
}

// GenerateOptions controls generation parameters.
type GenerateOptions struct {
	MaxTokens   int
	Temperature float32
	TopP        float32
	TopK        int
}

// ── Agent event types (SSE wire format) ──────────────────────────────

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
	ContextWindow     int                    `json:"contextWindow,omitempty"`
	Streaming         bool                   `json:"streaming,omitempty"`
	CompactedMessages int                    `json:"compactedMessages,omitempty"`
	KeptMessages      int                    `json:"keptMessages,omitempty"`
}

// ApprovalResponse is the user's response to an approval/question request.
type ApprovalResponse struct {
	Action  string
	Message string
}
