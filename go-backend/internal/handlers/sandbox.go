package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// SandboxHandler handles HTTP requests for sandbox operations
type SandboxHandler struct {
	manager *sandbox.Manager
	logger  *zap.Logger
}

// NewSandboxHandler creates a new sandbox handler
func NewSandboxHandler(manager *sandbox.Manager, logger *zap.Logger) *SandboxHandler {
	return &SandboxHandler{
		manager: manager,
		logger:  logger,
	}
}

// CreateSandboxRequest is the request body for creating a sandbox
type CreateSandboxRequest struct {
	ID          string `json:"id"`
	ProjectPath string `json:"project_path"`
	Policy      string `json:"policy"`
}

// CreateSandbox creates a new sandbox
// POST /api/sandbox
func (h *SandboxHandler) CreateSandbox(w http.ResponseWriter, r *http.Request) {
	var req CreateSandboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	sandbox, err := h.manager.CreateSandbox(r.Context(), sandbox.SandboxOptions{
		ID:          req.ID,
		ProjectPath: req.ProjectPath,
		Policy:      req.Policy,
	})
	if err != nil {
		h.logger.Error("failed to create sandbox", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sandbox)
}

// GetSandbox returns a sandbox by ID
// GET /api/sandbox/{id}
func (h *SandboxHandler) GetSandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s, err := h.manager.GetSandbox(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

// ListSandboxes returns all active sandboxes
// GET /api/sandboxes
func (h *SandboxHandler) ListSandboxes(w http.ResponseWriter, r *http.Request) {
	sandboxes := h.manager.ListSandboxes()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sandboxes)
}

// DestroySandbox destroys a sandbox
// DELETE /api/sandbox/{id}
func (h *SandboxHandler) DestroySandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.manager.DestroySandbox(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ExecCommand executes a command in a sandbox
// POST /api/sandbox/{id}/exec
func (h *SandboxHandler) ExecCommand(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		Cmd []string `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	output, err := h.manager.ExecInSandbox(r.Context(), id, req.Cmd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"output": output})
}

// EnableBrowserModeRequest is the request body for enabling browser mode
type EnableBrowserModeRequest struct {
	Reason string `json:"reason"`
}

// EnableBrowserMode enables browser mode (CDP only, lightweight)
// POST /api/sandbox/{id}/browser-mode
func (h *SandboxHandler) EnableBrowserMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req EnableBrowserModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Reason = ""
	}

	h.logger.Info("enabling browser mode via API",
		zap.String("sandbox_id", id),
		zap.String("reason", req.Reason),
	)

	session, err := h.manager.EnableBrowserMode(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to enable browser mode", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// EnableDesktopModeRequest is the request body for enabling desktop mode
type EnableDesktopModeRequest struct {
	Reason string `json:"reason"`
}

// EnableDesktopMode enables desktop mode (VNC + Xvfb, heavy)
// POST /api/sandbox/{id}/desktop-mode
func (h *SandboxHandler) EnableDesktopMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req EnableDesktopModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Reason = ""
	}

	h.logger.Info("enabling desktop mode via API",
		zap.String("sandbox_id", id),
		zap.String("reason", req.Reason),
	)

	session, err := h.manager.EnableDesktopMode(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to enable desktop mode", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// DisableMode disables any active mode (browser or desktop)
// DELETE /api/sandbox/{id}/mode
func (h *SandboxHandler) DisableMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.manager.DisableMode(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetDesktopViewer returns desktop viewer URLs for a sandbox
// GET /api/sandbox/{id}/desktop-viewer
func (h *SandboxHandler) GetDesktopViewer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	session, err := h.manager.GetDesktopSession(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"viewerUrl":  session.ViewerURL,
		"cdpUrl":     getCDPURL(session),
		"vncUrl":     getVNCURL(session),
		"novncUrl":   getNoVNCURL(session),
		"mode":       string(session.Mode),
	})
}

func getCDPURL(session *sandbox.DesktopSession) string {
	return fmt.Sprintf("http://localhost:%d", session.CDPPort)
}

func getVNCURL(session *sandbox.DesktopSession) string {
	return fmt.Sprintf("vnc://localhost:%d", session.VNCPort)
}

func getNoVNCURL(session *sandbox.DesktopSession) string {
	return fmt.Sprintf("http://localhost:%d/vnc.html", session.NoVNCPort)
}
