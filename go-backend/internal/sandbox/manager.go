package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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

// getContainerName returns the Docker container name for a sandbox.
// Currently all sandboxes share the orchestrator-openshell container.
func (m *Manager) getContainerName(sandboxID string) string {
	if name := os.Getenv("OPENSHELL_CONTAINER"); name != "" {
		return name
	}
	return "orchestrator-openshell"
}

// execInContainer runs a command inside a Docker container and returns the output.
// If detach is true, the command runs in the background.
func (m *Manager) execInContainer(ctx context.Context, containerName string, cmd []string, detach bool) (string, error) {
	execCreate, err := m.dockerClient.ExecCreate(ctx, containerName, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: !detach,
		AttachStderr: !detach,
	})
	if err != nil {
		return "", fmt.Errorf("exec create failed: %w", err)
	}

	if detach {
		// Start detached — fire and forget
		_, err = m.dockerClient.ExecStart(ctx, execCreate.ID, client.ExecStartOptions{
			Detach: true,
		})
		if err != nil {
			return "", fmt.Errorf("exec start (detached) failed: %w", err)
		}
		return "", nil
	}

	// Attached execution — capture stdout+stderr
	attach, err := m.dockerClient.ExecAttach(ctx, execCreate.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("exec attach failed: %w", err)
	}
	defer attach.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, attach.Reader)
	if err != nil {
		return "", fmt.Errorf("exec read failed: %w", err)
	}

	// Check exit code
	inspect, err := m.dockerClient.ExecInspect(ctx, execCreate.ID, client.ExecInspectOptions{})
	if err != nil {
		return buf.String(), nil
	}
	if inspect.ExitCode != 0 {
		return buf.String(), fmt.Errorf("exec exited with code %d: %s", inspect.ExitCode, buf.String())
	}

	return buf.String(), nil
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

	// Verify the OpenShell container is running
	containerName := m.getContainerName(opts.ID)
	inspect, err := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("sandbox container %s not found: %w", containerName, err)
	}
	if !inspect.Container.State.Running {
		return nil, fmt.Errorf("sandbox container %s is not running (state: %s)", containerName, inspect.Container.State.Status)
	}

	sandbox.ContainerID = inspect.Container.ID
	sandbox.Status = StatusRunning
	m.sandboxes[opts.ID] = sandbox

	m.logger.Info("sandbox created", zap.String("id", opts.ID), zap.String("container", containerName))

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

	// Clean up any active mode
	if sandbox.Mode != ModeCLI && sandbox.DesktopSession != nil {
		if err := m.disableModeLocked(ctx, id); err != nil {
			m.logger.Warn("failed to cleanup desktop mode", zap.Error(err))
		}
	}

	delete(m.sandboxes, id)
	sandbox.Status = StatusDestroyed

	m.logger.Info("sandbox destroyed", zap.String("id", id))

	return nil
}

// ExecInSandbox executes a command inside a sandbox container
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

	containerName := m.getContainerName(id)
	output, err := m.execInContainer(ctx, containerName, cmd, false)
	if err != nil {
		return "", fmt.Errorf("exec failed in sandbox %s: %w", id, err)
	}

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

	// Disable any existing mode first
	if sandbox.Mode != ModeCLI && sandbox.DesktopSession != nil {
		if err := m.disableModeLocked(ctx, sandboxID); err != nil {
			m.logger.Warn("failed to disable previous mode", zap.Error(err))
		}
	}

	m.logger.Info("enabling browser mode (live browser via VNC)", zap.String("sandbox_id", sandboxID))

	// Allocate ports
	m.portMutex.Lock()
	displayNum, vncPort, cdpPort, novncPort := m.portAllocator.AllocatePorts()
	m.portMutex.Unlock()

	containerName := m.getContainerName(sandboxID)
	display := fmt.Sprintf(":%d", displayNum)

	// Step 1: Start Xvfb (virtual framebuffer)
	_, err := m.execInContainer(ctx, containerName, []string{
		"Xvfb", display, "-screen", "0", "1280x800x24", "-ac", "+extension", "RANDR",
	}, true)
	if err != nil {
		m.portMutex.Lock()
		m.portAllocator.ReleasePorts(displayNum, vncPort, cdpPort, novncPort)
		m.portMutex.Unlock()
		return nil, fmt.Errorf("failed to start Xvfb: %w", err)
	}

	// Give Xvfb a moment to initialize
	time.Sleep(500 * time.Millisecond)

	// Step 2: Start x11vnc (VNC server exposing the X display)
	_, err = m.execInContainer(ctx, containerName, []string{
		"x11vnc", "-display", display, "-rfbport", fmt.Sprintf("%d", vncPort),
		"-forever", "-shared", "-nopw", "-bg",
	}, true)
	if err != nil {
		m.logger.Warn("x11vnc start warning", zap.Error(err))
	}

	// Step 3: Start noVNC (web-based VNC viewer)
	if novncErr := m.startNoVNC(ctx, containerName, novncPort, vncPort); novncErr != nil {
		m.logger.Warn("noVNC start failed", zap.Error(novncErr))
	}

	// Step 4: Start Chrome with CDP
	_, err = m.execInContainer(ctx, containerName, []string{
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
		fmt.Sprintf("--display=%s", display),
	}, true)
	if err != nil {
		m.logger.Warn("chrome start warning", zap.Error(err))
	}

	session := &DesktopSession{
		SandboxID:  sandboxID,
		Mode:       ModeBrowser,
		DisplayNum: displayNum,
		VNCPort:    vncPort,
		CDPPort:    cdpPort,
		NoVNCPort:  novncPort,
		ViewerURL:  fmt.Sprintf("/sandbox/%s/viewer", sandboxID),
		IsActive:   true,
		StartedAt:  time.Now(),
	}

	sandbox.Mode = ModeBrowser
	sandbox.DesktopSession = session
	m.desktopSessions[sandboxID] = session

	m.logger.Info("browser mode enabled",
		zap.String("sandbox_id", sandboxID),
		zap.Int("display", displayNum),
		zap.Int("vnc_port", vncPort),
		zap.Int("cdp_port", cdpPort),
		zap.Int("novnc_port", novncPort),
	)

	return session, nil
}

// EnableDesktopMode enables full desktop mode (VNC + Xvfb + XFCE4 + Chrome) for a sandbox
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

	// Disable any existing mode first
	if sandbox.Mode != ModeCLI && sandbox.DesktopSession != nil {
		if err := m.disableModeLocked(ctx, sandboxID); err != nil {
			m.logger.Warn("failed to disable previous mode", zap.Error(err))
		}
	}

	m.logger.Info("enabling desktop mode", zap.String("sandbox_id", sandboxID))

	// Allocate ports
	m.portMutex.Lock()
	displayNum, vncPort, cdpPort, novncPort := m.portAllocator.AllocatePorts()
	m.portMutex.Unlock()

	containerName := m.getContainerName(sandboxID)
	display := fmt.Sprintf(":%d", displayNum)

	// Step 1: Start Xvfb
	_, err := m.execInContainer(ctx, containerName, []string{
		"Xvfb", display, "-screen", "0", "1920x1080x24", "-ac", "+extension", "RANDR",
	}, true)
	if err != nil {
		m.portMutex.Lock()
		m.portAllocator.ReleasePorts(displayNum, vncPort, cdpPort, novncPort)
		m.portMutex.Unlock()
		return nil, fmt.Errorf("failed to start Xvfb: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Step 2: Start window manager (xfwm4 or openbox as fallback)
	_, err = m.execInContainer(ctx, containerName, []string{
		"sh", "-c", fmt.Sprintf("DISPLAY=%s xfwm4 &>/dev/null || DISPLAY=%s openbox &>/dev/null || true", display, display),
	}, true)
	if err != nil {
		m.logger.Warn("window manager start warning", zap.Error(err))
	}

	// Step 3: Start x11vnc
	_, err = m.execInContainer(ctx, containerName, []string{
		"x11vnc", "-display", display, "-rfbport", fmt.Sprintf("%d", vncPort),
		"-forever", "-shared", "-nopw", "-bg",
	}, true)
	if err != nil {
		m.logger.Warn("x11vnc start warning", zap.Error(err))
	}

	// Step 4: Start noVNC
	if novncErr := m.startNoVNC(ctx, containerName, novncPort, vncPort); novncErr != nil {
		m.logger.Warn("noVNC start failed", zap.Error(novncErr))
	}

	// Step 5: Start Chrome with CDP
	_, err = m.execInContainer(ctx, containerName, []string{
		"google-chrome",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
		"--window-size=1280,800",
		fmt.Sprintf("--display=%s", display),
	}, true)
	if err != nil {
		m.logger.Warn("chrome start warning", zap.Error(err))
	}

	session := &DesktopSession{
		SandboxID:  sandboxID,
		Mode:       ModeDesktop,
		DisplayNum: displayNum,
		VNCPort:    vncPort,
		CDPPort:    cdpPort,
		NoVNCPort:  novncPort,
		ViewerURL:  fmt.Sprintf("/sandbox/%s/viewer", sandboxID),
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

// startNoVNC launches a websockify + noVNC web viewer inside the container
func (m *Manager) startNoVNC(ctx context.Context, containerName string, novncPort, vncPort int) error {
	cmd := []string{
		"sh", "-c",
		fmt.Sprintf(
			"websockify --web=/usr/share/novnc/ %d localhost:%d &>/tmp/novnc-%d.log || "+
				"websockify --web=/opt/noVNC/ %d localhost:%d &>/tmp/novnc-%d.log || true",
			novncPort, vncPort, novncPort,
			novncPort, vncPort, novncPort,
		),
	}
	_, err := m.execInContainer(ctx, containerName, cmd, true)
	return err
}

// DisableMode disables any active mode (browser or desktop)
func (m *Manager) DisableMode(ctx context.Context, sandboxID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.disableModeLocked(ctx, sandboxID)
}

// disableModeLocked is the internal implementation (caller must hold lock)
func (m *Manager) disableModeLocked(ctx context.Context, sandboxID string) error {
	sandbox, exists := m.sandboxes[sandboxID]
	if !exists {
		return fmt.Errorf("sandbox %s not found", sandboxID)
	}

	if sandbox.Mode == ModeCLI || sandbox.DesktopSession == nil {
		return nil
	}

	session := sandbox.DesktopSession
	containerName := m.getContainerName(sandboxID)
	display := fmt.Sprintf(":%d", session.DisplayNum)

	m.logger.Info("disabling mode",
		zap.String("sandbox_id", sandboxID),
		zap.String("current_mode", string(sandbox.Mode)),
	)

	// Kill processes in reverse order: Chrome → x11vnc → Xvfb → websockify
	killCmds := [][]string{
		{"pkill", "-f", fmt.Sprintf("--display=%s", display)},
		{"pkill", "-f", fmt.Sprintf("rfbport %d", session.VNCPort)},
		{"pkill", "-f", fmt.Sprintf("Xvfb %s", display)},
		{"pkill", "-f", fmt.Sprintf("websockify.*%d", session.NoVNCPort)},
	}

	for _, cmd := range killCmds {
		_, err := m.execInContainer(ctx, containerName, cmd, false)
		if err != nil {
			m.logger.Debug("kill command (expected if process not running)",
				zap.Strings("cmd", cmd),
				zap.Error(err),
			)
		}
	}

	// Release allocated ports
	m.portMutex.Lock()
	m.portAllocator.ReleasePorts(session.DisplayNum, session.VNCPort, session.CDPPort, session.NoVNCPort)
	m.portMutex.Unlock()

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
