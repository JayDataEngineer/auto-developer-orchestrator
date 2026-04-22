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

// ── Size-based compaction (replaces turn-based) ────────────────────

// ShouldCompact returns (needMicro, needFull) based on context fill level.
// Uses actual token counts from the API response — not turn counts.
func ShouldCompact(session *Session) (bool, bool) {
	used, capacity := session.ContextUsage()
	if capacity == 0 || used == 0 {
		return false, false
	}
	ratio := float64(used) / float64(capacity)
	return ratio >= cfg.MicroCompactThreshold, ratio >= cfg.FullCompactThreshold
}

// ── Micro-compact (clear old tool results in-place) ──────────────

// MicroCompactInPlace clears old tool result content in the session's messages.
// Keeps the last keepResults tool results intact so the model has recent context.
// This is fast and cheap — no LLM call, no new session needed.
func MicroCompactInPlace(session *Session, keepResults int) {
	msgs := session.Messages()
	if keepResults <= 0 {
		keepResults = 4
	}

	// Count tool results from the end
	toolResultCount := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "tool" {
			toolResultCount++
		}
	}
	if toolResultCount <= keepResults {
		return // nothing to clear
	}

	cleared := 0
	kept := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "tool" {
			continue
		}
		if kept < keepResults {
			kept++
		} else {
			origLen := len(msgs[i].Content)
			msgs[i].Content = fmt.Sprintf("[Tool result cleared — was %d chars]", origLen)
			cleared++
		}
	}

	if cleared > 0 {
		session.SetMessages(msgs)
	}
}

// ── Message-based compaction ───────────────────────────────────────

// ShouldCompactMessages returns true if the message history is long enough to warrant compaction.
// Deprecated: use ShouldCompact(session) for size-based checking.
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

// ── LLM-based summarization ──────────────────────────────────────

const compactionSummaryPrompt = `You are summarizing an AI agent's conversation to free up context space.
The summary will replace older messages so the agent can continue working.

Create a structured summary with these sections:

1. **Original Task**: What was the user asking?
2. **Key Decisions**: Important choices made and why
3. **Tools Used**: Which tools were called and their outcomes (brief)
4. **Errors Encountered**: What went wrong and how it was fixed
5. **Current State**: Where are we now in the task?
6. **Pending Items**: What remains to be done?

Be concise. Focus on information needed to continue the task without losing critical context.
Omit code details unless they affect future decisions.`

// CompactWithSummary uses the model to generate a structured summary of conversation history.
// Falls back to extractive CompactMessages on failure.
func CompactWithSummary(messages []Message, systemPrompt string, engine *HTTPEngine, keepLast int) []Message {
	compCfg := CompactionConfig{
		KeepLastTurns:   keepLast,
		MaxCompactChars: 4000,
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
		return CompactMessages(messages, systemPrompt, compCfg)
	}

	// Determine which messages to summarize (middle portion)
	turnPairStart := 2
	lastNStart := len(messages) - keepLast*4
	if lastNStart <= turnPairStart {
		return messages
	}

	// Build a text representation of the middle messages for the summarizer
	var historyText strings.Builder
	for i := turnPairStart; i < lastNStart; i++ {
		m := messages[i]
		switch m.Role {
		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					args := tc.Function.Arguments
					if len(args) > 200 {
						args = args[:200] + "..."
					}
					fmt.Fprintf(&historyText, "Assistant called %s(%s)\n", tc.Function.Name, args)
				}
			}
			if m.Content != "" {
				text := m.Content
				if len(text) > 300 {
					text = text[:300] + "..."
				}
				fmt.Fprintf(&historyText, "Assistant: %s\n", text)
			}
		case "tool":
			result := m.Content
			if len(result) > 200 {
				result = result[:200] + "..."
			}
			fmt.Fprintf(&historyText, "Tool result (%s): %s\n", m.Name, result)
		case "user":
			fmt.Fprintf(&historyText, "User: %s\n", m.Content)
		}
	}

	// Call the model to generate a summary (use a temporary small session)
	summaryReq := fmt.Sprintf("%s\n\nConversation to summarize:\n%s", compactionSummaryPrompt, historyText.String())

	// Try to generate summary via the engine
	tempSession, err := engine.NewSession(4096)
	if err != nil {
		// Fall back to extractive
		return CompactMessages(messages, systemPrompt, compCfg)
	}
	defer tempSession.Close()

	ch, err := tempSession.ChatWithTools(
		"You are a conversation summarizer. Summarize concisely.",
		summaryReq,
		nil, // no tools
		GenerateOptions{MaxTokens: 1024, Temperature: 0.3},
	)
	if err != nil {
		return CompactMessages(messages, systemPrompt, compCfg)
	}

	var summary strings.Builder
	for evt := range ch {
		if evt.Type == ChatEventContent {
			summary.WriteString(evt.Content)
		}
		if evt.Type == ChatEventError {
			return CompactMessages(messages, systemPrompt, compCfg)
		}
	}

	summaryText := summary.String()
	if summaryText == "" {
		return CompactMessages(messages, systemPrompt, compCfg)
	}

	// Build new messages with the LLM-generated summary
	var newMessages []Message
	newMessages = append(newMessages, Message{Role: "system", Content: systemPrompt})
	newMessages = append(newMessages, Message{Role: "user", Content: originalTask})
	newMessages = append(newMessages, Message{Role: "assistant", Content: "[COMPACTED HISTORY]\n" + summaryText})
	newMessages = append(newMessages, Message{
		Role:    "user",
		Content: "The above is a compacted summary of previous actions. Continue from where you left off.",
	})
	for i := lastNStart; i < len(messages); i++ {
		newMessages = append(newMessages, messages[i])
	}

	return newMessages
}
