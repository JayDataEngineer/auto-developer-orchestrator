package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// VisualContext provides streaming frame data for vision optimization.
// When set on a VisionAwareExecutor, it enables caching: if the page hasn't
// changed since the last vision call, the cached description is reused
// instead of making another expensive API call.
type VisualContext interface {
	// LastChangeScore returns 0-1 (0 = identical, 1 = completely different).
	// Returns -1 if no frames are available.
	LastChangeScore() float64
}

// VisionResult wraps a tool result carrying extracted images for native vision delivery.
// The agent loop detects this type and routes data appropriately:
//   - OriginalResult → SSE event (frontend rendering, base64 intact)
//   - StrippedJSON → ToolResult.Content (LLM reads clean text, no base64 waste)
//   - Images → Message.Images → image_url content parts for the LLM
type VisionResult struct {
	OriginalResult any                 // raw result with base64 INTACT (for SSE → frontend)
	StrippedJSON   string              // JSON with base64 removed (for LLM tool message text)
	Images         []core.ContentImage // extracted images (for LLM image_url delivery)
}

// GetVisionData implements core.VisionCarrier.
func (vr *VisionResult) GetVisionData() (any, string, []core.ContentImage) {
	return vr.OriginalResult, vr.StrippedJSON, vr.Images
}

// VisionAwareExecutor wraps a ToolExecutor and post-processes results
// that contain image data. It has two modes:
//   - Native vision (nativeVision=true): extracts image, strips base64 from text,
//     returns VisionResult for image_url delivery to the LLM
//   - Fallback (nativeVision=false): describes image via FallbackChain,
//     injects text description into the result
type VisionAwareExecutor struct {
	inner        core.ToolExecutor
	chain        *FallbackChain
	logger       *log.Logger
	nativeVision bool // true when LLM supports image_url (Qwen/Gemini/etc)

	vc              VisualContext // optional: enables frame-based caching
	changeThreshold float64       // skip vision if score < this (default 0.05)

	mu       sync.Mutex
	lastDesc string // cached description from last vision call
}

// NewVisionAwareExecutor wraps the given executor with automatic vision processing.
// The chain may be nil (native-vision-only mode with no text fallback).
func NewVisionAwareExecutor(inner core.ToolExecutor, chain *FallbackChain, logger *log.Logger) *VisionAwareExecutor {
	if logger == nil {
		logger = log.Default()
	}
	return &VisionAwareExecutor{
		inner:           inner,
		chain:           chain,
		logger:          logger,
		changeThreshold: 0.05,
	}
}

// SetNativeVision controls whether the executor sends images via image_url (true)
// or falls back to text description via the chain (false).
func (e *VisionAwareExecutor) SetNativeVision(native bool) {
	e.nativeVision = native
}

// SetVisualContext enables frame-based vision caching. When the page hasn't
// changed since the last vision call (change score < threshold), the cached
// description is reused instead of calling the vision API again.
func (e *VisionAwareExecutor) SetVisualContext(vc VisualContext) {
	e.vc = vc
}

// SetChangeThreshold sets the change score below which vision calls are skipped.
// Default is 0.05 (5% pixel difference).
func (e *VisionAwareExecutor) SetChangeThreshold(threshold float64) {
	e.changeThreshold = threshold
}

// Execute delegates to the inner executor, then enhances results with vision if images are found.
func (e *VisionAwareExecutor) Execute(ctx context.Context, toolName string, args map[string]any) (any, error) {
	result, err := e.inner.Execute(ctx, toolName, args)
	if err != nil {
		return result, err
	}

	// Serialize result to detect images
	resultBytes, merr := json.Marshal(result)
	if merr != nil {
		return result, nil
	}
	resultStr := string(resultBytes)

	detection := DetectImage(toolName, resultStr)
	if detection == nil || !detection.HasImage || detection.AlreadyDescribed {
		return result, nil
	}

	// Native vision path: extract image, strip base64 from text, return VisionResult
	if e.nativeVision {
		return e.nativeVisionPath(result, resultStr, detection)
	}

	// Fallback path: describe via chain, inject text description
	return e.fallbackPath(ctx, result, detection, toolName)
}

// nativeVisionPath extracts the image and returns a VisionResult that routes
// data to the correct pipeline: original result (with base64) for SSE/frontend,
// stripped JSON for LLM text, and images for image_url delivery.
func (e *VisionAwareExecutor) nativeVisionPath(originalResult any, resultJSON string, detection *ImageDetection) (any, error) {
	stripped := stripBase64FromJSON(resultJSON, detection)

	dataURL := "data:" + detection.MIMEType + ";base64," + detection.Base64Data
	images := []core.ContentImage{{DataURL: dataURL}}

	e.logger.Printf("vision: native — extracted image from tool result (%d bytes base64, stripped %d chars)",
		len(detection.Base64Data), len(resultJSON)-len(stripped))

	return &VisionResult{
		OriginalResult: originalResult,
		StrippedJSON:   stripped,
		Images:         images,
	}, nil
}

// fallbackPath describes the image via the fallback chain, strips the base64
// from the result, and injects the description as a proper field.
func (e *VisionAwareExecutor) fallbackPath(ctx context.Context, result any, detection *ImageDetection, toolName string) (any, error) {
	if e.chain == nil {
		return result, nil
	}

	// Check if page hasn't changed — reuse cached description
	if e.vc != nil {
		score := e.vc.LastChangeScore()
		if score >= 0 && score < e.changeThreshold {
			e.mu.Lock()
			cached := e.lastDesc
			e.mu.Unlock()
			if cached != "" {
				e.logger.Printf("vision: skipping %s — page unchanged (score=%.4f, cached)", toolName, score)
				return stripAndDescribe(result, detection, cached)
			}
		}
	}

	// Describe the image
	desc, descErr := e.chain.Describe(ctx, ImageInput{
		Base64:   detection.Base64Data,
		MIMEType: detection.MIMEType,
		Source:   toolName,
		Prompt:   promptForTool(toolName),
	})
	if descErr != nil {
		e.logger.Printf("vision: failed to describe image from %s: %v", toolName, descErr)
		return result, nil // graceful — return without vision
	}

	// Cache the description for future reuse
	e.mu.Lock()
	e.lastDesc = desc.Text
	e.mu.Unlock()

	e.logger.Printf("vision: %s described image from %s in %dms", desc.Provider, toolName, desc.LatencyMs)

	return stripAndDescribe(result, detection, desc.Text)
}

// stripBase64FromJSON removes the image base64 data from a JSON string, replacing
// it with a placeholder. Keeps all other fields (URLs, accessibility tree, etc.).
func stripBase64FromJSON(resultJSON string, detection *ImageDetection) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(resultJSON), &m); err != nil {
		// Can't parse — return as-is with base64 truncated
		if len(resultJSON) > 500 {
			return resultJSON[:200] + fmt.Sprintf("...[truncated, %d bytes total]", len(resultJSON))
		}
		return resultJSON
	}

	// Strip known image fields
	for _, key := range []string{"screenshot", "image", "image_b64"} {
		if val, ok := m[key].(string); ok && len(val) > 200 {
			// Likely base64 image data — exact match or prefix (some tools wrap it)
			if val == detection.Base64Data || strings.HasPrefix(val, detection.Base64Data[:min(100, len(detection.Base64Data))]) {
				m[key] = "[image attached separately]"
			}
		}
	}

	stripped, err := json.Marshal(m)
	if err != nil {
		return resultJSON
	}
	return string(stripped)
}

// promptForTool returns a context-appropriate description prompt.
func promptForTool(toolName string) string {
	if browserTools[toolName] {
		return "Describe the web page shown in this screenshot. Focus on: page title, main content, navigation, forms, buttons, and any important text or data visible. Be concise but thorough."
	}
	if toolName == "desktop_observe" {
		return "Describe the desktop shown in this screenshot. Focus on: open windows, applications, dialogs, menus, buttons, and UI elements. Element coordinates are provided separately. Be concise."
	}
	if desktopTools[toolName] {
		return "Describe what you see in this desktop screenshot. Focus on: open windows, applications, dialogs, and any important text or UI elements visible. Be concise."
	}
	return "Describe what you see in this image in detail."
}

// stripAndDescribe strips base64 image data from the result and injects the
// vision description as a proper top-level field. This is the fallback path
// equivalent of nativeVisionPath — both strip base64, but fallback injects
// text instead of returning image_url parts.
func stripAndDescribe(result any, detection *ImageDetection, descText string) (any, error) {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return result, nil
	}

	var m map[string]any
	if err := json.Unmarshal(resultBytes, &m); err != nil {
		return result, nil
	}

	// Strip known image fields — the vision description replaces the raw image data
	for _, key := range []string{"screenshot", "image", "image_b64"} {
		if val, ok := m[key].(string); ok && len(val) > 200 {
			if val == detection.Base64Data || strings.HasPrefix(val, detection.Base64Data[:min(100, len(detection.Base64Data))]) {
				delete(m, key)
			}
		}
	}

	// Inject description: use existing "vision" field if present, otherwise "vision_description"
	if _, hasVision := m["vision"]; hasVision {
		m["vision"] = descText
	} else {
		m["vision_description"] = descText
	}

	return m, nil
}


