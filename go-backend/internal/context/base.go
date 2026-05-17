package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// BaseContextManager wraps a core.Session and provides baseline
// context management: hard truncation of tool results and
// direct passthrough to Session.BuildContext().
type BaseContextManager struct {
	session core.Session
	config  Config
	metrics ContextMetrics
}

func NewBaseContextManager(sess core.Session, cfg Config) *BaseContextManager {
	return &BaseContextManager{
		session: sess,
		config:  cfg,
	}
}

func (m *BaseContextManager) BuildContext(ctx context.Context) ([]core.Message, error) {
	msgs, err := m.session.BuildContext(ctx)
	if err != nil {
		return nil, err
	}

	// Safety net: hard-truncate any tool result still above the limit.
	// Keep the tail — errors and results at the end are most useful.
	maxSize := m.config.HardTruncateSize
	if maxSize <= 0 {
		maxSize = 6000
	}
	for i, msg := range msgs {
		if msg.Role == "tool" && len(msg.Content) > maxSize {
			tail := msg.Content[len(msg.Content)-maxSize:]
			if idx := strings.Index(tail, "\n"); idx >= 0 && idx < 200 {
				tail = tail[idx+1:]
			}
			msgs[i].Content = "...[truncated, showing tail]\n" + tail
		}
	}

	m.metrics = ContextMetrics{
		EstimatedTokens: EstimateTokensFromUsage(ctx, msgs),
		ContextSize:     m.config.ContextSize,
		MessageCount:    len(msgs),
	}
	if m.metrics.ContextSize > 0 {
		m.metrics.Utilization = float64(m.metrics.EstimatedTokens) / float64(m.metrics.ContextSize)
	}

	return msgs, nil
}

func (m *BaseContextManager) AppendMessage(msg core.Message) error {
	return m.session.AppendMessage(msg)
}

func (m *BaseContextManager) ProcessToolResult(_ context.Context, _, _, result string) (string, error) {
	maxSize := m.config.HardTruncateSize
	if maxSize <= 0 {
		maxSize = 6000
	}
	if len(result) > maxSize {
		// Keep the end — errors/results at the tail matter most
		tail := result[len(result)-maxSize:]
		// Avoid mid-line split
		if idx := strings.Index(tail, "\n"); idx >= 0 && idx < 200 {
			tail = tail[idx+1:]
		}
		return "...[output truncated, showing tail]\n" + tail, nil
	}
	return result, nil
}

func (m *BaseContextManager) LoadSpilledContent(_ string) (string, error) {
	return "", fmt.Errorf("spill not enabled in base context manager")
}

func (m *BaseContextManager) Usage() ContextMetrics {
	return m.metrics
}

func (m *BaseContextManager) SessionID() string {
	return m.session.ID()
}

func (m *BaseContextManager) Close() error {
	return nil
}

// Session returns the underlying session for orchestrator compatibility.
func (m *BaseContextManager) Session() core.Session {
	return m.session
}
