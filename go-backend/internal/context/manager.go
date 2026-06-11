package context

import (
	"context"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ContextManager owns the active context window between the agent loop
// and the session. It applies offloading, truncation, summarization,
// and structured state injection before messages reach the LLM.
//
// Decorator pattern: BaseContextManager wraps Session.
// OffloadingContextManager wraps BaseContextManager.
// SummarizingContextManager wraps any ContextManager.
type ContextManager interface {
	// BuildContext assembles the final message list for the LLM.
	// Applies compaction/offloading strategies before returning.
	BuildContext(ctx context.Context) ([]core.Message, error)

	// AppendMessage persists a message to the underlying session.
	AppendMessage(msg core.Message) error

	// ProcessToolResult takes a raw tool result and returns the version
	// the agent should see. May offload to spill files and return a preview.
	ProcessToolResult(ctx context.Context, toolName, toolCallID, result string) (string, error)

	// LoadSpilledContent retrieves the full content of a previously offloaded result.
	LoadSpilledContent(ref string) (string, error)

	// Usage returns current context window utilization metrics.
	Usage() ContextMetrics

	// SessionID returns the underlying session identifier.
	SessionID() string

	// Close releases resources (spill files, etc).
	Close() error
}

// Config controls how a ContextManager is constructed.
type Config struct {
	// ContextSize is the max token budget for the context window.
	ContextSize int

	// SpillDir is the directory for offloaded tool results.
	SpillDir string

	// OffloadThreshold is the size in bytes above which a tool result
	// is automatically offloaded to a spill file. 0 = disable offloading.
	OffloadThreshold int

	// PreviewSize is the number of characters kept as preview when a
	// result is offloaded.
	PreviewSize int

	// HardTruncateSize is the absolute max chars for in-context tool results.
	// Replaces the current 6000-char hard truncate in loop.go.
	HardTruncateSize int

	// MicroCompactThreshold is the ratio (0-1) at which micro-compaction
	// (truncate old tool results) is triggered.
	MicroCompactThreshold float64

	// MaskThreshold is the ratio at which old tool results are replaced
	// with short [ref: X] placeholders instead of removing them entirely.
	// Between MicroCompact and Prune. 0 = disable.
	MaskThreshold float64

	// PruneThreshold is the ratio at which old tool outputs are removed
	// entirely, keeping only assistant/user messages. Between Mask and Full.
	// 0 = disable.
	PruneThreshold float64

	// FullCompactThreshold is the ratio at which full compaction
	// (LLM summary) is triggered.
	FullCompactThreshold float64

	// KeepResults is the number of recent tool results preserved during
	// micro-compaction.
	KeepResults int

	// EnableSummary enables the LLM summarization decorator.
	EnableSummary bool

	// LLMProvider is used by the SummarizingContextManager to generate
	// structured summaries during full compaction. If nil, full compaction
	// falls back to micro-compaction.
	LLMProvider core.LLMProvider

	// CompactProvider is an optional cheaper model used for compaction
	// summaries. When set, full compaction uses this provider instead
	// of the main agent's provider. Saves tokens on the expensive model.
	// Falls back to LLMProvider if nil.
	CompactProvider core.LLMProvider

	// KeepRecentTokens is the token budget for recent messages preserved
	// during full compaction. 0 = 20% of ContextSize (legacy behavior).
	KeepRecentTokens int

	// ConversationLogEnabled writes evicted messages to an append-mode
	// markdown file the agent can read with file_read. Default true.
	ConversationLogEnabled bool

	// ReinjectFileCount is the number of recently-read file paths to
	// re-inject into the summary after compaction. 0 = disable. Default 5.
	ReinjectFileCount int

	// OffloadExemptTools is a set of tool names that should never be
	// auto-offloaded. Their full results are kept in context regardless
	// of size. Useful for tools where the model needs the complete output.
	// Default: ["scrape", "research", "delegate_to", "delegate_async"].
	// delegate_to/async are exempt because the CTO needs the full subagent
	// report — offloading creates an infinite loop where the CTO tries to
	// read the spill reference, which gets offloaded again.
	OffloadExemptTools []string
}

// DefaultConfig returns sensible defaults matching current behavior.
func DefaultConfig() Config {
	return Config{
		ContextSize:            32768,
		OffloadThreshold:       4096,
		PreviewSize:            500,
		HardTruncateSize:       6000,
		MicroCompactThreshold:  0.55,
		MaskThreshold:          0.65,
		PruneThreshold:         0.70,
		FullCompactThreshold:   0.75,
		KeepResults:            4,
		EnableSummary:          true,
		ConversationLogEnabled: true,
		ReinjectFileCount:      5,
		OffloadExemptTools:     []string{"scrape", "research", "delegate_to", "delegate_async"},
	}
}

// ContextMetrics describes the current state of the context window.
type ContextMetrics struct {
	EstimatedTokens int       // total tokens in the active context
	ContextSize     int       // max context size
	Utilization     float64   // ratio of tokens used (0-1)
	MessageCount    int       // number of messages in context
	SpilledCount    int       // number of offloaded tool results
	SpilledBytes    int64     // total bytes in spill files
	LastCompaction  time.Time // time of last compaction event
	CompactionType  string    // "micro" or "full" or ""
}

// Factory creates a ContextManager from a session and config.
// Builds the decorator stack: Summarizing(Offloading(Base(session)))
func Factory(sess core.Session, cfg Config) ContextManager {
	base := NewBaseContextManager(sess, cfg)
	mgr := ContextManager(base)

	if cfg.OffloadThreshold > 0 {
		mgr = NewOffloadingContextManager(mgr, cfg)
	}

	if cfg.EnableSummary {
		mgr = NewSummarizingContextManager(mgr, cfg, sess, cfg.LLMProvider)
	}

	return mgr
}
