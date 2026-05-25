package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/retry"
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
	MaxProviderRetries   int // max retries when the LLM provider fails mid-stream (0 = 2)

	// ToolResultProcessor intercepts tool result strings before they enter context.
	// When set, replaces the hardcoded 6000-char truncation.
	// Return the processed string the agent should see.
	// toolArgs contains the arguments from the tool call (for context enrichment).
	ToolResultProcessor func(ctx context.Context, toolName, toolCallID, result string, toolArgs map[string]any) string
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
	if cfg.MaxProviderRetries == 0 {
		cfg.MaxProviderRetries = 2
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
	return l.RunWithImages(ctx, userMsg, nil, subscriber)
}

// RunWithImages starts the agent loop for a multimodal user message (text + images).
func (l *AgentLoop) RunWithImages(ctx context.Context, userMsg string, images []ContentImage, subscriber chan<- AgentEvent) error {
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
	l.session.AppendMessage(Message{Role: "user", Content: userMsg, Images: images})

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
		DoomLoop:   NewDoomLoopDetector(6, 3),
	}

	// Notify hooks: agent started
	for _, h := range l.config.Hooks {
		if err := h.OnAgentStart(ctx, state); err != nil {
			l.logger.Printf("Hook %s OnAgentStart error: %v", h.Name(), err)
		}
	}

	round := 0

	for {
		SendEvent(subscriber, AgentEvent{
			Type: EventTypeStepStart,
			Data: AgentEventData{Round: round + 1},
		})
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

		// Build context and stream — with provider retry for transient errors
		var (
			contentBuf    strings.Builder
			thinkingBuf   strings.Builder
			finishReason  FinishReason
			lastUsage     *StreamUsage
			toolCallAccum map[int]*ToolCallResponse
			providerErr   error
		)

	providerRetry:
		for attempt := 0; attempt <= l.config.MaxProviderRetries; attempt++ {
			if attempt > 0 {
				backoff := retry.Config{BaseDelay: 1 * time.Second, MaxDelay: 30 * time.Second, Jitter: true}.Delay(attempt - 1)
				l.logger.Printf("Provider retry %d/%d after %v (error: %v)", attempt, l.config.MaxProviderRetries, backoff, providerErr)
				select {
				case <-ctx.Done():
					SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
					return ctx.Err()
				case <-time.After(backoff):
				}
			}

			// Reset accumulators for each attempt
			contentBuf.Reset()
			thinkingBuf.Reset()
			finishReason = ""
			lastUsage = nil
			toolCallAccum = make(map[int]*ToolCallResponse)

			sessCtx, err := l.session.BuildContext(ctx)
			if err != nil {
				SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
				SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return err
			}

			// Ensure system messages are first (strict Jinja templates require it)
			sessCtx = reorderSystemFirst(sessCtx)

			// Run hooks: OnBeforeModel — modify messages before sending to LLM
			for _, h := range l.config.Hooks {
				modified, err := h.OnBeforeModel(ctx, state, sessCtx)
				if err != nil {
					l.logger.Printf("Hook %s OnBeforeModel error: %v", h.Name(), err)
				} else if modified != nil {
					sessCtx = modified
				}
			}

			chatCh, err := l.provider.StreamChat(ctx, sessCtx, l.config.Tools, l.config.Opts)
			if err != nil {
				providerErr = err
				if ClassifyError(err) == ErrorTransient && attempt < l.config.MaxProviderRetries {
					continue providerRetry
				}
				SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
				SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return err
			}

			// Phase 1: Stream until generation completes
			var streamErr error
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
					streamErr = evt.Err
					l.logger.Printf("Generation error on attempt %d: %v", attempt, evt.Err)
					break streamLoop

				case ChatEventDone:
					finishReason = evt.Finish
					if evt.Usage != nil {
						lastUsage = evt.Usage
					}
					if len(toolCallAccum) == 0 {
						for _, tc := range evt.Deltas {
							sig := ""
							if tc.ExtraContent != nil && tc.ExtraContent.Google != nil {
								sig = tc.ExtraContent.Google.ThoughtSignature
							}
							toolCallAccum[tc.Index] = &ToolCallResponse{
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
					break streamLoop

				case ChatEventContent:
					contentBuf.WriteString(evt.Content)
					SendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: evt.Content}})

				case ChatEventThinking:
					thinkingBuf.WriteString(evt.Content)
					SendEvent(subscriber, AgentEvent{Type: EventTypeThinkingDelta, Data: AgentEventData{Text: evt.Content}})
				}

				for _, tc := range evt.Deltas {
					sig := ""
					if tc.ExtraContent != nil && tc.ExtraContent.Google != nil {
						sig = tc.ExtraContent.Google.ThoughtSignature
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
						if sig != "" {
							existing.ThoughtSignature = sig
						}
					} else {
						toolCallAccum[tc.Index] = &ToolCallResponse{
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

			// Handle stream errors — retry if transient
			if streamErr != nil {
				providerErr = streamErr
				if ClassifyError(streamErr) == ErrorTransient && attempt < l.config.MaxProviderRetries {
					continue providerRetry
				}
				SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: streamErr.Error()}})
				SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return streamErr
			}

			// Run hooks: OnAfterModel — after successful model response
			genResp := &GenerateResponse{
				Content:   contentBuf.String(),
				Thinking:  thinkingBuf.String(),
				ToolCalls: func() []ToolCallResponse {
					indices := make([]int, 0, len(toolCallAccum))
					for idx := range toolCallAccum { indices = append(indices, idx) }
					sort.Ints(indices)
					result := make([]ToolCallResponse, 0, len(indices))
					for _, idx := range indices { result = append(result, *toolCallAccum[idx]) }
					return result
				}(),
				Finish: finishReason,
				Usage:  lastUsage,
			}
			for _, h := range l.config.Hooks {
				if err := h.OnAfterModel(ctx, state, genResp); err != nil {
					l.logger.Printf("Hook %s OnAfterModel error: %v", h.Name(), err)
				}
			}

			// Success — break out of retry loop
			break providerRetry
		}

		// If all retries exhausted, providerErr holds the last error
		if providerErr != nil && finishReason == "" {
			SendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: fmt.Sprintf("Provider failed after %d retries: %v", l.config.MaxProviderRetries, providerErr)}})
			SendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
			return providerErr
		}

		// Track token usage
		state.TurnInputTokens = 0
		state.TurnOutputTokens = 0
		if lastUsage != nil {
			state.TotalInputTokens += lastUsage.PromptTokens
			state.TotalOutputTokens += lastUsage.CompletionTokens
			state.TurnInputTokens = lastUsage.PromptTokens
			state.TurnOutputTokens = lastUsage.CompletionTokens
			log.Printf("loop: usage in=%d out=%d total_in=%d total_out=%d",
				lastUsage.PromptTokens, lastUsage.CompletionTokens,
				state.TotalInputTokens, state.TotalOutputTokens)

			// Inject real token usage into context for accurate context manager estimates
			ctx = context.WithValue(ctx, TokenUsageKey{}, &TokenUsage{
				PromptTokens:     lastUsage.PromptTokens,
				CompletionTokens: lastUsage.CompletionTokens,
				ContextSize:      l.provider.ContextSize(),
			})
		}
		state.TurnModel = l.provider.ModelName()

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
		// Some providers (Gemini) send finish_reason="stop" even with tool calls,
		// so we only consider it a natural stop if there are NO tool calls.
		stoppedNaturally := len(toolCalls) == 0

		l.logger.Printf("loop: round=%d tools=%d finish=%s content=%d thinking=%d model=%s",
			round, len(toolCalls), string(finishReason), contentBuf.Len(), thinkingBuf.Len(), l.provider.ModelName())

		if stoppedNaturally || hitMaxRounds || circuitBroken {
			decision := "respond"

			// Wind-down: on forced termination, make one final LLM call without
			// tools to get a useful summary instead of hard-stopping.
			if (hitMaxRounds || circuitBroken) && contentStr != "" {
				windDown := l.windDownSummary(ctx, subscriber, hitMaxRounds, circuitBroken)
				if windDown != "" {
					contentStr = windDown
				}
			}

			if circuitBroken {
				decision = "error"
				l.logger.Printf("Circuit breaker tripped: %d consecutive failures", state.ConsecutiveFails)
				SendEvent(subscriber, AgentEvent{
					Type: EventTypeError,
					Data: AgentEventData{Error: fmt.Sprintf("Circuit breaker: %d consecutive tool failures", state.ConsecutiveFails)},
				})
			}

			SendEvent(subscriber, AgentEvent{
				Type: EventTypeStepEnd,
				Data: AgentEventData{Round: round + 1, Decision: decision},
			})

			for _, h := range l.config.Hooks {
				h.OnAgentEnd(ctx, state)
			}

			SendEvent(subscriber, AgentEvent{
				Type: EventTypeAgentEnd,
				Data: AgentEventData{
					Input:         float64(state.TotalInputTokens),
					Output:        float64(state.TotalOutputTokens),
					Model:         l.provider.ModelName(),
					ContextWindow: l.provider.ContextSize(),
				},
			})
			return nil
		}

		// Emit step end (continuing with tool execution)
		SendEvent(subscriber, AgentEvent{
			Type: EventTypeStepEnd,
			Data: AgentEventData{Round: round + 1, Decision: "delegate"},
		})

		// Phase 2: Execute tool calls with retry, timeout, and streaming
		round++
		l.logger.Printf("Tool calls detected: %d (round %d)", len(toolCalls), round)
		state.Round = round
		state.ContentLength = contentLen

		// Deduplicate tool calls — some models (DeepSeek) emit identical calls
		deduped := deduplicateToolCalls(toolCalls)
		if len(deduped) < len(toolCalls) {
			l.logger.Printf("Deduplicated tool calls: %d -> %d", len(toolCalls), len(deduped))
			toolCalls = deduped
		}

		// Doom loop detection — check if we're repeating the same tool call
		for _, tcr := range toolCalls {
			if state.DoomLoop.Record(tcr.Function.Name, tcr.ToToolCall().Args) {
				warning := state.DoomLoop.WarningMessage()
				l.logger.Printf("Doom loop detected: %s", warning)
				// Inject warning as a user message to force the model to change approach
				l.session.AppendMessage(Message{Role: "user", Content: warning})
				state.DoomLoop.Reset()
				break
			}
		}

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
			// Wrapped by any hooks that implement ToolCallWrapper
			executeFn := func(ctx context.Context, name string, args map[string]any) (any, error) {
				return l.executeTool(ctx, subscriber, tc, state)
			}
			for _, h := range l.config.Hooks {
				if w, ok := h.(ToolCallWrapper); ok {
					outer := executeFn
					hookName := h.Name()
					executeFn = func(ctx context.Context, name string, args map[string]any) (any, error) {
						return w.WrapToolCall(ctx, name, args, outer)
					}
					_ = hookName
				}
			}
			result, resultErr := executeFn(ctx, tc.Name, tc.Args)

			// Classify error and retry transient failures with exponential backoff
			if resultErr != nil && ClassifyError(resultErr) == ErrorTransient {
				transientCfg := retry.Short
			transientRetry:
				for attempt := 0; attempt < transientCfg.MaxAttempts; attempt++ {
					backoff := transientCfg.Delay(attempt)
					l.logger.Printf("Transient error on %s, retry %d after %v: %v", tc.Name, attempt+1, backoff, resultErr)
					select {
					case <-ctx.Done():
						break transientRetry
					case <-time.After(backoff):
					}
					result, resultErr = executeFn(ctx, tc.Name, tc.Args)
					if resultErr == nil || ClassifyError(resultErr) != ErrorTransient {
						break transientRetry
					}
				}
			}

			var resultStr string
			var resultForSSE any = result
			var visionImages []ContentImage

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
				state.DoomLoop.Reset()

				// Check if the executor returned a VisionCarrier (native vision path).
				// This separates the original result (with base64 for frontend) from
				// stripped text + images (for LLM consumption).
				if vc, ok := result.(VisionCarrier); ok {
					resultForSSE, resultStr, visionImages = vc.GetVisionData()
				} else {
					resultBytes, _ := json.Marshal(result)
					resultStr = string(resultBytes)
				}

				if l.config.ToolResultProcessor != nil {
					resultStr = l.config.ToolResultProcessor(ctx, tc.Name, tcr.ID, resultStr, tc.Args)
				} else if len(resultStr) > 10000 {
					// Fallback: keep the end (errors/results matter most)
					resultStr = resultStr[len(resultStr)-10000:]
					resultStr = "...[output truncated, showing last 10000 chars]\n" + resultStr
				}
			}

			SendEvent(subscriber, AgentEvent{
				Type: EventTypeToolEnd,
				Data: AgentEventData{
					ToolName:     tc.Name,
					ToolID:       tc.ID,
					Result:       resultForSSE,
					Error:        func() string { if resultErr != nil { return resultErr.Error() }; return "" }(),
					Artifact:     extractArtifact(tc.Name, resultForSSE),
					ModelContent: extractModelContent(resultStr, tc.Name),
				},
			})

			// Emit source events for URLs found in tool results
			if resultErr == nil {
				for _, src := range extractSources(resultStr) {
					SendEvent(subscriber, AgentEvent{
						Type: EventTypeSource,
						Data: src,
					})
				}
			}

			// Notify hooks after each tool call
			for _, h := range l.config.Hooks {
				h.OnAfterToolCall(ctx, state, tc.Name, tc.Args, resultStr, resultErr)
			}

			toolResults = append(toolResults, ToolResult{ToolCallID: tcr.ID, ToolName: tc.Name, Content: resultStr, Images: visionImages})
		}

		// Feed tool results back into session
		for _, tr := range toolResults {
			l.session.AppendMessage(Message{
				Role:       "tool",
				Content:    tr.Content,
				ToolCallID: tr.ToolCallID,
				Name:       tr.ToolName,
				Images:     tr.Images,
			})
		}

		state.ToolResults = toolResults
	}
}

// executeTool runs a single tool call with timeout and optional streaming.
func (l *AgentLoop) executeTool(ctx context.Context, subscriber chan<- AgentEvent, tc ToolCall, state *LoopState) (any, error) {
	timeout := time.Duration(l.config.ToolExecTimeoutSec) * time.Second
	if tc.Name == "delegate_to" || tc.Name == "delegate_async" || tc.Name == "delegate_continue" {
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

// windDownSummary makes one final LLM call without tools when the agent loop
// terminates abnormally (max rounds or circuit breaker). Strips tools from the
// payload and asks the model to summarize what it found.
func (l *AgentLoop) windDownSummary(ctx context.Context, subscriber chan<- AgentEvent, hitMaxRounds, circuitBroken bool) string {
	sessCtx, err := l.session.BuildContext(ctx)
	if err != nil {
		return ""
	}
	sessCtx = reorderSystemFirst(sessCtx)

	// Inject wind-down instruction
	reason := "max iterations reached"
	if circuitBroken {
		reason = "too many consecutive tool failures"
	}
	sessCtx = append(sessCtx, Message{
		Role: "user",
		Content: fmt.Sprintf(
			"[SYSTEM: %s. Provide a concise summary of what you accomplished and what remains undone. Do not attempt any more tool calls.]",
			reason),
	})

	// Stream with no tools — forces a pure text response
	opts := l.config.Opts
	opts.MaxTokens = 1024
	chatCh, err := l.provider.StreamChat(ctx, sessCtx, nil, opts)
	if err != nil {
		return ""
	}

	var summary strings.Builder
	for evt := range chatCh {
		if evt.Type == ChatEventContent {
			summary.WriteString(evt.Content)
			SendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: evt.Content}})
		}
		if evt.Type == ChatEventError {
			return ""
		}
	}

	result := strings.TrimSpace(summary.String())
	if len(result) < 20 {
		return ""
	}
	return result
}

// Close releases the agent's resources.
func (l *AgentLoop) Close() error {
	if l.cancel != nil {
		l.cancel()
	}
	if l.session != nil {
		return l.session.Close()
	}
	return nil
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

// reorderSystemFirst moves all system messages to the front while preserving
// relative order. Some models (Qwen, Gemma) require system messages first.
func reorderSystemFirst(msgs []Message) []Message {
	var system, rest []Message
	for _, m := range msgs {
		if m.Role == "system" {
			system = append(system, m)
		} else {
			rest = append(rest, m)
		}
	}
	return append(system, rest...)
}

// deduplicateToolCalls removes duplicate tool calls that have the same function
// name and arguments, or the same ID. Some models (e.g. DeepSeek) emit identical
// tool calls multiple times in a single response.
func deduplicateToolCalls(calls []ToolCallResponse) []ToolCallResponse {
	seenKey := make(map[string]bool)
	seenID := make(map[string]bool)
	result := make([]ToolCallResponse, 0, len(calls))
	for _, tc := range calls {
		// Dedup by ID first (most reliable for API correctness)
		if tc.ID != "" {
			if seenID[tc.ID] {
				continue
			}
			seenID[tc.ID] = true
		}
		// Also dedup by name+args
		key := tc.Function.Name + "\x00" + tc.Function.Arguments
		if seenKey[key] {
			continue
		}
		seenKey[key] = true
		result = append(result, tc)
	}
	return result
}

// extractArtifact extracts structured artifact data from tool results.
// Returns structured data suitable for rich rendering (diffs, file listings, command output).
func extractArtifact(toolName string, result any) any {
	if result == nil {
		return nil
	}
	m, ok := result.(map[string]any)
	if !ok {
		return nil
	}

	// Widget key — generic tool-ui contract
	if widget, exists := m["widget"]; exists {
		return map[string]any{"type": "widget", "widget": widget}
	}

	// Explicit artifact key takes precedence
	if artifact, exists := m["artifact"]; exists {
		return artifact
	}

	switch toolName {
	case "delegate_to", "delegate_async", "delegate_continue":
		// Delegate artifacts: diff + file changes
		artifact := map[string]any{}
		if diff, exists := m["diff"]; exists && diff != "" {
			artifact["type"] = "diff"
			artifact["content"] = diff
		}
		if changes, exists := m["changes"]; exists {
			artifact["changes"] = changes
		}
		if len(artifact) > 0 {
			return artifact
		}

	case "file_read", "read_file":
		// File read artifact: file path + snippet
		if path, exists := m["path"]; exists {
			content, _ := m["content"].(string)
			if content == "" {
				content, _ = m["result"].(string)
			}
			snippet := content
			if len(snippet) > 500 {
				snippet = snippet[:500] + "..."
			}
			return map[string]any{
				"type": "file_content",
				"path": path,
				"snippet": snippet,
				"size": len(content),
			}
		}

	case "file_write", "write_file", "file_edit":
		// File write artifact: path + line count
		if path, exists := m["path"]; exists {
			return map[string]any{
				"type": "file_write",
				"path": path,
			}
		}

	case "bash":
		// Bash artifact: command + structured output
		if cmd, exists := m["command"]; exists {
			output, _ := m["output"].(string)
			if output == "" {
				output, _ = m["result"].(string)
			}
			lines := strings.Count(output, "\n") + 1
			return map[string]any{
				"type": "command_output",
				"command": cmd,
				"lines": lines,
			}
		}
	}

	// Fallback: check for diff/changes in any result
	if diff, exists := m["diff"]; exists && diff != "" {
		return map[string]any{"type": "diff", "content": diff}
	}
	if changes, exists := m["changes"]; exists {
		return changes
	}

	return nil
}

// extractModelContent produces a model-visible content string from a tool result.
// This is what gets fed back to the LLM in subsequent turns — a condensed version
// of the display result. The display shows the full result; the model gets a summary.
func extractModelContent(resultStr, toolName string) string {
	switch toolName {
	case "delegate_to", "delegate_async", "delegate_continue":
		// Delegates: model gets a summary, not the full sub-agent text
		if len(resultStr) > 1500 {
			// Find the last meaningful paragraph
			idx := strings.LastIndex(resultStr[:1500], "\n\n")
			if idx > 500 {
				return resultStr[:idx] + "\n\n...[condensed for model context]"
			}
			return resultStr[:1500] + "\n...[condensed for model context]"
		}

	case "file_read", "read_file":
		// File reads: model gets path + size hint, not full content
		// (the full content is already in the session context via tool result)
		if len(resultStr) > 3000 {
			return resultStr[:3000] + "\n...[file content continues]"
		}

	case "file_glob":
		// Glob results: cap at 5000 chars (file lists can be enormous)
		if len(resultStr) > 5000 {
			return resultStr[:5000] + "\n...[file list truncated]"
		}

	case "bash":
		// Bash: model gets output up to 2000 chars
		if len(resultStr) > 2000 {
			return resultStr[:2000] + "\n...[output truncated for model]"
		}

	case "web_search", "web_fetch", "research":
		// Research: model gets the first 2000 chars (most relevant findings)
		if len(resultStr) > 2000 {
			return resultStr[:2000] + "\n...[research results condensed]"
		}
	}

	// For small results, model sees the same as display — no separation needed
	return ""
}

// urlRegex matches http/https URLs in text.
var urlRegex = regexp.MustCompile(`https?://[^\s)<>"']+`)

// extractSources finds URLs in tool results and returns source events.
// Limits to 5 sources per tool result to avoid flooding.
func extractSources(resultStr string) []AgentEventData {
	urls := urlRegex.FindAllString(resultStr, 10)
	if len(urls) == 0 {
		return nil
	}

	// Deduplicate
	seen := make(map[string]bool)
	var sources []AgentEventData
	for _, u := range urls {
		if seen[u] {
			continue
		}
		seen[u] = true
		if len(sources) >= 5 {
			break
		}
		sources = append(sources, AgentEventData{
			SourceType: "url",
			SourceURL:  u,
			SourceID:   fmt.Sprintf("src_%d", len(sources)+1),
		})
	}
	return sources
}
