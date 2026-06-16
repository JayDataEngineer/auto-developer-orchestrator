package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FindElementVisual locates a UI element visually via the MCP ground_ui tool
// (ShowUI-2B). Use this when DOM-based tools (find_element, snapshot_a11y,
// evaluate_js) cannot see the target — e.g., canvas/WebGL apps, image maps,
// or obfuscated SPAs where the element has no DOM node.
//
// The req map supports:
//   - query (string, required): natural-language description of the target
//     element, e.g., "the green color picker swatch" or "the play button"
//   - action (string, optional): "click" to click the located coordinates
//
// Returns the ground_ui JSON response (x, y, width, height, x_norm, y_norm).
// If action="click", also clicks the returned (x, y) via CDP Input.dispatchMouseEvent.
func (b *ComputerUseBridge) FindElementVisual(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	query, _ := req["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("find_element_visual: 'query' parameter required")
	}
	action, _ := req["action"].(string)

	if b.CU.mcpMulti == nil {
		return nil, fmt.Errorf("find_element_visual: MCP not wired (mcpMulti is nil)")
	}
	if !b.CU.mcpMulti.HasTool("ground_ui") {
		return nil, fmt.Errorf("find_element_visual: MCP server does not expose 'ground_ui' tool")
	}

	// 1. Take a CDP screenshot via the existing handler.
	shotResp, err := callHandler(ctx, b.CU.Screenshot, http.MethodGet,
		"/api/sandbox/{id}/computer-use/screenshot?format=json", nil, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("find_element_visual: take screenshot: %w", err)
	}
	b64, _ := shotResp["image"].(string)
	if b64 == "" {
		return nil, fmt.Errorf("find_element_visual: screenshot returned no image bytes")
	}

	// 2. Upload the PNG to the MCP server so ground_ui can fetch it locally.
	uploadResult, err := b.CU.mcpMulti.CallTool(ctx, "upload", map[string]any{
		"data":      b64,
		"mime_type": "image/png",
	})
	if err != nil {
		return nil, fmt.Errorf("find_element_visual: MCP upload: %w", err)
	}
	imageURL := extractUploadURL(uploadResult)
	if imageURL == "" {
		return nil, fmt.Errorf("find_element_visual: upload returned no URL (raw: %s)", truncate(uploadResult, 200))
	}

	// 3. Call ground_ui with the user's query.
	groundResult, err := b.CU.mcpMulti.CallTool(ctx, "ground_ui", map[string]any{
		"imageSource": imageURL,
		"query":       query,
	})
	if err != nil {
		return nil, fmt.Errorf("find_element_visual: MCP ground_ui: %w", err)
	}

	// 4. Parse the JSON response — ground_ui returns either raw JSON
	// like {"success":true,"x":640,"y":209,...} or SSE-encoded data.
	coords := parseGroundUIResponse(groundResult)
	if coords == nil {
		return nil, fmt.Errorf("find_element_visual: could not parse ground_ui response: %s", truncate(groundResult, 200))
	}
	coords["image_url"] = imageURL
	coords["query"] = query

	// 5. If action=click, dispatch a CDP mouse event at the coordinates.
	if strings.EqualFold(action, "click") {
		x, _ := coords["x"].(float64)
		y, _ := coords["y"].(float64)
		if x == 0 && y == 0 {
			return coords, fmt.Errorf("find_element_visual: ground_ui returned (0,0); refusing to click top-left corner")
		}

		b.CU.mu.RLock()
		client, ok := b.CU.clients[sandboxID]
		b.CU.mu.RUnlock()
		if !ok {
			return coords, fmt.Errorf("find_element_visual: no browser client for sandbox %q", sandboxID)
		}

		if _, err := client.ClickXY(ctx, x, y); err != nil {
			return coords, fmt.Errorf("find_element_visual: ClickXY(%v,%v): %w", x, y, err)
		}
		coords["clicked"] = true
	}

	return coords, nil
}

// extractUploadURL pulls the URL out of the MCP upload tool's response.
// The response is either a JSON object with a "url" field or plain text.
func extractUploadURL(raw string) string {
	// Try SSE-encoded first (data: {json})
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var obj map[string]any
			if json.Unmarshal([]byte(payload), &obj) == nil {
				if u, ok := obj["url"].(string); ok && u != "" {
					return u
				}
			}
		}
	}
	// Plain JSON
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) == nil {
		if u, ok := obj["url"].(string); ok && u != "" {
			return u
		}
	}
	// Last resort: if the whole response is a URL, return it.
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "http") && !strings.Contains(trimmed, " ") {
		return trimmed
	}
	return ""
}

// parseGroundUIResponse extracts the coordinate fields from ground_ui's response.
// Returns nil if the response can't be parsed.
func parseGroundUIResponse(raw string) map[string]interface{} {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") && !strings.HasPrefix(line, "{") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var obj map[string]any
		if json.Unmarshal([]byte(payload), &obj) != nil {
			continue
		}
		if _, ok := obj["x"]; !ok {
			continue
		}
		return obj
	}
	// Direct JSON
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) == nil {
		if _, ok := obj["x"]; ok {
			return obj
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Ensure the bridge satisfies any future interface that requires FindElementVisual.
var _ = (*ComputerUseBridge)(nil).FindElementVisual

// base64 import retained for future screenshot-decoding paths.
var _ = base64.StdEncoding
