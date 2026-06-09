package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"github.com/auto-developer-orchestrator/backend/internal/retry"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// ComputerUseHandler handles HTTP requests for computer use mode
type ComputerUseHandler struct {
	manager      *sandbox.Manager
	visionClient *browser.VisionClient
	logger       *zap.Logger
	clients      map[string]*browser.SandboxBrowserClient
	mu           sync.RWMutex

	// Host Chrome fallback (when Docker is unavailable)
	hostChromeCmd  *exec.Cmd
	hostChromePort int
}

// NewComputerUseHandler creates a new computer use handler
func NewComputerUseHandler(manager *sandbox.Manager, visionClient *browser.VisionClient, logger *zap.Logger) *ComputerUseHandler {
	return &ComputerUseHandler{
		manager:      manager,
		visionClient: visionClient,
		logger:       logger,
		clients:      make(map[string]*browser.SandboxBrowserClient),
	}
}

// VisionClient returns the vision client (nil if vision is not available).
func (h *ComputerUseHandler) VisionClient() *browser.VisionClient {
	return h.visionClient
}

// RegisterRoutes registers computer use routes on a chi.Router
func (h *ComputerUseHandler) RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
	Delete(string, http.HandlerFunc)
}) {
	r.Post("/enable", h.Enable)
	r.Post("/disable", h.Disable)
	r.Get("/screenshot", h.Screenshot)
	r.Get("/snapshot", h.Snapshot)
	r.Post("/act", h.Act)
	r.Post("/find", h.FindElement)
	r.Get("/a11y-snapshot", h.A11ySnapshot)
	r.Get("/cookies", h.GetCookies)
	r.Post("/cookies", h.SetCookie)
	r.Delete("/cookies", h.ClearCookies)
	r.Get("/storage", h.GetStorage)
	r.Post("/storage", h.SetStorage)
	r.Delete("/storage", h.ClearStorage)
	r.Post("/evaluate-js", h.EvaluateJS)
	r.Get("/read-page", h.ReadPage)
	r.Post("/download", h.DownloadFile)
}

// Enable enables computer use mode on a sandbox: creates desktop mode (VNC + Chrome) + SandboxBrowserClient
// POST /api/sandbox/{id}/computer-use/enable
//
// The JSON response is sent IMMEDIATELY — before any Docker operations.
// Docker container creation triggers ERR_NETWORK_CHANGED in Chromium, which aborts
// the browser's in-flight fetch response body stream. By sending the response first,
// we avoid this entirely. Docker/CDP operations run in a background goroutine.
func (h *ComputerUseHandler) Enable(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	if sandboxID == "" {
		JSONError(w, "sandbox id required", http.StatusBadRequest)
		return
	}

	h.logger.Info("enabling computer use", zap.String("sandbox_id", sandboxID))

	// When no sandbox manager (Docker down), we fall back to host Chrome.
	// The backgroundSetup goroutine handles both paths.

	// Fast path: already have a connected CDP client — pure in-memory check (~1ms).
	h.mu.RLock()
	existingClient, clientExists := h.clients[sandboxID]
	isConnected := clientExists && existingClient.IsConnected()
	h.mu.RUnlock()

	if isConnected {
		cdpPort := 9222 // host Chrome default
		viewerURL := ""
		novncPort := 0
		if h.manager != nil {
			sandbox, _ := h.manager.GetSandbox(sandboxID)
			cdpPort = 19222
			viewerURL = fmt.Sprintf("/sandbox/%s/viewer", sandboxID)
			novncPort = 6080
			if sandbox != nil && sandbox.DesktopSession != nil {
				cdpPort = sandbox.DesktopSession.CDPPort
				viewerURL = sandbox.DesktopSession.ViewerURL
				novncPort = sandbox.DesktopSession.NoVNCPort
			}
		}
		h.logger.Info("computer use already enabled (fast path)", zap.String("sandbox_id", sandboxID))
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":   true,
			"sandboxId": sandboxID,
			"cdpPort":   cdpPort,
			"viewerUrl": viewerURL,
			"novncPort": novncPort,
		})
		return
	}

	// Send the JSON response IMMEDIATELY — no Docker API calls above this line.
	// The frontend considers the sandbox "enabled". Docker/CDP setup happens in
	// the background and completes before the frontend tries to interact.
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   true,
		"sandboxId": sandboxID,
		"cdpPort":   19222,
		"viewerUrl": fmt.Sprintf("/sandbox/%s/viewer", sandboxID),
		"novncPort": 6080,
	})

	// All Docker operations run after the response is sent.
	go h.backgroundSetup(context.Background(), sandboxID)
}

// backgroundSetup runs all Docker/CDP operations after the HTTP response has been sent.
// Errors are logged, not returned to the client.
// Falls back to host Chrome when Docker is unavailable.
func (h *ComputerUseHandler) backgroundSetup(ctx context.Context, sandboxID string) {
	h.logger.Info("background: setting up computer use", zap.String("sandbox_id", sandboxID))

	// Docker path — only if manager is available
	if h.manager != nil {
		if h.dockerSetup(ctx, sandboxID) {
			return // success
		}
		h.logger.Info("background: Docker setup failed, falling back to host Chrome", zap.String("sandbox_id", sandboxID))
	} else {
		h.logger.Info("background: no sandbox manager, using host Chrome", zap.String("sandbox_id", sandboxID))
	}

	// Host Chrome fallback — runs Chrome directly on the host
	if err := h.setupHostChrome(ctx, sandboxID); err != nil {
		h.logger.Error("background: host Chrome setup also failed", zap.String("sandbox_id", sandboxID), zap.Error(err))
	}
}

// dockerSetup creates a Docker sandbox, enables browser mode, and connects CDP.
// Returns true on success, false on failure (caller should try host Chrome).
func (h *ComputerUseHandler) dockerSetup(ctx context.Context, sandboxID string) bool {
	// Step 1: Ensure the sandbox exists (create/recover if needed).
	if _, err := h.manager.GetSandbox(sandboxID); err != nil {
		h.logger.Info("background: sandbox not found, creating", zap.String("sandbox_id", sandboxID), zap.Error(err))
		_, createErr := h.manager.CreateSandbox(ctx, sandbox.SandboxOptions{ID: sandboxID})
		if createErr != nil {
			h.logger.Info("background: create failed, recovering", zap.String("sandbox_id", sandboxID), zap.Error(createErr))
			recoverErr := h.manager.RecoverSandbox(ctx, sandboxID)
			if recoverErr != nil {
				h.logger.Error("background: failed to create or recover sandbox", zap.Error(createErr), zap.Error(recoverErr))
				return false
			}
		}
	}

	// Step 2: Try browser mode first, fall back to desktop mode.
	session, err := h.manager.EnableBrowserMode(ctx, sandboxID)
	if err != nil {
		h.logger.Info("background: browser mode not available, starting desktop mode", zap.String("sandbox_id", sandboxID), zap.Error(err))
		session, err = h.manager.EnableDesktopMode(ctx, sandboxID)
		if err != nil {
			h.logger.Error("background: desktop mode also failed", zap.Error(err))
			return false
		}
	}

	// Step 3: Create SandboxBrowserClient
	client, err := h.getOrCreateClient(sandboxID, session.CDPPort)
	if err != nil {
		// Stale container — destroy and retry
		h.logger.Warn("background: client creation failed, retrying with fresh sandbox",
			zap.String("sandbox_id", sandboxID), zap.Error(err))
		_ = h.manager.DestroySandbox(ctx, sandboxID)

		_, createErr := h.manager.CreateSandbox(ctx, sandbox.SandboxOptions{ID: sandboxID})
		if createErr != nil {
			h.logger.Error("background: fresh sandbox creation failed", zap.Error(createErr))
			return false
		}

		session, sessionErr := h.manager.EnableBrowserMode(ctx, sandboxID)
		if sessionErr != nil {
			h.logger.Warn("background: browser mode on fresh sandbox failed, trying desktop mode", zap.Error(sessionErr))
			session, sessionErr = h.manager.EnableDesktopMode(ctx, sandboxID)
			if sessionErr != nil {
				h.logger.Error("background: desktop mode on fresh sandbox also failed", zap.Error(sessionErr))
				return false
			}
		}

		client, err = h.getOrCreateClient(sandboxID, session.CDPPort)
		if err != nil {
			h.logger.Error("background: client creation on fresh sandbox failed", zap.Error(err))
			return false
		}
	}

	// Step 4: Connect via CDP (with exponential backoff)
	cfg := retry.Long
	connectErr := retry.Do(ctx, cfg, func() error {
		err := client.Connect(ctx)
		if err != nil {
			h.logger.Info("background: waiting for Chrome CDP", zap.Error(err))
		}
		return err
	})
	if connectErr != nil {
		h.logger.Warn("background: CDP connect failed after retries", zap.String("sandbox_id", sandboxID), zap.Error(connectErr))
		return false
	}

	// Step 5: Install X11 tools + write landing page
	h.installX11Tools(ctx, sandboxID)
	h.writeLandingPage(ctx, sandboxID, session.DisplayNum)

	h.logger.Info("background: computer use setup complete", zap.String("sandbox_id", sandboxID))
	return true
}

// setupHostChrome launches Chrome directly on the host (no Docker) and connects
// via CDP. Used as a fallback when Docker is unavailable.
func (h *ComputerUseHandler) setupHostChrome(ctx context.Context, sandboxID string) error {
	const cdpPort = 9222

	// Launch Chrome if not already running
	if !h.isHostChromeReady(cdpPort) {
		chromePath, err := exec.LookPath("google-chrome")
		if err != nil {
			chromePath, err = exec.LookPath("chromium")
			if err != nil {
				return fmt.Errorf("no Chrome/Chromium binary found: %w", err)
			}
		}

		h.logger.Info("background: launching host Chrome", zap.String("path", chromePath), zap.Int("port", cdpPort))
		cmd := exec.Command(chromePath,
			"--headless=new",
			fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
			"--no-sandbox",
			"--disable-gpu",
			"--no-first-run",
			"--disable-extensions",
			"--user-data-dir=/tmp/pux-chrome",
		)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start Chrome: %w", err)
		}
		h.mu.Lock()
		h.hostChromeCmd = cmd
		h.hostChromePort = cdpPort
		h.mu.Unlock()

		// Wait for CDP to be ready (up to 30s)
		for i := 0; i < 30; i++ {
			if h.isHostChromeReady(cdpPort) {
				break
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if !h.isHostChromeReady(cdpPort) {
			return fmt.Errorf("Chrome did not become ready on port %d", cdpPort)
		}
	}

	h.logger.Info("background: host Chrome ready", zap.Int("port", cdpPort))

	// Create browser client pointing at localhost
	client, err := browser.NewSandboxBrowserClient(cdpPort, "localhost", h.logger)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	// Connect via CDP
	cfg := retry.Long
	if err := retry.Do(ctx, cfg, func() error {
		return client.Connect(ctx)
	}); err != nil {
		return fmt.Errorf("CDP connect: %w", err)
	}

	h.mu.Lock()
	h.clients[sandboxID] = client
	h.mu.Unlock()

	h.logger.Info("background: host Chrome setup complete", zap.String("sandbox_id", sandboxID))
	return nil
}

// isHostChromeReady checks if Chrome CDP is reachable on the given port.
func (h *ComputerUseHandler) isHostChromeReady(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/json/version", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// installX11Tools installs xdotool and imagemagick in the sandbox container
// for X11-based desktop automation (coordinate clicks, keyboard, screenshots).
func (h *ComputerUseHandler) installX11Tools(ctx context.Context, sandboxID string) {
	h.logger.Info("background: installing X11 automation tools", zap.String("sandbox_id", sandboxID))

	// Run apt-get install quietly; best-effort — don't fail if packages are already present.
	output, err := h.manager.ExecInSandbox(ctx, sandboxID, []string{
		"bash", "-c",
		"apt-get update -qq && apt-get install -y -qq xdotool imagemagick scrot 2>/dev/null || true",
	})
	if err != nil {
		h.logger.Warn("background: X11 tools install failed (non-fatal)", zap.Error(err))
		return
	}

	h.logger.Info("background: X11 tools installed", zap.String("sandbox_id", sandboxID), zap.String("output", output))
}

// Disable closes the SandboxBrowserClient and disables browser mode
// POST /api/sandbox/{id}/computer-use/disable
func (h *ComputerUseHandler) Disable(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")

	h.mu.Lock()
	if client, ok := h.clients[sandboxID]; ok {
		client.Close()
		delete(h.clients, sandboxID)
	}
	h.mu.Unlock()

	// Disable the sandbox browser/desktop mode
	if h.manager != nil {
		if err := h.manager.DisableMode(r.Context(), sandboxID); err != nil {
			h.logger.Warn("failed to disable sandbox mode", zap.Error(err))
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"disabled": true})
}

// ScreenshotResponse is the JSON response for the screenshot endpoint
type ScreenshotResponse struct {
	Image       string `json:"image,omitempty"`       // base64 PNG
	Description string `json:"description,omitempty"` // vision model description
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
}

// Screenshot takes a screenshot and optionally describes it via vision model
// GET /api/sandbox/{id}/computer-use/screenshot?describe=true&format=json
func (h *ComputerUseHandler) Screenshot(w http.ResponseWriter, r *http.Request) {
	describe := r.URL.Query().Get("describe") == "true"
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		pngBytes, err := client.Screenshot(r.Context())
		if err != nil {
			h.logger.Error("screenshot failed", zap.Error(err))
			JSONError(w, fmt.Sprintf("screenshot failed: %v", err), http.StatusInternalServerError)
			return
		}

		// Return raw PNG if format=png
		if format == "png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes)
			return
		}

		resp := ScreenshotResponse{
			Image: base64.StdEncoding.EncodeToString(pngBytes),
		}

		// Optionally describe via vision model
		if describe && h.visionClient != nil {
			description, err := h.visionClient.DescribePage(r.Context(), pngBytes)
			if err != nil {
				h.logger.Warn("vision description failed", zap.Error(err))
				resp.Description = fmt.Sprintf("(vision description failed: %v)", err)
			} else {
				resp.Description = description
			}
		}

		// Include page state
		if snapshot, _ := client.GetSnapshot(); snapshot != nil {
			resp.URL = snapshot.URL
			resp.Title = snapshot.Title
		}

		writeJSON(w, http.StatusOK, resp)
	})
}

// Snapshot returns accessibility snapshot (elements list + URL + title)
// GET /api/sandbox/{id}/computer-use/snapshot
func (h *ComputerUseHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		info, err := client.GetSnapshot()
		if err != nil {
			JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
}

// ActRequest is the request body for the act endpoint
type ActRequest struct {
	Action    string `json:"action"`              // click, type, scroll, navigate
	Element   int    `json:"element,omitempty"`   // element ID for click/type
	Text      string `json:"text,omitempty"`      // text for type
	URL       string `json:"url,omitempty"`       // URL for navigate
	Direction string `json:"direction,omitempty"` // up/down for scroll
	Amount    int    `json:"amount,omitempty"`    // scroll amount
	Submit    bool   `json:"submit,omitempty"`    // submit after typing
}

// Act executes a browser action
// POST /api/sandbox/{id}/computer-use/act
func (h *ComputerUseHandler) Act(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")

	req, ok := decodeReq[ActRequest](w, r)
	if !ok { return }

	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	var info *browser.PageInfo

	switch req.Action {
	case "click":
		info, err = client.Click(r.Context(), req.Element)
	case "type":
		info, err = client.Type(r.Context(), req.Element, req.Text, req.Submit)
	case "scroll":
		amount := req.Amount
		if amount == 0 {
			amount = 300
		}
		info, err = client.Scroll(r.Context(), req.Direction, amount)
	case "navigate":
		info, err = client.Navigate(r.Context(), req.URL)
	default:
		JSONError(w, fmt.Sprintf("unknown action: %s", req.Action), http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error("action failed", zap.String("action", req.Action), zap.Error(err))
		JSONError(w, fmt.Sprintf("%s failed: %v", req.Action, err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, info)
}

// getClient returns the SandboxBrowserClient for a sandbox, auto-reconnecting if needed.
func (h *ComputerUseHandler) getClient(sandboxID string) (*browser.SandboxBrowserClient, error) {
	h.mu.RLock()
	client, ok := h.clients[sandboxID]
	h.mu.RUnlock()

	if ok {
		if client.IsConnected() {
			return client, nil
		}
		// Stale client — remove and fall through to auto-reconnect
		h.logger.Info("removing stale CDP client", zap.String("sandbox_id", sandboxID))
		client.Close()
		h.mu.Lock()
		delete(h.clients, sandboxID)
		h.mu.Unlock()
	}

	// Client not in memory (server restart or first call) — attempt auto-reconnect.
	// The sandbox container is still running with Chrome listening on its CDP port.
	if h.manager == nil {
		return nil, fmt.Errorf("computer use not enabled for sandbox %s", sandboxID)
	}

	sandbox, err := h.manager.GetSandbox(sandboxID)
	if err != nil {
		return nil, fmt.Errorf("computer use not enabled for sandbox %s: %w", sandboxID, err)
	}

	// Auto-enable: if the sandbox exists but has no browser session, enable
	// browser mode now. This covers the delegate_to → browser_ops path where
	// Enable() was never called from the web UI but the sandbox was auto-created.
	if sandbox.DesktopSession == nil {
		h.logger.Info("auto-enabling browser mode for tool call", zap.String("sandbox_id", sandboxID))
		session, enableErr := h.manager.EnableBrowserMode(context.Background(), sandboxID)
		if enableErr != nil {
			h.logger.Info("auto-enable: browser mode failed, trying desktop mode", zap.Error(enableErr))
			session, enableErr = h.manager.EnableDesktopMode(context.Background(), sandboxID)
			if enableErr != nil {
				return nil, fmt.Errorf("sandbox %s: failed to auto-enable browser/desktop mode: %w", sandboxID, enableErr)
			}
		}
		// Refresh sandbox reference after enabling mode
		sandbox, err = h.manager.GetSandbox(sandboxID)
		if err != nil {
			return nil, fmt.Errorf("sandbox %s disappeared after enable: %w", sandboxID, err)
		}
		// Use the session from EnableBrowserMode directly if DesktopSession is somehow still nil
		if sandbox.DesktopSession == nil && session != nil {
			sandbox.DesktopSession = session
		}
		if sandbox.DesktopSession == nil {
			return nil, fmt.Errorf("sandbox %s: browser session not established after enable", sandboxID)
		}
	}

	// Reconnect: create a new CDP client and connect
	cdpPort := sandbox.DesktopSession.CDPPort
	client, err = h.getOrCreateClient(sandboxID, cdpPort)
	if err != nil {
		return nil, fmt.Errorf("failed to reconnect CDP client for %s: %w", sandboxID, err)
	}

	// Connect with backoff — Chrome may still be starting up
	connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connectErr := retry.Do(connectCtx, retry.Short, func() error {
		if err := client.Connect(connectCtx); err != nil {
			h.logger.Info("auto-enable: waiting for Chrome CDP", zap.String("sandbox_id", sandboxID), zap.Error(err))
			return err
		}
		return nil
	})
	if connectErr != nil {
		h.mu.Lock()
		delete(h.clients, sandboxID)
		h.mu.Unlock()
		return nil, fmt.Errorf("failed to connect to Chrome for %s after auto-enable: %w", sandboxID, connectErr)
	}

	h.logger.Info("auto-connected CDP client", zap.String("sandbox_id", sandboxID), zap.Int("cdp_port", cdpPort))
	return client, nil
}

// withClient extracts the sandbox ID from the URL path, resolves the browser
// client, and calls fn. Writes a 404 error if the client is not found.
func (h *ComputerUseHandler) withClient(w http.ResponseWriter, r *http.Request, fn func(*browser.SandboxBrowserClient)) {
	sandboxID := r.PathValue("id")
	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}
	fn(client)
}

// getOrCreateClient returns an existing client or creates a new one
func (h *ComputerUseHandler) getOrCreateClient(sandboxID string, cdpPort int) (*browser.SandboxBrowserClient, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client, ok := h.clients[sandboxID]; ok {
		return client, nil
	}

	// Resolve container IP directly to avoid Docker DNS failures
	containerIP, err := h.manager.GetContainerIP(context.TODO(), sandboxID)
	if err != nil {
		return nil, fmt.Errorf("failed to get container IP for %s: %w", sandboxID, err)
	}

	client, err := browser.NewSandboxBrowserClient(cdpPort, containerIP, h.logger)
	if err != nil {
		return nil, err
	}

	h.clients[sandboxID] = client
	return client, nil
}

// Shutdown closes all browser clients
func (h *ComputerUseHandler) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, client := range h.clients {
		client.Close()
		delete(h.clients, id)
	}

	// Kill host Chrome process if we launched one
	if h.hostChromeCmd != nil && h.hostChromeCmd.Process != nil {
		h.logger.Info("shutting down host Chrome", zap.Int("port", h.hostChromePort))
		_ = h.hostChromeCmd.Process.Kill()
		h.hostChromeCmd = nil
	}
}

// writeLandingPage creates a landing page file in the sandbox container.
// Does NOT navigate Chrome — the agent will navigate on its first call,
// and navigating here races with the agent's tool execution.
func (h *ComputerUseHandler) writeLandingPage(ctx context.Context, sandboxID string, displayNum int) {
	landingHTML := `<!DOCTYPE html><html><head><title>Sandbox Desktop</title><style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%)}.container{text-align:center;background:#fff;padding:48px 56px;border-radius:16px;box-shadow:0 8px 32px rgba(0,0,0,.15)}.ready{color:#4CAF50;font-size:28px;margin:0 0 12px}.hint{color:#888;font-size:16px;margin:0}</style></head><body><div class="container"><p class="ready">Desktop Ready</p><p class="hint">Use computer_use tools to navigate and interact</p></div></body></html>`

	// Write landing page file (for manual VNC access)
	escaped := sandbox.ShellEscape(landingHTML)
	cmd := fmt.Sprintf("echo '%s' > /tmp/landing.html", escaped)
	_, _ = h.manager.ExecInSandbox(ctx, sandboxID, []string{"sh", "-c", cmd})

	// Raise Chrome to the foreground so it's visible in VNC
	_, _ = h.manager.ExecInSandbox(ctx, sandboxID, []string{
		"sh", "-c", fmt.Sprintf("DISPLAY=:%d wmctrl -a 'Google Chrome' 2>/dev/null || true", displayNum),
	})

	// NOTE: Do NOT navigate Chrome here. The agent will navigate on its first
	// tool call. Navigating here races with the agent and corrupts tab state.
}

// FindRequest is the request body for the find endpoint.
type FindRequest struct {
	Role        string `json:"role,omitempty"`
	Name        string `json:"name,omitempty"`
	Label       string `json:"label,omitempty"`
	Text        string `json:"text,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Alt         string `json:"alt,omitempty"`
	TitleAttr   string `json:"title,omitempty"`
	TestID      string `json:"test_id,omitempty"`
	Selector    string `json:"selector,omitempty"`
	Action      string `json:"action,omitempty"` // click, type, or empty for find-only
	TypeText    string `json:"type_text,omitempty"`
	Submit      bool   `json:"submit,omitempty"`
}

// FindElement finds an element by semantic criteria and optionally performs an action.
// POST /api/sandbox/{id}/computer-use/find
func (h *ComputerUseHandler) FindElement(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	req, ok := decodeReq[FindRequest](w, r)
	if !ok { return }

	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	criteria := browser.FindCriteria{
		Role:        req.Role,
		Name:        req.Name,
		Label:       req.Label,
		Text:        req.Text,
		Placeholder: req.Placeholder,
		Alt:         req.Alt,
		Title:       req.TitleAttr,
		TestID:      req.TestID,
		Selector:    req.Selector,
	}

	switch req.Action {
	case "click":
		info, found, err := client.FindAndClick(r.Context(), criteria)
		if err != nil {
			JSONError(w, fmt.Sprintf("find+click failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"action":       "clicked",
			"found_element": found,
			"page_info":    info,
		})
	case "type":
		if req.TypeText == "" {
			JSONError(w, "type_text is required when action=type", http.StatusBadRequest)
			return
		}
		info, found, err := client.FindAndType(r.Context(), criteria, req.TypeText, req.Submit)
		if err != nil {
			JSONError(w, fmt.Sprintf("find+type failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"action":       "typed",
			"found_element": found,
			"page_info":    info,
		})
	default:
		found, err := client.FindElement(r.Context(), criteria)
		if err != nil {
			JSONError(w, fmt.Sprintf("find failed: %v", err), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, found)
	}
}

// A11ySnapshot returns the accessibility tree snapshot.
// GET /api/sandbox/{id}/computer-use/a11y-snapshot
func (h *ComputerUseHandler) A11ySnapshot(w http.ResponseWriter, r *http.Request) {
	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		snapshot, err := client.GetAccessibilityTree(r.Context())
		if err != nil {
			JSONError(w, fmt.Sprintf("a11y snapshot failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})
}

// CookieRequest represents a cookie operation.
type CookieRequest struct {
	Name   string  `json:"name"`
	Value  string  `json:"value,omitempty"`
	Domain string  `json:"domain,omitempty"`
	Path   string  `json:"path,omitempty"`
	Secure bool    `json:"secure,omitempty"`
	HTTPOnly bool  `json:"http_only,omitempty"`
	URLs   []string `json:"urls,omitempty"`
}

// GetCookies returns all cookies or cookies filtered by URL.
// GET /api/sandbox/{id}/computer-use/cookies?url=https://example.com
func (h *ComputerUseHandler) GetCookies(w http.ResponseWriter, r *http.Request) {
	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		urls := r.URL.Query()["url"]
		cookies, err := client.GetCookies(r.Context(), urls)
		if err != nil {
			JSONError(w, fmt.Sprintf("get cookies failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"cookies": cookies,
		})
	})
}

// SetCookie sets a browser cookie.
// POST /api/sandbox/{id}/computer-use/cookies
func (h *ComputerUseHandler) SetCookie(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	req, ok := decodeReq[CookieRequest](w, r)
	if !ok { return }
	if req.Name == "" || req.Value == "" || req.Domain == "" {
		JSONError(w, "name, value, and domain are required", http.StatusBadRequest)
		return
	}

	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	cookie := browser.Cookie{
		Name:   req.Name,
		Value:  req.Value,
		Domain: req.Domain,
		Path:   req.Path,
		Secure: req.Secure,
		HTTPOnly: req.HTTPOnly,
	}
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if err := client.SetCookie(r.Context(), cookie); err != nil {
		JSONError(w, fmt.Sprintf("set cookie failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"set": true})
}

// ClearCookies clears all cookies for the current page.
// DELETE /api/sandbox/{id}/computer-use/cookies
func (h *ComputerUseHandler) ClearCookies(w http.ResponseWriter, r *http.Request) {
	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		if err := client.ClearCookies(r.Context()); err != nil {
			JSONError(w, fmt.Sprintf("clear cookies failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
	})
}

// StorageRequest for localStorage/sessionStorage operations.
type StorageRequest struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// GetStorage returns localStorage contents.
// GET /api/sandbox/{id}/computer-use/storage
func (h *ComputerUseHandler) GetStorage(w http.ResponseWriter, r *http.Request) {
	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		data, err := client.GetLocalStorage(r.Context())
		if err != nil {
			JSONError(w, fmt.Sprintf("get storage failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"localStorage": data,
		})
	})
}

// SetStorage sets a localStorage item.
// POST /api/sandbox/{id}/computer-use/storage
func (h *ComputerUseHandler) SetStorage(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	req, ok := decodeReq[StorageRequest](w, r)
	if !ok { return }
	if req.Key == "" || req.Value == "" {
		JSONError(w, "key and value are required", http.StatusBadRequest)
		return
	}

	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := client.SetLocalStorageItem(r.Context(), req.Key, req.Value); err != nil {
		JSONError(w, fmt.Sprintf("set storage failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"set": true})
}

// ClearStorage clears all localStorage data.
// DELETE /api/sandbox/{id}/computer-use/storage
func (h *ComputerUseHandler) ClearStorage(w http.ResponseWriter, r *http.Request) {
	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		if err := client.ClearLocalStorage(r.Context()); err != nil {
			JSONError(w, fmt.Sprintf("clear storage failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
	})
}

// EvaluateJSRequest is the request body for the evaluate-js endpoint.
type EvaluateJSRequest struct {
	Code string `json:"code"`
}

// EvaluateJS executes JavaScript in the browser and returns the result.
// POST /api/sandbox/{id}/computer-use/evaluate-js
func (h *ComputerUseHandler) EvaluateJS(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	req, ok := decodeReq[EvaluateJSRequest](w, r)
	if !ok {
		return
	}
	if req.Code == "" {
		JSONError(w, "code is required", http.StatusBadRequest)
		return
	}

	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	result, jsType, err := client.EvaluateJS(r.Context(), req.Code)
	if err != nil {
		JSONError(w, fmt.Sprintf("evaluate_js failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"result": result,
		"type":   jsType,
	})
}

// ReadPage extracts structured content from the current page.
// GET /api/sandbox/{id}/computer-use/read-page
func (h *ComputerUseHandler) ReadPage(w http.ResponseWriter, r *http.Request) {
	h.withClient(w, r, func(client *browser.SandboxBrowserClient) {
		data, err := client.ReadPage(r.Context())
		if err != nil {
			JSONError(w, fmt.Sprintf("read_page failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, data)
	})
}

// DownloadFileRequest is the request body for the download endpoint.
type DownloadFileRequest struct {
	URL  string `json:"url"`
	Path string `json:"path,omitempty"`
}

// DownloadFile downloads a file to the sandbox via curl.
// POST /api/sandbox/{id}/computer-use/download
func (h *ComputerUseHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	req, ok := decodeReq[DownloadFileRequest](w, r)
	if !ok {
		return
	}
	if req.URL == "" {
		JSONError(w, "url is required", http.StatusBadRequest)
		return
	}

	if h.manager == nil {
		JSONError(w, "sandbox manager not available", http.StatusServiceUnavailable)
		return
	}

	destPath := req.Path
	if destPath == "" {
		// Default: extract filename from URL, save to workspace
		parts := strings.Split(req.URL, "/")
		filename := parts[len(parts)-1]
		if filename == "" || strings.Contains(filename, "?") {
			filename = "download"
		}
		// Strip query string
		if idx := strings.Index(filename, "?"); idx >= 0 {
			filename = filename[:idx]
		}
		destPath = "/sandbox/workspace/" + filename
	}

	// Use curl with proper user agent and follow redirects
	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`curl -L -s -o %s -w "%%{http_code}" -A "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36" --max-time 30 '%s'`, destPath, req.URL),
	}

	output, err := h.manager.ExecInSandbox(r.Context(), sandboxID, cmd)
	if err != nil {
		JSONError(w, fmt.Sprintf("download failed: %v", err), http.StatusInternalServerError)
		return
	}

	// output is the HTTP status code from -w
	statusCode := strings.TrimSpace(output)

	// Get file size
	sizeOutput, _ := h.manager.ExecInSandbox(r.Context(), sandboxID, []string{
		"bash", "-c", fmt.Sprintf(`stat -c '%%s' %s 2>/dev/null || echo "0"`, destPath),
	})
	fileSize := strings.TrimSpace(sizeOutput)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":        destPath,
		"size":        fileSize,
		"url":         req.URL,
		"http_status": statusCode,
	})
}

