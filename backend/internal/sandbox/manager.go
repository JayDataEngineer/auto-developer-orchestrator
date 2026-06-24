package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"go.uber.org/zap"
)

// ValidationError is returned when sandbox options fail input validation
// (e.g. an unsupported URL scheme in ProjectPath). Handlers should map this
// to HTTP 400 Bad Request — distinct from internal failures (HTTP 500).
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// newValidationError wraps a plain message into a ValidationError sentinel.
func newValidationError(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Manager handles OpenShell sandbox lifecycle
type Manager struct {
	dockerClient    *client.Client
	sandboxes       map[string]*Sandbox
	desktopSessions map[string]*DesktopSession
	portAllocator   *PortAllocator
	mu              sync.RWMutex
	portMutex       sync.Mutex
	logger          *zap.Logger

	// testContainerIPs overrides GetContainerIP in tests (nil in production).
	testContainerIPs map[string]string
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
	if m.dockerClient == nil {
		return "", fmt.Errorf("docker client not available")
	}
	execCreate, err := m.dockerClient.ExecCreate(ctx, containerName, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: !detach,
		AttachStderr: !detach,
		TTY:          !detach, // TTY avoids Docker multiplexed stream framing
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

	// Attached execution — use TTY mode to get a clean, non-multiplexed stream.
	// Without TTY, Docker sends a multiplexed stream with 8-byte headers per frame
	// that corrupt binary data (like base64-encoded screenshots).
	attach, err := m.dockerClient.ExecAttach(ctx, execCreate.ID, client.ExecAttachOptions{
		TTY: true,
	})
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
	image := getEnvOrDefault("OPENSHELL_IMAGE", "pux-sandbox:latest")
	// Org-mode image override: when the caller (pux_prompt.go) supplies an
	// Image in SandboxOptions, it wins over the env default. This is how
	// orgs like video-production get their specialized sandbox image used
	// instead of the generic pux-sandbox:latest.
	if opts.Image != "" {
		image = opts.Image
	}
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

	// Reject URL-style project paths (ssh://, file://, http://, ...).
	// Docker bind mounts require a local filesystem path. URL schemes contain
	// colons that corrupt Docker's `-v host:container[:mode]` parsing — for
	// ssh://user@host/path the container path `/sandbox/workspace` ends up in
	// the mode slot and Docker returns "invalid mode: /sandbox/workspace".
	// SSH projects are designed to run via SSHExecutor on the host, not inside
	// a local Docker sandbox, so this combination is unsupported.
	if parsed, perr := url.Parse(projectPath); perr == nil && parsed.Scheme != "" {
		m.mu.Unlock()
		return nil, newValidationError(
			"sandboxes require a local filesystem path; received %s URL %q. "+
				"Open a local project in the sidebar, or for remote projects use the host agent loop (SSHExecutor) instead of a sandbox.",
			parsed.Scheme, projectPath,
		)
	}

	// Docker requires absolute paths for bind mounts
	if absPath, err := filepath.Abs(projectPath); err == nil {
		projectPath = absPath
	}

	// Pull image if not present locally
	if m.dockerClient == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("docker client not available")
	}
	_, inspectErr := m.dockerClient.ImageInspect(ctx, image)
	if inspectErr != nil {
		// Image not found locally, pull it
		m.logger.Info("pulling sandbox image", zap.String("image", image))
		pullResp, pullErr := m.dockerClient.ImagePull(ctx, image, client.ImagePullOptions{})
		if pullErr != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("failed to pull image %s: %w", image, pullErr)
		}
		if err := pullResp.Wait(ctx); err != nil {
			pullResp.Close()
			m.mu.Unlock()
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
		"HOST_GATEWAY=host.docker.internal",
	}
	// Org-mode env propagation: caller may pass extra env vars (e.g.
	// VIDEO_PRODUCTION_ROOT) declared in the org's [sandbox.env] block.
	// These override defaults if they collide (last-wins, matching Docker
	// semantics).
	if len(opts.Env) > 0 {
		envVars = append(envVars, opts.Env...)
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

	// Build bind mounts
	binds := []string{
		projectPath + ":/sandbox/workspace",
		policiesDir + ":/etc/openshell/policies:ro",
		"/tmp:/sandbox/tmp",
		volumeName + ":/sandbox/persist",
	}
	// Org-mode volume propagation: caller may pass org-declared volumes
	// (e.g. video-production's named workspace volume mounted at
	// /workspace/video-productions). Each entry renders to a Docker bind
	// string via SandboxVolume.BindString(). Malformed entries are skipped
	// so one bad row doesn't fail sandbox creation.
	for _, vol := range opts.Volumes {
		bind := vol.BindString()
		if bind == "" {
			m.logger.Warn("Skipping malformed org sandbox volume",
				zap.String("type", vol.Type),
				zap.String("name", vol.Name),
				zap.String("container", vol.Container))
			continue
		}
		binds = append(binds, bind)
	}
	hostConfig := &container.HostConfig{
		Binds:     binds,
		Resources: resources,
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}
	netConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {},
		},
	}

	// Bridged mode: mount host X11 socket + use host network
	if opts.Tier == TierBridged {
		hostDisplay := os.Getenv("DISPLAY")
		if hostDisplay != "" {
			binds = append(binds, "/tmp/.X11-unix:/tmp/.X11-unix")
			envVars = append(envVars, "DISPLAY="+hostDisplay)
		}
		hostConfig.NetworkMode = "host"
		netConfig = nil // host network mode is incompatible with endpoint config
	}

	// Create the container
	createResp, err := m.dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:  image,
			Env:    envVars,
			Labels: map[string]string{
				"openshell.policy":       policy,
				"openshell.sandbox-id":   opts.ID,
				"openshell.project-path": projectPath,
			},
		},
		HostConfig:       hostConfig,
		NetworkingConfig: netConfig,
		Name:            containerName,
	})
	if err != nil {
		// Container name conflict — remove stale stopped container and retry
		if strings.Contains(err.Error(), "already in use") || strings.Contains(err.Error(), "Conflict") {
			m.logger.Info("removing stale container", zap.String("name", containerName))
			m.dockerClient.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true})
			createResp, err = m.dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
				Config: &container.Config{
					Image:  image,
					Env:    envVars,
					Labels: map[string]string{
						"openshell.policy":       policy,
						"openshell.sandbox-id":   opts.ID,
						"openshell.project-path": projectPath,
					},
				},
				HostConfig:       hostConfig,
				NetworkingConfig: netConfig,
				Name:            containerName,
			})
			if err != nil {
				m.mu.Unlock()
				return nil, fmt.Errorf("failed to create container %s (retry): %w", containerName, err)
			}
		} else {
			m.mu.Unlock()
			return nil, fmt.Errorf("failed to create container %s: %w", containerName, err)
		}
	}

	// Start the container
	if _, err := m.dockerClient.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{}); err != nil {
		// Clean up the created container on start failure
		m.dockerClient.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true})
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to start container %s: %w", containerName, err)
	}

	// Restore persisted state (Chrome profile, dotfiles, packages) synchronously.
	// This is just file copies — typically completes in <2s.
	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer restoreCancel()
	m.restorePersistedState(restoreCtx, containerName)

	sandbox := &Sandbox{
		ID:          opts.ID,
		ContainerID: createResp.ID,
		ProjectPath: projectPath,
		Policy:      policy,
		Mode:        ModeCLI,
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
		Tier:        opts.Tier,
	}

	m.sandboxes[opts.ID] = sandbox

	m.logger.Info("sandbox created",
		zap.String("id", opts.ID),
		zap.String("container", containerName),
		zap.String("container_id", createResp.ID),
	)

	// Auto-enable browser/desktop mode if requested.
	// Must release lock first since EnableBrowserMode/EnableDesktopMode acquire it.
	mode := opts.InitialMode
	if mode == "" {
		mode = ModeBrowser // default: OpenShell image already runs Chrome via supervisord
	}
	if mode == ModeCLI {
		m.mu.Unlock()
		return sandbox, nil
	}

	m.mu.Unlock() // release before calling enable methods that acquire the lock
	var session *DesktopSession
	var modeErr error
	if mode == ModeDesktop {
		session, modeErr = m.EnableDesktopMode(ctx, opts.ID)
	} else {
		session, modeErr = m.EnableBrowserMode(ctx, opts.ID)
	}
	if modeErr != nil {
		m.logger.Warn("auto-enable mode failed, sandbox stays in CLI mode",
			zap.String("mode", string(mode)),
			zap.Error(modeErr))
	} else {
		m.logger.Info("auto-enabled mode",
			zap.String("mode", string(mode)),
			zap.Int("cdp_port", session.CDPPort))
	}

	// Create workspace scaffold directories (memos, .pux/sessions) inside the container
	// so agents can write artifacts and session files without path errors.
	// We chown them to match the host project dir owner so they're writable by the user.
	scaffoldDirs := []string{
		"/sandbox/workspace/memos",
		"/sandbox/workspace/.pux/sessions",
	}
	// Detect host project dir ownership to set correct UID inside container
	var chownUID string
	if info, err := os.Stat(projectPath); err == nil {
		chownUID = fmt.Sprintf("%d", info.Sys().(*syscall.Stat_t).Uid)
	}
	for _, dir := range scaffoldDirs {
		execCreate, err := m.dockerClient.ExecCreate(ctx, containerName, client.ExecCreateOptions{
			Cmd: []string{"mkdir", "-p", dir},
		})
		if err != nil {
			continue
		}
		m.dockerClient.ExecAttach(ctx, execCreate.ID, client.ExecAttachOptions{})
		// Fix ownership so the dir is writable from the host
		if chownUID != "" {
			chownCreate, err := m.dockerClient.ExecCreate(ctx, containerName, client.ExecCreateOptions{
				Cmd: []string{"chown", "-R", chownUID + ":" + chownUID, dir},
			})
			if err == nil {
				m.dockerClient.ExecAttach(ctx, chownCreate.ID, client.ExecAttachOptions{})
			}
		}
	}

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

	if m.dockerClient == nil {
		// In-memory only (testing) — just remove from map
		delete(m.sandboxes, id)
		delete(m.desktopSessions, id)
		return nil
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

// ExecInSandboxRaw executes a command inside a sandbox and returns output + exit code.
// Unlike ExecInSandbox, non-zero exit codes are returned in the result, not as errors.
func (m *Manager) ExecInSandboxRaw(ctx context.Context, id string, cmd []string) (string, int, error) {
	m.mu.RLock()
	sandbox, exists := m.sandboxes[id]
	m.mu.RUnlock()

	if !exists {
		return "", -1, fmt.Errorf("sandbox %s not found", id)
	}

	if sandbox.Status != StatusRunning {
		return "", -1, fmt.Errorf("sandbox %s is not running (status: %s)", id, sandbox.Status)
	}

	if m.dockerClient == nil {
		return "", -1, fmt.Errorf("docker client not available")
	}

	containerName := m.getContainerName(id)

	execCreate, err := m.dockerClient.ExecCreate(ctx, containerName, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
	})
	if err != nil {
		return "", -1, fmt.Errorf("exec create failed: %w", err)
	}

	attach, err := m.dockerClient.ExecAttach(ctx, execCreate.ID, client.ExecAttachOptions{
		TTY: true,
	})
	if err != nil {
		return "", -1, fmt.Errorf("exec attach failed: %w", err)
	}
	defer attach.Close()

	var buf bytes.Buffer
	io.Copy(&buf, attach.Reader)

	inspect, err := m.dockerClient.ExecInspect(ctx, execCreate.ID, client.ExecInspectOptions{})
	if err != nil {
		return buf.String(), -1, nil
	}

	return buf.String(), inspect.ExitCode, nil
}

// CopyToSandbox uploads a local file into a sandbox container at the given path.
// Uses the base64 echo pipe pattern to safely transfer data through Docker exec.
func (m *Manager) CopyToSandbox(ctx context.Context, sandboxID, localPath, sandboxPath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file %s: %w", localPath, err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := fmt.Sprintf("mkdir -p '%s' && echo '%s' | base64 -d > '%s'",
		ShellEscape(filepath.Dir(sandboxPath)), encoded, ShellEscape(sandboxPath))

	if _, err := m.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", cmd}); err != nil {
		return fmt.Errorf("copy to sandbox failed: %w", err)
	}
	return nil
}

// PipInstall runs pip install for the given packages in a sandbox.
// Uses python3 -m pip which works reliably across OpenShell images.
func (m *Manager) PipInstall(ctx context.Context, sandboxID string, packages []string) error {
	if len(packages) == 0 {
		return nil
	}

	args := "python3 -m pip install --break-system-packages " + strings.Join(packages, " ")

	if _, err := m.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", args}); err != nil {
		return fmt.Errorf("pip install failed: %w", err)
	}
	return nil
}

// WriteEnvFile writes environment variables as a .env file in the sandbox.
// Values with ${VAR} syntax are resolved from the host environment.
func (m *Manager) WriteEnvFile(ctx context.Context, sandboxID string, envVars map[string]string) error {
	if len(envVars) == 0 {
		return nil
	}

	var buf strings.Builder
	for key, value := range envVars {
		// Resolve ${VAR} references from host environment
		resolved := envVarRegex.ReplaceAllStringFunc(value, func(match string) string {
			varName := match[2 : len(match)-1] // strip ${ and }
			if v := os.Getenv(varName); v != "" {
				return v
			}
			return match // leave unresolved
		})
		buf.WriteString(key)
		buf.WriteByte('=')
		buf.WriteString(resolved)
		buf.WriteByte('\n')
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(buf.String()))
	cmd := fmt.Sprintf("echo '%s' | base64 -d > /sandbox/.env", encoded)

	if _, err := m.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", cmd}); err != nil {
		return fmt.Errorf("write .env failed: %w", err)
	}
	return nil
}

// envVarRegex matches ${VAR_NAME} patterns in env var values.
var envVarRegex = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

// verifyContainerExists checks that the Docker container for a sandbox is actually
// running. Removes stale entries from the sandbox map if the container is gone.
func (m *Manager) verifyContainerExists(sb *Sandbox) bool {
	if m.dockerClient == nil {
		return true // no docker — assume it exists
	}
	ctx := context.Background()
	containerName := "orchestrator-sandbox-" + sb.ID
	if _, err := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{}); err != nil {
		// Container gone — remove stale entry
		m.mu.Lock()
		delete(m.sandboxes, sb.ID)
		m.mu.Unlock()
		return false
	}
	return true
}

// FindSandboxByProject finds a sandbox by project path basename or project path.
// Returns the first running sandbox whose ProjectPath ends with name, or whose ID matches name.
// Verifies the Docker container is actually running before returning.
// Prioritizes exact path matches over fuzzy basename matches to avoid routing collisions.
// If a stopped container is found, it is started and recovered automatically.
func (m *Manager) FindSandboxByProject(name string) *Sandbox {
	m.mu.RLock()

	// Phase 1: Exact match — by ID or full project path
	if sb, ok := m.sandboxes[name]; ok {
		m.mu.RUnlock()
		if m.verifyContainerExists(sb) {
			return sb
		}
		m.mu.RLock()
	}
	for _, sb := range m.sandboxes {
		if sb.Status != StatusRunning {
			continue
		}
		if sb.ProjectPath == name {
			m.mu.RUnlock()
			if m.verifyContainerExists(sb) {
				return sb
			}
			m.mu.RLock()
		}
	}

	// Phase 2: Fuzzy match — by basename or suffix (less reliable)
	var candidates []*Sandbox
	for _, sb := range m.sandboxes {
		if sb.Status != StatusRunning {
			continue
		}
		if filepath.Base(sb.ProjectPath) == name || strings.HasSuffix(sb.ProjectPath, "/"+name) {
			// Skip sandboxes whose project path doesn't actually match —
			// avoids routing to a sandbox with different bind mounts.
			if sb.ProjectPath != name {
				continue
			}
			candidates = append(candidates, sb)
		}
	}
	m.mu.RUnlock()

	// Verify the Docker container actually exists for each candidate
	for _, sb := range candidates {
		if m.verifyContainerExists(sb) {
			return sb
		}
	}

	// Phase 1.5: discover by openshell.project-path label.
	// Catches containers started outside Pux (e.g. `docker compose up` via
	// org bootstrap.sh) that carry the same label Pux uses internally on
	// its own containers. Without this, Pux silently spins up a sibling
	// container instead of adopting the running one — wasteful and a
	// classic source of "agent writes to container A, user reads from
	// container B" file-visibility bugs.
	if m.dockerClient != nil {
		if sb := m.discoverByProjectLabel(name); sb != nil {
			return sb
		}
	}

	// No in-memory match — try to find a stopped Docker container by project name.
	// Try both the full path (legacy, when req.Project was an absolute path) and
	// the basename (current, when ID is derived from filepath.Base).
	if m.dockerClient != nil {
		ctx := context.Background()
		candidates := []string{name}
		if base := filepath.Base(name); base != name {
			candidates = append(candidates, base)
		}
		for _, candidate := range candidates {
			containerName := "orchestrator-sandbox-" + candidate
			result, err := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
			if err != nil {
				continue
			}
			if result.Container.State == nil || result.Container.State.Running {
				continue
			}
			m.logger.Info("found stopped sandbox container, restarting", zap.String("name", containerName))
			if _, err := m.dockerClient.ContainerStart(ctx, result.Container.ID, client.ContainerStartOptions{}); err != nil {
				m.logger.Warn("failed to start stopped container", zap.Error(err))
				return nil
			}
			// Recover into in-memory map using the container's actual sandbox-id label
			sandboxID := candidate
			projectPath := ""
			policy := "developer"
			if result.Container.Config != nil && result.Container.Config.Labels != nil {
				if id := result.Container.Config.Labels["openshell.sandbox-id"]; id != "" {
					sandboxID = id
				}
				projectPath = result.Container.Config.Labels["openshell.project-path"]
				if p := result.Container.Config.Labels["openshell.policy"]; p != "" {
					policy = p
				}
			}
			// Validate: the restarted sandbox's mount must match the requested project.
			// A mismatch means an old container with different bind mounts — using it
			// would cause file visibility bugs (CTO and sub-agents write to different dirs).
			if projectPath != name {
				m.logger.Warn("stopped sandbox has different project path, discarding",
					zap.String("container", containerName),
					zap.String("sandbox_project", projectPath),
					zap.String("requested", name))
				// Stop the container we just started — CreateSandbox will make a fresh one
				timeout := 5
				_, _ = m.dockerClient.ContainerStop(ctx, result.Container.ID, client.ContainerStopOptions{Timeout: &timeout})
				return nil
			}
			sb := &Sandbox{
				ID:          sandboxID,
				ContainerID: result.Container.ID,
				ProjectPath: projectPath,
				Policy:      policy,
				Mode:        ModeCLI,
				Status:      StatusRunning,
				CreatedAt:   time.Now(),
			}
			m.mu.Lock()
			m.sandboxes[sandboxID] = sb
			m.mu.Unlock()
			return sb
		}
	}

	return nil
}

// discoverByProjectLabel queries Docker for running containers carrying
// openshell.project-path=<projectPath> that aren't already tracked in
// m.sandboxes. Returns the first match adopted into the in-memory map.
//
// Contract: org bootstrap.sh exports OPENSHELL_PROJECT_PATH so compose
// can attach both the label AND the matching bind mount
// (${OPENSHELL_PROJECT_PATH}:/sandbox/workspace). The label is the
// discovery signal; the matching mount is what makes the adoption safe
// — the bash executor assumes /sandbox/workspace maps to ProjectPath,
// so a container with a different mount would silently break file ops.
//
// Containers created by Pux already carry this label, but Phase 1 of
// FindSandboxByProject catches them via the in-memory map first. We
// reach here only when no in-memory match exists, so the discovery
// exclusively catches externally-created (compose, manual docker run)
// containers.
func (m *Manager) discoverByProjectLabel(projectPath string) *Sandbox {
	if projectPath == "" || m.dockerClient == nil {
		return nil
	}
	ctx := context.Background()
	result, err := m.dockerClient.ContainerList(ctx, client.ContainerListOptions{
		Filters: client.Filters{
			"label": {"openshell.project-path=" + projectPath: true},
		},
	})
	if err != nil {
		m.logger.Warn("container discovery by label failed",
			zap.String("project", projectPath),
			zap.Error(err))
		return nil
	}
	for _, c := range result.Items {
		// Skip already-tracked sandboxes — Phase 1 should have caught these,
		// but State transitions can race. Re-checking under the read lock is cheap.
		existingID := c.Labels["openshell.sandbox-id"]
		m.mu.RLock()
		_, inMem := m.sandboxes[existingID]
		m.mu.RUnlock()
		if inMem {
			continue
		}
		sb := adoptContainerAsSandbox(c, projectPath)
		if sb == nil {
			continue
		}
		m.mu.Lock()
		m.sandboxes[sb.ID] = sb
		m.mu.Unlock()
		m.logger.Info("adopted external container by project-path label",
			zap.String("sandbox_id", sb.ID),
			zap.String("container_id", c.ID),
			zap.String("project", projectPath),
			zap.String("image", c.Image))
		return sb
	}
	return nil
}

// adoptContainerAsSandbox constructs a *Sandbox from a Docker container
// summary, deriving ID/policy from labels with sensible fallbacks.
// Pure function — extracted so the adoption contract can be unit-tested
// without spinning up Docker.
//
// Returns nil if the container lacks the minimum signal (project-path
// label must match what we queried by).
func adoptContainerAsSandbox(c container.Summary, projectPath string) *Sandbox {
	if c.Labels == nil {
		return nil
	}
	if c.Labels["openshell.project-path"] != projectPath {
		return nil
	}
	sandboxID := c.Labels["openshell.sandbox-id"]
	if sandboxID == "" {
		// Compose containers may not carry this label; fall back to name
		// (preferred — human-readable) or container ID short hash.
		if len(c.Names) > 0 {
			sandboxID = strings.TrimPrefix(c.Names[0], "/")
		} else if len(c.ID) >= 12 {
			sandboxID = c.ID[:12]
		} else {
			sandboxID = c.ID
		}
	}
	policy := c.Labels["openshell.policy"]
	if policy == "" {
		policy = "developer"
	}
	return &Sandbox{
		ID:          sandboxID,
		ContainerID: c.ID,
		ProjectPath: projectPath,
		Policy:      policy,
		Mode:        ModeCLI,
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}
}

func (m *Manager) GetSandbox(id string) (*Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sandbox, exists := m.sandboxes[id]
	if !exists {
		return nil, fmt.Errorf("sandbox %s not found", id)
	}

	return sandbox, nil
}

// RecoverSandbox recovers an existing Docker container into the in-memory map
// without creating a new one. Used when the backend restarts but containers are still running.
func (m *Manager) RecoverSandbox(ctx context.Context, sandboxID string) error {
	if m.dockerClient == nil {
		return fmt.Errorf("docker client not available")
	}
	containerName := m.getContainerName(sandboxID)
	result, err := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("container %s not found: %w", containerName, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Recover metadata from container labels
	projectPath := ""
	policy := "developer"
	if result.Container.Config != nil && result.Container.Config.Labels != nil {
		projectPath = result.Container.Config.Labels["openshell.project-path"]
		if p := result.Container.Config.Labels["openshell.policy"]; p != "" {
			policy = p
		}
	}

	sandbox := &Sandbox{
		ID:          sandboxID,
		ContainerID: result.Container.ID,
		ProjectPath: projectPath,
		Policy:      policy,
		Mode:        ModeCLI,
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}
	m.sandboxes[sandboxID] = sandbox

	m.logger.Info("recovered existing sandbox container",
		zap.String("sandbox_id", sandboxID),
		zap.String("container_id", result.Container.ID),
	)
	return nil
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

// IsReady returns true if the sandbox's current mode is fully operational.
func (m *Manager) IsReady(sandboxID string) bool {
	m.mu.RLock()
	sb, exists := m.sandboxes[sandboxID]
	m.mu.RUnlock()
	if !exists {
		return false
	}
	switch sb.Mode {
	case ModeCLI:
		return sb.Status == StatusRunning
	case ModeBrowser, ModeDesktop:
		return sb.DesktopSession != nil && sb.DesktopSession.IsActive
	}
	return false
}

// GetContainerIP returns the IP address of a sandbox container on the shared network.
func (m *Manager) GetContainerIP(ctx context.Context, sandboxID string) (string, error) {
	// Test override — allows integration tests to redirect VNC proxy to fake websockify.
	if m.testContainerIPs != nil {
		m.mu.RLock()
		ip, ok := m.testContainerIPs[sandboxID]
		m.mu.RUnlock()
		if ok {
			return ip, nil
		}
	}
	if m.dockerClient == nil {
		return "", fmt.Errorf("docker client not available")
	}
	containerName := m.getContainerName(sandboxID)
	result, err := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect container %s: %w", containerName, err)
	}

	// Try the shared-infra network first (our primary network)
	networkName := getEnvOrDefault("OPENSHELL_NETWORK", "shared-infra")
	if net, ok := result.Container.NetworkSettings.Networks[networkName]; ok {
		if net.IPAddress.IsValid() {
			return net.IPAddress.String(), nil
		}
	}

	// Fall back to any network with a valid IP
	for _, net := range result.Container.NetworkSettings.Networks {
		if net.IPAddress.IsValid() {
			return net.IPAddress.String(), nil
		}
	}
	return "", fmt.Errorf("no IP address found for container %s (networks: %d)", containerName, len(result.Container.NetworkSettings.Networks))
}

// RecoverAllSandboxes discovers running OpenShell containers and recovers them
// into the in-memory map. Called on startup to restore state after a backend restart.
func (m *Manager) RecoverAllSandboxes(ctx context.Context) error {
	if m.dockerClient == nil {
		return nil
	}

	result, err := m.dockerClient.ContainerList(ctx, client.ContainerListOptions{
		Filters: client.Filters{
			"label": {"openshell.sandbox-id": true},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range result.Items {
		sandboxID := c.Labels["openshell.sandbox-id"]
		if sandboxID == "" {
			continue
		}
		projectPath := c.Labels["openshell.project-path"]
		policy := c.Labels["openshell.policy"]
		if policy == "" {
			policy = "developer"
		}

		m.mu.Lock()
		if _, exists := m.sandboxes[sandboxID]; !exists {
			m.sandboxes[sandboxID] = &Sandbox{
				ID:          sandboxID,
				ContainerID: c.ID,
				ProjectPath: projectPath,
				Policy:      policy,
				Mode:        ModeCLI,
				Status:      StatusRunning,
				CreatedAt:   time.Now(),
			}
			m.logger.Info("recovered sandbox from Docker",
				zap.String("id", sandboxID),
				zap.String("project_path", projectPath),
			)
		}
		m.mu.Unlock()
	}

	return nil
}
