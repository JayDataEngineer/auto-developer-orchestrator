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
			Messages:        s.messages,
			Tools:           s.tools,
			MaxTokens:       opts.MaxTokens,
			Temperature:     opts.Temperature,
			TopP:            opts.TopP,
			TopK:            opts.TopK,
			RepeatPenalty:   cfg.RepeatPenalty,
			PresencePenalty: cfg.PresencePenalty,
			MinP:            cfg.MinP,
			CachePrompt:     !s.engine.IsCloud(), // only local llama-server supports KV caching
			SessionID:       s.sessionID,
			Stream:          true,
		}

		// Cloud providers require the model field
		if s.engine.IsCloud() {
			req.Model = s.engine.ModelName()
			req.SessionID = "" // no session slot concept for cloud
		}

		// Accumulate the full assistant response (content + tool calls)
		toolCallAccum := make(map[int]*ToolCallResponse)
		var contentBuf strings.Builder
		tokenCount := 0
		thinkingTokens := 0
		thinkingBudget := cfg.ThinkingBudgetTokens

		err := s.engine.chatCompleteStream(req, func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool {
			// Content delta
			if delta.Content != "" {
				tokenCount++
				contentBuf.WriteString(delta.Content)
				ch <- ChatEvent{Type: ChatEventContent, Content: delta.Content}
			}
			// Thinking/reasoning delta — enforce budget
			if delta.ReasoningContent != "" {
				thinkingTokens++
				if thinkingBudget > 0 && thinkingTokens > thinkingBudget {
					// Budget exceeded — stop forwarding thinking but let generation continue.
					// The model will naturally transition to content generation.
					// Emit a single truncation notice so the frontend knows thinking was capped.
					if thinkingTokens == thinkingBudget+1 {
						ch <- ChatEvent{Type: ChatEventThinking, Content: "\n[Thinking budget reached — committing to implementation]"}
					}
				} else {
					ch <- ChatEvent{Type: ChatEventThinking, Content: delta.ReasoningContent}
				}
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
				// Store token usage from API response
				if usage != nil {
					s.totalInputTokens = usage.PromptTokens // absolute, not accumulated
					s.totalOutputTokens += usage.CompletionTokens
				}

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
		// Only accumulate manual count if API didn't provide usage
		if s.totalOutputTokens == 0 && tokenCount > 0 {
			s.totalOutputTokens += tokenCount
		}

		s.engine.logger.Debug("Chat generation complete",
			zap.Int("outputTokens", tokenCount),
			zap.Int("promptTokens", s.totalInputTokens),
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

// SetMessages replaces the message history (used after compaction).
func (s *Session) SetMessages(messages []Message) {
	s.messages = messages
}

// SetTools updates the tool definitions for this session.
func (s *Session) SetTools(tools []OpenAITool) {
	s.tools = tools
}

// GetTools returns the current tool definitions for this session.
func (s *Session) GetTools() []OpenAITool {
	return s.tools
}

// ── Shared methods ─────────────────────────────────────────────────

// TokenCounts returns total input and output token counts.
func (s *Session) TokenCounts() (input, output int) {
	return s.totalInputTokens, s.totalOutputTokens
}

// ContextUsage returns the current prompt token count and context capacity.
// Used by the compaction system to decide when to compact.
// Falls back to estimating from message content when the API doesn't provide usage data.
func (s *Session) ContextUsage() (usedTokens int, capacity int) {
	if s.totalInputTokens > 0 {
		return s.totalInputTokens, s.ctxSize
	}
	// API didn't return usage — estimate from message content (~4 chars/token)
	return s.estimateTokens(), s.ctxSize
}

// estimateTokens roughly estimates token count from all message content.
// Used as fallback when the streaming API doesn't return usage data.
func (s *Session) estimateTokens() int {
	chars := 0
	for _, m := range s.messages {
		chars += len(m.Content)
		for _, tc := range m.ToolCalls {
			chars += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	// Rough estimate: 1 token ≈ 4 characters for English text
	// Add 20% overhead for formatting, roles, tool definitions
	return int(float64(chars) * 0.3)
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
