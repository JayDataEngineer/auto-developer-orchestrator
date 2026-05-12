package context

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SummarizingContextManager decorates any ContextManager with compaction
// strategies: micro-compaction (truncate old tool results) and
// full-compaction (LLM summary) based on context utilization thresholds.
type SummarizingContextManager struct {
	inner    ContextManager
	config   Config
	session  core.Session   // for TruncateToolResults/Compact
	provider core.LLMProvider
	logger   *log.Logger
	metrics  ContextMetrics
	subCh    chan<- core.AgentEvent // cached from context during BuildContext
}

func NewSummarizingContextManager(inner ContextManager, cfg Config, sess core.Session, provider core.LLMProvider) *SummarizingContextManager {
	return &SummarizingContextManager{
		inner:    inner,
		config:   cfg,
		session:  sess,
		provider: provider,
		logger:   log.Default(),
	}
}

func (m *SummarizingContextManager) BuildContext(ctx context.Context) ([]core.Message, error) {
	// Cache subscriber from context for compaction event emission
	if sub, ok := ctx.Value(core.SubscriberKey{}).(chan<- core.AgentEvent); ok && sub != nil {
		m.subCh = sub
	}

	// Check compaction thresholds before delegating to inner
	if m.session != nil {
		msgs, _ := m.session.BuildContext(ctx)
		tokens := EstimateTokens(msgs)
		contextSize := m.config.ContextSize
		if contextSize <= 0 {
			contextSize = 32768
		}
		ratio := float64(tokens) / float64(contextSize)

		m.metrics = ContextMetrics{
			EstimatedTokens: tokens,
			ContextSize:     contextSize,
			Utilization:     ratio,
			MessageCount:    len(msgs),
		}

		if ratio >= m.config.FullCompactThreshold {
			m.fullCompact(ctx, msgs)
		} else if ratio >= m.config.MicroCompactThreshold {
			m.microCompact()
		}
	}

	return m.inner.BuildContext(ctx)
}

func (m *SummarizingContextManager) AppendMessage(msg core.Message) error {
	return m.inner.AppendMessage(msg)
}

func (m *SummarizingContextManager) ProcessToolResult(ctx context.Context, toolName, toolCallID, result string) (string, error) {
	return m.inner.ProcessToolResult(ctx, toolName, toolCallID, result)
}

func (m *SummarizingContextManager) LoadSpilledContent(ref string) (string, error) {
	return m.inner.LoadSpilledContent(ref)
}

func (m *SummarizingContextManager) Usage() ContextMetrics {
	inner := m.inner.Usage()
	// Merge our compaction metrics with inner metrics
	if m.metrics.CompactionType != "" {
		inner.CompactionType = m.metrics.CompactionType
		inner.LastCompaction = m.metrics.LastCompaction
	}
	return inner
}

func (m *SummarizingContextManager) SessionID() string {
	return m.inner.SessionID()
}

func (m *SummarizingContextManager) Close() error {
	return m.inner.Close()
}

func (m *SummarizingContextManager) microCompact() {
	keep := m.config.KeepResults
	if keep <= 0 {
		keep = 4
	}
	truncated, err := m.session.TruncateToolResults(keep)
	if err != nil {
		m.logger.Printf("SummarizingContextManager: micro-compact failed: %v", err)
		return
	}
	if truncated > 0 {
		m.logger.Printf("SummarizingContextManager: micro-compact: kept %d, truncated %d", keep, truncated)
		m.metrics.CompactionType = "micro"
		m.metrics.LastCompaction = time.Now()
		m.emitCompactionEvent(truncated, 0)
	}
}

func (m *SummarizingContextManager) fullCompact(ctx context.Context, msgs []core.Message) {
	if m.session == nil || len(msgs) < 6 {
		return
	}

	// 1. Archive the full conversation to disk (lossless preservation)
	archivePath := m.archiveConversation(msgs)

	// 2. Generate LLM summary of older messages
	summary := m.generateSummary(ctx, msgs)
	if summary == "" {
		m.logger.Printf("SummarizingContextManager: LLM summary failed, falling back to micro-compact")
		m.microCompact()
		return
	}

	// 3. Call session.Compact with the real summary
	_, err := m.session.Compact(ctx, summary)
	if err != nil {
		m.logger.Printf("SummarizingContextManager: full compact failed: %v", err)
		return
	}

	m.logger.Printf("SummarizingContextManager: full compaction done (archive: %s, summary: %d chars)",
		archivePath, len(summary))
	m.metrics.CompactionType = "full"
	m.metrics.LastCompaction = time.Now()
	m.emitCompactionEvent(len(msgs), 0)
}

// generateSummary calls the LLM to produce a structured summary of older messages.
// It keeps the most recent messages intact and summarizes everything before them.
func (m *SummarizingContextManager) generateSummary(ctx context.Context, msgs []core.Message) string {
	if m.provider == nil {
		return ""
	}

	// Keep the last ~20% of messages intact, summarize the rest
	keepRecent := len(msgs) / 5
	if keepRecent < 4 {
		keepRecent = 4
	}
	if keepRecent > 20 {
		keepRecent = 20
	}

	oldMsgs := msgs[:len(msgs)-keepRecent]
	if len(oldMsgs) < 2 {
		return ""
	}

	// Build the conversation text for summarization
	var b strings.Builder
	b.WriteString("=== CONVERSATION TO SUMMARIZE ===\n\n")
	for _, msg := range oldMsgs {
		switch msg.Role {
		case "user":
			b.WriteString(fmt.Sprintf("[USER]: %s\n", truncateForSummary(msg.Content, 500)))
		case "assistant":
			if msg.Content != "" {
				b.WriteString(fmt.Sprintf("[ASSISTANT]: %s\n", truncateForSummary(msg.Content, 500)))
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString(fmt.Sprintf("[ASSISTANT called %s(%s)]\n", tc.Function.Name, truncateForSummary(tc.Function.Arguments, 200)))
			}
		case "tool":
			name := msg.Name
			if name == "" {
				name = "tool"
			}
			b.WriteString(fmt.Sprintf("[TOOL RESULT %s]: %s\n", name, truncateForSummary(msg.Content, 300)))
		}
		b.WriteString("\n")
	}

	summaryPrompt := `Summarize the conversation above into a structured summary that preserves:
1. The user's original goal/intent
2. Key decisions made and why
3. Important findings, facts, or data discovered
4. Tools used and their results (brief)
5. Any errors encountered and how they were resolved
6. Current state of progress

Be concise but comprehensive. The agent needs this summary to continue working effectively.`

	summarizeMessages := []core.Message{
		{
			Role:    "user",
			Content: b.String() + "\n\n" + summaryPrompt,
		},
	}

	// Call the LLM provider
	opts := core.GenerateOptions{
		MaxTokens:   2048,
		Temperature: 0.3,
		TopP:        0.9,
	}

	ch, err := m.provider.StreamChat(ctx, summarizeMessages, nil, opts)
	if err != nil {
		m.logger.Printf("SummarizingContextManager: LLM call failed: %v", err)
		return ""
	}

	// Collect the streaming response
	var summary strings.Builder
	for evt := range ch {
		if evt.Type == core.ChatEventContent {
			summary.WriteString(evt.Content)
		}
		if evt.Type == core.ChatEventError {
			m.logger.Printf("SummarizingContextManager: LLM stream error: %v", evt.Err)
			return ""
		}
	}

	result := strings.TrimSpace(summary.String())
	if len(result) < 50 {
		return "" // too short to be useful
	}
	return result
}

// archiveConversation saves the full conversation to disk before summarization.
// This ensures no data is ever lost — the archive is the ground truth.
func (m *SummarizingContextManager) archiveConversation(msgs []core.Message) string {
	if m.config.SpillDir == "" {
		return ""
	}

	archiveDir := filepath.Join(filepath.Dir(m.config.SpillDir), "archives")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		m.logger.Printf("SummarizingContextManager: failed to create archive dir: %v", err)
		return ""
	}

	timestamp := time.Now().Format("20060102-150405")
	archivePath := filepath.Join(archiveDir, fmt.Sprintf("conversation-%s.md", timestamp))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Conversation Archive\n"))
	b.WriteString(fmt.Sprintf("# Session: %s\n", m.SessionID()))
	b.WriteString(fmt.Sprintf("# Archived: %s\n", time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("# Messages: %d\n\n", len(msgs)))

	for i, msg := range msgs {
		switch msg.Role {
		case "system":
			b.WriteString(fmt.Sprintf("## [System]\n%s\n\n", msg.Content))
		case "user":
			b.WriteString(fmt.Sprintf("## [User]\n%s\n\n", msg.Content))
		case "assistant":
			b.WriteString(fmt.Sprintf("## [Assistant]\n%s\n\n", msg.Content))
			for _, tc := range msg.ToolCalls {
				b.WriteString(fmt.Sprintf("### Tool Call: %s\n```json\n%s\n```\n\n", tc.Function.Name, tc.Function.Arguments))
			}
		case "tool":
			name := msg.Name
			if name == "" {
				name = "tool"
			}
			content := msg.Content
			if len(content) > 10000 {
				content = content[:10000] + "\n... [truncated in archive, full content in session JSONL]"
			}
			b.WriteString(fmt.Sprintf("## [Tool Result: %s]\n%s\n\n", name, content))
		}
		_ = i
	}

	if err := os.WriteFile(archivePath, []byte(b.String()), 0644); err != nil {
		m.logger.Printf("SummarizingContextManager: failed to write archive: %v", err)
		return ""
	}

	// Clean up old archives (keep last 10)
	m.cleanupOldArchives(archiveDir, 10)

	return archivePath
}

// cleanupOldArchives removes old archive files, keeping only the N most recent.
func (m *SummarizingContextManager) cleanupOldArchives(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}

	// Sort by name (timestamp-prefixed, so lexicographic = chronological)
	// Remove oldest first
	for i := 0; i < len(entries)-keep; i++ {
		path := filepath.Join(dir, entries[i].Name())
		os.Remove(path)
	}
}

// emitCompactionEvent sends a compaction_end event via the SSE callback
// so the TUI/frontend can display context management state.
func (m *SummarizingContextManager) emitCompactionEvent(compactedMsgs, keptMsgs int) {
	if m.subCh == nil {
		return
	}
	core.SendEvent(m.subCh, core.AgentEvent{
		Type: core.EventTypeCompactionEnd,
		Data: core.AgentEventData{
			CompactionType:   m.metrics.CompactionType,
			ContextTokens:    m.metrics.EstimatedTokens,
			ContextSize:      m.metrics.ContextSize,
			ContextUtil:      m.metrics.Utilization,
			CompactedMessages: compactedMsgs,
			KeptMessages:     keptMsgs,
		},
	})
}

// truncateForSummary truncates content for inclusion in the summarization prompt.
func truncateForSummary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// compile-time check
var _ io.Closer = (*SummarizingContextManager)(nil)
