package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// nonPrintableRe matches any non-printable characters (Docker multiplexed stream
// framing bytes that leak through execInContainer's output reader).
var nonPrintableRe = regexp.MustCompile(`[\x00-\x08\x0b\x0e-\x1f]+`)

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
	r.Get("/observe", h.Observe)
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

// EnsureDesktopMode ensures the sandbox has a full X11 desktop session running.
// If the sandbox is already in desktop mode, this is a no-op. If it's in browser
// or CLI mode, it escalates to desktop mode (starts Xvfb + window manager + VNC).
// Also installs X11 screenshot tools (imagemagick, scrot) if missing.
func (h *X11Handler) EnsureDesktopMode(ctx context.Context, sandboxID string) error {
	// Check if already in desktop mode — idempotent
	if session, err := h.manager.GetDesktopSession(sandboxID); err == nil && session.Mode == "desktop" {
		return nil
	}

	h.logger.Info("auto-escalating sandbox to desktop mode", zap.String("sandbox_id", sandboxID))

	// Install X11 screenshot tools if missing (apt is cached, fast on 2nd call)
	_, _ = h.manager.ExecInSandbox(ctx, sandboxID, []string{
		"bash", "-c",
		"dpkg -l | grep -q imagemagick || (apt-get update -qq && apt-get install -y -qq imagemagick scrot xdotool 2>/dev/null) || true",
	})
	// Install OCR tools for desktop_observe (graceful: script handles missing tesseract)
	_, _ = h.manager.ExecInSandbox(ctx, sandboxID, []string{
		"bash", "-c",
		"dpkg -l | grep -q tesseract-ocr || (apt-get update -qq && apt-get install -y -qq tesseract-ocr tesseract-ocr-eng 2>/dev/null) || true",
	})

	_, err := h.manager.EnableDesktopMode(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("failed to enable desktop mode for %s: %w", sandboxID, err)
	}
	return nil
}

// exec runs a command in the sandbox with DISPLAY set.
func (h *X11Handler) exec(r *http.Request, sandboxID, display string, cmd []string) (string, error) {
	// Wrap with DISPLAY env
	fullCmd := append([]string{"env", "DISPLAY=" + display}, cmd...)
	out, err := h.manager.ExecInSandbox(r.Context(), sandboxID, fullCmd)
	return cleanOutput(out), err
}

// cleanOutput strips Docker multiplexed stream framing bytes that leak
// through the exec attach reader.
func cleanOutput(s string) string {
	return strings.TrimSpace(nonPrintableRe.ReplaceAllString(s, ""))
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
	req, ok := decodeReq[mouseRequest](w, r)
	if !ok { return }

	display := h.displayForSandbox(sandboxID)

	switch req.Action {
	case "click":
		button := req.Button
		if button == 0 {
			button = 1
		}
		x, y := req.X, req.Y

		// Move cursor to target. Brief pause lets VNC render the cursor
		// arriving before the click fires — makes mouse movement visible
		// to anyone watching through noVNC.
		_, _ = h.exec(r, sandboxID, display, []string{
			"bash", "-c", fmt.Sprintf("xdotool mousemove %d %d && sleep 0.15", x, y),
		})

		// Execute the click
		_, err := h.exec(r, sandboxID, display, []string{
			"xdotool", "click", fmt.Sprintf("%d", button),
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
	req, ok := decodeReq[keyboardRequest](w, r)
	if !ok { return }

	display := h.displayForSandbox(sandboxID)

	switch req.Action {
	case "type":
		if req.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required for type action"})
			return
		}
		// Unicode text via clipboard paste (Agent-S pattern): xdotool type fails on
		// special characters. Try xclip paste first, fall back to xdotool type.
		escaped := sandbox.ShellEscape(req.Text)
		if containsNonASCII(req.Text) {
			// Clipboard paste for any non-ASCII text
			_, err := h.exec(r, sandboxID, display, []string{
				"bash", "-c",
				fmt.Sprintf("printf '%%s' '%s' | xclip -selection clipboard 2>/dev/null && xdotool key ctrl+v || xdotool type -- '%s'",
					escaped, escaped),
			})
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		} else {
			_, err := h.exec(r, sandboxID, display, []string{
				"bash", "-c", fmt.Sprintf("xdotool type -- '%s'", escaped),
			})
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
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

	// Screenshot fallback chain: import → scrot → xdotool
	// Each method uses base64 to avoid Docker multiplexed stream framing issues.
	screenshotCmds := []string{
		// Method 1: ImageMagick import (fastest, most reliable)
		"import -window root /tmp/x11-screenshot.png && base64 -w0 /tmp/x11-screenshot.png",
		// Method 2: scrot (lightweight alternative)
		"scrot /tmp/x11-screenshot.png && base64 -w0 /tmp/x11-screenshot.png",
		// Method 3: xdotool + xwd (X11 native, always available)
		"xwd -root -out /tmp/x11-screenshot.xwd && convert xwd:/tmp/x11-screenshot.xwd /tmp/x11-screenshot.png && base64 -w0 /tmp/x11-screenshot.png",
	}

	var output string
	var err error
	for _, cmd := range screenshotCmds {
		output, err = h.exec(r, sandboxID, display, []string{
			"bash", "-c", cmd + " | tr -d '\\0'",
		})
		if err == nil && len(output) > 100 {
			break
		}
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Strip Docker framing bytes and any non-base64 characters
	b64 := nonPrintableRe.ReplaceAllString(output, "")
	b64 = strings.TrimSpace(b64)
	// Remove any remaining non-base64 chars (newlines, spaces)
	var clean strings.Builder
	for _, c := range b64 {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			clean.WriteRune(c)
		}
	}
	b64 = clean.String()

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
		"image_b64": b64,
	})
}

// ── Observe (OCR + Elements) ────────────────────────────────────────────

// GET /api/sandbox/{id}/x11/observe
func (h *X11Handler) Observe(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	if err := h.EnsureDesktopMode(r.Context(), sandboxID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	display := h.displayForSandbox(sandboxID)

	output, err := h.exec(r, sandboxID, display, []string{
		"python3", "/usr/local/bin/desktop_observe.py",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Script outputs JSON — parse and re-emit
	output = cleanOutput(output)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to parse observe output: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

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

// containsNonASCII returns true if the string contains any non-ASCII characters.
// Used to determine whether to use clipboard paste instead of xdotool type.
func containsNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}
