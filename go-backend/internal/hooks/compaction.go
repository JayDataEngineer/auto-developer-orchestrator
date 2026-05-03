package hooks

import (
	"context"
	"fmt"
	"log"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// CompactionHook monitors context size and triggers compaction when thresholds are exceeded.
type CompactionHook struct {
	logger              *log.Logger
	microThreshold      float64
	fullThreshold       float64
	keepResults         int
	maxFailures         int
	consecutiveFailures int
	session              core.Session
}

// NewCompactionHook creates a compaction hook.
// microThreshold: fraction of context at which to micro-compact (e.g., 0.55)
// fullThreshold: fraction of context at which to full compact (e.g., 0.75)
// keepResults: number of recent tool results to preserve during micro-compaction
func NewCompactionHook(s core.Session, microThreshold, fullThreshold float64, keepResults int) *CompactionHook {
	return &CompactionHook{
		logger:         log.Default(),
		session:        s,
		microThreshold: microThreshold,
		fullThreshold:  fullThreshold,
		keepResults:    keepResults,
		maxFailures:    3,
	}
}

func (h *CompactionHook) Name() string { return "compaction" }

func (h *CompactionHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	return nil
}

func (h *CompactionHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	if h.consecutiveFailures >= h.maxFailures {
		return nil, nil // circuit breaker
	}

	// Estimate context usage from session
	messages, _ := h.session.BuildContext(ctx)
	estimatedTokens := estimateTokens(messages)
	contextSize := 32768 // default

	ratio := float64(estimatedTokens) / float64(contextSize)

	if ratio >= h.fullThreshold {
		if err := h.fullCompact(ctx); err != nil {
			h.consecutiveFailures++
			h.logger.Printf("CompactionHook: full compaction failed: %v", err)
		} else {
			h.consecutiveFailures = 0
		}
	} else if ratio >= h.microThreshold {
		h.microCompact()
	}

	return nil, nil // no messages to inject
}

func (h *CompactionHook) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	return nil
}

func (h *CompactionHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	return nil
}

// microCompact replaces old tool result content with a short placeholder.
// This is cheap — no LLM call needed, no new session.
func (h *CompactionHook) microCompact() {
	keep := h.keepResults
	if keep <= 0 {
		keep = 4
	}

	truncated, err := h.session.TruncateToolResults(keep)
	if err != nil {
		h.logger.Printf("CompactionHook: micro-compact failed: %v", err)
		return
	}

	if truncated > 0 {
		h.logger.Printf("CompactionHook: micro-compact: kept %d tool results, truncated %d", keep, truncated)
	}
}

// fullCompact creates an LLM-generated summary of old messages.
func (h *CompactionHook) fullCompact(ctx context.Context) error {
	_, err := h.session.Compact(ctx, nil)
	if err != nil {
		return fmt.Errorf("full compact failed: %w", err)
	}
	h.logger.Printf("CompactionHook: full compaction triggered")
	return nil
}

// estimateTokens estimates tokens from message content.
func estimateTokens(messages []core.Message) int {
	chars := 0
	for _, m := range messages {
		chars += len(m.Content)
		chars += len(m.ReasoningContent)
		for _, tc := range m.ToolCalls {
			chars += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	return int(float64(chars) * 0.3)
}
