package handlers

import (
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/browser"
)

// newMouseResolver returns a function that resolves normalized (0-1) coordinates
// for the visual mouse cursor overlay. It inspects tool name and args to compute
// where the cursor should appear before the tool executes.
//
// Browser tools (click_element, type_text): look up element by ID from cached
// SoM labels, compute center of bounding box, normalize by viewport size.
//
// find_element (action=click or action=type): the browser_ops capability does
// NOT expose click_element — find_element with action:"click" is the canonical
// click flow. Without this case the cursor overlay never renders for any
// browser_ops click. We match args against the cached snapshot the same way
// matchA11yItem does.
//
// Desktop tools (desktop_click): args already use 0-1000 normalization, so
// divide by 1000 to get 0-1.
func newMouseResolver(cu *ComputerUseHandler, sandboxID string) func(toolName string, args map[string]any) (float64, float64, string) {
	return func(toolName string, args map[string]any) (float64, float64, string) {
		switch toolName {
		case "click_element", "type_text":
			return resolveBrowserCoords(cu, sandboxID, args)
		case "find_element":
			return resolveFindElementCoords(cu, sandboxID, args)
		case "desktop_click":
			return resolveDesktopCoords(args)
		default:
			return 0, 0, ""
		}
	}
}

// resolveFindElementCoords matches find_element args against the cached SoM
// snapshot and returns the matched element's center normalized to 0-1.
// Mirrors matchA11yItem semantics: role/name/label/text/placeholder/selector.
// Returns "" action when no match — the caller treats that as "no overlay".
func resolveFindElementCoords(cu *ComputerUseHandler, sandboxID string, args map[string]any) (float64, float64, string) {
	action, _ := args["action"].(string)
	if action != "click" && action != "type" {
		return 0, 0, ""
	}

	client, err := cu.getClient(sandboxID)
	if err != nil {
		return 0, 0, ""
	}
	snapshot, err := client.GetSnapshot()
	if err != nil || snapshot == nil {
		return 0, 0, ""
	}
	vw, vh := client.LastViewportSize()
	if vw <= 0 || vh <= 0 {
		return 0, 0, ""
	}

	return matchElementCoords(snapshot.Elements, vw, vh, args, action)
}

// matchElementCoords is the pure matcher — extracted so tests can drive it
// without a live sandbox. Returns (normX, normY, action) or (0, 0, "") on
// no match. action must be "click" or "type" — anything else returns empty.
func matchElementCoords(elements []browser.LabeledElement, vw, vh int, args map[string]any, action string) (float64, float64, string) {
	if action != "click" && action != "type" {
		return 0, 0, ""
	}
	wantRole, _ := args["role"].(string)
	wantName, _ := args["name"].(string)
	wantLabel, _ := args["label"].(string)
	wantText, _ := args["text"].(string)
	wantPlaceholder, _ := args["placeholder"].(string)
	wantSelector, _ := args["selector"].(string)

	for i := range elements {
		el := &elements[i]
		if wantSelector != "" && el.Selector != wantSelector {
			continue
		}
		if wantRole != "" && !strings.EqualFold(el.Tag, wantRole) && !strings.EqualFold(el.Role, wantRole) {
			continue
		}
		// SoM labels put the accessible-name into el.Text. Match case-insensitively
		// as a substring, same as matchA11yItem.
		hay := strings.ToLower(el.Text)
		if wantName != "" && !strings.Contains(hay, strings.ToLower(wantName)) {
			continue
		}
		if wantLabel != "" && !strings.Contains(hay, strings.ToLower(wantLabel)) {
			continue
		}
		if wantText != "" && !strings.Contains(hay, strings.ToLower(wantText)) {
			continue
		}
		if wantPlaceholder != "" && !strings.Contains(hay, strings.ToLower(wantPlaceholder)) {
			continue
		}

		// Matched. Compute center and normalize.
		if el.W <= 0 || el.H <= 0 {
			return 0, 0, ""
		}
		cx := float64(el.X) + float64(el.W)/2.0
		cy := float64(el.Y) + float64(el.H)/2.0
		nx := cx / float64(vw)
		ny := cy / float64(vh)
		if nx < 0 {
			nx = 0
		}
		if nx > 1 {
			nx = 1
		}
		if ny < 0 {
			ny = 0
		}
		if ny > 1 {
			ny = 1
		}
		if action == "type" {
			return nx, ny, "type"
		}
		return nx, ny, "click"
	}
	return 0, 0, ""
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
