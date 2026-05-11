package core

import (
	"context"
	"encoding/json"
)

// MessageRole identifies the role in a chat message.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// Message is a chat message in OpenAI format.
type Message struct {
	Role             string             `json:"role"`
	Content          string             `json:"content"`
	ToolCalls        []ToolCallResponse `json:"tool_calls,omitempty"`
	ToolCallID       string             `json:"tool_call_id,omitempty"`
	Name             string             `json:"name,omitempty"`
	ReasoningContent string             `json:"reasoning_content,omitempty"`
}

// ToolCallResponse is a structured tool call from the model.
type ToolCallResponse struct {
	ID               string           `json:"id"`
	Type             string           `json:"type"`
	Function         FunctionCallData `json:"function"`
	ThoughtSignature string           `json:"thought_signature,omitempty"` // Gemini 3 requirement — must echo back
}

// FunctionCallData holds the function name and arguments.
type FunctionCallData struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToToolCall converts a ToolCallResponse to a parsed ToolCall.
func (tcr *ToolCallResponse) ToToolCall() ToolCall {
	var args map[string]any
	if err := json.Unmarshal([]byte(tcr.Function.Arguments), &args); err != nil {
		args = map[string]any{"raw": tcr.Function.Arguments}
	}
	return ToolCall{
		ID:   tcr.ID,
		Name: tcr.Function.Name,
		Args: args,
	}
}

// ToolCall represents a parsed tool call.
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	Raw  string         `json:"raw"`
}

// ToolResult holds a tool execution result for feeding back to the model.
type ToolResult struct {
	ToolCallID string
	ToolName   string
	Content    string
}

// OpenAITool is the tool definition format for /v1/chat/completions.
type OpenAITool struct {
	Type     string       `json:"type"`
	Function FunctionDef  `json:"function"`
}

// FunctionDef is the function definition in an OpenAI tool.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// StreamDelta is a streaming chunk from chat completions SSE.
type StreamDelta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"`
	ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta is a partial tool call in a streaming chunk.
type ToolCallDelta struct {
	Index       int               `json:"index"`
	ID          string            `json:"id,omitempty"`
	Type        string            `json:"type,omitempty"`
	Function    FunctionCallDelta `json:"function"`
	ExtraContent *ExtraContent    `json:"extra_content,omitempty"`
}

// ExtraContent captures provider-specific fields in streaming tool call chunks.
// Gemini 3 returns thought_signature via extra_content.google.thought_signature.
type ExtraContent struct {
	Google *GoogleExtra `json:"google,omitempty"`
}

// GoogleExtra holds Google-specific extra fields in streaming tool call chunks.
type GoogleExtra struct {
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

// FunctionCallDelta holds partial function call data.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// StreamUsage holds token usage from streaming SSE chunks.
type StreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// FinishReason indicates why the model stopped.
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishToolCalls FinishReason = "tool_calls"
)

// GenerateOptions controls generation parameters.
type GenerateOptions struct {
	MaxTokens   int
	Temperature float32
	TopP        float32
	TopK        int
}

// LLMProvider abstracts an LLM backend (llama-server, Gemini, OpenRouter, etc.).
type LLMProvider interface {
	// StreamChat streams a chat completion with tool definitions.
	// Returns a channel of chat events. The channel is closed when streaming ends.
	StreamChat(ctx context.Context, messages []Message, tools []OpenAITool, opts GenerateOptions) (<-chan ChatEvent, error)

	// ModelName returns the current model identifier.
	ModelName() string

	// ContextSize returns the maximum context size for this provider.
	ContextSize() int
}
