package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// X11Handler handles X11 desktop automation via xdotool and imagemagick.
// It is stateless — all operations execute commands inside the sandbox container.
type X11Handler struct {
	manager *sandbox.Manager
	logger  *zap.Logger
}

// NewX11Handler creates a new X11 automation handler.
func NewX11Handler(manager *sandbox.Manager, logger *zap.Logger) *X11Handler {
	return &X11Handler{
		manager: manager,
		logger:  logger,
	}
}

// RegisterRoutes registers X11 automation routes.
func (h *X11Handler) RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
}) {
	r.Post("/mouse", h.Mouse)
	r.Post("/keyboard", h.Keyboard)
	r.Get("/screenshot", h.Screenshot)
	r.Get("/resolution", h.Resolution)
	r.Get("/active-window", h.ActiveWindow)
}

// displayForSandbox resolves the X11 display number for a sandbox.
// Falls back to :99 (browser mode default) if no desktop session exists.
func (h *X11Handler) displayForSandbox(sandboxID string) string {
	if session, err := h.manager.GetDesktopSession(sandboxID); err == nil {
		return fmt.Sprintf(":%d", session.DisplayNum)
	}
	return ":99"
}

// exec runs a command in the sandbox with DISPLAY set.
func (h *X11Handler) exec(r *http.Request, sandboxID, display string, cmd []string) (string, error) {
	// Wrap with DISPLAY env
	fullCmd := append([]string{"env", "DISPLAY=" + display}, cmd...)
	return h.manager.ExecInSandbox(r.Context(), sandboxID, fullCmd)
}

// ── Mouse ────────────────────────────────────────────────────────────────

type mouseRequest struct {
	Action string `json:"action"` // "click", "move"
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button int    `json:"button,omitempty"` // 1=left (default), 2=middle, 3=right
}

// POST /api/sandbox/{id}/x11/mouse
func (h *X11Handler) Mouse(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	var req mouseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	display := h.displayForSandbox(sandboxID)

	switch req.Action {
	case "click":
		button := req.Button
		if button == 0 {
			button = 1
		}
		_, err := h.exec(r, sandboxID, display, []string{
			"xdotool", "mousemove", fmt.Sprintf("%d", req.X), fmt.Sprintf("%d", req.Y),
			"click", fmt.Sprintf("%d", button),
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	case "move":
		_, err := h.exec(r, sandboxID, display, []string{
			"xdotool", "mousemove", fmt.Sprintf("%d", req.X), fmt.Sprintf("%d", req.Y),
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be 'click' or 'move'"})
	}
}

// ── Keyboard ─────────────────────────────────────────────────────────────

type keyboardRequest struct {
	Action string `json:"action"` // "type" or "key"
	Text   string `json:"text,omitempty"`
	Key    string `json:"key,omitempty"`  // e.g. "Return", "ctrl+a", "alt+F4"
}

// POST /api/sandbox/{id}/x11/keyboard
func (h *X11Handler) Keyboard(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	var req keyboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	display := h.displayForSandbox(sandboxID)

	switch req.Action {
	case "type":
		if req.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required for type action"})
			return
		}
		// Shell-escape single quotes for xdotool
		escaped := strings.ReplaceAll(req.Text, "'", "'\\''")
		_, err := h.exec(r, sandboxID, display, []string{
			"bash", "-c", fmt.Sprintf("xdotool type -- '%s'", escaped),
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	case "key":
		if req.Key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required for key action"})
			return
		}
		_, err := h.exec(r, sandboxID, display, []string{
			"xdotool", "key", req.Key,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be 'type' or 'key'"})
	}
}

// ── Screenshot ───────────────────────────────────────────────────────────

// GET /api/sandbox/{id}/x11/screenshot?format=json
func (h *X11Handler) Screenshot(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	display := h.displayForSandbox(sandboxID)

	// Use imagemagick import for full desktop screenshot
	output, err := h.exec(r, sandboxID, display, []string{
		"bash", "-c", "import -window root /tmp/x11-screenshot.png && base64 /tmp/x11-screenshot.png",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Trim whitespace from base64 output
	b64 := strings.TrimSpace(output)

	format := r.URL.Query().Get("format")
	if format == "png" {
		pngBytes, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "base64 decode failed"})
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"image": b64,
	})
}

// ── Resolution ───────────────────────────────────────────────────────────

// GET /api/sandbox/{id}/x11/resolution
func (h *X11Handler) Resolution(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	display := h.displayForSandbox(sandboxID)

	output, err := h.exec(r, sandboxID, display, []string{
		"xdotool", "getdisplaygeometry",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Output format: "1920 1080\n"
	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) < 2 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unexpected geometry output: " + output})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"width":  parts[0],
		"height": parts[1],
	})
}

// ── Active Window ────────────────────────────────────────────────────────

// GET /api/sandbox/{id}/x11/active-window
func (h *X11Handler) ActiveWindow(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	display := h.displayForSandbox(sandboxID)

	// Get active window ID
	windowID, err := h.exec(r, sandboxID, display, []string{
		"xdotool", "getactivewindow",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Get window name
	windowName, err := h.exec(r, sandboxID, display, []string{
		"xdotool", "getwindowname", strings.TrimSpace(windowID),
	})
	if err != nil {
		// Window might have disappeared — still return the ID
		windowName = ""
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"windowId":   strings.TrimSpace(windowID),
		"windowName": strings.TrimSpace(windowName),
	})
}
