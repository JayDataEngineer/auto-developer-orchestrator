package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
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
	dockerClient, err := client.New(client.FromEnv)
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
func (m *Manager) getContainerName(sandboxID string) string {
	return fmt.Sprintf("orchestrator-sandbox-%s", sandboxID)
}

// getEnvOrDefault returns the value of an environment variable or a default.
func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
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

// CreateSandbox creates a new OpenShell sandbox by provisioning a Docker container.
func (m *Manager) CreateSandbox(ctx context.Context, opts SandboxOptions) (*Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if empty
	if opts.ID == "" {
		opts.ID = fmt.Sprintf("sandbox-%d", time.Now().UnixMilli())
	}

	m.logger.Info("creating sandbox",
		zap.String("id", opts.ID),
		zap.String("project", opts.ProjectPath),
		zap.String("policy", opts.Policy),
	)

	containerName := m.getContainerName(opts.ID)
	image := getEnvOrDefault("OPENSHELL_IMAGE", "nvidia/openshell:latest")
	policy := opts.Policy
	if policy == "" {
		policy = "developer"
	}
	networkName := getEnvOrDefault("OPENSHELL_NETWORK", "shared-infra")
	policiesDir := getEnvOrDefault("OPENSHELL_POLICIES_DIR", "/etc/openshell/policies")

	// Resolve project path — default to PROJECT_ROOT env var if not set
	projectPath := opts.ProjectPath
	if projectPath == "" {
		projectPath = os.Getenv("PROJECT_ROOT")
	}
	if projectPath == "" {
		projectPath = "/app/projects"
	}
	// Docker requires absolute paths for bind mounts
	if absPath, err := filepath.Abs(projectPath); err == nil {
		projectPath = absPath
	}

	// Pull image if not present locally
	_, inspectErr := m.dockerClient.ImageInspect(ctx, image)
	if inspectErr != nil {
		// Image not found locally, pull it
		m.logger.Info("pulling sandbox image", zap.String("image", image))
		pullResp, pullErr := m.dockerClient.ImagePull(ctx, image, client.ImagePullOptions{})
		if pullErr != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", image, pullErr)
		}
		if err := pullResp.Wait(ctx); err != nil {
			pullResp.Close()
			return nil, fmt.Errorf("image pull failed for %s: %w", image, err)
		}
		pullResp.Close()
	} else {
		m.logger.Info("using local sandbox image", zap.String("image", image))
	}

	// Build environment variables
	networkAllow := opts.NetworkAllow
	if networkAllow == "" {
		networkAllow = "github.com,api.anthropic.com,api.openai.com,api.openrouter.com"
	}
	envVars := []string{
		"SANDBOX_POLICY=" + policy,
		"NETWORK_ALLOW=" + networkAllow,
		"FS_READONLY=/etc,/usr,/bin,/lib,/lib64",
		"FS_READWRITE=/sandbox/workspace,/sandbox/tmp",
		"DOCKER_HOST=unix:///var/run/docker.sock",
	}

	// Create a persistent Docker volume for this sandbox
	volumeName := fmt.Sprintf("sandbox-%s-persist", opts.ID)
	if _, volErr := m.dockerClient.VolumeInspect(ctx, volumeName, client.VolumeInspectOptions{}); volErr != nil {
		// Volume doesn't exist — create it
		_, volErr = m.dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
			Name:   volumeName,
			Driver: "local",
		})
		if volErr != nil {
			m.logger.Warn("Failed to create persist volume", zap.Error(volErr))
		} else {
			m.logger.Info("Created persistent volume", zap.String("volume", volumeName))
		}
	}

	// Build resource limits
	resources := container.Resources{}
	if opts.MemoryLimit > 0 {
		resources.Memory = int64(opts.MemoryLimit) * 1024 * 1024 // MB to bytes
	}
	if opts.CPULimit > 0 {
		resources.NanoCPUs = int64(opts.CPULimit * 1e9) // cores to nanocpus
	}

	// Create the container
	createResp, err := m.dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:  image,
			Env:    envVars,
			Labels: map[string]string{"openshell.policy": policy, "openshell.sandbox-id": opts.ID},
		},
		HostConfig: &container.HostConfig{
			Binds: []string{
				projectPath + ":/sandbox/workspace",
				policiesDir + ":/etc/openshell/policies:ro",
				"/var/run/docker.sock:/var/run/docker.sock:ro",
				"/tmp:/sandbox/tmp",
				volumeName + ":/sandbox/persist",
			},
			Resources: resources,
		},
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkName: {},
			},
		},
		Name: containerName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create container %s: %w", containerName, err)
	}

	// Start the container
	if _, err := m.dockerClient.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{}); err != nil {
		// Clean up the created container on start failure
		m.dockerClient.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to start container %s: %w", containerName, err)
	}

	// Wait for supervisord to start, then restore persisted state in background
	go func() {
		time.Sleep(5 * time.Second)
		m.restorePersistedState(context.Background(), containerName)
	}()

	sandbox := &Sandbox{
		ID:          opts.ID,
		ContainerID: createResp.ID,
		ProjectPath: opts.ProjectPath,
		Policy:      policy,
		Mode:        ModeCLI,
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}

	m.sandboxes[opts.ID] = sandbox

	m.logger.Info("sandbox created",
		zap.String("id", opts.ID),
		zap.String("container", containerName),
		zap.String("container_id", createResp.ID),
	)

	return sandbox, nil
}

// DestroySandbox destroys a sandbox, stops and removes its Docker container, and cleans up resources.
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

	// Save persisted state before destroying
	containerName := m.getContainerName(id)
	m.savePersistedState(ctx, containerName)

	// Stop and remove the Docker container
	timeout := 10
	if _, err := m.dockerClient.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		m.logger.Warn("failed to stop container", zap.String("container", containerName), zap.Error(err))
	}
	if _, err := m.dockerClient.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true}); err != nil {
		m.logger.Warn("failed to remove container", zap.String("container", containerName), zap.Error(err))
	}

	delete(m.sandboxes, id)
	sandbox.Status = StatusDestroyed

	m.logger.Info("sandbox destroyed", zap.String("id", id))

	return nil
}

// restorePersistedState restores saved state from the persistent volume after container start.
func (m *Manager) restorePersistedState(ctx context.Context, containerName string) {
	cmds := []string{
		"sh", "-c",
		`# Restore Chrome profile
if [ -d /tmp/chrome-profile ] && [ -z "$(ls -A /tmp/chrome-profile 2>/dev/null)" ] && [ -d /sandbox/persist/chrome-profile ] && [ -n "$(ls -A /sandbox/persist/chrome-profile 2>/dev/null)" ]; then
  cp -a /sandbox/persist/chrome-profile/. /tmp/chrome-profile/
  echo "Restored Chrome profile"
fi
# Restore home dotfiles
if [ -d /sandbox/persist/home ] && [ -n "$(ls -A /sandbox/persist/home 2>/dev/null)" ]; then
  cp -a /sandbox/persist/home/. /root/ 2>/dev/null
  echo "Restored home dotfiles"
fi
# Reinstall previously installed packages (only those not in base image)
if [ -f /sandbox/persist/installed-packages.txt ] && [ -s /sandbox/persist/installed-packages.txt ]; then
  # Get list of packages from base image
  dpkg-query -W -f='${Package}\n' 2>/dev/null | sort > /tmp/base-packages.txt
  # Find packages not in base image
  comm -23 /sandbox/persist/installed-packages.txt /tmp/base-packages.txt > /tmp/extra-packages.txt
  if [ -s /tmp/extra-packages.txt ]; then
    EXTRA=$(cat /tmp/extra-packages.txt | tr '\n' ' ')
    apt-get update -qq 2>/dev/null
    apt-get install -y $EXTRA 2>/dev/null
    echo "Restored $(wc -w < /tmp/extra-packages.txt) extra packages"
  fi
fi`,
	}
	_, _ = m.execInContainer(ctx, containerName, cmds, false)
}

// savePersistedState saves important state to the persistent volume before container stop.
func (m *Manager) savePersistedState(ctx context.Context, containerName string) {
	cmds := []string{
		"sh", "-c",
		`# Save Chrome profile
if [ -d /tmp/chrome-profile ]; then
  mkdir -p /sandbox/persist/chrome-profile
  cp -a /tmp/chrome-profile/. /sandbox/persist/chrome-profile/
  echo "Saved Chrome profile"
fi
# Save home dotfiles
if [ -d /root ]; then
  mkdir -p /sandbox/persist/home
  cp -a /root/.bashrc /root/.profile /root/.wget-hsts /root/.config /sandbox/persist/home/ 2>/dev/null
  echo "Saved home dotfiles"
fi
# Save list of installed packages (for reinstallation on next start)
dpkg-query -W -f='${Package}\n' 2>/dev/null | sort > /sandbox/persist/installed-packages.txt
echo "Saved $(wc -l < /sandbox/persist/installed-packages.txt) packages list"`,
	}
	_, _ = m.execInContainer(ctx, containerName, cmds, false)
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

// EnableBrowserMode enables lightweight browser mode for a sandbox.
// The sandbox container image already runs Xvfb + Chrome + VNC + socat via supervisord.
// This method verifies Chrome is up and returns the session info.
func (m *Manager) EnableBrowserMode(ctx context.Context, sandboxID string) (*DesktopSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sandbox, exists := m.sandboxes[sandboxID]
	if !exists {
		// Sandbox not in memory — check if container exists and recover it
		containerName := m.getContainerName(sandboxID)
		_, inspectErr := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return nil, fmt.Errorf("sandbox %s not found", sandboxID)
		}

		// Container exists — recover it into the in-memory map
		m.logger.Info("recovering existing sandbox container",
			zap.String("sandbox_id", sandboxID),
			zap.String("container", containerName),
		)
		sandbox = &Sandbox{
			ID:          sandboxID,
			ContainerID: containerName,
			ProjectPath: "",
			Policy:      "developer",
			Mode:        ModeCLI,
			Status:      StatusRunning,
			CreatedAt:   time.Now(),
		}
		m.sandboxes[sandboxID] = sandbox
	}

	// Idempotent: return existing session if already in browser mode
	if sandbox.Mode == ModeBrowser && sandbox.DesktopSession != nil {
		return sandbox.DesktopSession, nil
	}

	m.logger.Info("enabling browser mode", zap.String("sandbox_id", sandboxID))

	// The sandbox container runs Chrome on internal port 9222 (127.0.0.1).
	// Supervisord runs socat to forward 0.0.0.0:19222 -> 127.0.0.1:9222
	// so other containers can reach CDP via the Docker network.
	displayNum := 99
	vncPort := 5900
	cdpPort := 19222  // External port (socat-forwarded)
	novncPort := 6080

	containerName := m.getContainerName(sandboxID)

	// Wait for Chrome to be ready (supervisord starts it at container boot)
	for i := range 10 {
		output, err := m.execInContainer(ctx, containerName, []string{
			"wget", "-qO-", "http://127.0.0.1:9222/json/version",
		}, false)
		if err == nil && output != "" {
			m.logger.Info("Chrome CDP ready", zap.String("output", output[:min(len(output), 100)]))
			break
		}
		m.logger.Info("waiting for Chrome CDP", zap.Int("attempt", i+1))
		time.Sleep(1 * time.Second)
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
		zap.Int("cdp_port", cdpPort),
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

	// Step 5b: Forward CDP port to 0.0.0.0 via socat
	_, socatErr := m.execInContainer(ctx, containerName, []string{
		"socat", fmt.Sprintf("TCP-LISTEN:%d,fork,bind=0.0.0.0", cdpPort),
		fmt.Sprintf("TCP:127.0.0.1:%d", cdpPort),
	}, true)
	if socatErr != nil {
		m.logger.Warn("socat forward warning", zap.Error(socatErr))
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

// GetContainerIP returns the IP address of a sandbox container on the shared network.
func (m *Manager) GetContainerIP(ctx context.Context, sandboxID string) (string, error) {
	containerName := m.getContainerName(sandboxID)
	result, err := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect container %s: %w", containerName, err)
	}

	// Get IP from the first valid network address
	for _, net := range result.Container.NetworkSettings.Networks {
		if net.IPAddress.IsValid() {
			return net.IPAddress.String(), nil
		}
	}
	return "", fmt.Errorf("no IP address found for container %s", containerName)
}
