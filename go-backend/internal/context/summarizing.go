package context

import (
	"context"
	"encoding/json"
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

	// Iterative summary merging — carries forward previous summary
	lastSummary string

	// Conversation log — append-mode markdown file the agent can read
	convLog     *os.File
	convLogPath string

	// File operation tracking — cumulative across compactions
	readFiles     []string
	modifiedFiles []string
	readSet       map[string]bool
	modifiedSet   map[string]bool
}

func NewSummarizingContextManager(inner ContextManager, cfg Config, sess core.Session, provider core.LLMProvider) *SummarizingContextManager {
	m := &SummarizingContextManager{
		inner:       inner,
		config:      cfg,
		session:     sess,
		provider:    provider,
		logger:      log.Default(),
		readSet:     make(map[string]bool),
		modifiedSet: make(map[string]bool),
	}

	// Open conversation log if enabled and spill dir is configured
	if cfg.ConversationLogEnabled && cfg.SpillDir != "" {
		m.convLogPath = filepath.Join(cfg.SpillDir, "conversation_log.md")
		// Ensure spill directory exists
		if err := os.MkdirAll(cfg.SpillDir, 0755); err != nil {
			m.logger.Printf("SummarizingContextManager: failed to create spill dir: %v", err)
		} else {
			f, err := os.OpenFile(m.convLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				m.logger.Printf("SummarizingContextManager: failed to open conversation log: %v", err)
			} else {
				m.convLog = f
			}
		}
	}

	return m
}

func (m *SummarizingContextManager) BuildContext(ctx context.Context) ([]core.Message, error) {
	// Cache subscriber from context for compaction event emission
	if sub, ok := ctx.Value(core.SubscriberKey{}).(chan<- core.AgentEvent); ok && sub != nil {
		m.subCh = sub
	}

	// Check compaction thresholds before delegating to inner
	if m.session != nil {
		msgs, _ := m.session.BuildContext(ctx)
		tokens := EstimateTokensFromUsage(ctx, msgs)
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
	if m.convLog != nil {
		m.convLog.Close()
	}
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

	// 2. Append to conversation log (agent-readable file)
	m.appendToConversationLog(msgs)

	// 3. Scan file operations from messages being compacted
	m.scanFileOperations(msgs)

	// 4. Generate LLM summary of older messages
	summary := m.generateSummary(ctx, msgs)
	if summary == "" {
		m.logger.Printf("SummarizingContextManager: LLM summary failed, falling back to micro-compact")
		m.microCompact()
		return
	}

	// 5. Append file tracking and re-injection metadata to summary
	summary = m.enrichSummary(summary)

	// 6. Call session.Compact with the real summary
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

// findCutPoint walks backwards through messages accumulating token estimates,
// finding a safe cut point that respects turn boundaries. It never splits
// an assistant→tool_result pair. Falls back to len(msgs)/5 when keepTokens <= 0.
func findCutPoint(msgs []core.Message, keepTokens int) int {
	if keepTokens <= 0 {
		// Legacy behavior: keep last ~20%
		keep := len(msgs) / 5
		if keep < 4 {
			keep = 4
		}
		if keep > 20 {
			keep = 20
		}
		return len(msgs) - keep
	}

	// Walk backwards accumulating tokens until we hit the budget
	accumulated := 0
	minKeep := 4
	if len(msgs) <= minKeep {
		return 0
	}

	for i := len(msgs) - 1; i >= minKeep; i-- {
		msg := msgs[i]
		tok := estimateMessageTokens(msg)

		// Don't split turn boundaries: if this is a tool result, include the
		// assistant message that triggered it (walk past tool results)
		if msg.Role == "tool" && i > minKeep {
			// Accumulate this tool result
			accumulated += tok
			// Also count the preceding assistant message (it must stay with its results)
			if msgs[i-1].Role == "assistant" {
				accumulated += estimateMessageTokens(msgs[i-1])
				i-- // skip the assistant on next iteration
			}
			continue
		}

		accumulated += tok
		if accumulated >= keepTokens {
			// We've accumulated enough — cut here, keep everything from i onward
			// But make sure we're not cutting at a tool result (move back to assistant)
			cut := i
			for cut > 0 && msgs[cut].Role == "tool" {
				cut--
			}
			if cut < minKeep {
				return 0
			}
			return cut
		}
	}

	return 0 // Not enough messages to exceed budget, summarize from start
}

// SummarizeMessages calls the LLM to produce a structured summary of messages.
// This is the reusable core used by both automatic and manual compaction.
// prevSummary is optional — when provided, the LLM merges new content into it.
// cutPoint specifies how many leading messages to summarize (msgs[:cutPoint]).
func SummarizeMessages(ctx context.Context, provider core.LLMProvider, msgs []core.Message, cutPoint int, prevSummary string) string {
	if provider == nil || len(msgs) < 2 {
		return ""
	}
	if cutPoint <= 0 || cutPoint > len(msgs) {
		cutPoint = len(msgs)
	}
	oldMsgs := msgs[:cutPoint]
	if len(oldMsgs) < 2 {
		return ""
	}

	// Build the conversation text for summarization
	var b strings.Builder

	if prevSummary != "" {
		const maxPrevSummaryChars = 5000
		ps := prevSummary
		if len(ps) > maxPrevSummaryChars {
			ps = ps[:maxPrevSummaryChars] + "\n...[previous summary truncated]"
		}
		b.WriteString("=== PREVIOUS SUMMARY ===\n")
		b.WriteString(ps)
		b.WriteString("\n\n=== NEW CONVERSATION TO MERGE ===\n\n")
	} else {
		b.WriteString("=== CONVERSATION TO SUMMARIZE ===\n\n")
	}

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

	var summaryPrompt string
	if prevSummary != "" {
		summaryPrompt = `You are merging NEW conversation content into an EXISTING summary above.

Update the structured summary to incorporate the new information. Preserve:
1. The user's original goal/intent (update if it changed)
2. Key decisions made and why (add new decisions)
3. Important findings, facts, or data discovered (accumulate)
4. Tools used and their results (add new ones, brief)
5. Any errors encountered and how they were resolved
6. Current state of progress (reflect latest state)

Output the complete updated summary. Be concise but comprehensive.`
	} else {
		summaryPrompt = `Summarize the conversation above into a structured summary that preserves:
1. The user's original goal/intent
2. Key decisions made and why
3. Important findings, facts, or data discovered
4. Tools used and their results (brief)
5. Any errors encountered and how they were resolved
6. Current state of progress

Be concise but comprehensive. The agent needs this summary to continue working effectively.`
	}

	summarizeMessages := []core.Message{
		{
			Role:    "user",
			Content: b.String() + "\n\n" + summaryPrompt,
		},
	}

	opts := core.GenerateOptions{
		MaxTokens:   2048,
		Temperature: 0.3,
		TopP:        0.9,
	}

	ch, err := provider.StreamChat(ctx, summarizeMessages, nil, opts)
	if err != nil {
		return ""
	}

	var summary strings.Builder
	for evt := range ch {
		if evt.Type == core.ChatEventContent {
			summary.WriteString(evt.Content)
		}
		if evt.Type == core.ChatEventError {
			return ""
		}
	}

	result := strings.TrimSpace(summary.String())
	if len(result) < 50 {
		return ""
	}

	return result
}

// generateSummary calls the LLM to produce a structured summary of older messages.
// Uses turn-boundary-aware cut points and supports iterative summary merging.
func (m *SummarizingContextManager) generateSummary(ctx context.Context, msgs []core.Message) string {
	if m.provider == nil {
		return ""
	}

	keepTokens := m.config.KeepRecentTokens
	if keepTokens <= 0 {
		keepTokens = m.config.ContextSize / 5
	}

	cutPoint := findCutPoint(msgs, keepTokens)
	result := SummarizeMessages(ctx, m.provider, msgs, cutPoint, m.lastSummary)
	if result != "" {
		m.lastSummary = result
	}
	return result
}

// scanFileOperations scans assistant messages for file_read, file_write, and bash
// tool calls, extracting file paths for cumulative tracking across compactions.
func (m *SummarizingContextManager) scanFileOperations(msgs []core.Message) {
	for _, msg := range msgs {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			args := tc.Function.Arguments
			if args == "" {
				continue
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(args), &parsed); err != nil {
				continue
			}

			switch tc.Function.Name {
			case "file_read", "read_file":
				if path, ok := parsed["path"].(string); ok && path != "" && !m.readSet[path] {
					m.readSet[path] = true
					m.readFiles = append(m.readFiles, path)
				}
			case "file_write", "write_file", "edit_file":
				if path, ok := parsed["path"].(string); ok && path != "" && !m.modifiedSet[path] {
					m.modifiedSet[path] = true
					m.modifiedFiles = append(m.modifiedFiles, path)
				}
			}
		}
	}
}

// enrichSummary appends file tracking metadata, conversation log reference,
// and recently-accessed file re-injection to the summary.
func (m *SummarizingContextManager) enrichSummary(summary string) string {
	var b strings.Builder
	b.WriteString(summary)

	// File tracking XML (Pi-Mono pattern)
	if len(m.readFiles) > 0 || len(m.modifiedFiles) > 0 {
		b.WriteString("\n\n<file-tracking>\n")
		if len(m.readFiles) > 0 {
			b.WriteString("<read-files>\n")
			for _, f := range m.readFiles {
				b.WriteString(fmt.Sprintf("  <file>%s</file>\n", f))
			}
			b.WriteString("</read-files>\n")
		}
		if len(m.modifiedFiles) > 0 {
			b.WriteString("<modified-files>\n")
			for _, f := range m.modifiedFiles {
				b.WriteString(fmt.Sprintf("  <file>%s</file>\n", f))
			}
			b.WriteString("</modified-files>\n")
		}
		b.WriteString("</file-tracking>")
	}

	// Conversation log reference (DeepAgents pattern)
	if m.convLogPath != "" {
		b.WriteString(fmt.Sprintf("\n\n<conversation-log path=\"%s\">Full conversation history saved here. Use file_read to access it.</conversation-log>", m.convLogPath))
	}

	// Recently-accessed file re-injection (post-compact recovery)
	reinjectCount := m.config.ReinjectFileCount
	if reinjectCount > 0 && len(m.readFiles) > 0 {
		start := len(m.readFiles) - reinjectCount
		if start < 0 {
			start = 0
		}
		recent := m.readFiles[start:]
		b.WriteString("\n\n<recently-accessed-files>\n")
		for _, f := range recent {
			b.WriteString(fmt.Sprintf("  <file>%s</file>\n", f))
		}
		b.WriteString("</recently-accessed-files>")
	}

	return b.String()
}

// appendToConversationLog writes evicted messages to the append-mode markdown
// conversation log. The agent can read this file with file_read at any time.
func (m *SummarizingContextManager) appendToConversationLog(msgs []core.Message) {
	if m.convLog == nil {
		return
	}

	timestamp := time.Now().Format(time.RFC3339)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n\n---\n## Compaction at %s (%d messages)\n\n", timestamp, len(msgs)))

	for _, msg := range msgs {
		switch msg.Role {
		case "user":
			b.WriteString(fmt.Sprintf("**User**: %s\n\n", truncateForSummary(msg.Content, 1000)))
		case "assistant":
			if msg.Content != "" {
				b.WriteString(fmt.Sprintf("**Assistant**: %s\n\n", truncateForSummary(msg.Content, 1000)))
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString(fmt.Sprintf("**Tool Call `%s`**: `%s`\n\n", tc.Function.Name, truncateForSummary(tc.Function.Arguments, 300)))
			}
		case "tool":
			name := msg.Name
			if name == "" {
				name = "tool"
			}
			b.WriteString(fmt.Sprintf("**Tool Result (%s)**: %s\n\n", name, truncateForSummary(msg.Content, 500)))
		}
	}

	if _, err := m.convLog.WriteString(b.String()); err != nil {
		m.logger.Printf("SummarizingContextManager: failed to append to conversation log: %v", err)
	}
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
