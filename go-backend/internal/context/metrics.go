package context

import "github.com/auto-developer-orchestrator/backend/internal/core"

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
