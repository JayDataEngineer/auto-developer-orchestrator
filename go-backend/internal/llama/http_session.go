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
	engine    ChatProvider
	sessionID string
	ctxSize   int

	messages []Message
	tools    []OpenAITool // tool definitions for this session

	mu     sync.Mutex
	closed bool

	// Per-session thinking budget override (0 = use global default)
	thinkingBudget int

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

// GenerateStream runs the model using the session's current messages and tools.
// Exported for use by the adapter when rebuilding after compaction.
func (s *Session) GenerateStream(opts GenerateOptions) <-chan ChatEvent {
	return s.generateChatStream(opts)
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

		// Pre-flight: deduplicate tool_call_ids in the message list.
		// DeepSeek sometimes emits duplicate IDs which cause API 400 errors.
		sanitizeMessages(&req.Messages)

		// Accumulate the full assistant response (content + tool calls + reasoning)
		toolCallAccum := make(map[int]*ToolCallResponse)
		var contentBuf strings.Builder
		var reasoningBuf strings.Builder
		tokenCount := 0
		thinkingTokens := 0
		thinkingBudget := s.thinkingBudget
		if thinkingBudget == 0 {
			thinkingBudget = cfg.ThinkingBudgetTokens
		}
		var lastUsage *StreamUsage // track usage across chunks

		var pendingFinish FinishReason // finish reason waiting for usage chunk
		var pendingToolCalls []ToolCallResponse
		var pendingContent string
		var pendingReasoning string

		err := s.engine.chatCompleteStream(req, func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool {
			// Capture usage from any chunk that has it
			if usage != nil {
				lastUsage = usage
				s.totalInputTokens = usage.PromptTokens
				s.totalOutputTokens += usage.CompletionTokens
			}
			// Content delta
			if delta.Content != "" {
				tokenCount++
				contentBuf.WriteString(delta.Content)
				ch <- ChatEvent{Type: ChatEventContent, Content: delta.Content}
			}
			// Thinking/reasoning delta — enforce budget
			reasoningDelta := delta.ReasoningContent
			if reasoningDelta == "" {
				reasoningDelta = delta.Reasoning
			}
			if reasoningDelta != "" {
				reasoningBuf.WriteString(reasoningDelta)
				thinkingTokens++
				if thinkingBudget > 0 && thinkingTokens > thinkingBudget {
					if thinkingTokens == thinkingBudget+1 {
						ch <- ChatEvent{Type: ChatEventThinking, Content: "\n[Thinking budget reached — committing to implementation]"}
					}
				} else {
					ch <- ChatEvent{Type: ChatEventThinking, Content: reasoningDelta}
				}
			}
			// Tool call chunks — accumulate across streaming
			for _, tc := range delta.ToolCalls {
				// Count tool call argument chunks as output tokens
				if tc.Function.Name != "" || tc.Function.Arguments != "" {
					tokenCount++
				}
				if existing, ok := toolCallAccum[tc.Index]; ok {
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
			// On finish, save state and check if usage is already available
			if finish != "" {
				pendingFinish = finish
				// Collect accumulated tool calls in index order, deduplicating by ID
				indices := make([]int, 0, len(toolCallAccum))
				for idx := range toolCallAccum {
					indices = append(indices, idx)
				}
				sort.Ints(indices)
				seenIDs := make(map[string]bool)
				for _, idx := range indices {
					tc := toolCallAccum[idx]
					if tc.ID != "" && seenIDs[tc.ID] {
						continue // skip duplicate tool call ID
					}
					if tc.ID != "" {
						seenIDs[tc.ID] = true
					}
					pendingToolCalls = append(pendingToolCalls, *tc)
				}
				pendingContent = contentBuf.String()
				pendingReasoning = reasoningBuf.String()

				// Reasoning models — promote reasoning to content
				if pendingContent == "" && pendingReasoning != "" && len(pendingToolCalls) == 0 {
					pendingContent = pendingReasoning
					pendingReasoning = ""
					ch <- ChatEvent{Type: ChatEventContent, Content: pendingContent}
				}

				// Store assistant message in conversation history
				s.messages = append(s.messages, Message{
					Role:             "assistant",
					Content:          pendingContent,
					ToolCalls:        pendingToolCalls,
					ReasoningContent: pendingReasoning,
				})
			}
			// Send done event when we have finish AND usage (or usage arrived with finish)
			if pendingFinish != "" && lastUsage != nil {
				ch <- ChatEvent{
					Type:    ChatEventDone,
					Finish:  pendingFinish,
					Content: serializeToolCalls(pendingToolCalls),
					Usage:   lastUsage,
				}
				pendingFinish = "" // prevent double send
			}
			return true
		})

		elapsed := time.Since(t0)
		// Only accumulate manual count if API didn't provide usage
		if s.totalOutputTokens == 0 && tokenCount > 0 {
			s.totalOutputTokens += tokenCount
		}

		// If finish arrived but usage never came (local llama-server doesn't
		// include usage in SSE streams), estimate prompt tokens and build usage.
		if pendingFinish != "" {
			if lastUsage == nil {
				// Always re-estimate from current messages for accurate per-turn counts
				promptEstimate := s.estimateTokens()
				outputTokens := tokenCount
				if outputTokens == 0 {
					outputTokens = s.totalOutputTokens
				}
				s.totalInputTokens = promptEstimate
				s.totalOutputTokens += outputTokens
				lastUsage = &StreamUsage{
					PromptTokens:     promptEstimate,
					CompletionTokens: outputTokens,
				}
			}
			ch <- ChatEvent{
				Type:    ChatEventDone,
				Finish:  pendingFinish,
				Content: serializeToolCalls(pendingToolCalls),
				Usage:   lastUsage,
			}
		}

		zap.L().Debug("Chat generation complete",
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

// sanitizeMessages ensures no duplicate tool_call_ids exist across the message list.
// DeepSeek emits duplicate IDs which cause API 400 errors.
func sanitizeMessages(msgs *[]Message) {
	// First pass: collect all tool_call_ids from assistant messages
	seenIDs := make(map[string]bool)
	for i := range *msgs {
		m := &(*msgs)[i]
		if len(m.ToolCalls) == 0 {
			continue
		}
		// Dedup tool calls by ID within this assistant message
		deduped := make([]ToolCallResponse, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if tc.ID != "" && seenIDs[tc.ID] {
				continue
			}
			if tc.ID != "" {
				seenIDs[tc.ID] = true
			}
			deduped = append(deduped, tc)
		}
		m.ToolCalls = deduped
	}

	// Second pass: remove tool result messages whose ID was deduped away
	validIDs := make(map[string]bool)
	for _, m := range *msgs {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				validIDs[tc.ID] = true
			}
		}
	}
	cleaned := make([]Message, 0, len(*msgs))
	for _, m := range *msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			if !validIDs[m.ToolCallID] {
				continue // drop orphaned tool result
			}
		}
		cleaned = append(cleaned, m)
	}
	*msgs = cleaned
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

// SetThinkingBudget sets a per-session thinking budget override.
func (s *Session) SetThinkingBudget(tokens int) {
	s.thinkingBudget = tokens
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

// estimateTokens roughly estimates token count from all message content + tool definitions.
// Used as fallback when the streaming API doesn't return usage data.
func (s *Session) estimateTokens() int {
	chars := 0
	for _, m := range s.messages {
		chars += len(m.Content)
		for _, tc := range m.ToolCalls {
			chars += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	// Include tool definition overhead (name + description + parameter schema)
	for _, t := range s.tools {
		chars += len(t.Function.Name) + len(t.Function.Description) + len(t.Function.Parameters)
	}
	// Rough estimate: 1 token ≈ 3.3 characters (~4 chars/token + 20% overhead)
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

	zap.L().Debug("Session closed, KV cache freed",
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
