package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
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
	InitialMode string `json:"initial_mode"` // "cli", "browser", "desktop" — default: "browser"
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
		InitialMode: sandbox.SandboxMode(req.InitialMode),
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

// IsReady returns whether a sandbox's current mode is fully operational.
// GET /api/sandbox/{id}/ready
func (h *SandboxHandler) IsReady(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ready := h.manager.IsReady(id)
	writeJSON(w, http.StatusOK, map[string]bool{"ready": ready})
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
		"viewerUrl": session.ViewerURL,
		"cdpUrl":    getCDPURL(session),
		"vncUrl":    getVNCURL(session),
		"novncUrl":  getNoVNCURL(session),
		"mode":      string(session.Mode),
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

// ---------------------------------------------------------------------------
// VNC Proxy — gorilla/websocket based
// ---------------------------------------------------------------------------

// vncUpgrader upgrades HTTP connections to WebSocket for VNC proxying.
var vncUpgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// vncConn tracks a single VNC proxy connection for lifecycle management.
type vncConn struct {
	id         string
	clientConn *websocket.Conn
	targetConn *websocket.Conn // gorilla WebSocket connection to websockify
	cancel     context.CancelFunc
	startedAt  time.Time
}

// activeVNCConns tracks active VNC proxy connections for cleanup and stats.
var activeVNCConns sync.Map // key: connID string, value: *vncConn

// generateConnID creates a unique connection identifier.
func generateConnID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// VNCProxy proxies requests to the sandbox's noVNC web server.
// Handles both HTTP (for vnc.html page) and WebSocket (for screen streaming).
func (h *SandboxHandler) VNCProxy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	_, err := h.manager.GetSandbox(id)
	if err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	// Resolve the actual noVNC port from the desktop session.
	novncPort := 6080
	if session, sessErr := h.manager.GetDesktopSession(id); sessErr == nil {
		novncPort = session.NoVNCPort
	}

	// Resolve container address (IP or container name)
	containerHost, err := h.manager.GetContainerIP(r.Context(), id)
	if err != nil {
		JSONError(w, fmt.Sprintf("sandbox not reachable (container may not be running): %v", err), http.StatusBadGateway)
		return
	}

	// Build the proxy request path
	proxyPath := r.URL.Path
	prefix := fmt.Sprintf("/api/sandbox/vnc/%s", id)
	if strings.HasPrefix(proxyPath, prefix) {
		proxyPath = strings.TrimPrefix(proxyPath, prefix)
		if proxyPath == "" {
			proxyPath = "/vnc.html"
		}
	} else {
		proxyPath = "/vnc.html"
	}

	// Check for WebSocket upgrade
	if websocket.IsWebSocketUpgrade(r) {
		h.proxyVNCWebSocket(w, r, containerHost, novncPort, proxyPath, id)
		return
	}

	// Regular HTTP reverse proxy
	target := fmt.Sprintf("http://%s%s", net.JoinHostPort(containerHost, fmt.Sprintf("%d", novncPort)), proxyPath)
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	h.logger.Debug("VNC HTTP proxy", zap.String("target", target))

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		JSONError(w, "proxy request failed", http.StatusInternalServerError)
		return
	}
	for k, vv := range r.Header {
		for _, v := range vv {
			proxyReq.Header.Add(k, v)
		}
	}
	proxyReq.Host = containerHost

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

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// proxyVNCWebSocket handles WebSocket proxying for VNC screen streaming.
// Uses gorilla/websocket on BOTH sides — browser and websockify — so WebSocket
// frames are handled correctly (no raw TCP framing mismatch).
func (h *SandboxHandler) proxyVNCWebSocket(w http.ResponseWriter, r *http.Request, containerHost string, novncPort int, path string, sandboxID string) {
	// Upgrade client (browser) connection
	clientConn, err := vncUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("VNC WebSocket upgrade failed", zap.Error(err))
		return
	}

	// Connect to container's websockify via gorilla/websocket Dialer
	targetAddr := net.JoinHostPort(containerHost, fmt.Sprintf("%d", novncPort))
	wsURL := fmt.Sprintf("ws://%s%s", targetAddr, path)
	dialer := websocket.Dialer{
		ReadBufferSize:  8192,
		WriteBufferSize: 8192,
	}
	targetConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		h.logger.Error("VNC websockify connect failed", zap.Error(err), zap.String("url", wsURL))
		clientConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "websockify unreachable"))
		clientConn.Close()
		return
	}

	// Track connection
	connID := generateConnID()
	ctx, cancel := context.WithCancel(context.Background())
	conn := &vncConn{
		id:         connID,
		clientConn: clientConn,
		targetConn: targetConn,
		cancel:     cancel,
		startedAt:  time.Now(),
	}
	activeVNCConns.Store(connID, conn)

	h.logger.Info("VNC WebSocket connected",
		zap.String("conn_id", connID),
		zap.String("sandbox_id", sandboxID),
	)

	// Idle timeout — close connection after 10 minutes of no data
	const idleTimeout = 10 * time.Minute
	clientConn.SetReadDeadline(time.Now().Add(idleTimeout))
	targetConn.SetReadDeadline(time.Now().Add(idleTimeout))

	// Bidirectional relay — both sides use gorilla ReadMessage/WriteMessage
	done := make(chan struct{}, 2)

	// Client → Target
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					h.logger.Debug("VNC client read error", zap.String("conn_id", connID), zap.Error(err))
				}
				return
			}
			if err := targetConn.WriteMessage(mt, msg); err != nil {
				return
			}
			clientConn.SetReadDeadline(time.Now().Add(idleTimeout))
			targetConn.SetReadDeadline(time.Now().Add(idleTimeout))
		}
	}()

	// Target → Client
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			mt, msg, err := targetConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				return
			}
			clientConn.SetReadDeadline(time.Now().Add(idleTimeout))
			targetConn.SetReadDeadline(time.Now().Add(idleTimeout))
		}
	}()

	// Wait for one direction to close, then cleanup
	select {
	case <-done:
	case <-ctx.Done():
	}

	// Cleanup
	activeVNCConns.Delete(connID)
	clientConn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutting down"))
	clientConn.Close()
	targetConn.Close()

	h.logger.Info("VNC WebSocket disconnected",
		zap.String("conn_id", connID),
		zap.String("sandbox_id", sandboxID),
		zap.Duration("duration", time.Since(conn.startedAt)),
	)
}

// CleanupVNCConnections closes all active VNC proxy connections.
// Call during server shutdown.
func (h *SandboxHandler) CleanupVNCConnections() {
	activeVNCConns.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*vncConn); ok {
			conn.cancel()
			conn.clientConn.Close()
			conn.targetConn.Close()
		}
		activeVNCConns.Delete(key)
		return true
	})
}

// VNCReadinessCheck checks whether websockify is accepting connections
// inside the sandbox container. GET /api/sandbox/{id}/vnc-health
func (h *SandboxHandler) VNCReadinessCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Verify sandbox exists
	if _, err := h.manager.GetSandbox(id); err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	// Resolve container IP
	containerHost, err := h.manager.GetContainerIP(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"healthy": false,
			"error":   "container not reachable: " + err.Error(),
		})
		return
	}

	// Resolve noVNC port from desktop session
	novncPort := 6080
	if session, sessErr := h.manager.GetDesktopSession(id); sessErr == nil {
		novncPort = session.NoVNCPort
	}

	// TCP dial with short timeout to check websockify is listening
	addr := net.JoinHostPort(containerHost, fmt.Sprintf("%d", novncPort))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"healthy": false,
			"error":   "websockify not listening: " + err.Error(),
		})
		return
	}
	conn.Close()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"healthy": true,
	})
}

// VNCStats returns stats about active VNC proxy connections.
// GET /api/sandbox/vnc-stats
func (h *SandboxHandler) VNCStats(w http.ResponseWriter, r *http.Request) {
	var count int
	activeVNCConns.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active_connections": count,
	})
}
