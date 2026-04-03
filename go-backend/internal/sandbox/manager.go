package sandbox

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/moby/moby/client"
	"go.uber.org/zap"
)

// Manager handles OpenShell sandbox lifecycle
type Manager struct {
	dockerClient   *client.Client
	sandboxes      map[string]*Sandbox
	desktopSessions map[string]*DesktopSession
	portAllocator  *PortAllocator
	mu             sync.RWMutex
	portMutex      sync.Mutex
	logger         *zap.Logger
}

// NewManager creates a new sandbox manager
func NewManager(logger *zap.Logger) (*Manager, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Manager{
		dockerClient:    dockerClient,
		sandboxes:       make(map[string]*Sandbox),
		desktopSessions: make(map[string]*DesktopSession),
		portAllocator:   NewPortAllocator(),
		logger:          logger,
	}, nil
}

// CreateSandbox creates a new OpenShell sandbox
func (m *Manager) CreateSandbox(ctx context.Context, opts SandboxOptions) (*Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("creating sandbox",
		zap.String("id", opts.ID),
		zap.String("project", opts.ProjectPath),
		zap.String("policy", opts.Policy),
	)

	sandbox := &Sandbox{
		ID:          opts.ID,
		ProjectPath: opts.ProjectPath,
		Policy:      opts.Policy,
		Mode:        ModeCLI,
		Status:      StatusPending,
		CreatedAt:   time.Now(),
	}

	// TODO: Actually create Docker container with OpenShell image
	// For now, we just track it in memory
	// In production, this would:
	// 1. Create container with nvidia/openshell:latest
	// 2. Apply security policies (filesystem, network, process)
	// 3. Mount project volume
	// 4. Start container

	sandbox.Status = StatusRunning
	m.sandboxes[opts.ID] = sandbox

	m.logger.Info("sandbox created", zap.String("id", opts.ID))

	return sandbox, nil
}

// DestroySandbox destroys a sandbox and cleans up resources
func (m *Manager) DestroySandbox(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sandbox, exists := m.sandboxes[id]
	if !exists {
		return fmt.Errorf("sandbox %s not found", id)
	}

	m.logger.Info("destroying sandbox", zap.String("id", id))

	// Clean up any active mode if active
	if sandbox.Mode != ModeCLI && sandbox.DesktopSession != nil {
		if err := m.disableModeLocked(id); err != nil {
			m.logger.Warn("failed to cleanup desktop mode", zap.Error(err))
		}
	}

	// TODO: Actually destroy Docker container
	delete(m.sandboxes, id)
	sandbox.Status = StatusDestroyed

	m.logger.Info("sandbox destroyed", zap.String("id", id))

	return nil
}

// ExecInSandbox executes a command inside a sandbox
func (m *Manager) ExecInSandbox(ctx context.Context, id string, cmd []string) (string, error) {
	m.mu.RLock()
	sandbox, exists := m.sandboxes[id]
	m.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("sandbox %s not found", id)
	}

	if sandbox.Status != StatusRunning {
		return "", fmt.Errorf("sandbox %s is not running (status: %s)", id, sandbox.Status)
	}

	m.logger.Debug("executing command in sandbox",
		zap.String("id", id),
		zap.Strings("cmd", cmd),
	)

	// TODO: Actually execute command in Docker container
	// For now, return a placeholder
	output := fmt.Sprintf("[sandbox %s] executed: %v", id, cmd)

	return output, nil
}

// GetSandbox returns a sandbox by ID
func (m *Manager) GetSandbox(id string) (*Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sandbox, exists := m.sandboxes[id]
	if !exists {
		return nil, fmt.Errorf("sandbox %s not found", id)
	}

	return sandbox, nil
}

// ListSandboxes returns all active sandboxes
func (m *Manager) ListSandboxes() []*Sandbox {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Sandbox, 0, len(m.sandboxes))
	for _, s := range m.sandboxes {
		result = append(result, s)
	}

	return result
}

// EnableBrowserMode enables lightweight browser mode (Xvfb + Chrome + VNC) for a sandbox
// This gives you a LIVE browser window via VNC, not just screenshots
func (m *Manager) EnableBrowserMode(ctx context.Context, sandboxID string) (*DesktopSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sandbox, exists := m.sandboxes[sandboxID]
	if !exists {
		return nil, fmt.Errorf("sandbox %s not found", sandboxID)
	}

	// Idempotent: return existing session if already in browser mode
	if sandbox.Mode == ModeBrowser && sandbox.DesktopSession != nil {
		return sandbox.DesktopSession, nil
	}

	m.logger.Info("enabling browser mode (live browser via VNC)", zap.String("sandbox_id", sandboxID))

	// Allocate ports
	m.portMutex.Lock()
	displayNum, vncPort, cdpPort, novncPort := m.portAllocator.AllocatePorts()
	m.portMutex.Unlock()

	// Start minimal browser environment:
	// 1. Xvfb - virtual display for Chrome
	// 2. x11vnc - expose Chrome window via VNC
	// 3. Chrome - browser with CDP enabled
	browserCmds := [][]string{
		// Start Xvfb virtual display (Chrome-only, no desktop environment)
		{"Xvfb", fmt.Sprintf(":%d", displayNum), "-screen", "0", "1280x800x24", "-ac", "+extension", "RANDR"},
		
		// Start x11vnc VNC server (exposes Chrome window)
		{"x11vnc", "-display", fmt.Sprintf(":%d", displayNum), "-rfbport", fmt.Sprintf("%d", vncPort), "-forever", "-shared", "-nopw"},
		
		// Start Google Chrome with CDP (visible in VNC)
		{
			"google-chrome",
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-gpu",
			fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
			"--window-size=1280,800",
			"--start-maximized",
			"--disable-extensions",
			"--disable-background-networking",
			"--disable-default-apps",
			"--disable-sync",
			fmt.Sprintf("--display=:%d", displayNum),
		},
	}

	for _, cmd := range browserCmds {
		m.logger.Debug("would execute in sandbox", zap.Strings("cmd", cmd))
	}

	session := &DesktopSession{
		SandboxID:  sandboxID,
		Mode:       ModeBrowser,
		DisplayNum: displayNum,
		VNCPort:    vncPort,
		CDPPort:    cdpPort,
		NoVNCPort:  novncPort,
		ViewerURL:  fmt.Sprintf("/sandbox/%s/browser-viewer", sandboxID),
		IsActive:   true,
		StartedAt:  time.Now(),
	}

	sandbox.Mode = ModeBrowser
	sandbox.DesktopSession = session
	m.desktopSessions[sandboxID] = session

	m.logger.Info("browser mode enabled (live browser)",
		zap.String("sandbox_id", sandboxID),
		zap.Int("display", displayNum),
		zap.Int("vnc_port", vncPort),
		zap.Int("cdp_port", cdpPort),
	)

	return session, nil
}

// EnableDesktopMode enables full desktop mode (VNC + Xvfb + Chrome) for a sandbox
func (m *Manager) EnableDesktopMode(ctx context.Context, sandboxID string) (*DesktopSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sandbox, exists := m.sandboxes[sandboxID]
	if !exists {
		return nil, fmt.Errorf("sandbox %s not found", sandboxID)
	}

	// Idempotent: return existing session if already in desktop mode
	if sandbox.Mode == ModeDesktop && sandbox.DesktopSession != nil {
		return sandbox.DesktopSession, nil
	}

	m.logger.Info("enabling desktop mode", zap.String("sandbox_id", sandboxID))

	// Allocate ports (with locking to prevent race conditions)
	m.portMutex.Lock()
	displayNum, vncPort, cdpPort, novncPort := m.portAllocator.AllocatePorts()
	m.portMutex.Unlock()

	// Start desktop environment inside sandbox
	desktopCmds := [][]string{
		// Start Xvfb virtual display
		{"Xvfb", fmt.Sprintf(":%d", displayNum), "-screen", "0", "1280x800x24", "-ac", "+extension", "RANDR"},
		
		// Start x11vnc VNC server
		{"x11vnc", "-display", fmt.Sprintf(":%d", displayNum), "-rfbport", fmt.Sprintf("%d", vncPort), "-forever", "-shared", "-nopw"},
		
		// Start Google Chrome with CDP
		{
			"google-chrome",
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-gpu",
			fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
			"--window-size=1280,800",
			"--start-maximized",
			fmt.Sprintf("--display=:%d", displayNum),
		},
	}

	for _, cmd := range desktopCmds {
		m.logger.Debug("would execute in sandbox", zap.Strings("cmd", cmd))
	}

	session := &DesktopSession{
		SandboxID:  sandboxID,
		Mode:       ModeDesktop,
		DisplayNum: displayNum,
		VNCPort:    vncPort,
		CDPPort:    cdpPort,
		NoVNCPort:  novncPort,
		ViewerURL:  fmt.Sprintf("/sandbox/%s/desktop-viewer", sandboxID),
		IsActive:   true,
		StartedAt:  time.Now(),
	}

	sandbox.Mode = ModeDesktop
	sandbox.DesktopSession = session
	m.desktopSessions[sandboxID] = session

	m.logger.Info("desktop mode enabled",
		zap.String("sandbox_id", sandboxID),
		zap.Int("display", displayNum),
		zap.Int("vnc_port", vncPort),
		zap.Int("cdp_port", cdpPort),
		zap.Int("novnc_port", novncPort),
	)

	return session, nil
}

// DisableMode disables any active mode (browser or desktop)
func (m *Manager) DisableMode(ctx context.Context, sandboxID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.disableModeLocked(sandboxID)
}

// disableModeLocked is the internal implementation (caller must hold lock)
func (m *Manager) disableModeLocked(sandboxID string) error {
	sandbox, exists := m.sandboxes[sandboxID]
	if !exists {
		return fmt.Errorf("sandbox %s not found", sandboxID)
	}

	if sandbox.Mode == ModeCLI || sandbox.DesktopSession == nil {
		return nil // Already in CLI mode or no session
	}

	m.logger.Info("disabling mode",
		zap.String("sandbox_id", sandboxID),
		zap.String("current_mode", string(sandbox.Mode)),
	)

	// TODO: Actually stop Chrome, Xvfb, x11vnc processes in sandbox
	// This would execute kill commands or send termination signals

	delete(m.desktopSessions, sandboxID)
	sandbox.Mode = ModeCLI
	sandbox.DesktopSession = nil

	m.logger.Info("sandbox mode reset to CLI", zap.String("sandbox_id", sandboxID))

	return nil
}

// Deprecated: Use DisableMode instead
func (m *Manager) DisableDesktopMode(ctx context.Context, sandboxID string) error {
	return m.DisableMode(ctx, sandboxID)
}

// disableDesktopModeLocked is the internal implementation (caller must hold lock)
// Deprecated: Use disableModeLocked instead
func (m *Manager) disableDesktopModeLocked(sandboxID string) error {
	return m.disableModeLocked(sandboxID)
}

// GetDesktopSession returns the desktop session for a sandbox
func (m *Manager) GetDesktopSession(sandboxID string) (*DesktopSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.desktopSessions[sandboxID]
	if !exists {
		return nil, fmt.Errorf("no desktop session for sandbox %s", sandboxID)
	}

	return session, nil
}
