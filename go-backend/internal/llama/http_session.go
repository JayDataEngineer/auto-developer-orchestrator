package llama

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Session manages an inference session via the HTTP engine.
// It tracks conversation messages and maintains a session_id for KV cache
// persistence on the llama-server side.
//
// Each agent gets its own Session with a unique session_id.
// llama-server maps session_id → slot, keeping the KV cache warm between calls.
type Session struct {
	engine    *HTTPEngine
	sessionID string
	ctxSize   int

	messages []Message
	tools    []OpenAITool // tool definitions for this session

	mu     sync.Mutex
	closed bool

	// Token count tracking
	totalInputTokens  int
	totalOutputTokens int
}

// ── New chat completions API methods ───────────────────────────────

// ChatWithTools sends a system + user message with tool definitions.
// This is the primary entry point for the agent loop.
func (s *Session) ChatWithTools(system string, userMsg string, tools []OpenAITool, opts GenerateOptions) (<-chan ChatEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	s.messages = []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userMsg},
	}
	s.tools = tools

	return s.generateChatStream(opts), nil
}

// FeedToolResults appends tool result messages and continues generation.
// The assistant message is already stored by generateChatStream, so we only add tool results.
func (s *Session) FeedToolResults(assistantMsg Message, toolResults []ToolResult, goalNudge string, opts GenerateOptions) (<-chan ChatEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	// Tool result messages (one per tool call)
	for _, tr := range toolResults {
		s.messages = append(s.messages, Message{
			Role:       "tool",
			Content:    tr.Content,
			ToolCallID: tr.ToolCallID,
			Name:       tr.ToolName,
		})
	}

	// Inject goal reminder as a user message if provided
	if goalNudge != "" {
		s.messages = append(s.messages, Message{
			Role:    "user",
			Content: goalNudge,
		})
	}

	return s.generateChatStream(opts), nil
}

// FeedUserMessage appends a user message and continues generation.
func (s *Session) FeedUserMessage(userMsg string, opts GenerateOptions) (<-chan ChatEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	s.messages = append(s.messages, Message{Role: "user", Content: userMsg})
	return s.generateChatStream(opts), nil
}

// generateChatStream runs the model via /v1/chat/completions and returns a channel of ChatEvents.
func (s *Session) generateChatStream(opts GenerateOptions) <-chan ChatEvent {
	ch := make(chan ChatEvent, 256)

	go func() {
		defer close(ch)

		if opts.MaxTokens == 0 {
			opts.MaxTokens = cfg.MaxTokens
		}

		t0 := time.Now()

		req := ChatCompletionRequest{
			Messages:      s.messages,
			Tools:         s.tools,
			MaxTokens:     opts.MaxTokens,
			Temperature:   opts.Temperature,
			TopP:          opts.TopP,
			TopK:          opts.TopK,
			RepeatPenalty: 1.1,
			CachePrompt:   true,
			SessionID:     s.sessionID,
			Stream:        true,
		}

		// Accumulate the full assistant response (content + tool calls)
		toolCallAccum := make(map[int]*ToolCallResponse)
		var contentBuf strings.Builder
		tokenCount := 0

		err := s.engine.chatCompleteStream(req, func(delta StreamDelta, finish FinishReason) bool {
			// Content delta
			if delta.Content != "" {
				tokenCount++
				contentBuf.WriteString(delta.Content)
				ch <- ChatEvent{Type: ChatEventContent, Content: delta.Content}
			}
			// Thinking/reasoning delta
			if delta.ReasoningContent != "" {
				ch <- ChatEvent{Type: ChatEventThinking, Content: delta.ReasoningContent}
			}
			// Tool call chunks — accumulate across streaming
			for _, tc := range delta.ToolCalls {
				if existing, ok := toolCallAccum[tc.Index]; ok {
					// Append to existing accumulation
					if tc.Function.Name != "" {
						existing.Function.Name += tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						existing.Function.Arguments += tc.Function.Arguments
					}
					if tc.ID != "" {
						existing.ID = tc.ID
					}
					if tc.Type != "" {
						existing.Type = tc.Type
					}
				} else {
					// New tool call
					toolCallAccum[tc.Index] = &ToolCallResponse{
						ID:   tc.ID,
						Type: tc.Type,
						Function: FunctionCallData{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					}
				}
			}
			// On finish, store assistant message on session and send done event
			if finish != "" {
				// Collect accumulated tool calls in index order
				var toolCalls []ToolCallResponse
				indices := make([]int, 0, len(toolCallAccum))
				for idx := range toolCallAccum {
					indices = append(indices, idx)
				}
				sort.Ints(indices)
				for _, idx := range indices {
					toolCalls = append(toolCalls, *toolCallAccum[idx])
				}

				// Store assistant message in conversation history
				assistantMsg := Message{
					Role:      "assistant",
					Content:   contentBuf.String(),
					ToolCalls: toolCalls,
				}
				s.messages = append(s.messages, assistantMsg)

				ch <- ChatEvent{
					Type:    ChatEventDone,
					Finish:  finish,
					Content: serializeToolCalls(toolCalls),
				}
				return false
			}
			return true
		})

		elapsed := time.Since(t0)
		s.totalOutputTokens += tokenCount

		s.engine.logger.Debug("Chat generation complete",
			zap.Int("tokens", tokenCount),
			zap.Duration("duration", elapsed),
			zap.Float64("tok_per_sec", tokPerSec(tokenCount, elapsed)),
		)

		if err != nil {
			ch <- ChatEvent{Type: ChatEventError, Err: err}
		}
	}()

	return ch
}

// serializeToolCalls serializes accumulated tool calls as JSON for the done event content.
func serializeToolCalls(calls []ToolCallResponse) string {
	if len(calls) == 0 {
		return ""
	}
	b, _ := json.Marshal(calls)
	return string(b)
}

// tokPerSec safely calculates tokens per second.
func tokPerSec(tokens int, d time.Duration) float64 {
	if d.Seconds() == 0 {
		return 0
	}
	return float64(tokens) / d.Seconds()
}

// ── New message-based accessors ───────────────────────────────────

// Messages returns the conversation messages.
func (s *Session) Messages() []Message {
	return s.messages
}

// MessagesLen returns the number of messages.
func (s *Session) MessagesLen() int {
	return len(s.messages)
}

// TrackAssistantMessage appends an assistant message to the messages history.
func (s *Session) TrackAssistantMessage(content string, toolCalls []ToolCallResponse) {
	s.messages = append(s.messages, Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	})
}

// SetMessages replaces the message history (used after compaction).
func (s *Session) SetMessages(messages []Message) {
	s.messages = messages
}

// SetTools updates the tool definitions for this session.
func (s *Session) SetTools(tools []OpenAITool) {
	s.tools = tools
}

// ── Shared methods ─────────────────────────────────────────────────

// TokenCounts returns total input and output token counts.
func (s *Session) TokenCounts() (input, output int) {
	return s.totalInputTokens, s.totalOutputTokens
}

// Close releases the session slot on llama-server.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	_ = s.freeSlot()

	s.engine.logger.Debug("Session closed, KV cache freed",
		zap.String("sessionId", s.sessionID),
	)
	return nil
}

// freeSlot sends a minimal request to free the slot's KV cache.
func (s *Session) freeSlot() error {
	req := ChatCompletionRequest{
		Messages:    []Message{{Role: "user", Content: ""}},
		MaxTokens:   0,
		CachePrompt: false,
		SessionID:   s.sessionID,
	}
	_, err := s.engine.chatComplete(req)
	return err
}
