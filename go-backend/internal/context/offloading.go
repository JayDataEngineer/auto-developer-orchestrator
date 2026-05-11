package context

import (
	"context"
	"fmt"
	"log"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// OffloadingContextManager decorates any ContextManager with automatic
// offloading of large tool results to spill files.
type OffloadingContextManager struct {
	inner  ContextManager
	config Config
	spill  *SpillStore
	logger *log.Logger
}

func NewOffloadingContextManager(inner ContextManager, cfg Config) *OffloadingContextManager {
	spillDir := cfg.SpillDir
	if spillDir == "" {
		spillDir = fmt.Sprintf("/tmp/pux-spill/%s", inner.SessionID())
	}
	return &OffloadingContextManager{
		inner:  inner,
		config: cfg,
		spill:  NewSpillStore(spillDir),
		logger: log.Default(),
	}
}

func (m *OffloadingContextManager) BuildContext(ctx context.Context) ([]core.Message, error) {
	msgs, err := m.inner.BuildContext(ctx)
	if err != nil {
		return nil, err
	}

	// Second pass: offload any large tool results that were reloaded from session
	threshold := m.config.OffloadThreshold
	if threshold <= 0 {
		return msgs, nil
	}

	for i, msg := range msgs {
		if msg.Role == "tool" && len(msg.Content) > threshold {
			preview := msg.Content
			if len(preview) > m.config.PreviewSize {
				preview = preview[:m.config.PreviewSize]
			}
			entry, spillErr := m.spill.Spill(msg.Name, msg.ToolCallID, msg.Content, preview)
			if spillErr == nil {
				msgs[i].Content = fmt.Sprintf(
				"%s\n\n...[result offloaded (%d bytes). Use load_spilled(\"%s\") to retrieve full content]",
					preview, entry.Size, entry.Ref,
				)
			}
		}
	}

	return msgs, nil
}

func (m *OffloadingContextManager) AppendMessage(msg core.Message) error {
	return m.inner.AppendMessage(msg)
}

func (m *OffloadingContextManager) ProcessToolResult(ctx context.Context, toolName, toolCallID, result string) (string, error) {
	// If the result is large enough to offload
	if len(result) > m.config.OffloadThreshold {
		preview := result
		if len(preview) > m.config.PreviewSize {
			preview = preview[:m.config.PreviewSize]
		}
		entry, spillErr := m.spill.Spill(toolName, toolCallID, result, preview)
		if spillErr == nil {
			return fmt.Sprintf(
			"%s\n\n...[result offloaded (%d bytes). Use load_spilled(\"%s\") to retrieve full content]",
				preview, entry.Size, entry.Ref,
			), nil
		}
		m.logger.Printf("OffloadingContextManager: spill failed, falling back to truncation: %v", spillErr)
	}

	// Fall through to inner (hard truncation safety net)
	return m.inner.ProcessToolResult(ctx, toolName, toolCallID, result)
}

func (m *OffloadingContextManager) LoadSpilledContent(ref string) (string, error) {
	return m.spill.Load(ref)
}

func (m *OffloadingContextManager) Usage() ContextMetrics {
	metrics := m.inner.Usage()
	metrics.SpilledCount = m.spill.Count()
	metrics.SpilledBytes = m.spill.TotalBytes()
	return metrics
}

func (m *OffloadingContextManager) SessionID() string {
	return m.inner.SessionID()
}

func (m *OffloadingContextManager) Close() error {
	m.spill.Cleanup()
	return m.inner.Close()
}
