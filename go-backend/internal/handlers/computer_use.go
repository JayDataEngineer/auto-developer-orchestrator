package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

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

// Enable enables computer use mode on a sandbox: creates browser mode + SandboxBrowserClient
// POST /api/sandbox/{id}/computer-use/enable
func (h *ComputerUseHandler) Enable(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	if sandboxID == "" {
		http.Error(w, "sandbox id required", http.StatusBadRequest)
		return
	}

	h.logger.Info("enabling computer use", zap.String("sandbox_id", sandboxID))

	// Step 1: Enable browser mode on sandbox (starts Xvfb + Chrome + VNC)
	session, err := h.manager.EnableBrowserMode(r.Context(), sandboxID)
	if err != nil {
		h.logger.Error("failed to enable browser mode for computer use", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to enable browser mode: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 2: Create SandboxBrowserClient connected to the CDP port
	client, err := h.getOrCreateClient(sandboxID, session.CDPPort)
	if err != nil {
		h.logger.Error("failed to create sandbox browser client", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to create browser client: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 3: Connect via CDP
	if err := client.Connect(r.Context()); err != nil {
		h.logger.Error("failed to connect to sandbox Chrome", zap.Error(err))
		http.Error(w, fmt.Sprintf("failed to connect to Chrome: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":   true,
		"sandboxId": sandboxID,
		"cdpPort":   session.CDPPort,
		"viewerUrl": session.ViewerURL,
		"novncPort": session.NoVNCPort,
	})
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
	if err := h.manager.DisableMode(r.Context(), sandboxID); err != nil {
		h.logger.Warn("failed to disable sandbox mode", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"disabled": true})
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	pngBytes, err := client.Screenshot(r.Context())
	if err != nil {
		h.logger.Error("screenshot failed", zap.Error(err))
		http.Error(w, fmt.Sprintf("screenshot failed: %v", err), http.StatusInternalServerError)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Snapshot returns accessibility snapshot (elements list + URL + title)
// GET /api/sandbox/{id}/computer-use/snapshot
func (h *ComputerUseHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")

	client, err := h.getClient(sandboxID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	info, err := client.GetSnapshot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// ActRequest is the request body for the act endpoint
type ActRequest struct {
	Action    string `json:"action"`              // click, type, scroll, navigate
	Element   int    `json:"element,omitempty"`   // element ID for click/type
	Text      string `json:"text,omitempty"`      // text for type
	URL       string `json:"url,omitempty"`        // URL for navigate
	Direction string `json:"direction,omitempty"`  // up/down for scroll
	Amount    int    `json:"amount,omitempty"`     // scroll amount
	Submit    bool   `json:"submit,omitempty"`     // submit after typing
}

// Act executes a browser action
// POST /api/sandbox/{id}/computer-use/act
func (h *ComputerUseHandler) Act(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")

	var req ActRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	client, err := h.getClient(sandboxID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, fmt.Sprintf("unknown action: %s", req.Action), http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error("action failed", zap.String("action", req.Action), zap.Error(err))
		http.Error(w, fmt.Sprintf("%s failed: %v", req.Action, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// getClient returns the SandboxBrowserClient for a sandbox
func (h *ComputerUseHandler) getClient(sandboxID string) (*browser.SandboxBrowserClient, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	client, ok := h.clients[sandboxID]
	if !ok {
		return nil, fmt.Errorf("computer use not enabled for sandbox %s", sandboxID)
	}
	return client, nil
}

// getOrCreateClient returns an existing client or creates a new one
func (h *ComputerUseHandler) getOrCreateClient(sandboxID string, cdpPort int) (*browser.SandboxBrowserClient, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client, ok := h.clients[sandboxID]; ok {
		return client, nil
	}

	client, err := browser.NewSandboxBrowserClient(cdpPort, h.logger)
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
