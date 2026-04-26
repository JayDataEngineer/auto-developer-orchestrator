package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

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

// subscriberKey is the context key for injecting the SSE subscriber channel.
type subscriberKeyType struct{}

var subscriberKeyTypeVal = subscriberKeyType{}

// ContextWithSubscriber injects the SSE subscriber channel into a context.
func ContextWithSubscriber(ctx context.Context, ch chan<- AgentEvent) context.Context {
	return context.WithValue(ctx, subscriberKeyTypeVal, ch)
}

// SubscriberFromContext retrieves the SSE subscriber channel from a context.
func SubscriberFromContext(ctx context.Context) chan<- AgentEvent {
	ch, _ := ctx.Value(subscriberKeyTypeVal).(chan<- AgentEvent)
	return ch
}

// AgentEvent is an event emitted by the agent loop.
type AgentEvent struct {
	Type AgentEventType   `json:"type"`
	Data AgentEventData   `json:"data"`
	Raw  json.RawMessage  `json:"-"`
}

// AgentEventData holds the payload of an agent event.
type AgentEventData struct {
	Text     string                 `json:"text,omitempty"`
	ToolName string                 `json:"toolName,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolID   string                 `json:"toolId,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Input    float64                `json:"input,omitempty"`
	Output   float64                `json:"output,omitempty"`
	Cache    float64                `json:"cache,omitempty"`
	Model    string                 `json:"model,omitempty"`
	Streaming bool                  `json:"streaming,omitempty"`
	CompactedMessages int           `json:"compactedMessages,omitempty"`
	KeptMessages      int           `json:"keptMessages,omitempty"`
}

// ToolExecutor executes a tool and returns its result.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error)
}

// ToolExecutorStreaming is an optional interface for tools that stream partial results.
type ToolExecutorStreaming interface {
	ToolExecutor
	ExecuteStreaming(ctx context.Context, toolName string, args map[string]interface{}, onUpdate func(string)) (interface{}, error)
}

// TranscriptSaver persists pre-compaction message snapshots.
type TranscriptSaver interface {
	SaveTranscript(messagesJSON []byte, reason string, tokenCount int)
}

// ApprovalResponse is the user's response to an approval/question request.
type ApprovalResponse struct {
	Action  string
	Message string
}

// ApprovalManager manages pending approval/question requests.
type ApprovalManager interface {
	Register(requestID string) <-chan ApprovalResponse
	Resolve(requestID string, resp ApprovalResponse) bool
	Cleanup(requestID string)
}

// sendEvent sends an event to the subscriber channel without blocking.
func sendEvent(ch chan<- AgentEvent, evt AgentEvent) {
	defer func() { recover() }()
	select {
	case ch <- evt:
	default:
	}
}

// ComputerUseProvider provides computer use / desktop automation capabilities.
type ComputerUseProvider interface {
	IsReady(sandboxID string) bool
	Enable(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	Screenshot(ctx context.Context, sandboxID string, describe bool) (map[string]interface{}, error)
	Snapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	Act(ctx context.Context, sandboxID string, action string, args map[string]interface{}) (map[string]interface{}, error)

	DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error)
	DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error)
	DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error)

	Resolution(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	ExtractPageContent(ctx context.Context, sandboxID string, rawHTML bool) (string, error)
}

// SandboxToolExecutor executes tools via the sandbox manager and computer use provider.
type SandboxToolExecutor struct {
	SandboxID   string
	Manager     *sandbox.Manager
	CU          ComputerUseProvider
	Logger      *zap.Logger
	Creds       *CredentialStore
	MCPClient   *mcp.Client
	ApprovalMgr ApprovalManager
	Vision      *browser.VisionClient

	VisionEnabled bool

	lastElements     map[string]map[string]bool
	elemIndex        map[string][]indexedElement
	credsLoaded      bool
	pageFingerprints map[string][]pageFingerprint
	fileOps          *SandboxFileOps
	scrapedURLs      map[string]string

	// Action cache: SHA256(instruction + URL) → cached element ID (Stagehand pattern)
	actionCache map[string]int
}

// pageFingerprint is a compact hash of page state for detecting stagnation.
type pageFingerprint struct {
	url     string
	elCount int
	sig     string
}

// AgentLoopConfig holds configuration for the agent loop.
type AgentLoopConfig struct {
	SystemPrompt   string
	MaxToolRounds  int
	MaxTokens      int
	ContextSize    int
	ThinkingBudget int
	Tools          []OpenAITool
	Opts           GenerateOptions
	Compaction     CompactionConfig
}

// DefaultAgentLoopConfig returns sensible defaults from ModelConfig.
func DefaultAgentLoopConfig() AgentLoopConfig {
	return AgentLoopConfig{
		SystemPrompt:  "You are a coding assistant with access to tools.",
		MaxToolRounds: cfg.DefaultMaxToolRounds,
		MaxTokens:     cfg.MaxTokens,
		ContextSize:   cfg.DefaultContextSize,
		Opts: GenerateOptions{
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
			TopP:        cfg.TopP,
			TopK:        cfg.TopK,
		},
	}
}

// AgentLoop runs the full agent loop: generate → parse tool calls → execute → feed back.
type AgentLoop struct {
	engine   *LLMClient
	session  *Session
	executor ToolExecutor
	config   AgentLoopConfig
	logger   *zap.Logger
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc

	consecutiveCompactionFailures int
	saver                         TranscriptSaver
}

// NewAgentLoop creates a new agent loop bound to an engine.
func NewAgentLoop(engine *LLMClient, executor ToolExecutor, cfg AgentLoopConfig, logger *zap.Logger) (*AgentLoop, error) {
	if !engine.IsLoaded() {
		return nil, fmt.Errorf("engine model not loaded")
	}
	session, err := engine.NewSession(cfg.ContextSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	if len(cfg.Tools) > 0 {
		session.SetTools(cfg.Tools)
	}
	if cfg.ThinkingBudget > 0 {
		session.SetThinkingBudget(cfg.ThinkingBudget)
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AgentLoop{
		engine:   engine,
		session:  session,
		executor: executor,
		config:   cfg,
		logger:   logger,
	}, nil
}

// Run starts the agent loop for a user message and emits events to the subscriber channel.
func (loop *AgentLoop) Run(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	loop.mu.Lock()
	if loop.running {
		loop.mu.Unlock()
		return fmt.Errorf("agent loop already running")
	}
	loop.running = true
	ctx, loop.cancel = context.WithCancel(ctx)
	loop.mu.Unlock()

	defer func() {
		loop.mu.Lock()
		loop.running = false
		loop.mu.Unlock()
	}()

	sendEvent(subscriber, AgentEvent{Type: EventTypeAgentStart})

	opts := loop.config.Opts
	opts.MaxTokens = loop.config.MaxTokens

	chatCh, err := loop.session.ChatWithTools(loop.config.SystemPrompt, userMsg, loop.config.Tools, opts)
	if err != nil {
		sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
		sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
		return err
	}

	return loop.runLoop(ctx, chatCh, subscriber, opts)
}

// Continue sends a follow-up message within an existing session.
func (loop *AgentLoop) Continue(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	loop.mu.Lock()
	if loop.running {
		loop.mu.Unlock()
		return fmt.Errorf("agent loop already running")
	}
	loop.running = true
	ctx, loop.cancel = context.WithCancel(ctx)
	loop.mu.Unlock()

	defer func() {
		loop.mu.Lock()
		loop.running = false
		loop.mu.Unlock()
	}()

	sendEvent(subscriber, AgentEvent{Type: EventTypeAgentStart})

	opts := loop.config.Opts
	opts.MaxTokens = loop.config.MaxTokens

	chatCh, err := loop.session.FeedUserMessage(userMsg, opts)
	if err != nil {
		sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
		sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
		return err
	}

	return loop.runLoop(ctx, chatCh, subscriber, opts)
}

// runLoop is the core generation → execute tools → feed-back cycle.
func (loop *AgentLoop) runLoop(ctx context.Context, chatCh <-chan ChatEvent, subscriber chan<- AgentEvent, opts GenerateOptions) error {
	ctx = ContextWithSubscriber(ctx, subscriber)

	round := 0
	failCounts := make(map[string]int)
	consecutiveTotalFails := 0
	const maxConsecutiveTotalFails = 5
	cycleDetector := NewCycleDetector(10)

	for {
		var contentBuf strings.Builder
		var finishReason FinishReason

		// Phase 1: Stream ChatEvents until generation completes
		for evt := range chatCh {
			if ctx.Err() != nil {
				sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return ctx.Err()
			}

			switch evt.Type {
			case ChatEventError:
				loop.logger.Error("Generation error", zap.Error(evt.Err))
				sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: evt.Err.Error()}})
				sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return evt.Err

			case ChatEventDone:
				finishReason = evt.Finish

			case ChatEventContent:
				contentBuf.WriteString(evt.Content)
				sendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: evt.Content}})

			case ChatEventThinking:
				sendEvent(subscriber, AgentEvent{Type: EventTypeThinkingDelta, Data: AgentEventData{Text: evt.Content}})
			}

			if evt.Type == ChatEventDone {
				break
			}
		}

		// Phase 2: Check if we got tool calls
		msgs := loop.session.Messages()
		var toolCalls []ToolCallResponse
		if len(msgs) > 0 {
			lastMsg := msgs[len(msgs)-1]
			if lastMsg.Role == "assistant" {
				toolCalls = lastMsg.ToolCalls
			}
		}

		maxRounds := loop.config.MaxToolRounds
		hitMaxRounds := maxRounds > 0 && round >= maxRounds
		contentLen := contentBuf.Len()
		needsSynthesis := false
		unlimited := maxRounds == 0

		if unlimited || maxRounds > 15 {
			if contentLen == 0 && round >= 10 {
				needsSynthesis = true
			} else if round >= 15 && contentLen < 3000 {
				needsSynthesis = true
			}
		}

		stoppedNaturally := len(toolCalls) == 0 || finishReason != FinishToolCalls
		if stoppedNaturally && round >= 5 && contentLen < 3000 && !needsSynthesis {
			needsSynthesis = true
		}

		if len(toolCalls) == 0 || finishReason != FinishToolCalls || hitMaxRounds || needsSynthesis {
			if hitMaxRounds || needsSynthesis {
				loop.logger.Warn("Agent has many tool rounds with no text, forcing synthesis",
					zap.Int("round", round), zap.Int("maxRounds", maxRounds),
					zap.Bool("unlimitedRounds", maxRounds == 0))

				synthesisPrompt := "Based on all your research and tool results above, provide your comprehensive final answer now. Do NOT call any more tools. Write your answer directly in your response (NOT in your thinking/reasoning). Just write the final report."
				synthOpts := loop.config.Opts
				synthOpts.MaxTokens = loop.config.MaxTokens
				savedTools := loop.session.GetTools()
				loop.session.SetTools(nil)

				synthCh, err := loop.session.FeedUserMessage(synthesisPrompt, synthOpts)
				if err == nil {
					var thinkingBuf strings.Builder
					for evt := range synthCh {
						if evt.Type == ChatEventContent {
							contentBuf.WriteString(evt.Content)
							sendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: evt.Content}})
						} else if evt.Type == ChatEventThinking {
							thinkingBuf.WriteString(evt.Content)
						}
					}
					if contentBuf.Len() == 0 && thinkingBuf.Len() > 0 {
						contentBuf.WriteString(thinkingBuf.String())
						sendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: thinkingBuf.String()}})
					}
				}
				loop.session.SetTools(savedTools)
			} else if hitMaxRounds {
				loop.logger.Warn("Max tool rounds reached", zap.Int("round", round), zap.Int("maxRounds", maxRounds))
			}

			inputTokens, outputTokens := loop.session.TokenCounts()
			sendEvent(subscriber, AgentEvent{
				Type: EventTypeAgentEnd,
				Data: AgentEventData{Input: float64(inputTokens), Output: float64(outputTokens), Model: "llama-server/gemma-4-26b"},
			})
			return nil
		}

		round++
		loop.logger.Info("Tool calls detected", zap.Int("count", len(toolCalls)), zap.Int("round", round))

		// Phase 3: Execute tool calls
		var toolResults []ToolResult
		var delegateCalls, sequentialCalls []ToolCallResponse
		for _, tcr := range toolCalls {
			if tcr.Function.Name == "delegate_to" {
				delegateCalls = append(delegateCalls, tcr)
			} else {
				sequentialCalls = append(sequentialCalls, tcr)
			}
		}

		for _, tcr := range sequentialCalls {
			tc := tcr.ToToolCall()

			sendEvent(subscriber, AgentEvent{
				Type: EventTypeToolStart,
				Data: AgentEventData{ToolName: tc.Name, ToolArgs: tc.Args, ToolID: tc.ID},
			})

			argsJSON, _ := json.Marshal(tc.Args)
			loop.logger.Info("AGENT TOOL CALL", zap.Int("round", round),
				zap.String("tool", tc.Name), zap.String("args", string(argsJSON)))

			if failCounts[tc.Name] >= cfg.MaxRetriesPerTool {
				resultStr := fmt.Sprintf("[SYSTEM: Tool '%s' has failed %d times. Do NOT retry it. Use a COMPLETELY DIFFERENT approach or tool.]",
					tc.Name, cfg.MaxRetriesPerTool)
				loop.logger.Warn("Tool retry limit reached", zap.String("tool", tc.Name), zap.Int("failCount", failCounts[tc.Name]))
				toolResults = append(toolResults, ToolResult{ToolCallID: tcr.ID, ToolName: tc.Name, Content: resultStr})
				sendEvent(subscriber, AgentEvent{Type: EventTypeToolEnd, Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Error: resultStr}})
				continue
			}

			startTime := time.Now()
			var streamer ToolExecutorStreaming
			if s, ok := loop.executor.(ToolExecutorStreaming); ok {
				streamer = s
			}

			result, err := func() (interface{}, error) {
				timeout := time.Duration(cfg.ToolExecTimeoutSec) * time.Second
				if tc.Name == "delegate_to" || tc.Name == "delegate_async" {
					timeout = 30 * time.Minute
				}
				useStreaming := streamer != nil

				if timeout <= 0 {
					if useStreaming {
						return streamer.ExecuteStreaming(ctx, tc.Name, tc.Args, func(update string) {
							sendEvent(subscriber, AgentEvent{
								Type: EventTypeToolUpdate,
								Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Text: update},
							})
						})
					}
					return loop.executor.Execute(ctx, tc.Name, tc.Args)
				}

				toolCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()

				type toolResult struct {
					val interface{}
					err error
				}
				ch := make(chan toolResult, 1)
				go func() {
					var val interface{}
					var err error
					if useStreaming {
						val, err = streamer.ExecuteStreaming(toolCtx, tc.Name, tc.Args, func(update string) {
							sendEvent(subscriber, AgentEvent{
								Type: EventTypeToolUpdate,
								Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Text: update},
							})
						})
					} else {
						val, err = loop.executor.Execute(toolCtx, tc.Name, tc.Args)
					}
					ch <- toolResult{val, err}
				}()
				select {
				case r := <-ch:
					return r.val, r.err
				case <-toolCtx.Done():
					return nil, fmt.Errorf("tool '%s' timed out after %ds", tc.Name, cfg.ToolExecTimeoutSec)
				}
			}()
			elapsed := time.Since(startTime)

			if err != nil && classifyError(err) == ErrorTransient {
				backoff := time.Duration(min(500*time.Millisecond*time.Duration(1<<min(failCounts[tc.Name], 4)), 10*time.Second))
				loop.logger.Warn("Transient error, retrying", zap.String("tool", tc.Name),
					zap.Duration("backoff", backoff), zap.Error(err))
				time.Sleep(backoff)
				retryCtx, retryCancel := context.WithTimeout(ctx, time.Duration(cfg.ToolExecTimeoutSec)*time.Second)
				result, err = loop.executor.Execute(retryCtx, tc.Name, tc.Args)
				retryCancel()
				elapsed = time.Since(startTime)
			}

			var resultStr string
			if err != nil {
				failCounts[tc.Name]++
				consecutiveTotalFails++
				resultStr = fmt.Sprintf("<tool_use_error>%s</tool_use_error>", err.Error())
				if consecutiveTotalFails >= maxConsecutiveTotalFails {
					resultStr += "\n\n[SYSTEM: Too many consecutive failures. Call yield_artifact{\"output\":\"Failed: ...\"} to end.]"
				}
				loop.logger.Error("AGENT TOOL ERROR", zap.String("tool", tc.Name),
					zap.Duration("elapsed", elapsed), zap.Int("failCount", failCounts[tc.Name]),
					zap.Int("totalConsecFails", consecutiveTotalFails), zap.Error(err))
			} else {
				delete(failCounts, tc.Name)
				consecutiveTotalFails = 0
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
				if len(resultStr) > cfg.ToolResultMaxChars {
					resultStr = resultStr[:cfg.ToolResultMaxChars] + "...[truncated]"
				}
				loop.logger.Info("AGENT TOOL RESULT", zap.String("tool", tc.Name),
					zap.Duration("elapsed", elapsed), zap.Int("resultLen", len(resultStr)))
			}

			sendEvent(subscriber, AgentEvent{
				Type: EventTypeToolEnd,
				Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Result: result,
					Error: func() string { if err != nil { return err.Error() }; return "" }()},
			})

			toolResults = append(toolResults, ToolResult{ToolCallID: tcr.ID, ToolName: tc.Name, Content: resultStr})

			if cycleDetector.Record(tc.Name, tc.Args, resultStr, round) {
				loop.logger.Warn("Cycle detected, injecting nudge", zap.String("tool", tc.Name), zap.Int("round", round))
				toolResults = append(toolResults, ToolResult{
					ToolCallID: "__cycle_nudge__", ToolName: "system", Content: CycleNudge(),
				})
			}

			// Periodic reflection every 3 tool calls (Agent-S pattern: check progress without prescribing fixes)
			if round%3 == 0 {
				toolResults = append(toolResults, ToolResult{
					ToolCallID: "__reflect_nudge__", ToolName: "system",
					Content: "REFLECT: Review the last few actions. Did they make progress toward your goal? If not, change your approach. Are you close to done? If so, stop using tools and write your final answer.",
				})
			}

			if tc.Name == "yield_artifact" {
				if output, _ := tc.Args["output"].(string); output != "" {
					sendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: output}})
				}
				loop.logger.Info("Sub-agent yielded artifact, terminating loop", zap.String("tool", tc.Name), zap.Int("round", round))
				inputTokens, outputTokens := loop.session.TokenCounts()
				sendEvent(subscriber, AgentEvent{
					Type: EventTypeAgentEnd,
					Data: AgentEventData{Input: float64(inputTokens), Output: float64(outputTokens), Model: "llama-server/gemma-4-26b"},
				})
				return nil
			}
		}

		if len(delegateCalls) > 0 {
			loop.logger.Info("Executing delegate_to calls concurrently", zap.Int("count", len(delegateCalls)), zap.Int("round", round))
			delegateResults := loop.executeDelegatesConcurrently(ctx, delegateCalls, subscriber, failCounts, round)
			toolResults = append(toolResults, delegateResults...)
		}

		// Phase 4: Goal nudge
		var goalReminder string
		if maxRounds > 0 {
			budgetWarning := round >= int(float64(maxRounds)*0.75) && round < maxRounds
			goalReminder, _ = RenderTemplate("goal_nudge", GoalNudgeData{
				Round: round, MaxRounds: maxRounds, StepsLeft: maxRounds - round, BudgetWarning: budgetWarning,
			})
		}

		if ste, ok := loop.executor.(*SandboxToolExecutor); ok && ste.Creds != nil {
			for i := range toolResults {
				toolResults[i].Content = ste.Creds.Redact(toolResults[i].Content)
			}
		}

		// Context compaction
		if loop.consecutiveCompactionFailures < cfg.MaxCompactionFailures {
			needMicro, needFull := ShouldCompact(loop.session)
			if needFull {
				if err := loop.compactSession(subscriber); err != nil {
					loop.consecutiveCompactionFailures++
					loop.logger.Warn("Full compaction failed", zap.Error(err), zap.Int("consecutiveFailures", loop.consecutiveCompactionFailures))
				} else {
					loop.consecutiveCompactionFailures = 0
				}
			} else if needMicro {
				MicroCompactInPlace(loop.session, 4)
				if subscriber != nil {
					sendEvent(subscriber, AgentEvent{
						Type: EventTypeCompactionEnd,
						Data: AgentEventData{Result: map[string]interface{}{"type": "micro"}},
					})
				}
			}
		}

		// Phase 5: Feed tool results back
		assistantMsg := Message{
			Role:      "assistant",
			Content:   contentBuf.String(),
			ToolCalls: toolCalls,
		}

		nextCh, err := loop.session.FeedToolResults(assistantMsg, toolResults, goalReminder, opts)
		if err != nil {
			sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
			sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
			return err
		}
		chatCh = nextCh
	}
}

// Abort cancels the running agent loop.
func (loop *AgentLoop) Abort() {
	loop.mu.Lock()
	defer loop.mu.Unlock()
	if loop.cancel != nil {
		loop.cancel()
	}
}

// Close releases the session and frees VRAM.
func (loop *AgentLoop) Close() error {
	if loop.cancel != nil {
		loop.cancel()
	}
	return loop.session.Close()
}

// IsRunning returns whether the loop is currently active.
func (loop *AgentLoop) IsRunning() bool {
	loop.mu.Lock()
	defer loop.mu.Unlock()
	return loop.running
}

// SetTranscriptSaver configures the transcript saver for pre-compaction snapshots.
func (loop *AgentLoop) SetTranscriptSaver(saver TranscriptSaver) {
	loop.saver = saver
}

// Session returns the underlying session for inspection.
func (loop *AgentLoop) Session() *Session {
	return loop.session
}

// compactSession performs extractive compaction by creating a new session
// with compacted history, freeing the old KV cache.
func (loop *AgentLoop) compactSession(subscriber chan<- AgentEvent) error {
	messages := loop.session.Messages()
	systemPrompt := loop.config.SystemPrompt
	compCfg := loop.config.Compaction

	if subscriber != nil {
		used, capacity := loop.session.ContextUsage()
		sendEvent(subscriber, AgentEvent{
			Type: EventTypeCompactionStart,
			Data: AgentEventData{
				Result: map[string]interface{}{"type": "full", "messageCount": len(messages), "usedTokens": used, "capacity": capacity},
			},
		})
	}

	if loop.saver != nil {
		if msgsJSON, err := json.Marshal(messages); err == nil {
			used, _ := loop.session.ContextUsage()
			loop.saver.SaveTranscript(msgsJSON, "full_compaction", used)
		}
	}

	newMessages := CompactWithSummary(messages, systemPrompt, loop.engine, compCfg.KeepLastTurns)

	loop.logger.Info("Compacting session", zap.Int("oldMessages", len(messages)), zap.Int("newMessages", len(newMessages)))

	if err := loop.session.Close(); err != nil {
		return fmt.Errorf("failed to close old session: %w", err)
	}

	newSession, err := loop.engine.NewSession(loop.config.ContextSize)
	if err != nil {
		return fmt.Errorf("failed to create new session: %w", err)
	}

	newSession.SetMessages(newMessages)
	newSession.SetTools(loop.config.Tools)

	loop.session = newSession
	loop.logger.Info("Session compaction complete")

	if subscriber != nil {
		sendEvent(subscriber, AgentEvent{
			Type: EventTypeCompactionEnd,
			Data: AgentEventData{Result: map[string]interface{}{"type": "full", "oldMessages": len(messages), "newMessages": len(newMessages)}},
		})
	}

	return nil
}

// ── Delegate concurrency ─────────────────────────────────────────────

// executeDelegatesConcurrently runs multiple delegate_to calls in parallel goroutines.
func (loop *AgentLoop) executeDelegatesConcurrently(
	ctx context.Context,
	delegateCalls []ToolCallResponse,
	subscriber chan<- AgentEvent,
	failCounts map[string]int,
	round int,
) []ToolResult {
	maxConcurrent := cfg.MaxConcurrentAgents - 1
	if loop.engine.IsCloud() {
		maxConcurrent = len(delegateCalls)
	}
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	results := make([]ToolResult, len(delegateCalls))

	for i, tcr := range delegateCalls {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, call ToolCallResponse) {
			defer wg.Done()
			defer func() { <-sem }()

			tc := call.ToToolCall()

			sendEvent(subscriber, AgentEvent{
				Type: EventTypeToolStart,
				Data: AgentEventData{ToolName: tc.Name, ToolArgs: tc.Args, ToolID: tc.ID},
			})

			argsJSON, _ := json.Marshal(tc.Args)
			loop.logger.Info("CONCURRENT DELEGATE", zap.Int("round", round), zap.Int("index", idx), zap.String("args", string(argsJSON)))

			var result interface{}
			var err error
			timeout := time.Duration(cfg.ToolExecTimeoutSec) * time.Second
			if tc.Name == "delegate_to" || tc.Name == "delegate_async" {
				timeout = 30 * time.Minute
			}
			if timeout <= 0 {
				result, err = loop.executor.Execute(ctx, tc.Name, tc.Args)
			} else {
				toolCtx, cancel := context.WithTimeout(ctx, timeout)
				result, err = loop.executor.Execute(toolCtx, tc.Name, tc.Args)
				cancel()
			}

			var resultStr string
			if err != nil {
				failCounts[tc.Name]++
				resultStr = fmt.Sprintf("<tool_use_error>%s</tool_use_error>", err.Error())
				loop.logger.Error("CONCURRENT DELEGATE ERROR", zap.Int("index", idx), zap.Error(err))
			} else {
				delete(failCounts, tc.Name)
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
				if len(resultStr) > cfg.ToolResultMaxChars {
					resultStr = resultStr[:cfg.ToolResultMaxChars] + "...[truncated]"
				}
				loop.logger.Info("CONCURRENT DELEGATE DONE", zap.Int("index", idx), zap.Int("resultLen", len(resultStr)))
			}

			sendEvent(subscriber, AgentEvent{
				Type: EventTypeToolEnd,
				Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Result: result,
					Error: func() string { if err != nil { return err.Error() }; return "" }()},
			})

			results[idx] = ToolResult{ToolCallID: call.ID, ToolName: tc.Name, Content: resultStr}
		}(i, tcr)
	}

	wg.Wait()
	return results
}

// ── Error classification ─────────────────────────────────────────────

// ErrorClass categorizes tool execution errors.
type ErrorClass int

const (
	ErrorTransient ErrorClass = iota
	ErrorPermanent
	ErrorUnknown
)

// classifyError categorizes an error for retry decisions.
func classifyError(err error) ErrorClass {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "connection") ||
		strings.Contains(msg, "reset") || strings.Contains(msg, "temporarily") ||
		strings.Contains(msg, "refused") || strings.Contains(msg, "eof") ||
		strings.Contains(msg, "context canceled") {
		return ErrorTransient
	}
	if strings.Contains(msg, "not found") || strings.Contains(msg, "denied") ||
		strings.Contains(msg, "invalid") || strings.Contains(msg, "unknown persona") ||
		strings.Contains(msg, "missing") || strings.Contains(msg, "not available") {
		return ErrorPermanent
	}
	return ErrorUnknown
}

// ── JSON extraction helpers ──────────────────────────────────────────

// extractJSONStringValue extracts a value from a loosely-formatted JSON string.
func extractJSONStringValue(raw, key string) string {
	patterns := []string{
		fmt.Sprintf(`"%s"\s*:\s*"((?:[^"\\]|\\.)*)"`, key),
		fmt.Sprintf(`%s\s*:\s*"((?:[^"\\]|\\.)*)"`, key),
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		m := re.FindStringSubmatch(raw)
		if len(m) >= 2 {
			return strings.ReplaceAll(m[1], `\"`, `"`)
		}
	}
	singlePatterns := []string{
		fmt.Sprintf(`"%s"\s*:\s*'([^']*)'`, key),
		fmt.Sprintf(`%s\s*:\s*'([^']*)'`, key),
	}
	for _, p := range singlePatterns {
		re := regexp.MustCompile(p)
		m := re.FindStringSubmatch(raw)
		if len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}

// executeHTTPRequest makes an HTTP request from the Go backend.
func (e *SandboxToolExecutor) executeHTTPRequest(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	method, _ := args["method"].(string)
	targetURL, _ := args["url"].(string)
	if method == "" {
		method = "GET"
	}
	if targetURL == "" {
		return nil, fmt.Errorf("missing 'url' argument")
	}
	timeout := 30
	if t, ok := args["timeout"]; ok {
		if f, ok := t.(float64); ok && f > 0 {
			timeout = int(f)
		}
	}
	if timeout > 120 {
		timeout = 120
	}
	var bodyReader io.Reader
	if body, ok := args["body"]; ok && body != nil {
		bodyStr, isStr := body.(string)
		if !isStr {
			bodyBytes, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to encode body: %w", err)
			}
			bodyStr = string(bodyBytes)
		}
		bodyReader = strings.NewReader(bodyStr)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if headers, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				req.Header.Set(k, vs)
			}
		}
	}
	if bodyReader != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	bodyStr := string(respBody)
	var jsonBody interface{}
	isJSON := json.Unmarshal(respBody, &jsonBody) == nil
	result := map[string]interface{}{
		"status_code": resp.StatusCode,
		"status":      resp.Status,
		"url":         resp.Request.URL.String(),
	}
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}
	result["headers"] = respHeaders
	if isJSON {
		result["body"] = jsonBody
	} else {
		if len(bodyStr) > 8000 {
			bodyStr = bodyStr[:7997] + "..."
		}
		result["body"] = bodyStr
	}
	return result, nil
}
