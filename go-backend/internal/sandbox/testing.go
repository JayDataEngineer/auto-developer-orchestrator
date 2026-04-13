package sandbox

import "go.uber.org/zap"

// NewTestManager creates a Manager for testing without Docker.
func NewTestManager() *Manager {
	return &Manager{
		sandboxes:       make(map[string]*Sandbox),
		desktopSessions: make(map[string]*DesktopSession),
		portAllocator:   NewPortAllocator(),
		logger:          zap.NewNop(),
	}
}

// AddTestSandbox adds a sandbox to the in-memory map (testing only).
func (m *Manager) AddTestSandbox(s *Sandbox) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sandboxes[s.ID] = s
}

// AddTestDesktopSession adds a desktop session and links it to the sandbox.
func (m *Manager) AddTestDesktopSession(id string, session *DesktopSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sandboxes[id]; ok {
		s.DesktopSession = session
		s.Mode = session.Mode
	}
	m.desktopSessions[id] = session
}
