package context

import (
	"context"
	"log"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SummarizingContextManager decorates any ContextManager with compaction
// strategies: micro-compaction (truncate old tool results) and
// full-compaction (LLM summary) based on context utilization thresholds.
type SummarizingContextManager struct {
	inner   ContextManager
	config  Config
	session core.Session // for TruncateToolResults/Compact
	logger  *log.Logger
}

func NewSummarizingContextManager(inner ContextManager, cfg Config, sess core.Session) *SummarizingContextManager {
	return &SummarizingContextManager{
		inner:   inner,
		config:  cfg,
		session: sess,
		logger:  log.Default(),
	}
}

func (m *SummarizingContextManager) BuildContext(ctx context.Context) ([]core.Message, error) {
	// Check compaction thresholds before delegating to inner
	if m.session != nil {
		msgs, _ := m.session.BuildContext(ctx)
		tokens := EstimateTokens(msgs)
		contextSize := m.config.ContextSize
		if contextSize <= 0 {
			contextSize = 32768
		}
		ratio := float64(tokens) / float64(contextSize)

		if ratio >= m.config.FullCompactThreshold {
			m.fullCompact(ctx)
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
	return m.inner.Usage()
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
	}
}

func (m *SummarizingContextManager) fullCompact(ctx context.Context) {
	if m.session == nil {
		return
	}
	_, err := m.session.Compact(ctx, nil)
	if err != nil {
		m.logger.Printf("SummarizingContextManager: full compact failed: %v", err)
		return
	}
	m.logger.Printf("SummarizingContextManager: full compaction triggered")
}
