// Package approval provides a channel-based approval system decoupled from
// the Pi subprocess. When the agent needs human approval (risky bash command,
// plan confirmation, etc.), it registers a pending request. The frontend sends
// the response via HTTP, which resolves the channel.
package approval

import (
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
)

// Manager handles pending approval requests via channels.
// It is decoupled from PiClient — any code path (Pi subprocess, orchestrator,
// scheduler) can register and resolve approvals.
type Manager struct {
	mu       sync.Mutex
	pending  map[string]chan pi.ApprovalResponse // requestID → response channel
	timeout  time.Duration
}

// NewManager creates an approval manager with the given default timeout.
func NewManager(timeout time.Duration) *Manager {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Manager{
		pending: make(map[string]chan pi.ApprovalResponse),
		timeout: timeout,
	}
}

// Register creates a pending approval request and returns a channel that
// will receive the user's response. The caller should select on the channel
// with a timeout. The channel is buffered(1) so Resolve never blocks.
func (m *Manager) Register(requestID string) <-chan pi.ApprovalResponse {
	ch := make(chan pi.ApprovalResponse, 1)
	m.mu.Lock()
	m.pending[requestID] = ch
	m.mu.Unlock()
	return ch
}

// Resolve delivers the user's response to a pending approval request.
// Returns false if no pending request exists for the given ID.
func (m *Manager) Resolve(requestID string, resp pi.ApprovalResponse) bool {
	m.mu.Lock()
	ch, ok := m.pending[requestID]
	if ok {
		delete(m.pending, requestID)
	}
	m.mu.Unlock()

	if !ok {
		return false
	}
	ch <- resp
	close(ch)
	return true
}

// Cleanup removes a pending approval without resolving it (e.g. on context cancel).
func (m *Manager) Cleanup(requestID string) {
	m.mu.Lock()
	ch, ok := m.pending[requestID]
	if ok {
		delete(m.pending, requestID)
	}
	m.mu.Unlock()

	if ok {
		close(ch)
	}
}

// Timeout returns the default approval timeout.
func (m *Manager) Timeout() time.Duration {
	return m.timeout
}
