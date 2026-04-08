package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/go-chi/chi/v5"
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
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	sandbox, err := h.manager.CreateSandbox(r.Context(), sandbox.SandboxOptions{
		ID:          req.ID,
		ProjectPath: req.ProjectPath,
		Policy:      req.Policy,
	})
	if err != nil {
		h.logger.Error("failed to create sandbox", zap.Error(err))
		JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, sandbox)
}

// GetSandbox returns a sandbox by ID
// GET /api/sandbox/{id}
func (h *SandboxHandler) GetSandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s, err := h.manager.GetSandbox(id)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, s)
}

// ListSandboxes returns all active sandboxes
// GET /api/sandboxes
func (h *SandboxHandler) ListSandboxes(w http.ResponseWriter, r *http.Request) {
	sandboxes := h.manager.ListSandboxes()

	writeJSON(w, http.StatusOK, sandboxes)
}

// DestroySandbox destroys a sandbox
// DELETE /api/sandbox/{id}
func (h *SandboxHandler) DestroySandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.manager.DestroySandbox(r.Context(), id); err != nil {
		JSONError(w, err.Error(), http.StatusInternalServerError)
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
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	output, err := h.manager.ExecInSandbox(r.Context(), id, req.Cmd)
	if err != nil {
		JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"output": output})
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
		JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, session)
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
		JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, session)
}

// DisableMode disables any active mode (browser or desktop)
// DELETE /api/sandbox/{id}/mode
func (h *SandboxHandler) DisableMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.manager.DisableMode(r.Context(), id); err != nil {
		JSONError(w, err.Error(), http.StatusInternalServerError)
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
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
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

// VNCProxy proxies requests to the sandbox's noVNC web server.
// This allows the frontend to display the sandbox desktop in an iframe.
// Handles both HTTP (for vnc.html page) and WebSocket (for screen streaming).
func (h *SandboxHandler) VNCProxy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	_, err := h.manager.GetSandbox(id)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	// Resolve container IP directly (avoids Docker DNS which isn't available from host)
	containerIP, err := h.manager.GetContainerIP(r.Context(), id)
	if err != nil {
		// Fallback to container name for hostname resolution
		containerIP = fmt.Sprintf("orchestrator-sandbox-%s", id)
	}

	// The sandbox container runs noVNC/websockify on port 6080.
	containerName := containerIP

	// Build the proxy request path
	proxyPath := r.URL.Path
	// Strip the /api/sandbox/vnc/{id} prefix (matching the chi route)
	prefix := fmt.Sprintf("/api/sandbox/vnc/%s", id)
	if strings.HasPrefix(proxyPath, prefix) {
		proxyPath = strings.TrimPrefix(proxyPath, prefix)
		if proxyPath == "" {
			proxyPath = "/vnc.html"
		}
	} else {
		proxyPath = "/vnc.html"
	}

	target := fmt.Sprintf("http://%s:6080%s", containerName, proxyPath)
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	h.logger.Debug("VNC proxy", zap.String("target", target))

	// Check for WebSocket upgrade
	if r.Header.Get("Upgrade") == "websocket" {
		h.handleWebSocket(w, r, containerName, proxyPath)
		return
	}

	// Regular HTTP proxy
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		JSONError(w, "proxy request failed", http.StatusInternalServerError)
		return
	}

	// Copy headers (but change Host)
	for k, vv := range r.Header {
		for _, v := range vv {
			proxyReq.Header.Add(k, v)
		}
	}
	proxyReq.Host = containerName

	// Use a short-timeout transport for container network
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	}
	resp, err := transport.RoundTrip(proxyReq)
	if err != nil {
		h.logger.Warn("VNC proxy upstream error", zap.Error(err))
		JSONError(w, fmt.Sprintf("sandbox desktop not reachable: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleWebSocket takes over the HTTP connection and proxies raw TCP to websockify.
// This is needed because noVNC uses WebSocket for the actual VNC screen stream.
func (h *SandboxHandler) handleWebSocket(w http.ResponseWriter, r *http.Request, containerName string, path string) {
	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		JSONError(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		h.logger.Error("failed to hijack connection", zap.Error(err))
		JSONError(w, "hijack failed", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// Connect to the sandbox container's websockify
	targetAddr := fmt.Sprintf("%s:6080", containerName)
	targetConn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		h.logger.Error("failed to connect to websockify", zap.Error(err))
		return
	}
	defer targetConn.Close()

	// Rebuild the original request and send it to websockify
	// We need to reconstruct the HTTP request that the client was trying to make
	reqPath := path
	if r.URL.RawQuery != "" {
		reqPath = reqPath + "?" + r.URL.RawQuery
	}
	requestLine := fmt.Sprintf("%s %s %s\r\n", r.Method, reqPath, r.Proto)
	targetConn.Write([]byte(requestLine))
	targetConn.Write([]byte(fmt.Sprintf("Host: %s\r\n", containerName)))
	// Forward all original headers
	for k, vv := range r.Header {
		for _, v := range vv {
			targetConn.Write([]byte(fmt.Sprintf("%s: %s\r\n", k, v)))
		}
	}
	targetConn.Write([]byte("\r\n"))

	// Bidirectional pipe — proxy data between client and target
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(conn, targetConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(targetConn, conn)
		done <- struct{}{}
	}()

	// Wait for one side to close, then cleanup
	<-done
	h.logger.Debug("VNC WebSocket proxy connection closed", zap.String("sandbox_id", chi.URLParam(r, "id")))
}
