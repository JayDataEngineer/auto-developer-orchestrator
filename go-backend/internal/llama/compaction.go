package llama

import (
	"fmt"
	"strings"
)

// CompactionConfig controls when and how context compaction happens.
type CompactionConfig struct {
	TriggerAfterTurns int // compact after this many model+tool turn pairs (default: 8)
	KeepLastTurns     int // preserve this many full turns at the end (default: 3)
	MaxCompactChars   int // max chars for the compacted summary block (default: 2000)
}

// DefaultCompactionConfig returns sensible defaults from ModelConfig.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		TriggerAfterTurns: cfg.CompactionTriggerTurns,
		KeepLastTurns:     cfg.CompactionKeepTurns,
		MaxCompactChars:   cfg.CompactionMaxChars,
	}
}

// SubAgentCompactionConfig returns compaction settings for ephemeral sub-agents.
// More aggressive compaction since sub-agents have smaller context (8K).
func SubAgentCompactionConfig() CompactionConfig {
	return CompactionConfig{
		TriggerAfterTurns: 4,  // compact early
		KeepLastTurns:     2,  // keep fewer turns
		MaxCompactChars:   1500,
	}
}

// ShouldCompactMessages returns true if the message history is long enough to warrant compaction.

// ── Message-based compaction ───────────────────────────────────────

// ShouldCompactMessages returns true if the message history is long enough to warrant compaction.
// Counts assistant→tool message pairs.
func ShouldCompactMessages(messages []Message, cfg CompactionConfig) bool {
	pairs := countMessagePairs(messages)
	return pairs >= cfg.TriggerAfterTurns
}

// countMessagePairs counts assistant→tool message pairs.
func countMessagePairs(messages []Message) int {
	pairs := 0
	for i := 1; i < len(messages); i++ {
		if messages[i-1].Role == "assistant" && messages[i].Role == "tool" {
			pairs++
		}
	}
	return pairs
}

// CompactMessages performs extractive compaction on message-based history.
// Returns a new []Message suitable for setting on a fresh session:
//
//	[system, user(original_task), assistant(compacted_summary), user("Continue"), ...lastNMessages]
func CompactMessages(messages []Message, systemPrompt string, cfg CompactionConfig) []Message {
	if len(messages) < 4 {
		return messages
	}

	// Find original user task
	var originalTask string
	for _, m := range messages {
		if m.Role == "user" {
			originalTask = m.Content
			break
		}
	}
	if originalTask == "" {
		return messages
	}

	// Split: [system, user_task, ...middle..., ...lastN...]
	turnPairStart := 2
	lastNStart := len(messages) - cfg.KeepLastTurns*4 // each round ≈ 4 messages (assistant + tool + user goal)
	if lastNStart <= turnPairStart {
		return messages
	}

	// Build compacted summary from middle messages
	var compacted strings.Builder
	compacted.WriteString("[COMPACTED HISTORY — summary of previous actions]\n")

	for i := turnPairStart; i < lastNStart; i++ {
		m := messages[i]
		switch m.Role {
		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					argsSummary := tc.Function.Arguments
					if len(argsSummary) > 80 {
						argsSummary = argsSummary[:80] + "..."
					}
					compacted.WriteString(fmt.Sprintf("Called %s(%s)\n", tc.Function.Name, argsSummary))
				}
			} else {
				text := strings.TrimSpace(m.Content)
				if len(text) > 100 {
					text = text[:100] + "..."
				}
				if text != "" {
					compacted.WriteString("Responded: " + text + "\n")
				}
			}
		case "tool":
			result := m.Content
			if len(result) > 150 {
				result = result[:150] + "..."
			}
			compacted.WriteString("→ " + result + "\n")
		}
	}

	summary := compacted.String()
	if len(summary) > cfg.MaxCompactChars {
		summary = summary[:cfg.MaxCompactChars] + "\n...[further history omitted]"
	}

	// Build new messages
	var newMessages []Message
	newMessages = append(newMessages, Message{Role: "system", Content: systemPrompt})
	newMessages = append(newMessages, Message{Role: "user", Content: originalTask})
	newMessages = append(newMessages, Message{Role: "assistant", Content: summary})
	newMessages = append(newMessages, Message{
		Role:    "user",
		Content: "The above is a compacted summary of previous actions. Continue from where you left off.",
	})

	for i := lastNStart; i < len(messages); i++ {
		newMessages = append(newMessages, messages[i])
	}

	return newMessages
}
