package context

import (
	"context"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// EstimateTokens provides a rough token count from messages.
// Uses the same char-to-token ratio as the original CompactionHook.
func EstimateTokens(messages []core.Message) int {
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

// EstimateTokensFromUsage uses real API token usage when available,
// falling back to the char-based heuristic. This mirrors Claude Code's
// tokenCountWithEstimation: take the last real usage and add rough
// estimates for any messages appended since.
func EstimateTokensFromUsage(ctx context.Context, messages []core.Message) int {
	if usage, ok := ctx.Value(core.TokenUsageKey{}).(*core.TokenUsage); ok && usage != nil && usage.PromptTokens > 0 {
		base := usage.PromptTokens
		// Estimate tokens for messages appended after the last API call
		if usage.TrailingMessages > 0 && usage.TrailingMessages < len(messages) {
			trailing := messages[len(messages)-usage.TrailingMessages:]
			base += EstimateTokens(trailing)
		}
		return base
	}
	return EstimateTokens(messages)
}

// estimateMessageTokens estimates tokens for a single message.
// Used by findCutPoint for per-message token accumulation.
func estimateMessageTokens(msg core.Message) int {
	chars := len(msg.Content) + len(msg.ReasoningContent)
	for _, tc := range msg.ToolCalls {
		chars += len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	return int(float64(chars) * 0.3)
}
