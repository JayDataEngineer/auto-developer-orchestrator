package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ErrMaxRounds is returned when a loop exhausts its round budget without
// the CTO returning a final stop_reason=end_turn.
var ErrMaxRounds = errors.New("agent: max rounds exhausted")

// LoopConfig is the input to a single Plan/Act/Observe loop. Constructed
// by a loop factory (see delegate.go + main.go wiring) — never directly
// from a caller outside this package.
type LoopConfig struct {
	Provider     core.LLMProvider
	Executor     core.ToolExecutor
	SystemPrompt string
	Tools        []core.OpenAITool // already filtered to the role's whitelist
	MaxRounds    int               // 0 → defaultMaxRounds
	MaxTokens    int               // 0 → provider default
	Thinking     bool              // enable Anthropic extended thinking

	// Status is an optional sink the dispatch layer wires up so pollers
	// can observe round counter + transcript tail. Nil = no status updates.
	Status *Status

	// TaskID correlates this loop's observer events to a dispatch task. Empty
	// for loops not driven by the dispatch surface (e.g. tests). Passed
	// through to ChatObserver + ToolObserver fire sites.
	TaskID string

	// Role identifies which agent is running this loop. The dispatch surface
	// stamps "cto" for the CTO loop; DelegateTool stamps the role name for
	// delegated children. Empty is normalized to "cto" in NewLoop. Forwarded
	// to observer fire sites so history can correlate events to a delegation
	// chain (which role did what).
	Role string

	// ChatObserver receives one event per non-empty assistant turn. Optional.
	ChatObserver core.ChatObserver

	// ToolObserver receives one event per in-loop tool dispatch. Optional.
	// Distinct from the MCP-server audit hook, which catches external calls.
	ToolObserver core.ToolObserver
}

// Status is the per-task progress signal read by get_task_status. Writes
// happen inside Loop.Run; reads happen from the MCP tool. Safe for
// concurrent access.
type Status struct {
	mu      sync.RWMutex
	round   int
	tail    []string
	lastErr string
}

// Round returns the current/last round number.
func (s *Status) Round() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.round
}

// Tail returns a copy of the last N assistant text messages (progress
// signal for pollers).
func (s *Status) Tail() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.tail))
	copy(out, s.tail)
	return out
}

// LastError returns the most recent error message, or empty if none.
func (s *Status) LastError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastErr
}

const (
	defaultMaxRounds     = 30
	transcriptTailMaxLen = 5
)

// Loop is one Plan/Act/Observe agent cycle. Construct via NewLoop and run
// via Run. A Loop is single-use — Run panics if called twice.
type Loop struct {
	cfg       LoopConfig
	maxRounds int

	mu       sync.Mutex
	messages []core.Message
	ran      bool
}

// NewLoop constructs a loop from config. Validates that Provider + Executor
// are non-nil; MaxRounds gets the default if unset.
func NewLoop(cfg LoopConfig) (*Loop, error) {
	if cfg.Provider == nil {
		return nil, errors.New("agent: LoopConfig.Provider is required")
	}
	if cfg.Executor == nil {
		return nil, errors.New("agent: LoopConfig.Executor is required")
	}
	if cfg.SystemPrompt == "" {
		return nil, errors.New("agent: LoopConfig.SystemPrompt is required")
	}
	max := cfg.MaxRounds
	if max <= 0 {
		max = defaultMaxRounds
	}
	if cfg.Role == "" {
		cfg.Role = "cto"
	}
	return &Loop{cfg: cfg, maxRounds: max}, nil
}

// Run executes the Plan/Act/Observe cycle against the supplied task. The
// returned string is the CTO's final text content when finish=end_turn.
// ErrMaxRounds indicates the budget was exhausted (caller surfaces as failed).
func (l *Loop) Run(ctx context.Context, task string) (string, error) {
	l.mu.Lock()
	if l.ran {
		l.mu.Unlock()
		panic("agent: Loop.Run called twice")
	}
	l.ran = true
	l.messages = []core.Message{
		{Role: string(core.RoleSystem), Content: l.cfg.SystemPrompt},
		{Role: string(core.RoleUser), Content: task},
	}
	l.mu.Unlock()

	for round := 1; round <= l.maxRounds; round++ {
		l.setStatusRound(round)

		// ── Plan: ask the provider what to do next ────────────────────
		events, err := l.cfg.Provider.StreamChat(ctx, l.snapshotMessages(),
			l.cfg.Tools, core.GenerateOptions{
				MaxTokens: l.cfg.MaxTokens,
				Thinking:  l.cfg.Thinking,
			})
		if err != nil {
			l.setStatusError(err.Error())
			return "", fmt.Errorf("agent round %d: stream chat: %w", round, err)
		}

		content, thinking, toolCalls, finish, streamErr := drainStream(events)
		if streamErr != nil {
			l.setStatusError(streamErr.Error())
			return "", fmt.Errorf("agent round %d: stream: %w", round, streamErr)
		}

		// Append the assistant turn before any tool dispatch — the model
		// expects to see its own utterance, including the tool_use IDs, in
		// the next round's input.
		l.appendMessage(core.Message{
			Role:             string(core.RoleAssistant),
			Content:          content,
			ReasoningContent: thinking,
			ToolCalls:        toolCalls,
		})

		if content != "" {
			l.appendTranscriptTail(content)
			l.fireAssistantMessage(ctx, round, content)
		}

		// ── Stop conditions ────────────────────────────────────────────
		if len(toolCalls) == 0 || finish == core.FinishStop {
			return content, nil
		}

		// ── Act + Observe: dispatch tools in parallel, append results ─
		if err := l.dispatchTools(ctx, round, toolCalls); err != nil {
			l.setStatusError(err.Error())
			return "", fmt.Errorf("agent round %d: dispatch: %w", round, err)
		}
	}

	l.setStatusError(ErrMaxRounds.Error())
	return "", ErrMaxRounds
}

// snapshotMessages returns the current message slice under the loop lock.
// The provider gets a stable view; concurrent append waits for it to
// return before mutating.
func (l *Loop) snapshotMessages() []core.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.Message, len(l.messages))
	copy(out, l.messages)
	return out
}

// appendMessage appends a new message to the loop's history.
func (l *Loop) appendMessage(m core.Message) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, m)
}

// appendTranscriptTail keeps the last N assistant text messages for
// progress polling. The slice is bounded.
func (l *Loop) appendTranscriptTail(s string) {
	if l.cfg.Status == nil {
		return
	}
	l.cfg.Status.mu.Lock()
	defer l.cfg.Status.mu.Unlock()
	l.cfg.Status.tail = append(l.cfg.Status.tail, s)
	if len(l.cfg.Status.tail) > transcriptTailMaxLen {
		l.cfg.Status.tail = l.cfg.Status.tail[len(l.cfg.Status.tail)-transcriptTailMaxLen:]
	}
}

func (l *Loop) setStatusRound(r int) {
	if l.cfg.Status == nil {
		return
	}
	l.cfg.Status.mu.Lock()
	defer l.cfg.Status.mu.Unlock()
	l.cfg.Status.round = r
}

func (l *Loop) setStatusError(msg string) {
	if l.cfg.Status == nil {
		return
	}
	l.cfg.Status.mu.Lock()
	defer l.cfg.Status.mu.Unlock()
	l.cfg.Status.lastErr = msg
}

// dispatchTools fans out tool calls in parallel. Each tool result is
// appended to the loop's history as a tool-role message. If any tool
// returns an error, the error itself becomes the tool's result content
// (the model can see what went wrong and recover) — the loop only fails
// fast if ctx is cancelled.
//
// `round` is the 1-based Plan/Act/Observe cycle that produced these calls.
// Forwarded to ToolObserver so recorded events correlate to their round.
func (l *Loop) dispatchTools(ctx context.Context, round int, calls []core.ToolCallResponse) error {
	results := make([]core.Message, len(calls))
	g, gctx := errgroup.WithContext(ctx)
	for i, c := range calls {
		g.Go(func() error {
			args := decodeArgs(c.Function.Arguments)
			start := time.Now()
			res, err := l.cfg.Executor.Execute(gctx, c.Function.Name, args)
			duration := time.Since(start)
			var body string
			if err != nil {
				// Surface tool error to the model — don't kill the loop.
				// Tool errors are normal: file not found, command failed, etc.
				body = fmt.Sprintf("[error] %s", err)
			} else {
				body = renderResult(res)
			}
			l.fireToolCall(ctx, round, c.Function.Name, c.Function.Arguments, body, duration, err)
			results[i] = core.Message{
				Role:       string(core.RoleTool),
				Content:    body,
				ToolCallID: c.ID,
				Name:       c.Function.Name,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	l.mu.Lock()
	l.messages = append(l.messages, results...)
	l.mu.Unlock()
	return nil
}

// fireAssistantMessage forwards the turn's content to the chat observer
// (if wired). No-op when ChatObserver is nil — the common case for tests
// and any loop that isn't driven by the dispatch surface.
func (l *Loop) fireAssistantMessage(ctx context.Context, round int, content string) {
	if l.cfg.ChatObserver == nil {
		return
	}
	l.cfg.ChatObserver.OnAssistantMessage(ctx, l.cfg.TaskID, l.cfg.Role, round, content)
}

// fireToolCall forwards the call's metadata to the tool observer (if wired).
// No-op when ToolObserver is nil.
func (l *Loop) fireToolCall(ctx context.Context, round int, name, argsRaw, result string, duration time.Duration, err error) {
	if l.cfg.ToolObserver == nil {
		return
	}
	l.cfg.ToolObserver.OnToolCall(ctx, l.cfg.TaskID, l.cfg.Role, round, name, argsRaw, result, duration, err)
}

// drainStream consumes the provider's event channel end-to-end and returns
// the accumulated assistant message parts. Tool-call fragments are stitched
// by Index — same pattern as OpenAI's chunked tool_calls.
//
// finish defaults to FinishStop if the stream ended without a message_delta
// (defensive — providers should always send one, but we don't want to loop
// forever if they don't).
func drainStream(events <-chan core.ChatEvent) (content, thinking string, calls []core.ToolCallResponse, finish core.FinishReason, err error) {
	finish = core.FinishStop
	accum := map[int]*core.ToolCallResponse{}
	var keys []int

	for ev := range events {
		switch ev.Type {
		case core.ChatEventContent:
			content += ev.Content
		case core.ChatEventThinking:
			thinking += ev.Content
		case core.ChatEventToolChunk:
			for _, d := range ev.Deltas {
				slot, ok := accum[d.Index]
				if !ok {
					slot = &core.ToolCallResponse{
						ID:   d.ID,
						Type: "function",
						Function: core.FunctionCallData{
							Name: d.Function.Name,
						},
					}
					accum[d.Index] = slot
					keys = append(keys, d.Index)
				}
				if d.ID != "" {
					slot.ID = d.ID
				}
				if d.Function.Name != "" {
					slot.Function.Name = d.Function.Name
				}
				slot.Function.Arguments += d.Function.Arguments
			}
		case core.ChatEventDone:
			if ev.Finish != "" {
				finish = ev.Finish
			}
		case core.ChatEventError:
			if ev.Err != nil {
				err = ev.Err
			}
			return
		}
	}

	// Sort tool calls by index for stable order across rounds.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	calls = make([]core.ToolCallResponse, 0, len(keys))
	for _, k := range keys {
		calls = append(calls, *accum[k])
	}
	return
}

// decodeArgs parses a JSON-encoded argument string into a map. Returns an
// empty map if input is empty or malformed (so the tool gets a known
// shape rather than nil).
func decodeArgs(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{"_raw": raw}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

// renderResult flattens a tool result into the string the model sees in a
// tool_result block. Tools return one of: string, []byte, or JSON-able
// map/slice. We JSON-encode structured types so the model can parse them.
func renderResult(res any) string {
	switch v := res.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// messagesForTest exposes the current message slice for tests. Production
// code never needs this — the provider reads it via StreamChat.
func (l *Loop) messagesForTest() []core.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.Message, len(l.messages))
	copy(out, l.messages)
	return out
}

// ensure strings is referenced even if all string utilities move out —
// keeps go fmt from removing the import during partial edits.
var _ = strings.TrimSpace
