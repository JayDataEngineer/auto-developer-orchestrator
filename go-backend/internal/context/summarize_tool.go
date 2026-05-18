package context

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SummarizeTool lets the agent proactively compress its context.
// Instead of waiting for the automatic threshold, the agent can call this
// to summarize older messages and free up context space.
type SummarizeTool struct {
	ctxMgr  ContextManager
	session core.Session
	provider core.LLMProvider
	logger  *log.Logger
}

func NewSummarizeTool(ctxMgr ContextManager, session core.Session, provider core.LLMProvider) *SummarizeTool {
	return &SummarizeTool{
		ctxMgr:   ctxMgr,
		session:  session,
		provider: provider,
		logger:   log.Default(),
	}
}

func (t *SummarizeTool) Name() string { return "summarize_context" }

func (t *SummarizeTool) Description() string {
	return "Compress older messages in your context into a concise summary. " +
		"Use when context is getting long, before starting a major new task, " +
		"or when you notice repetition. Keeps recent messages intact. " +
		"Optional 'focus' parameter hints at what to preserve."
}

func (t *SummarizeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"focus": {
				"type": "string",
				"description": "Optional hint about what information to preserve in the summary (e.g., 'file paths and test results')"
			}
		}
	}`)
}

func (t *SummarizeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.provider == nil {
		return map[string]any{
			"error": "summarization not available (no LLM provider)",
		}, nil
	}

	// Get metrics before
	before := t.ctxMgr.Usage()

	// Build messages for summarization
	msgs, err := t.session.BuildContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build context: %w", err)
	}

	if len(msgs) < 6 {
		return map[string]any{
			"status":  "skipped",
			"message": "too few messages to summarize",
			"count":   len(msgs),
		}, nil
	}

	// Generate summary
	focus, _ := args["focus"].(string)
	summary := t.generateAgentSummary(ctx, msgs, focus)
	if summary == "" {
		return map[string]any{
			"status":  "error",
			"message": "failed to generate summary (LLM error or empty response)",
		}, nil
	}

	// Compact the session
	_, err = t.session.Compact(ctx, summary)
	if err != nil {
		return map[string]any{
			"status":  "error",
			"message": fmt.Sprintf("compaction failed: %v", err),
		}, nil
	}

	// Get metrics after
	after := t.ctxMgr.Usage()

	t.logger.Printf("SummarizeTool: compacted %d → %d messages, %d → %d estimated tokens",
		before.MessageCount, after.MessageCount, before.EstimatedTokens, after.EstimatedTokens)

	return map[string]any{
		"status":           "completed",
		"messages_before":  before.MessageCount,
		"messages_after":   after.MessageCount,
		"tokens_before":    before.EstimatedTokens,
		"tokens_after":     after.EstimatedTokens,
		"summary_preview":  truncateForSummary(summary, 200),
	}, nil
}

func (t *SummarizeTool) generateAgentSummary(ctx context.Context, msgs []core.Message, focus string) string {
	// Keep system prompt + last few messages, summarize the rest
	keep := 4
	cutPoint := len(msgs) - keep
	if cutPoint < 2 {
		cutPoint = 2
	}
	oldMsgs := msgs[1:cutPoint] // skip system prompt

	var b strings.Builder
	for _, msg := range oldMsgs {
		switch msg.Role {
		case "user":
			b.WriteString(fmt.Sprintf("[USER]: %s\n", truncateForSummary(msg.Content, 300)))
		case "assistant":
			if msg.Content != "" {
				b.WriteString(fmt.Sprintf("[ASSISTANT]: %s\n", truncateForSummary(msg.Content, 300)))
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString(fmt.Sprintf("[TOOL CALL %s(%s)]\n", tc.Function.Name, truncateForSummary(tc.Function.Arguments, 150)))
			}
		case "tool":
			name := msg.Name
			if name == "" {
				name = "tool"
			}
			b.WriteString(fmt.Sprintf("[TOOL RESULT %s]: %s\n", name, truncateForSummary(msg.Content, 200)))
		}
		b.WriteString("\n")
	}

	focusSection := ""
	if focus != "" {
		focusSection = fmt.Sprintf("\nPay special attention to preserving information about: %s", focus)
	}

	prompt := fmt.Sprintf(`Summarize the agent conversation below into a structured summary that preserves:
1. The user's original goal and any changes to it
2. Key decisions made and reasoning
3. Important findings, data, or discoveries
4. Files read, written, or modified (with paths)
5. Errors encountered and their resolution
6. Current state of progress and what remains%s

Be concise but comprehensive. The agent needs this to continue working effectively.`, focusSection)

	summarizeMsgs := []core.Message{
		{Role: "user", Content: b.String() + "\n\n" + prompt},
	}

	opts := core.GenerateOptions{
		MaxTokens:   1024,
		Temperature: 0.3,
		TopP:        0.9,
	}

	ch, err := t.provider.StreamChat(ctx, summarizeMsgs, nil, opts)
	if err != nil {
		t.logger.Printf("SummarizeTool: LLM call failed: %v", err)
		return ""
	}

	var result strings.Builder
	for evt := range ch {
		if evt.Type == core.ChatEventContent {
			result.WriteString(evt.Content)
		}
		if evt.Type == core.ChatEventError {
			t.logger.Printf("SummarizeTool: LLM stream error: %v", evt.Err)
			return ""
		}
	}

	summary := strings.TrimSpace(result.String())
	if len(summary) < 30 {
		return ""
	}
	return summary
}

// ContextStatusTool returns current context window metrics.
// Lightweight — no LLM calls, just reads cached metrics.
type ContextStatusTool struct {
	ctxMgr ContextManager
}

func NewContextStatusTool(ctxMgr ContextManager) *ContextStatusTool {
	return &ContextStatusTool{ctxMgr: ctxMgr}
}

func (t *ContextStatusTool) Name() string { return "context_status" }

func (t *ContextStatusTool) Description() string {
	return "Check your current context window utilization. Returns estimated token count, " +
		"message count, and how close you are to automatic compaction thresholds. " +
		"Use this to decide whether to call summarize_context."
}

func (t *ContextStatusTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *ContextStatusTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	metrics := t.ctxMgr.Usage()

	status := "ok"
	if metrics.Utilization >= 0.75 {
		status = "critical"
	} else if metrics.Utilization >= 0.55 {
		status = "high"
	}

	return map[string]any{
		"estimated_tokens": metrics.EstimatedTokens,
		"context_size":     metrics.ContextSize,
		"utilization":      fmt.Sprintf("%.0f%%", metrics.Utilization*100),
		"message_count":    metrics.MessageCount,
		"status":           status,
		"last_compaction":  metrics.CompactionType,
		"spilled_results":  metrics.SpilledCount,
	}, nil
}
