package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// AgentLoopConfig holds the configuration for an agent loop.
type AgentLoopConfig struct {
	SystemPrompt   string
	MaxToolRounds  int
	MaxTokens      int
	ContextSize    int
	ThinkingBudget int
	Tools          []OpenAITool
	Opts           GenerateOptions
	Hooks          []LoopHook
	ProjectDir     string
	SandboxID      string

	// Execution control
	MaxRetriesPerTool    int // max consecutive failures before forcing a new approach (0 = 3)
	MaxConsecutiveFails  int // circuit breaker: force yield after N consecutive failures (0 = 5)
	ToolExecTimeoutSec   int // seconds before a tool call times out (0 = 300, delegation gets 30m)
}

// AgentLoop runs the full agent loop: generate → parse tool calls → execute → feed back.
type AgentLoop struct {
	provider LLMProvider
	session  Session
	executor ToolExecutor
	config   AgentLoopConfig
	logger   *log.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// NewAgentLoop creates a new agent loop.
func NewAgentLoop(provider LLMProvider, executor ToolExecutor, s Session, cfg AgentLoopConfig) *AgentLoop {
	// Apply defaults
	if cfg.MaxRetriesPerTool == 0 {
		cfg.MaxRetriesPerTool = 3
	}
	if cfg.MaxConsecutiveFails == 0 {
		cfg.MaxConsecutiveFails = 5
	}
	if cfg.ToolExecTimeoutSec == 0 {
		cfg.ToolExecTimeoutSec = 300
	}
	return &AgentLoop{
		provider: provider,
		session:  s,
		executor: executor,
		config:   cfg,
		logger:   log.Default(),
	}
}

// SetLogger sets a custom logger.
func (l *AgentLoop) SetLogger(logger *log.Logger) {
	l.logger = logger
}

// Run starts the agent loop for a user message, emitting events to the subscriber channel.
func (l *AgentLoop) Run(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return fmt.Errorf("agent loop already running")
	}
	l.running = true
	ctx, l.cancel = context.WithCancel(ctx)
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	SendEvent(subscriber, AgentEvent{Type: EventTypeAgentStart})

	// Build initial context from session tree
	sessCtx, err := l.session.BuildContext(ctx)
	if err != nil {
		SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
		SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
		return err
	}

	// If session has no system prompt yet, set it
	if len(sessCtx) == 0 || sessCtx[0].Role != "system" {
		l.session.AppendMessage(Message{Role: "system", Content: l.config.SystemPrompt})
	}
	l.session.AppendMessage(Message{Role: "user", Content: userMsg})

	return l.runLoop(ctx, subscriber)
}

// Continue sends a follow-up message within an existing session.
func (l *AgentLoop) Continue(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return fmt.Errorf("agent loop already running")
	}
	l.running = true
	ctx, l.cancel = context.WithCancel(ctx)
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	SendEvent(subscriber, AgentEvent{Type: EventTypeAgentStart})
	l.session.AppendMessage(Message{Role: "user", Content: userMsg})

	return l.runLoop(ctx, subscriber)
}

// runLoop is the core generation → execute tools → feed-back cycle.
func (l *AgentLoop) runLoop(ctx context.Context, subscriber chan<- AgentEvent) error {
	ctx = context.WithValue(ctx, SubscriberKey{}, subscriber)

	state := &LoopState{
		SessionID:  l.session.ID(),
		ProjectDir: l.config.ProjectDir,
		SandboxID:  l.config.SandboxID,
		FailCounts: make(map[string]int),
		StartedAt:  time.Now(),
	}

	// Notify hooks: agent started
	for _, h := range l.config.Hooks {
		if err := h.OnAgentStart(ctx, state); err != nil {
			l.logger.Printf("Hook %s OnAgentStart error: %v", h.Name(), err)
		}
	}

	round := 0

	for {
		// Check hook-injected messages before each turn
		for _, h := range l.config.Hooks {
			nudges, err := h.OnBeforeTurn(ctx, state)
			if err != nil {
				l.logger.Printf("Hook %s OnBeforeTurn error: %v", h.Name(), err)
			}
			for _, nudge := range nudges {
				l.session.AppendMessage(Message{Role: "user", Content: nudge})
			}
		}

		// Build context and stream
		sessCtx, err := l.session.BuildContext(ctx)
		if err != nil {
			SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
			SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
			return err
		}

		chatCh, err := l.provider.StreamChat(ctx, sessCtx, l.config.Tools, l.config.Opts)
		if err != nil {
			SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
			SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
			return err
		}

		// Phase 1: Stream until generation completes
		var contentBuf strings.Builder
		var thinkingBuf strings.Builder
		var finishReason FinishReason
		var lastUsage *StreamUsage
		toolCallAccum := make(map[int]*ToolCallResponse)

	streamLoop:
		for evt := range chatCh {
			if ctx.Err() != nil {
				SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return ctx.Err()
			}

			if evt.Usage != nil {
				lastUsage = evt.Usage
			}

			switch evt.Type {
			case ChatEventError:
				l.logger.Printf("Generation error: %v", evt.Err)
				SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: evt.Err.Error()}})
				SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return evt.Err

			case ChatEventDone:
				finishReason = evt.Finish
				if evt.Usage != nil {
					lastUsage = evt.Usage
				}
				// Accumulate tool calls from the done event before breaking.
				// The session accumulates tool calls and sends them in the done event.
				for _, tc := range evt.Deltas {
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
				break streamLoop

			case ChatEventContent:
				contentBuf.WriteString(evt.Content)
				SendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: evt.Content}})

			case ChatEventThinking:
				thinkingBuf.WriteString(evt.Content)
				SendEvent(subscriber, AgentEvent{Type: EventTypeThinkingDelta, Data: AgentEventData{Text: evt.Content}})
			}

			// Accumulate tool call chunks
			for _, tc := range evt.Deltas {
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
		}

		// Track token usage
		if lastUsage != nil {
			state.TotalInputTokens += lastUsage.PromptTokens
			state.TotalOutputTokens += lastUsage.CompletionTokens
		}

		// Collect tool calls in index order
		var toolCalls []ToolCallResponse
		indices := make([]int, 0, len(toolCallAccum))
		for idx := range toolCallAccum {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		for _, idx := range indices {
			toolCalls = append(toolCalls, *toolCallAccum[idx])
		}

		// Store assistant message in session
		contentStr := contentBuf.String()
		thinkingStr := thinkingBuf.String()

		// Reasoning models (DeepSeek V4, etc.) sometimes put the full response
		// in the reasoning stream with empty content. Promote reasoning to content
		// so the response reaches the user.
		if contentStr == "" && thinkingStr != "" && len(toolCalls) == 0 {
			contentStr = thinkingStr
			thinkingStr = ""
			// Emit as text delta so the SSE stream includes the response
			SendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: contentStr}})
		}

		assistantMsg := Message{
			Role:             "assistant",
			Content:          contentStr,
			ToolCalls:        toolCalls,
			ReasoningContent: thinkingStr,
		}
		l.session.AppendMessage(assistantMsg)

		contentLen := contentBuf.Len()

		// Check if we should stop
		maxRounds := l.config.MaxToolRounds
		hitMaxRounds := maxRounds > 0 && round >= maxRounds
		circuitBroken := l.config.MaxConsecutiveFails > 0 && state.ConsecutiveFails >= l.config.MaxConsecutiveFails
		stoppedNaturally := len(toolCalls) == 0 || finishReason != FinishToolCalls

		l.logger.Printf("LOOP DECISION: round=%d toolCalls=%d finishReason=%q contentLen=%d thinkingLen=%d stoppedNaturally=%v hitMaxRounds=%v circuitBroken=%v model=%s",
			round, len(toolCalls), string(finishReason), contentBuf.Len(), thinkingBuf.Len(), stoppedNaturally, hitMaxRounds, circuitBroken, l.provider.ModelName())

		if stoppedNaturally || hitMaxRounds || circuitBroken {
			if circuitBroken {
				l.logger.Printf("Circuit breaker tripped: %d consecutive failures", state.ConsecutiveFails)
				SendEvent(subscriber, AgentEvent{
					Type: EventTypeError,
					Data: AgentEventData{Error: fmt.Sprintf("Circuit breaker: %d consecutive tool failures", state.ConsecutiveFails)},
				})
			}

			for _, h := range l.config.Hooks {
				h.OnAgentEnd(ctx, state)
			}

			SendEvent(subscriber, AgentEvent{
				Type: EventTypeAgentEnd,
				Data: AgentEventData{
					Input:  float64(state.TotalInputTokens),
					Output: float64(state.TotalOutputTokens),
					Model:  l.provider.ModelName(),
				},
			})
			return nil
		}

		// Phase 2: Execute tool calls with retry, timeout, and streaming
		round++
		l.logger.Printf("Tool calls detected: %d (round %d)", len(toolCalls), round)
		state.Round = round
		state.ContentLength = contentLen

		var toolResults []ToolResult
		for _, tcr := range toolCalls {
			tc := tcr.ToToolCall()

			SendEvent(subscriber, AgentEvent{
				Type: EventTypeToolStart,
				Data: AgentEventData{ToolName: tc.Name, ToolArgs: tc.Args, ToolID: tc.ID},
			})

			argsJSON, _ := json.Marshal(tc.Args)
			l.logger.Printf("AGENT TOOL CALL: round=%d tool=%s args=%s", round, tc.Name, string(argsJSON))

			// Check retry limit
			if l.config.MaxRetriesPerTool > 0 && state.FailCounts[tc.Name] >= l.config.MaxRetriesPerTool {
				resultStr := fmt.Sprintf("[SYSTEM: Tool '%s' has failed %d times. Do NOT retry it. Use a completely different approach or tool.]",
					tc.Name, l.config.MaxRetriesPerTool)
				toolResults = append(toolResults, ToolResult{ToolCallID: tcr.ID, ToolName: tc.Name, Content: resultStr})
				SendEvent(subscriber, AgentEvent{
					Type: EventTypeToolEnd,
					Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Error: resultStr},
				})
				continue
			}

			// Execute with timeout, streaming, and retry
			result, resultErr := l.executeTool(ctx, subscriber, tc, state)

			// Classify error and retry transient failures once
			if resultErr != nil && ClassifyError(resultErr) == ErrorTransient {
				backoff := time.Duration(500 * time.Millisecond)
				l.logger.Printf("Transient error on %s, retrying after %v: %v", tc.Name, backoff, resultErr)
				time.Sleep(backoff)
				result, resultErr = l.executeTool(ctx, subscriber, tc, state)
			}

			var resultStr string
			if resultErr != nil {
				state.FailCounts[tc.Name]++
				state.ConsecutiveFails++
				resultStr = fmt.Sprintf("<tool_use_error>%s</tool_use_error>", resultErr.Error())
				l.logger.Printf("AGENT TOOL ERROR: tool=%s err=%v", tc.Name, resultErr)

				// Add suggestion for permanent errors
				if ClassifyError(resultErr) == ErrorPermanent {
					resultStr += "\nThis error is permanent. Do NOT retry this tool with the same arguments. Try a different approach."
				}
			} else {
				state.FailCounts[tc.Name] = 0
				state.ConsecutiveFails = 0
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
				if len(resultStr) > 6000 {
					resultStr = resultStr[:6000] + "...[truncated]"
				}
			}

			SendEvent(subscriber, AgentEvent{
				Type: EventTypeToolEnd,
				Data: AgentEventData{
					ToolName: tc.Name,
					ToolID:   tc.ID,
					Result:   result,
					Error:    func() string { if resultErr != nil { return resultErr.Error() }; return "" }(),
				},
			})

			// Notify hooks after each tool call
			for _, h := range l.config.Hooks {
				h.OnAfterToolCall(ctx, state, tc.Name, tc.Args, resultStr, resultErr)
			}

			toolResults = append(toolResults, ToolResult{ToolCallID: tcr.ID, ToolName: tc.Name, Content: resultStr})
		}

		// Feed tool results back into session
		for _, tr := range toolResults {
			l.session.AppendMessage(Message{
				Role:       "tool",
				Content:    tr.Content,
				ToolCallID: tr.ToolCallID,
				Name:       tr.ToolName,
			})
		}

		state.ToolResults = toolResults
	}
}

// executeTool runs a single tool call with timeout and optional streaming.
func (l *AgentLoop) executeTool(ctx context.Context, subscriber chan<- AgentEvent, tc ToolCall, state *LoopState) (any, error) {
	timeout := time.Duration(l.config.ToolExecTimeoutSec) * time.Second
	if tc.Name == "delegate_to" || tc.Name == "delegate_async" {
		timeout = 30 * time.Minute
	}

	// Check for streaming executor
	var streamer ToolExecutorStreaming
	if s, ok := l.executor.(ToolExecutorStreaming); ok {
		streamer = s
	}

	// No timeout: execute directly
	if timeout <= 0 {
		if streamer != nil {
			return streamer.ExecuteStreaming(ctx, tc.Name, tc.Args, func(update string) {
				SendEvent(subscriber, AgentEvent{
					Type: EventTypeToolUpdate,
					Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Text: update},
				})
			})
		}
		return l.executor.Execute(ctx, tc.Name, tc.Args)
	}

	// With timeout: execute in goroutine
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type toolResult struct {
		val any
		err error
	}
	ch := make(chan toolResult, 1)

	go func() {
		var val any
		var err error
		if streamer != nil {
			val, err = streamer.ExecuteStreaming(toolCtx, tc.Name, tc.Args, func(update string) {
				SendEvent(subscriber, AgentEvent{
					Type: EventTypeToolUpdate,
					Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Text: update},
				})
			})
		} else {
			val, err = l.executor.Execute(toolCtx, tc.Name, tc.Args)
		}
		ch <- toolResult{val, err}
	}()

	select {
	case r := <-ch:
		return r.val, r.err
	case <-toolCtx.Done():
		return nil, fmt.Errorf("tool %q timed out after %v", tc.Name, timeout)
	}
}

// Abort cancels the running agent loop.
func (l *AgentLoop) Abort() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
}

// Close releases the agent's resources.
func (l *AgentLoop) Close() error {
	if l.cancel != nil {
		l.cancel()
	}
	return l.session.Close()
}

// IsRunning returns whether the loop is currently active.
func (l *AgentLoop) IsRunning() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}

// Session returns the underlying session.
func (l *AgentLoop) Session() Session {
	return l.session
}
