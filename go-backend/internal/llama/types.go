package llama

import "encoding/json"

// ── Types for /v1/chat/completions native tool calling ──────────────

// Message represents a single message in the OpenAI chat completions format.
// Replaces Turn for all new code.
type Message struct {
	Role       string             `json:"role"`                    // "system", "user", "assistant", "tool"
	Content    string             `json:"content"`                 // text content (empty for tool_calls-only)
	ToolCalls  []ToolCallResponse `json:"tool_calls,omitempty"`    // assistant messages with tool calls
	ToolCallID string             `json:"tool_call_id,omitempty"`  // tool result messages
	Name       string             `json:"name,omitempty"`          // tool name for tool messages
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
	Content string        // text or thinking content
	Delta   *ToolCallDelta // tool call chunk (for ChatEventToolChunk)
	Finish  FinishReason   // set on ChatEventDone
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
