package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/retry"
	"github.com/moby/moby/client"
	"go.uber.org/zap"
)

// detectVNCBackend checks the container's VNC_BACKEND env var to determine
// whether it runs standard (noVNC/websockify) or KasmVNC.
func (m *Manager) detectVNCBackend(ctx context.Context, containerName string) VNCBackend {
	output, err := m.execInContainer(ctx, containerName, []string{
		"sh", "-c", "echo $VNC_BACKEND",
	}, false)
	if err == nil {
		trimmed := strings.TrimSpace(output)
		if trimmed == "kasm" {
			return BackendKasm
		}
	}
	return BackendStandard
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
		inspectResult, inspectErr := m.dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
		if inspectErr != nil {
			return nil, fmt.Errorf("sandbox %s not found", sandboxID)
		}

		// Container exists — recover it into the in-memory map
		m.logger.Info("recovering existing sandbox container",
			zap.String("sandbox_id", sandboxID),
			zap.String("container", containerName),
		)

		// Recover metadata from container labels
		projectPath := ""
		policy := "developer"
		if inspectResult.Container.Config != nil && inspectResult.Container.Config.Labels != nil {
			projectPath = inspectResult.Container.Config.Labels["openshell.project-path"]
			if p := inspectResult.Container.Config.Labels["openshell.policy"]; p != "" {
				policy = p
			}
		}

		sandbox = &Sandbox{
			ID:          sandboxID,
			ContainerID: inspectResult.Container.ID,
			ProjectPath: projectPath,
			Policy:      policy,
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
	cdpPort := 19222 // External port (socat-forwarded)

	containerName := m.getContainerName(sandboxID)

	// Detect VNC backend — KasmVNC uses port 8444, standard uses 6080
	backend := m.detectVNCBackend(ctx, containerName)
	var novncPort int
	if backend == BackendKasm {
		novncPort = 8444
	} else {
		novncPort = 6080
	}

	// Store backend info on sandbox
	sandbox.VNCBackend = backend

	// Wait for Chrome to be ready (supervisord starts it at container boot)
	cfg := retry.Long
	chromeErr := retry.Do(ctx, cfg, func() error {
		output, err := m.execInContainer(ctx, containerName, []string{
			"wget", "-qO-", "http://127.0.0.1:9222/json/version",
		}, false)
		if err != nil || output == "" {
			m.logger.Info("waiting for Chrome CDP")
			return fmt.Errorf("Chrome CDP not ready: %w", err)
		}
		m.logger.Info("Chrome CDP ready", zap.String("output", output[:min(len(output), 100)]))
		return nil
	})
	if chromeErr != nil {
		return nil, fmt.Errorf("Chrome CDP not ready after retries: %w", chromeErr)
	}

	// The pux-sandbox image's supervisord.conf already starts x11vnc with -xrandr,
	// so no restart is needed. KasmVNC handles resize natively.
	// If x11vnc lacks -xrandr (e.g. third-party image), resize=remote falls back
	// to client-side scaling — still functional, just not pixel-perfect.

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
		Backend:    backend,
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

	// Detect VNC backend — affects how we start the VNC server
	backend := m.detectVNCBackend(ctx, containerName)
	sandbox.VNCBackend = backend

	// Step 0: Fix configs and kill existing desktop processes.
	// 1) Blank fluxbox rootCommand — prevents xsetroot from clobbering pcmanfm on restart.
	// 2) Write pcmanfm config with correct background color. pcmanfm 1.3 uses [*] section
	//    header and overwrites its config on exit, losing the original settings.
	// 3) Kill processes — supervisord will restart them with the fixed configs.
	_, _ = m.execInContainer(ctx, containerName, []string{
		"bash", "-c",
		"sed -i 's/session.screen0.rootCommand:.*/session.screen0.rootCommand:/' /root/.fluxbox/init 2>/dev/null; " +
			"printf '[*]\\nwallpaper_mode=color\\nwallpaper_common=1\\nwallpaper=#1e1e2e\\n" +
			"desktop_bg=#1e1e2e\\ndesktop_fg=#ffffff\\ndesktop_shadow=#000000\\n" +
			"desktop_font=Sans 12\\nshow_wm_menu=0\\nshow_documents=0\\nshow_mounts=0\\nshow_trash=0\\n' " +
			"> /root/.config/pcmanfm/default/desktop-items-0.conf 2>/dev/null; " +
			"pkill -f 'fluxbox' 2>/dev/null; pkill -f 'pcmanfm --desktop' 2>/dev/null; " +
			"sleep 0.5",
	}, false)

	// Step 1: Start Xvfb with large virtual screen for RANDR resize support.
	// 4096x2160 gives RANDR a large maximum so x11vnc can resize to any viewport.
	_, err := m.execInContainer(ctx, containerName, []string{
		"Xvfb", display, "-screen", "0", "4096x2160x24", "-ac", "+extension", "RANDR",
	}, true)
	if err != nil {
		m.portMutex.Lock()
		m.portAllocator.ReleasePorts(displayNum, vncPort, cdpPort, novncPort)
		m.portMutex.Unlock()
		return nil, fmt.Errorf("failed to start Xvfb: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Step 1b: Configure RANDR for dynamic resize — disable fixed output so
	// XRRSetScreenSize can resize without CRTC conflicts, then set initial fb.
	_, _ = m.execInContainer(ctx, containerName, []string{
		"bash", "-c", fmt.Sprintf(
			"DISPLAY=%s xrandr --output screen --off 2>/dev/null; "+
				"DISPLAY=%s xrandr --fb 1280x720 2>/dev/null || true",
			display, display),
	}, false)

	// Step 2: Start window manager (fluxbox for menu support, then fallbacks)
	// Step 2: Start window manager (fluxbox for menu support, then fallbacks)
	_, err = m.execInContainer(ctx, containerName, []string{
		"sh", "-c", fmt.Sprintf("DISPLAY=%s fluxbox &>/dev/null || DISPLAY=%s xfwm4 &>/dev/null || DISPLAY=%s openbox &>/dev/null || true", display, display, display),
	}, true)
	if err != nil {
		m.logger.Warn("window manager start warning", zap.Error(err))
	}

	// Step 2b: Start pcmanfm for desktop icons and background.
	// pcmanfm --desktop manages the root window (background + icons).
	// It reads its config from /root/.config/pcmanfm/default/ for colors.
	// Sleep lets fluxbox initialize before pcmanfm claims the root window.
	_, _ = m.execInContainer(ctx, containerName, []string{
		"bash", "-c", fmt.Sprintf(
			"sleep 0.5; DISPLAY=%s pcmanfm --desktop &>/tmp/pcmanfm-start.log &",
			display),
	}, false)

	// Step 3: Start VNC server — KasmVNC or standard x11vnc
	if backend == BackendKasm {
		// KasmVNC — built-in web server with H.264/WebRTC
		_, err = m.execInContainer(ctx, containerName, []string{
			"vncserver", display,
			"-geometry", "1280x720",
			"-websocketPort", fmt.Sprintf("%d", novncPort),
			"-config", "/root/.vnc/kasmvnc.yaml",
		}, true)
		if err != nil {
			m.logger.Warn("KasmVNC start warning", zap.Error(err))
		}
	} else {
		// Standard — x11vnc + websockify + noVNC
		_, err = m.execInContainer(ctx, containerName, []string{
			"x11vnc", "-display", display, "-rfbport", fmt.Sprintf("%d", vncPort),
			"-forever", "-shared", "-nopw", "-bg", "-xrandr",
		}, true)
		if err != nil {
			m.logger.Warn("x11vnc start warning", zap.Error(err))
		}

		// Step 4: Start noVNC
		if novncErr := m.startNoVNC(ctx, containerName, novncPort, vncPort); novncErr != nil {
			m.logger.Warn("noVNC start failed", zap.Error(novncErr))
		}
	}

	// Step 5: Start Chrome with CDP
	_, err = m.execInContainer(ctx, containerName, []string{
		"google-chrome",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--disable-gpu",
		"--enable-features=WebContentsForceDark",
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
		Backend:    backend,
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
			"websockify --web /usr/share/novnc %d localhost:%d &>/tmp/novnc-%d.log || "+
				"websockify --web /opt/noVNC %d localhost:%d &>/tmp/novnc-%d.log || true",
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
