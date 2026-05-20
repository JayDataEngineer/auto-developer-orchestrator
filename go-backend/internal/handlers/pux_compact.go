package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ctxpkg "github.com/auto-developer-orchestrator/backend/internal/context"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/llm"
	"github.com/auto-developer-orchestrator/backend/internal/session"
)

// CompactResult holds the outcome of a manual compaction operation.
type CompactResult struct {
	Status            string `json:"status"`
	CompactionType    string `json:"compactionType"`
	TokensBefore      int    `json:"tokensBefore"`
	TokensAfter       int    `json:"tokensAfter"`
	MessagesCompacted int    `json:"messagesCompacted"`
	Message           string `json:"message,omitempty"`
}

// compactSession performs a full LLM-based compaction on the session JSONL file.
func (h *PuxHandler) compactSession(ctx context.Context, project, agentID string) CompactResult {
	// 1. Resolve session path — same logic as promptWithOrchestrator
	sandboxID := sanitizeSandboxID(project)
	if h.sandboxMgr != nil {
		if sb := h.sandboxMgr.FindSandboxByProject(project); sb != nil {
			sandboxID = sb.ID
		}
	}

	home, _ := os.UserHomeDir()
	sessionDir := filepath.Join(home, ".pi", "agent", "sessions", sandboxID)
	sessionPath := filepath.Join(sessionDir, agentID+".jsonl")

	// 2. Load the JSONL session
	tree, err := session.Load(sessionPath)
	if err != nil {
		// No session file — nothing to compact
		return CompactResult{
			Status:  "noop",
			Message: fmt.Sprintf("no session file found for %s/%s", project, agentID),
		}
	}
	defer tree.Close()

	// 3. Get current messages and estimate tokens
	msgs, err := tree.BuildContext(ctx)
	if err != nil {
		return CompactResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to build context: %v", err),
		}
	}

	if len(msgs) < 6 {
		return CompactResult{
			Status:  "noop",
			Message: "not enough messages to compact (need at least 6)",
		}
	}

	tokensBefore := ctxpkg.EstimateTokensFromUsage(ctx, msgs)

	// 4. Resolve LLM provider
	engine := h.llamaEngine
	if engine == nil {
		engine = h.clusterEngine
	}
	if engine == nil {
		// No LLM available — fall back to micro-compact (truncate tool results)
		return h.microCompactOnly(ctx, tree, msgs, tokensBefore)
	}

	provider := llm.NewAdapter(engine, 32768)
	defer provider.Close()

	// 5. Calculate cut point — keep recent 20% of context (same as default)
	contextSize := 32768
	keepTokens := contextSize / 5
	cutPoint := findCutPointSimple(msgs, keepTokens)

	if cutPoint < 2 {
		return CompactResult{
			Status:  "noop",
			Message: "not enough old messages to summarize",
		}
	}

	// 6. Generate LLM summary
	summary := ctxpkg.SummarizeMessages(ctx, provider, msgs, cutPoint, "")
	if summary == "" {
		// LLM summarization failed — fall back to micro-compact
		return h.microCompactOnly(ctx, tree, msgs, tokensBefore)
	}

	// 7. Apply compaction to the session
	_, err = tree.Compact(ctx, summary)
	if err != nil {
		return CompactResult{
			Status:  "error",
			Message: fmt.Sprintf("compact failed: %v", err),
		}
	}

	// 8. Estimate tokens after compaction
	msgsAfter, _ := tree.BuildContext(ctx)
	tokensAfter := ctxpkg.EstimateTokensFromUsage(ctx, msgsAfter)

	return CompactResult{
		Status:            "ok",
		CompactionType:    "full",
		TokensBefore:      tokensBefore,
		TokensAfter:       tokensAfter,
		MessagesCompacted: cutPoint,
	}
}

// microCompactOnly does a tool-result-truncation-only compaction without LLM summarization.
func (h *PuxHandler) microCompactOnly(ctx context.Context, tree *session.SessionTree, _ []core.Message, tokensBefore int) CompactResult {
	truncated, err := tree.TruncateToolResults(4)
	if err != nil {
		return CompactResult{
			Status:  "error",
			Message: fmt.Sprintf("micro-compact failed: %v", err),
		}
	}
	if truncated == 0 {
		return CompactResult{
			Status:  "noop",
			Message: "no tool results to truncate",
		}
	}

	msgsAfter, _ := tree.BuildContext(ctx)
	tokensAfter := ctxpkg.EstimateTokensFromUsage(ctx, msgsAfter)

	return CompactResult{
		Status:            "ok",
		CompactionType:    "micro",
		TokensBefore:      tokensBefore,
		TokensAfter:       tokensAfter,
		MessagesCompacted: truncated,
	}
}

// sanitizeSandboxID mirrors the sanitization in pux_prompt.go.
func sanitizeSandboxID(project string) string {
	id := strings.ReplaceAll(project, "/", "-")
	id = strings.ReplaceAll(id, "_", "-")
	id = strings.Trim(id, "-")
	return id
}

// findCutPointSimple determines how many leading messages to summarize.
// Keeps recent messages worth keepTokens, returns the cut index.
func findCutPointSimple(msgs []core.Message, keepTokens int) int {
	if len(msgs) <= 4 {
		return 0
	}

	if keepTokens <= 0 {
		keep := len(msgs) / 5
		if keep < 4 {
			keep = 4
		}
		return len(msgs) - keep
	}

	accumulated := 0
	minKeep := 4

	for i := len(msgs) - 1; i >= minKeep; i-- {
		msg := msgs[i]
		accumulated += estimateTokenCount(msg)

		// Don't split turn boundaries
		if msg.Role == "tool" && i > minKeep {
			if msgs[i-1].Role == "assistant" {
				accumulated += estimateTokenCount(msgs[i-1])
				i--
			}
			continue
		}

		if accumulated >= keepTokens {
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

	return 0
}

// estimateTokenCount gives a rough token count for a message.
func estimateTokenCount(msg core.Message) int {
	content := msg.Content
	for _, tc := range msg.ToolCalls {
		content += tc.Function.Arguments
	}
	return len(content) / 4
}
