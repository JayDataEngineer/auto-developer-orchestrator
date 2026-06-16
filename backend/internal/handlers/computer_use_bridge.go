package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/tools/desktop"
	"go.uber.org/zap"
)

// ComputerUseBridge implements the llama.ComputerUseProvider interface
// and the desktop.DesktopProvider interface.
//
// Compile-time interface assertions:
var _ desktop.DesktopProvider = (*ComputerUseBridge)(nil)
// by calling the existing HTTP handlers directly (no network hop).
//
// It uses httptest.NewRecorder to capture handler responses. This pattern is
// necessary because the handlers (ComputerUseHandler.Enable, X11Handler.Mouse, etc.)
// are http.HandlerFunc — they write to http.ResponseWriter and read chi path params
// via r.PathValue("id"). Converting to direct function calls would require refactoring
// every handler to accept sandboxID as an explicit parameter. The recorder adds ~0.1ms
// overhead per call vs agent loop taking 1-3 seconds per tool call.
type ComputerUseBridge struct {
	CU  *ComputerUseHandler
	X11 *X11Handler
	Log *zap.Logger
}

// callHandler invokes an HTTP handler and returns the decoded JSON response.
func callHandler(ctx context.Context, handler http.HandlerFunc, method, path string, body interface{}, sandboxID string) (map[string]interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetPathValue("id", sandboxID)

	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code >= 400 {
		var errResp map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &errResp)
		errMsg := "unknown error"
		if e, ok := errResp["error"]; ok {
			errMsg = fmt.Sprintf("%v", e)
		} else if e, ok := errResp["message"]; ok {
			errMsg = fmt.Sprintf("%v", e)
		}
		return nil, fmt.Errorf("handler returned %d: %s", rec.Code, errMsg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// IsReady checks if the CDP client is connected without making any CDP calls.
// This is a fast in-memory check (~1ms) suitable for polling.
func (b *ComputerUseBridge) IsReady(sandboxID string) bool {
	b.CU.mu.RLock()
	client, ok := b.CU.clients[sandboxID]
	b.CU.mu.RUnlock()
	return ok && client.IsConnected()
}

// Enable enables the sandbox desktop environment.
func (b *ComputerUseBridge) Enable(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.Enable, http.MethodPost, "/api/sandbox/{id}/computer-use/enable", nil, sandboxID)
}

// EnsureReady ensures the browser (Chrome/CDP) is running in the sandbox.
// If CDP is already connected, returns immediately (~1ms). Otherwise triggers
// browser setup (Docker container + Chrome + CDP connect) and waits up to 60s.
// This mirrors X11Handler.EnsureDesktopMode for desktop auto-escalation.
func (b *ComputerUseBridge) EnsureReady(ctx context.Context, sandboxID string) error {
	// Fast path: CDP already connected
	if b.IsReady(sandboxID) {
		return nil
	}

	b.Log.Info("auto-provisioning browser sandbox", zap.String("sandbox_id", sandboxID))

	// Trigger Enable — sends HTTP response immediately, runs backgroundSetup in goroutine
	_, err := b.Enable(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("enable browser: %w", err)
	}

	// Poll until CDP client connects (backgroundSetup is async, takes 10-30s)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.After(60 * time.Second)

	for {
		select {
		case <-ticker.C:
			if b.IsReady(sandboxID) {
				b.Log.Info("browser sandbox ready", zap.String("sandbox_id", sandboxID))
				return nil
			}
		case <-timeout:
			return fmt.Errorf("browser setup timed out after 60s for sandbox %s", sandboxID)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Screenshot takes a browser screenshot via CDP.
func (b *ComputerUseBridge) Screenshot(ctx context.Context, sandboxID string, describe bool) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/sandbox/{id}/computer-use/screenshot?describe=%v&format=json", describe)
	return callHandler(ctx, b.CU.Screenshot, http.MethodGet, path, nil, sandboxID)
}

// Snapshot returns the page elements with IDs for targeting.
func (b *ComputerUseBridge) Snapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.Snapshot, http.MethodGet, "/api/sandbox/{id}/computer-use/snapshot", nil, sandboxID)
}

// Act performs a browser action (click, type, navigate, scroll).
func (b *ComputerUseBridge) Act(ctx context.Context, sandboxID string, action string, args map[string]interface{}) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.Act, http.MethodPost, "/api/sandbox/{id}/computer-use/act", args, sandboxID)
}

// DesktopScreenshot takes a full X11 desktop screenshot.
// Auto-escalates sandbox to desktop mode if needed.
func (b *ComputerUseBridge) DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	if err := b.X11.EnsureDesktopMode(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("desktop mode: %w", err)
	}
	return callHandler(ctx, b.X11.Screenshot, http.MethodGet, "/api/sandbox/{id}/x11/screenshot?format=json", nil, sandboxID)
}

// DesktopClick clicks at absolute coordinates on the desktop.
// Auto-escalates sandbox to desktop mode if needed.
func (b *ComputerUseBridge) DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error) {
	if err := b.X11.EnsureDesktopMode(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("desktop mode: %w", err)
	}
	return callHandler(ctx, b.X11.Mouse, http.MethodPost, "/api/sandbox/{id}/x11/mouse", map[string]interface{}{
		"action": "click",
		"x":      int(x),
		"y":      int(y),
		"button": button,
	}, sandboxID)
}

// DesktopType types text into the focused window.
// Auto-escalates sandbox to desktop mode if needed.
func (b *ComputerUseBridge) DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error) {
	if err := b.X11.EnsureDesktopMode(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("desktop mode: %w", err)
	}
	return callHandler(ctx, b.X11.Keyboard, http.MethodPost, "/api/sandbox/{id}/x11/keyboard", map[string]interface{}{
		"action": "type",
		"text":   text,
	}, sandboxID)
}

// DesktopKey presses a special key or key combination.
// Auto-escalates sandbox to desktop mode if needed.
func (b *ComputerUseBridge) DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error) {
	if err := b.X11.EnsureDesktopMode(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("desktop mode: %w", err)
	}
	return callHandler(ctx, b.X11.Keyboard, http.MethodPost, "/api/sandbox/{id}/x11/keyboard", map[string]interface{}{
		"action": "key",
		"key":    key,
	}, sandboxID)
}

// Resolution returns the screen dimensions (width x height) for coordinate normalization.
func (b *ComputerUseBridge) Resolution(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.X11.Resolution, http.MethodGet, "/api/sandbox/{id}/x11/resolution", nil, sandboxID)
}

// DesktopObserve captures screenshot + OCR elements + window list in one call.
func (b *ComputerUseBridge) DesktopObserve(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.X11.Observe, http.MethodGet, "/api/sandbox/{id}/x11/observe", nil, sandboxID)
}

// Navigate navigates the browser to a URL and returns page info.
func (b *ComputerUseBridge) Navigate(ctx context.Context, sandboxID string, url string) (map[string]interface{}, error) {
	req := map[string]interface{}{"action": "navigate", "url": url}
	return callHandler(ctx, b.CU.Act, http.MethodPost, "/api/sandbox/{id}/computer-use/act", req, sandboxID)
}

// FindElement performs a semantic find and optional action.
func (b *ComputerUseBridge) FindElement(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.FindElement, http.MethodPost, "/api/sandbox/{id}/computer-use/find", req, sandboxID)
}

// A11ySnapshot returns the accessibility tree.
func (b *ComputerUseBridge) A11ySnapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.A11ySnapshot, http.MethodGet, "/api/sandbox/{id}/computer-use/a11y-snapshot", nil, sandboxID)
}

// GetCookies returns browser cookies.
func (b *ComputerUseBridge) GetCookies(ctx context.Context, sandboxID string, urls []string) (map[string]interface{}, error) {
	path := "/api/sandbox/{id}/computer-use/cookies"
	if len(urls) > 0 {
		path += "?url=" + urls[0]
	}
	return callHandler(ctx, b.CU.GetCookies, http.MethodGet, path, nil, sandboxID)
}

// SetCookie sets a browser cookie.
func (b *ComputerUseBridge) SetCookie(ctx context.Context, sandboxID string, cookie map[string]interface{}) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.SetCookie, http.MethodPost, "/api/sandbox/{id}/computer-use/cookies", cookie, sandboxID)
}

// ClearCookies clears browser cookies.
func (b *ComputerUseBridge) ClearCookies(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.ClearCookies, http.MethodDelete, "/api/sandbox/{id}/computer-use/cookies", nil, sandboxID)
}

// GetStorage returns localStorage data.
func (b *ComputerUseBridge) GetStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.GetStorage, http.MethodGet, "/api/sandbox/{id}/computer-use/storage", nil, sandboxID)
}

// SetStorage sets a localStorage item.
func (b *ComputerUseBridge) SetStorage(ctx context.Context, sandboxID string, key, value string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.SetStorage, http.MethodPost, "/api/sandbox/{id}/computer-use/storage",
		map[string]interface{}{"key": key, "value": value}, sandboxID)
}

// ClearStorage clears localStorage.
func (b *ComputerUseBridge) ClearStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.ClearStorage, http.MethodDelete, "/api/sandbox/{id}/computer-use/storage", nil, sandboxID)
}

// BrowserScreenshot takes a CDP screenshot of the current browser page.
// Returns image data as {"image_b64": "..."} for the vision pipeline.
func (b *ComputerUseBridge) BrowserScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	result, err := callHandler(ctx, b.CU.Screenshot, http.MethodGet, "/api/sandbox/{id}/computer-use/screenshot?describe=true&format=json", nil, sandboxID)
	if err != nil {
		return nil, err
	}
	// Normalize field name: CDP handler returns "image", vision detector expects "image_b64"
	if img, ok := result["image"].(string); ok && img != "" {
		result["image_b64"] = img
		delete(result, "image")
	}
	return result, nil
}

// EvaluateJS executes JavaScript in the browser and returns the result.
func (b *ComputerUseBridge) EvaluateJS(ctx context.Context, sandboxID, code string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.EvaluateJS, http.MethodPost, "/api/sandbox/{id}/computer-use/evaluate-js",
		map[string]interface{}{"code": code}, sandboxID)
}

// ReadPage extracts structured content from the current page.
func (b *ComputerUseBridge) ReadPage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.ReadPage, http.MethodGet, "/api/sandbox/{id}/computer-use/read-page", nil, sandboxID)
}

// DownloadFile downloads a file to the sandbox via curl.
func (b *ComputerUseBridge) DownloadFile(ctx context.Context, sandboxID, url, path string) (map[string]interface{}, error) {
	return callHandler(ctx, b.CU.DownloadFile, http.MethodPost, "/api/sandbox/{id}/computer-use/download",
		map[string]interface{}{"url": url, "path": path}, sandboxID)
}

// SelectOption selects an option in a <select> dropdown by value or visible text.
func (b *ComputerUseBridge) SelectOption(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	selector, _ := req["selector"].(string)
	if selector == "" {
		selector = "select"
	}
	value, _ := req["value"].(string)
	text, _ := req["text"].(string)

	// Escape single quotes to prevent JS injection
	escape := func(s string) string {
		return strings.ReplaceAll(s, "'", "\\'")
	}

	// Build JS: set value by option value or visible text, dispatch change event
	var js string
	if value != "" {
		escapedVal := escape(value)
		js = fmt.Sprintf(`(function(){var s=document.querySelector('%s');if(!s)return{error:'select not found'};s.value='%s';if(!s.value)s.value='%s';s.dispatchEvent(new Event('change',{bubbles:true}));s.dispatchEvent(new Event('input',{bubbles:true}));return{selected:s.value,selectedIndex:s.selectedIndex}})()`,
			selector, escapedVal, escapedVal)
	} else {
		// Find by visible text
		escapedText := escape(text)
		js = fmt.Sprintf(`(function(){var s=document.querySelector('%s');if(!s)return{error:'select not found'};for(var i=0;i<s.options.length;i++){if(s.options[i].textContent.trim()==='%s'){s.selectedIndex=i;s.dispatchEvent(new Event('change',{bubbles:true}));s.dispatchEvent(new Event('input',{bubbles:true}));return{selected:s.value,selectedIndex:i}}}return{error:'option not found: %s'}})()`,
			selector, escapedText, escapedText)
	}
	return b.EvaluateJS(ctx, sandboxID, js)
}

// UploadFile uploads a file to an <input type="file"> element via CDP.
// Uses DOM.setFileInputFiles which is the proper CDP method, far more reliable than JS.
func (b *ComputerUseBridge) UploadFile(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	filePath, _ := req["file_path"].(string)
	selector, _ := req["selector"].(string)

	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	body := map[string]interface{}{
		"file_path": filePath,
	}
	if selector != "" {
		body["selector"] = selector
	}

	return callHandler(ctx, b.CU.UploadFile, http.MethodPost, "/api/sandbox/{id}/computer-use/upload-file", body, sandboxID)
}

// SaveSession saves cookies + localStorage to a file for persistence.
// Writes directly to the sandbox container filesystem.
func (b *ComputerUseBridge) SaveSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error) {
	if b.CU.manager == nil {
		return nil, fmt.Errorf("sandbox manager not available")
	}

	// Get cookies via handler
	cookiesResult, err := b.GetCookies(ctx, sandboxID, nil)
	if err != nil {
		return nil, fmt.Errorf("get cookies: %w", err)
	}

	// Get localStorage via handler
	storageResult, err := b.GetStorage(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("get storage: %w", err)
	}

	// Build session JSON
	session := map[string]interface{}{
		"cookies":   cookiesResult,
		"storage":   storageResult,
		"savedAt":   time.Now().Format(time.RFC3339),
		"sandboxID": sandboxID,
	}
	sessionJSON, _ := json.MarshalIndent(session, "", "  ")

	// Write to file using sandbox manager
	_, err = b.CU.manager.ExecInSandbox(ctx, sandboxID, []string{
		"bash", "-c",
		fmt.Sprintf(`cat > '%s' << 'SESSEOF'
%s
SESSEOF`, path, string(sessionJSON)),
	})
	if err != nil {
		return nil, fmt.Errorf("write session file: %w", err)
	}

	return map[string]interface{}{
		"session_path": path,
		"saved":        true,
		"savedAt":      session["savedAt"],
	}, nil
}

// RestoreSession restores cookies + localStorage from a saved session file.
// Reads the session file from the sandbox container and restores cookies/storage.
func (b *ComputerUseBridge) RestoreSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error) {
	if b.CU.manager == nil {
		return nil, fmt.Errorf("sandbox manager not available")
	}

	// Read session file
	output, err := b.CU.manager.ExecInSandbox(ctx, sandboxID, []string{
		"bash", "-c",
		fmt.Sprintf("cat '%s'", path),
	})
	if err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}

	// Parse session JSON
	var session struct {
		Cookies json.RawMessage `json:"cookies"`
		Storage json.RawMessage `json:"storage"`
	}
	if err := json.Unmarshal([]byte(output), &session); err != nil {
		return nil, fmt.Errorf("parse session file: %w (content: %.200s)", err, output)
	}

	restored := []string{}
	errors := []string{}

	// Restore cookies if present
	if len(session.Cookies) > 0 && string(session.Cookies) != "null" {
		var cookiesData map[string]interface{}
		if err := json.Unmarshal(session.Cookies, &cookiesData); err == nil {
			if cookiesList, ok := cookiesData["cookies"].([]interface{}); ok {
				for _, c := range cookiesList {
					if cookie, ok := c.(map[string]interface{}); ok {
						_, err := b.SetCookie(ctx, sandboxID, cookie)
						if err != nil {
							errors = append(errors, fmt.Sprintf("cookie: %v", err))
						} else {
							if name, _ := cookie["name"].(string); name != "" {
								restored = append(restored, "cookie:"+name)
							}
						}
					}
				}
			}
		}
	}

	// Restore localStorage if present
	if len(session.Storage) > 0 && string(session.Storage) != "null" {
		var storageData map[string]interface{}
		if err := json.Unmarshal(session.Storage, &storageData); err == nil {
			if entries, ok := storageData["entries"].(map[string]interface{}); ok {
				for key, value := range entries {
					valStr, _ := value.(string)
					_, err := b.SetStorage(ctx, sandboxID, key, valStr)
					if err != nil {
						errors = append(errors, fmt.Sprintf("storage:%s: %v", key, err))
					} else {
						restored = append(restored, "storage:"+key)
					}
				}
			}
		}
	}

	result := map[string]interface{}{
		"restored": restored,
		"count":    len(restored),
	}
	if len(errors) > 0 {
		result["errors"] = errors
	}
	return result, nil
}

// ── Inject File ──

// InjectFile writes base64-encoded content to a file in the sandbox.
func (b *ComputerUseBridge) InjectFile(ctx context.Context, sandboxID, destPath, contentBase64 string) (map[string]interface{}, error) {
	if b.CU.manager == nil {
		return nil, fmt.Errorf("sandbox manager not available")
	}

	// Create parent directory and decode base64 to destination file.
	// Base64 only contains [A-Za-z0-9+/=] so heredoc delimiters are safe.
	_, err := b.CU.manager.ExecInSandbox(ctx, sandboxID, []string{
		"bash", "-c",
		fmt.Sprintf("mkdir -p $(dirname '%s') && base64 -d > '%s' << 'B64EOF'\n%s\nB64EOF",
			destPath, destPath, contentBase64),
	})
	if err != nil {
		return nil, fmt.Errorf("inject file: %w", err)
	}

	// Get file size for confirmation
	sizeOutput, _ := b.CU.manager.ExecInSandbox(ctx, sandboxID, []string{
		"bash", "-c", fmt.Sprintf("stat -c '%%s' '%s' 2>/dev/null || echo '0'", destPath),
	})
	fileSize := strings.TrimSpace(sizeOutput)

	return map[string]interface{}{
		"path":      destPath,
		"injected":  true,
		"size":      fileSize,
	}, nil
}

// ── Credential Get ──

// CredentialGet reads credentials from environment variables for a given service.
func (b *ComputerUseBridge) CredentialGet(ctx context.Context, sandboxID, service string) (map[string]interface{}, error) {
	upperService := strings.ToUpper(service)

	// Try various env var naming conventions
	username := os.Getenv(upperService + "_USERNAME")
	if username == "" {
		username = os.Getenv(upperService + "_EMAIL")
	}
	password := os.Getenv(upperService + "_PASSWORD")
	if password == "" {
		password = os.Getenv(upperService + "_PASS")
	}

	if username == "" && password == "" {
		return nil, fmt.Errorf("no credentials found for service %q. Set %s_USERNAME and %s_PASSWORD environment variables",
			service, upperService, upperService)
	}

	result := map[string]interface{}{
		"service":  service,
		"found":    username != "",
	}
	if username != "" {
		result["username"] = username
	}
	if password != "" {
		result["password"] = password
	}
	return result, nil
}

// ── User Profile ──

// UserProfile reads the user's profile from a JSON config file.
func (b *ComputerUseBridge) UserProfile(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	// Check multiple paths in priority order
	paths := []string{
		os.Getenv("PROFILE_PATH"),
		filepath.Join(os.Getenv("HOME"), ".pux", "user_profile.json"),
		filepath.Join(os.Getenv("PROJECT_ROOT"), "user_profile.json"),
	}

	var data []byte
	var usedPath string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if d, err := os.ReadFile(p); err == nil {
			data = d
			usedPath = p
			break
		}
	}

	if data == nil {
		return nil, fmt.Errorf("user profile not found. Create ~/.pux/user_profile.json with your profile info")
	}

	var profile map[string]interface{}
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse profile from %s: %w", usedPath, err)
	}

	profile["_source"] = usedPath
	profile["found"] = true

	return profile, nil
}
