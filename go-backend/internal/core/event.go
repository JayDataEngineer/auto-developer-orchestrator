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

// AgentEvent is an event emitted by the agent loop to SSE subscribers.
// Data is a typed EventPayload — each event type has its own struct.
type AgentEvent struct {
	Type AgentEventType `json:"type"`
	Data EventPayload   `json:"data"`
	Raw  json.RawMessage `json:"-"`
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

// DataAsText returns the text content from a TextDelta or ThinkingDelta payload.
// Returns empty string for other payload types.
func DataAsText(d EventPayload) string {
	switch p := d.(type) {
	case TextDelta:
		return p.Text
	case ThinkingDelta:
		return p.Text
	default:
		return ""
	}
}
