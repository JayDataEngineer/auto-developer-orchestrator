package handlers

import (
	"github.com/auto-developer-orchestrator/backend/internal/browser"
)

// newMouseResolver returns a function that resolves normalized (0-1) coordinates
// for the visual mouse cursor overlay. It inspects tool name and args to compute
// where the cursor should appear before the tool executes.
//
// Browser tools (click_element, type_text): look up element by ID from cached
// SoM labels, compute center of bounding box, normalize by viewport size.
//
// Desktop tools (desktop_click): args already use 0-1000 normalization, so
// divide by 1000 to get 0-1.
func newMouseResolver(cu *ComputerUseHandler, sandboxID string) func(toolName string, args map[string]any) (float64, float64, string) {
	return func(toolName string, args map[string]any) (float64, float64, string) {
		switch toolName {
		case "click_element", "type_text":
			return resolveBrowserCoords(cu, sandboxID, args)
		case "desktop_click":
			return resolveDesktopCoords(args)
		default:
			return 0, 0, ""
		}
	}
}

// resolveBrowserCoords looks up an element by ID from the cached SoM labels
// and returns its center normalized to 0-1 viewport coordinates.
func resolveBrowserCoords(cu *ComputerUseHandler, sandboxID string, args map[string]any) (float64, float64, string) {
	// Extract element ID
	var elementID int
	if id, ok := args["element"].(float64); ok {
		elementID = int(id)
	}
	if id, ok := args["element"].(int); ok {
		elementID = id
	}
	if elementID <= 0 {
		return 0, 0, ""
	}

	// Get the browser client
	client, err := cu.getClient(sandboxID)
	if err != nil {
		return 0, 0, ""
	}

	// Get cached elements and viewport
	snapshot, err := client.GetSnapshot()
	if err != nil || snapshot == nil {
		return 0, 0, ""
	}
	vw, vh := client.LastViewportSize()
	if vw <= 0 || vh <= 0 {
		return 0, 0, ""
	}

	// Find the element by ID
	var el *browser.LabeledElement
	for i := range snapshot.Elements {
		if snapshot.Elements[i].ID == elementID {
			el = &snapshot.Elements[i]
			break
		}
	}
	if el == nil {
		return 0, 0, ""
	}

	// Compute center of bounding box
	centerX := float64(el.X) + float64(el.W)/2.0
	centerY := float64(el.Y) + float64(el.H)/2.0

	// Normalize to 0-1
	normX := centerX / float64(vw)
	normY := centerY / float64(vh)

	// Clamp to 0-1
	if normX < 0 {
		normX = 0
	}
	if normX > 1 {
		normX = 1
	}
	if normY < 0 {
		normY = 0
	}
	if normY > 1 {
		normY = 1
	}

	action := "click"
	return normX, normY, action
}

// resolveDesktopCoords normalizes desktop tool coordinates from 0-1000 to 0-1.
func resolveDesktopCoords(args map[string]any) (float64, float64, string) {
	x, _ := args["x"].(float64)
	y, _ := args["y"].(float64)

	// Desktop tools use 0-1000 normalized coordinates
	if x < 0 || x > 1000 || y < 0 || y > 1000 {
		return 0, 0, ""
	}

	return x / 1000.0, y / 1000.0, "click"
}
