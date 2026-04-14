package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/browser"
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

// RegisterRoutes registers computer use routes on a chi.Router
func (h *ComputerUseHandler) RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
}) {
	r.Post("/enable", h.Enable)
	r.Post("/disable", h.Disable)
	r.Get("/screenshot", h.Screenshot)
	r.Get("/snapshot", h.Snapshot)
	r.Post("/act", h.Act)
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

	if h.manager == nil {
		JSONError(w, "sandbox manager not available", http.StatusServiceUnavailable)
		return
	}

	// Fast path: already have a connected CDP client — pure in-memory check (~1ms).
	h.mu.RLock()
	existingClient, clientExists := h.clients[sandboxID]
	h.mu.RUnlock()
	if clientExists && existingClient.IsConnected() {
		sandbox, _ := h.manager.GetSandbox(sandboxID)
		cdpPort := 19222
		viewerURL := fmt.Sprintf("/sandbox/%s/viewer", sandboxID)
		novncPort := 6080
		if sandbox != nil && sandbox.DesktopSession != nil {
			cdpPort = sandbox.DesktopSession.CDPPort
			viewerURL = sandbox.DesktopSession.ViewerURL
			novncPort = sandbox.DesktopSession.NoVNCPort
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
func (h *ComputerUseHandler) backgroundSetup(ctx context.Context, sandboxID string) {
	h.logger.Info("background: setting up computer use", zap.String("sandbox_id", sandboxID))

	// Step 1: Enable desktop mode (Xvfb + VNC + Chrome on display) so the user
	// can SEE the agent's actions in the center panel VNC viewer.
	// First ensure the sandbox exists (create/recover if needed).
	if _, err := h.manager.GetSandbox(sandboxID); err != nil {
		h.logger.Info("background: sandbox not found, creating", zap.String("sandbox_id", sandboxID), zap.Error(err))
		_, createErr := h.manager.CreateSandbox(ctx, sandbox.SandboxOptions{ID: sandboxID})
		if createErr != nil {
			h.logger.Info("background: create failed, recovering", zap.String("sandbox_id", sandboxID), zap.Error(createErr))
			recoverErr := h.manager.RecoverSandbox(ctx, sandboxID)
			if recoverErr != nil {
				h.logger.Error("background: failed to create or recover sandbox", zap.Error(createErr), zap.Error(recoverErr))
				return
			}
		}
	}

	// Try browser mode first — the OpenShell image already provides a full
	// desktop via supervisord (Xvfb + fluxbox + Chrome + websockify on 6080).
	// Only fall back to desktop mode for bare containers that need everything started.
	session, err := h.manager.EnableBrowserMode(ctx, sandboxID)
	if err != nil {
		h.logger.Info("background: browser mode not available, starting desktop mode", zap.String("sandbox_id", sandboxID), zap.Error(err))
		session, err = h.manager.EnableDesktopMode(ctx, sandboxID)
		if err != nil {
			h.logger.Error("background: desktop mode also failed", zap.Error(err))
			return
		}
	}

	// Step 2: Create SandboxBrowserClient
	client, err := h.getOrCreateClient(sandboxID, session.CDPPort)
	if err != nil {
		// Stale container — destroy and retry
		h.logger.Warn("background: client creation failed, retrying with fresh sandbox",
			zap.String("sandbox_id", sandboxID), zap.Error(err))
		_ = h.manager.DestroySandbox(ctx, sandboxID)

		_, createErr := h.manager.CreateSandbox(ctx, sandbox.SandboxOptions{ID: sandboxID})
		if createErr != nil {
			h.logger.Error("background: fresh sandbox creation failed", zap.Error(createErr))
			return
		}

		session, sessionErr := h.manager.EnableBrowserMode(ctx, sandboxID)
		if sessionErr != nil {
			h.logger.Warn("background: browser mode on fresh sandbox failed, trying desktop mode", zap.Error(sessionErr))
			session, sessionErr = h.manager.EnableDesktopMode(ctx, sandboxID)
			if sessionErr != nil {
				h.logger.Error("background: desktop mode on fresh sandbox also failed", zap.Error(sessionErr))
				return
			}
		}

		client, err = h.getOrCreateClient(sandboxID, session.CDPPort)
		if err != nil {
			h.logger.Error("background: client creation on fresh sandbox failed", zap.Error(err))
			return
		}
	}

	// Step 3: Connect via CDP (with retry)
	for i := range 10 {
		connectErr := client.Connect(ctx)
		if connectErr == nil {
			break
		}
		if ctx.Err() != nil {
			h.logger.Warn("background: CDP connect timed out", zap.String("sandbox_id", sandboxID))
			return
		}
		h.logger.Info("background: waiting for Chrome CDP", zap.Int("attempt", i+1), zap.Error(connectErr))
		time.Sleep(2 * time.Second)
	}

	// Step 4: Install X11 automation tools (xdotool, imagemagick) BEFORE writing
	// the landing page so the sandbox is fully ready when /viewer returns 200.
	h.installX11Tools(ctx, sandboxID)

	// Step 5: Write landing page — signals "ready" to polling clients
	h.writeLandingPage(ctx, sandboxID, session.DisplayNum)

	h.logger.Info("background: computer use setup complete", zap.String("sandbox_id", sandboxID))
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
	sandboxID := r.PathValue("id")
	describe := r.URL.Query().Get("describe") == "true"
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

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
}

// Snapshot returns accessibility snapshot (elements list + URL + title)
// GET /api/sandbox/{id}/computer-use/snapshot
func (h *ComputerUseHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")

	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	info, err := client.GetSnapshot()
	if err != nil {
		JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, info)
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

	var req ActRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

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
		return client, nil
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
	if sandbox.DesktopSession == nil {
		return nil, fmt.Errorf("sandbox %s has no browser session", sandboxID)
	}

	// Reconnect: create a new CDP client and connect
	cdpPort := sandbox.DesktopSession.CDPPort
	client, err = h.getOrCreateClient(sandboxID, cdpPort)
	if err != nil {
		return nil, fmt.Errorf("failed to reconnect CDP client for %s: %w", sandboxID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		// Connection failed — remove stale client
		h.mu.Lock()
		delete(h.clients, sandboxID)
		h.mu.Unlock()
		return nil, fmt.Errorf("failed to reconnect to Chrome for %s: %w", sandboxID, err)
	}

	h.logger.Info("auto-reconnected CDP client", zap.String("sandbox_id", sandboxID), zap.Int("cdp_port", cdpPort))
	return client, nil
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
}

// writeLandingPage creates a landing page in the sandbox container and navigates Chrome to it.
func (h *ComputerUseHandler) writeLandingPage(ctx context.Context, sandboxID string, displayNum int) {
	landingHTML := `<!DOCTYPE html><html><head><title>Sandbox Desktop</title><style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%)}.container{text-align:center;background:#fff;padding:48px 56px;border-radius:16px;box-shadow:0 8px 32px rgba(0,0,0,.15)}.ready{color:#4CAF50;font-size:28px;margin:0 0 12px}.hint{color:#888;font-size:16px;margin:0}</style></head><body><div class="container"><p class="ready">Desktop Ready</p><p class="hint">Use computer_use tools to navigate and interact</p></div></body></html>`

	// Write landing page file
	escaped := strings.ReplaceAll(landingHTML, "'", "'\\''")
	cmd := fmt.Sprintf("echo '%s' > /tmp/landing.html", escaped)
	_, _ = h.manager.ExecInSandbox(ctx, sandboxID, []string{"sh", "-c", cmd})

	// Raise Chrome to the foreground so it's visible in VNC
	_, _ = h.manager.ExecInSandbox(ctx, sandboxID, []string{
		"sh", "-c", fmt.Sprintf("DISPLAY=:%d wmctrl -a 'Google Chrome' 2>/dev/null || true", displayNum),
	})

	// Navigate Chrome to the landing page
	if client, ok := h.clients[sandboxID]; ok {
		_, _ = client.Navigate(ctx, "file:///tmp/landing.html")
	}
}
