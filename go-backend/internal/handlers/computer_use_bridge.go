package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"go.uber.org/zap"
)

// ComputerUseBridge implements the llama.ComputerUseProvider interface
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
func (b *ComputerUseBridge) DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.X11.Screenshot, http.MethodGet, "/api/sandbox/{id}/x11/screenshot?format=json", nil, sandboxID)
}

// DesktopClick clicks at absolute coordinates on the desktop.
func (b *ComputerUseBridge) DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error) {
	return callHandler(ctx, b.X11.Mouse, http.MethodPost, "/api/sandbox/{id}/x11/mouse", map[string]interface{}{
		"action": "click",
		"x":      int(x),
		"y":      int(y),
		"button": button,
	}, sandboxID)
}

// DesktopType types text into the focused window.
func (b *ComputerUseBridge) DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error) {
	return callHandler(ctx, b.X11.Keyboard, http.MethodPost, "/api/sandbox/{id}/x11/keyboard", map[string]interface{}{
		"action": "type",
		"text":   text,
	}, sandboxID)
}

// DesktopKey presses a special key or key combination.
func (b *ComputerUseBridge) DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error) {
	return callHandler(ctx, b.X11.Keyboard, http.MethodPost, "/api/sandbox/{id}/x11/keyboard", map[string]interface{}{
		"action": "key",
		"key":    key,
	}, sandboxID)
}

// Resolution returns the screen dimensions (width x height) for coordinate normalization.
func (b *ComputerUseBridge) Resolution(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return callHandler(ctx, b.X11.Resolution, http.MethodGet, "/api/sandbox/{id}/x11/resolution", nil, sandboxID)
}
