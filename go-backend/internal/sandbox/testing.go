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

// SetTestContainerIP overrides GetContainerIP to return the given IP for a sandbox.
// This allows integration tests to redirect the VNC proxy to a fake websockify server.
func (m *Manager) SetTestContainerIP(sandboxID, ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.testContainerIPs == nil {
		m.testContainerIPs = make(map[string]string)
	}
	m.testContainerIPs[sandboxID] = ip
}
