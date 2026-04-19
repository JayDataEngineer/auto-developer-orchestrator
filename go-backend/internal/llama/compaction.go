package llama

import (
	"encoding/json"
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

// ShouldCompact returns true if the history is long enough to warrant compaction.
// A "turn pair" is a model response followed by a user/tool result.
func ShouldCompact(history []Turn, cfg CompactionConfig) bool {
	pairs := countTurnPairs(history)
	return pairs >= cfg.TriggerAfterTurns
}

// countTurnPairs counts model→user turn pairs in the history.
func countTurnPairs(history []Turn) int {
	pairs := 0
	for i := 1; i < len(history); i++ {
		if history[i-1].Role == "model" && history[i].Role == "user" {
			pairs++
		}
	}
	return pairs
}

// CompactHistory performs extractive compaction on session history.
// No LLM call needed — we extract tool calls and compress tool results.
//
// Returns a new []Turn suitable for replaying into a fresh session:
//   [system, user(original_task), model(compacted_summary), user("Continue"), ...lastNTurns]
func CompactHistory(history []Turn, systemPrompt string, cfg CompactionConfig) []Turn {
	if len(history) < 4 {
		return history // not enough to compact
	}

	// Find the original user task (first user turn after system)
	var originalTask string
	for _, t := range history {
		if t.Role == "user" {
			originalTask = t.Content
			break
		}
	}
	if originalTask == "" {
		return history
	}

	// Split history: [system, user_task, ...middle_turns..., ...lastNTurns]
	// The "middle" section gets compacted.
	turnPairStart := 2 // index after system + first user
	lastNStart := len(history) - cfg.KeepLastTurns*2 // each pair = 2 turns
	if lastNStart <= turnPairStart {
		return history // not enough middle turns to compact
	}

	// Build compacted summary from middle turns
	var compacted strings.Builder
	compacted.WriteString("[COMPACTED HISTORY — summary of previous actions]\n")

	for i := turnPairStart; i < lastNStart; i++ {
		t := history[i]
		switch t.Role {
		case "model":
			// Extract only tool calls from model output, drop explanatory text
			toolCalls, _ := ParseToolCalls(t.Content)
			if len(toolCalls) > 0 {
				for _, tc := range toolCalls {
					compacted.WriteString(fmt.Sprintf("Called %s(", tc.Name))
					// Summarize args in one line
					argsJSON, _ := jsonMarshalOneLine(tc.Args)
					if len(argsJSON) > 80 {
						argsJSON = argsJSON[:80] + "..."
					}
					compacted.WriteString(argsJSON)
					compacted.WriteString(")")
				}
			} else {
				// No tool calls — was a text-only response, keep first 100 chars
				text := strings.TrimSpace(t.Content)
				text = stripSpecialTokens(text)
				if len(text) > 100 {
					text = text[:100] + "..."
				}
				if text != "" {
					compacted.WriteString("Responded: " + text)
				}
			}
			compacted.WriteString("\n")
		case "user":
			// Tool result — first 150 chars
			result := t.Content
			if len(result) > 150 {
				result = result[:150] + "..."
			}
			compacted.WriteString("→ " + result + "\n")
		}
	}

	// Truncate compacted block if too long
	summary := compacted.String()
	if len(summary) > cfg.MaxCompactChars {
		summary = summary[:cfg.MaxCompactChars] + "\n...[further history omitted]"
	}

	// Build new history
	var newHistory []Turn

	// System prompt
	newHistory = append(newHistory, Turn{Role: "system", Content: systemPrompt})

	// Original user task
	newHistory = append(newHistory, Turn{Role: "user", Content: originalTask})

	// Compacted summary as a model response
	newHistory = append(newHistory, Turn{
		Role:    "model",
		Content: summary,
	})

	// "Continue" user message
	newHistory = append(newHistory, Turn{
		Role: "user",
		Content: "The above is a compacted summary of previous actions. Continue from where you left off. " +
			"The original task is still active. Call the next tool.",
	})

	// Last N full turns (preserve context for current task)
	for i := lastNStart; i < len(history); i++ {
		newHistory = append(newHistory, history[i])
	}

	return newHistory
}

// stripSpecialTokens removes Gemma 4 special tokens from text.
func stripSpecialTokens(text string) string {
	text = strings.ReplaceAll(text, "<|channel|>thought", "")
	text = strings.ReplaceAll(text, "<|channel>thought", "")
	text = strings.ReplaceAll(text, "<|channel|>", "")
	text = strings.ReplaceAll(text, "<|channel>", "")
	text = strings.ReplaceAll(text, "<|tool_call>", "")
	text = strings.ReplaceAll(text, "<tool_call|>", "")
	text = strings.ReplaceAll(text, "<|end|>", "")
	text = strings.ReplaceAll(text, "<end_of_turn>", "")
	return strings.TrimSpace(text)
}

// jsonMarshalOneLine marshals to a single-line JSON string.
func jsonMarshalOneLine(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
