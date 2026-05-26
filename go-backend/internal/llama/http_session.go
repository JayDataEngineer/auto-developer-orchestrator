package llama

import (
	"context"
	"encoding/json"
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

// ── Chat completions API ───────────────────────────────

// GenerateStream runs the model using the session's current messages and tools.
// Exported for use by the adapter when rebuilding after compaction.
func (s *Session) GenerateStream(ctx context.Context, opts GenerateOptions) <-chan ChatEvent {
	return s.generateChatStream(ctx, opts)
}

// generateChatStream runs the model via /v1/chat/completions and returns a channel of ChatEvents.
func (s *Session) generateChatStream(ctx context.Context, opts GenerateOptions) <-chan ChatEvent {
	ch := make(chan ChatEvent, 256)

	go func() {
		defer close(ch)

		if opts.MaxTokens == 0 {
			opts.MaxTokens = cfg.MaxTokens
		}

		req := s.buildRequest(opts)
		sanitizeAPIMessages(&req.Messages)

		acc := newStreamAccumulator(s.thinkingBudget)

		t0 := time.Now()

		// Some proxy backends (e.g. MCP hub at 30080) don't support SSE streaming.
		// Fall back to non-streaming and convert the response into streaming events.
		if llm, ok := s.engine.(*LLMClient); ok && llm.StreamingDisabled() {
			req.Stream = false
			resp, err := s.engine.chatComplete(req)
			if err == nil && len(resp.Choices) > 0 {
				choice := resp.Choices[0]
				delta := streamDeltaFromResponse(choice)
				usage := &StreamUsage{
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
				}
				acc.processChunk(ch, delta, choice.FinishReason, usage, s)
			}
			if err != nil {
				ch <- ChatEvent{Type: ChatEventError, Err: err}
			}
		} else {
			err := s.engine.chatCompleteStream(ctx, req, func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool {
				return acc.processChunk(ch, delta, finish, usage, s)
			})
			if err != nil {
				ch <- ChatEvent{Type: ChatEventError, Err: err}
			}
		}

		elapsed := time.Since(t0)
		acc.finalize(ch, s)

		zap.L().Debug("Chat generation complete",
			zap.Int("outputTokens", acc.tokenCount),
			zap.Int("promptTokens", s.totalInputTokens),
			zap.Duration("duration", elapsed),
			zap.Float64("tok_per_sec", tokPerSec(acc.tokenCount, elapsed)),
		)
	}()

	return ch
}

// streamDeltaFromResponse converts a non-streaming ChatChoice into a StreamDelta
// so it can be fed through the same streaming accumulator pipeline.
func streamDeltaFromResponse(choice ChatChoice) StreamDelta {
	msg := choice.Message
	delta := StreamDelta{
		Content:          msg.Content,
		ReasoningContent: msg.ReasoningContent,
	}
	for i, tc := range msg.ToolCalls {
		delta.ToolCalls = append(delta.ToolCalls, ToolCallDelta{
			Index: i,
			ID:    tc.ID,
			Type:  tc.Type,
			Function: FunctionCallDelta{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return delta
}

// buildRequest constructs a ChatCompletionRequest from session state and options.
func (s *Session) buildRequest(opts GenerateOptions) ChatCompletionRequest {
	// Prepare messages: cache breakpoints, Gemini signatures (cloud only).
	// Must happen on []core.Message before conversion to API wire format.
	prepared := s.messages
	if llm, ok := s.engine.(*LLMClient); ok {
		prepared = llm.prepareMessages(prepared)
	}

	// Convert core.Messages to API wire format — messages with images
	// become multimodal content arrays, others stay as plain strings.
	apiMsgs := make([]APIChatMessage, len(prepared))
	for i, m := range prepared {
		apiMsgs[i] = toAPIMessage(m)
	}

	req := ChatCompletionRequest{
		Messages:        apiMsgs,
		Tools:           s.tools,
		MaxTokens:       opts.MaxTokens,
		Temperature:     opts.Temperature,
		TopP:            opts.TopP,
		TopK:            opts.TopK,
		RepeatPenalty:   cfg.RepeatPenalty,
		PresencePenalty: cfg.PresencePenalty,
		MinP:            cfg.MinP,
		CachePrompt:     !s.engine.IsCloud(),
		SessionID:       s.sessionID,
		Stream:          true,
		ResponseFormat:  opts.ResponseFormat,
	}
	if s.engine.IsCloud() {
		req.Model = s.engine.ModelName()
		req.SessionID = ""
	}
	return req
}

// streamAccumulator holds state for accumulating a streaming response.
type streamAccumulator struct {
	toolCallAccum  map[int]*ToolCallResponse
	contentBuf     strings.Builder
	reasoningBuf   strings.Builder
	tokenCount     int
	thinkingTokens int
	thinkingBudget int
	lastUsage      *StreamUsage
	pendingFinish  FinishReason
	pendingCalls   []ToolCallResponse
	finished       bool
}

func newStreamAccumulator(thinkingBudget int) *streamAccumulator {
	tb := thinkingBudget
	if tb == 0 {
		tb = cfg.ThinkingBudgetTokens
	}
	return &streamAccumulator{
		toolCallAccum:  make(map[int]*ToolCallResponse),
		thinkingBudget: tb,
	}
}

// processChunk handles a single streaming delta. Returns true to continue streaming.
func (a *streamAccumulator) processChunk(ch chan<- ChatEvent, delta StreamDelta, finish FinishReason, usage *StreamUsage, s *Session) bool {
	if usage != nil {
		a.lastUsage = usage
		s.totalInputTokens = usage.PromptTokens
		s.totalOutputTokens += usage.CompletionTokens
	}

	if delta.Content != "" {
		a.tokenCount++
		a.contentBuf.WriteString(delta.Content)
		ch <- ChatEvent{Type: ChatEventContent, Content: delta.Content}
	}

	a.processReasoning(ch, delta)

	// Forward tool call deltas so the agent loop can capture ExtraContent
	// (e.g. Gemini's thought_signature) before accumulating.
	if len(delta.ToolCalls) > 0 {
		ch <- ChatEvent{Type: ChatEventToolChunk, Deltas: delta.ToolCalls}
	}

	a.accumulateToolCalls(delta)

	if finish != "" {
		a.handleFinish(ch, s, finish)
	}

	// Emit done when both finish and usage are available
	if a.pendingFinish != "" && a.lastUsage != nil {
		a.emitDone(ch)
	}
	return true
}

// processReasoning handles thinking/reasoning deltas with budget enforcement.
func (a *streamAccumulator) processReasoning(ch chan<- ChatEvent, delta StreamDelta) {
	reasoningDelta := delta.ReasoningContent
	if reasoningDelta == "" {
		reasoningDelta = delta.Reasoning
	}
	if reasoningDelta == "" {
		return
	}
	a.reasoningBuf.WriteString(reasoningDelta)
	a.thinkingTokens++
	if a.thinkingBudget > 0 && a.thinkingTokens > a.thinkingBudget {
		if a.thinkingTokens == a.thinkingBudget+1 {
			ch <- ChatEvent{Type: ChatEventThinking, Content: "\n[Thinking budget reached — committing to implementation]"}
		}
	} else {
		ch <- ChatEvent{Type: ChatEventThinking, Content: reasoningDelta}
	}
}

// accumulateToolCalls merges incoming tool call chunks into the accumulator.
func (a *streamAccumulator) accumulateToolCalls(delta StreamDelta) {
	for _, tc := range delta.ToolCalls {
		if tc.Function.Name != "" || tc.Function.Arguments != "" {
			a.tokenCount++
		}
		if existing, ok := a.toolCallAccum[tc.Index]; ok {
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
			if tc.ExtraContent != nil && tc.ExtraContent.Google != nil && tc.ExtraContent.Google.ThoughtSignature != "" {
				existing.ThoughtSignature = tc.ExtraContent.Google.ThoughtSignature
			}
		} else {
			sig := ""
			if tc.ExtraContent != nil && tc.ExtraContent.Google != nil {
				sig = tc.ExtraContent.Google.ThoughtSignature
			}
			a.toolCallAccum[tc.Index] = &ToolCallResponse{
				ID:               tc.ID,
				Type:             tc.Type,
				ThoughtSignature: sig,
				Function: FunctionCallData{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}
}

// handleFinish collects accumulated data when the stream signals completion.
func (a *streamAccumulator) handleFinish(ch chan<- ChatEvent, s *Session, reason FinishReason) {
	a.pendingFinish = reason

	// Collect tool calls in index order, deduplicating by ID
	indices := make([]int, 0, len(a.toolCallAccum))
	for idx := range a.toolCallAccum {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	seenIDs := make(map[string]bool)
	for _, idx := range indices {
		tc := a.toolCallAccum[idx]
		if tc.ID != "" && seenIDs[tc.ID] {
			continue
		}
		if tc.ID != "" {
			seenIDs[tc.ID] = true
		}
		a.pendingCalls = append(a.pendingCalls, *tc)
	}

	content := a.contentBuf.String()
	reasoning := a.reasoningBuf.String()

	// Reasoning models — promote reasoning to content when no explicit content
	if content == "" && reasoning != "" && len(a.pendingCalls) == 0 {
		content = reasoning
		reasoning = ""
		ch <- ChatEvent{Type: ChatEventContent, Content: content}
	}

	// Store assistant message in conversation history
	s.messages = append(s.messages, Message{
		Role:             "assistant",
		Content:          content,
		ToolCalls:        a.pendingCalls,
		ReasoningContent: reasoning,
	})
}

// emitDone sends the done event and clears pending state to prevent double-sends.
func (a *streamAccumulator) emitDone(ch chan<- ChatEvent) {
	ch <- ChatEvent{
		Type:    ChatEventDone,
		Finish:  a.pendingFinish,
		Content: serializeToolCalls(a.pendingCalls),
		Usage:   a.lastUsage,
	}
	a.pendingFinish = ""
}

// finalize handles post-stream cleanup: estimate usage if missing, emit pending done.
func (a *streamAccumulator) finalize(ch chan<- ChatEvent, s *Session) {
	// Accumulate manual count if API didn't provide usage
	if s.totalOutputTokens == 0 && a.tokenCount > 0 {
		s.totalOutputTokens += a.tokenCount
	}

	// If finish arrived but usage never came, estimate tokens
	if a.pendingFinish != "" {
		if a.lastUsage == nil {
			promptEstimate := s.estimateTokens()
			outputTokens := a.tokenCount
			if outputTokens == 0 {
				outputTokens = s.totalOutputTokens
			}
			s.totalInputTokens = promptEstimate
			s.totalOutputTokens += outputTokens
			a.lastUsage = &StreamUsage{
				PromptTokens:     promptEstimate,
				CompletionTokens: outputTokens,
			}
		}
		a.emitDone(ch)
	}
}

// serializeToolCalls serializes accumulated tool calls as JSON for the done event content.
func serializeToolCalls(calls []ToolCallResponse) string {
	if len(calls) == 0 {
		return ""
	}
	b, _ := json.Marshal(calls)
	return string(b)
}

// sanitizeAPIMessages ensures no duplicate tool_call_ids exist across the message list.
// DeepSeek emits duplicate IDs which cause API 400 errors.
func sanitizeAPIMessages(msgs *[]APIChatMessage) {
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
	cleaned := make([]APIChatMessage, 0, len(*msgs))
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

// ── Message-based accessors ───────────────────────────────────

// SetMessages replaces the message history (used after compaction).
func (s *Session) SetMessages(messages []Message) {
	s.messages = messages
}

// SetTools updates the tool definitions for this session.
func (s *Session) SetTools(tools []OpenAITool) {
	s.tools = tools
}

// ── Shared methods ─────────────────────────────────────────────────

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
		Messages:    []APIChatMessage{{Role: "user", Content: []byte(`""`)}},
		MaxTokens:   0,
		CachePrompt: false,
		SessionID:   s.sessionID,
	}
	_, err := s.engine.chatComplete(req)
	return err
}
