package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
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
	isConnected := clientExists && existingClient.IsConnected()
	h.mu.RUnlock()

	if isConnected {
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
	var req FindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

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
	sandboxID := r.PathValue("id")
	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	snapshot, err := client.GetAccessibilityTree(r.Context())
	if err != nil {
		JSONError(w, fmt.Sprintf("a11y snapshot failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
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
	sandboxID := r.PathValue("id")
	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	urls := r.URL.Query()["url"]
	cookies, err := client.GetCookies(r.Context(), urls)
	if err != nil {
		JSONError(w, fmt.Sprintf("get cookies failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cookies": cookies,
	})
}

// SetCookie sets a browser cookie.
// POST /api/sandbox/{id}/computer-use/cookies
func (h *ComputerUseHandler) SetCookie(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	var req CookieRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
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
	sandboxID := r.PathValue("id")
	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := client.ClearCookies(r.Context()); err != nil {
		JSONError(w, fmt.Sprintf("clear cookies failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}

// StorageRequest for localStorage/sessionStorage operations.
type StorageRequest struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// GetStorage returns localStorage contents.
// GET /api/sandbox/{id}/computer-use/storage
func (h *ComputerUseHandler) GetStorage(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	data, err := client.GetLocalStorage(r.Context())
	if err != nil {
		JSONError(w, fmt.Sprintf("get storage failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"localStorage": data,
	})
}

// SetStorage sets a localStorage item.
// POST /api/sandbox/{id}/computer-use/storage
func (h *ComputerUseHandler) SetStorage(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	var req StorageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
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
	sandboxID := r.PathValue("id")
	client, err := h.getClient(sandboxID)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := client.ClearLocalStorage(r.Context()); err != nil {
		JSONError(w, fmt.Sprintf("clear storage failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}
