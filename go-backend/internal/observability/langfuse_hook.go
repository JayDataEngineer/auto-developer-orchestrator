package observability

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// LangfuseHook records agent runs, LLM generations, and tool executions
// as Langfuse traces, spans, and generations.
// Implements core.LoopHook — add it to orchestrator.Config.ExtraHooks.
type LangfuseHook struct {
	client    *LangfuseClient
	modelName string
	cfg       TraceConfig

	// per-run state
	trace   *TraceHandle
	startAt time.Time

	// per-turn state (generation tracking)
	turnStart time.Time
	prevModel string
}

// NewLangfuseHook creates a new Langfuse tracing hook.
// If client is nil, all methods are no-ops.
func NewLangfuseHook(client *LangfuseClient, modelName string, cfg TraceConfig) *LangfuseHook {
	return &LangfuseHook{
		client:    client,
		modelName: modelName,
		cfg:       cfg,
	}
}

func (h *LangfuseHook) Name() string { return "langfuse" }

func (h *LangfuseHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	if h.client == nil || !h.client.Enabled() {
		return nil
	}

	h.startAt = state.StartedAt
	if h.startAt.IsZero() {
		h.startAt = time.Now()
	}

	// Build TraceConfig from hook config + runtime state
	cfg := h.cfg
	cfg.SessionID = state.SessionID
	if cfg.UserID == "" {
		cfg.UserID = state.SandboxID
	}

	h.client.TraceRun("orchestrator-run", cfg, func(th *TraceHandle) {
		h.trace = th
	})

	h.turnStart = time.Time{}
	h.prevModel = ""

	log.Printf("Langfuse trace started: session=%s project=%s", state.SessionID, h.cfg.Project)
	return nil
}

func (h *LangfuseHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	// Record generation for the PREVIOUS turn (now complete).
	// When OnBeforeTurn fires for round N, state.TurnInputTokens holds
	// the tokens from round N-1 (set by loop.go after that turn's stream).
	if state.Round > 0 && h.trace != nil && !h.turnStart.IsZero() {
		model := h.prevModel
		if model == "" {
			model = h.modelName
		}
		end := time.Now()
		h.trace.Generation(
			fmt.Sprintf("turn-%d", state.Round-1),
			model,
			h.turnStart,
			end,
			state.TurnInputTokens,
			state.TurnOutputTokens,
			state.TurnInputTokens+state.TurnOutputTokens,
		)
	}

	// Start timing THIS turn
	h.turnStart = time.Now()
	h.prevModel = state.TurnModel
	return nil, nil
}

func (h *LangfuseHook) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	if h.trace == nil {
		return nil
	}

	end := time.Now()
	start := h.turnStart
	if start.IsZero() {
		start = end.Add(-time.Second)
	}

	output := map[string]any{
		"round": state.Round,
	}
	if err != nil {
		output["error"] = err.Error()
	} else if len(result) > 500 {
		output["result_preview"] = result[:500]
	} else {
		output["result"] = result
	}

	h.trace.Span("tool:"+toolName, start, end, args, output)
	return nil
}

func (h *LangfuseHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	if h.trace == nil {
		return nil
	}

	// Record the final turn's generation
	if !h.turnStart.IsZero() {
		model := h.prevModel
		if model == "" {
			model = h.modelName
		}
		h.trace.Generation(
			fmt.Sprintf("turn-%d-final", state.Round),
			model,
			h.turnStart,
			time.Now(),
			state.TurnInputTokens,
			state.TurnOutputTokens,
			state.TurnInputTokens+state.TurnOutputTokens,
		)
	}

	// Flush all buffered events
	h.client.Flush()

	log.Printf("Langfuse trace closed: session=%s rounds=%d tokens_in=%d tokens_out=%d",
		state.SessionID, state.Round, state.TotalInputTokens, state.TotalOutputTokens)
	return nil
}
