package vision

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// VisionAwareExecutor wraps a ToolExecutor and post-processes results
// that contain image data, injecting text descriptions from the vision chain.
type VisionAwareExecutor struct {
	inner  core.ToolExecutor
	chain  *FallbackChain
	logger *log.Logger
}

// NewVisionAwareExecutor wraps the given executor with automatic vision descriptions.
func NewVisionAwareExecutor(inner core.ToolExecutor, chain *FallbackChain, logger *log.Logger) *VisionAwareExecutor {
	if logger == nil {
		logger = log.Default()
	}
	return &VisionAwareExecutor{inner: inner, chain: chain, logger: logger}
}

// Execute delegates to the inner executor, then enhances results with vision if images are found.
func (e *VisionAwareExecutor) Execute(ctx context.Context, toolName string, args map[string]any) (any, error) {
	result, err := e.inner.Execute(ctx, toolName, args)
	if err != nil {
		return result, err
	}

	// No chain = no vision
	if e.chain == nil {
		return result, nil
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

	// Inject description into the result
	return injectDescription(result, desc.Text), nil
}

// promptForTool returns a context-appropriate description prompt.
func promptForTool(toolName string) string {
	if browserTools[toolName] {
		return "Describe the web page shown in this screenshot. Focus on: page title, main content, navigation, forms, buttons, and any important text or data visible. Be concise but thorough."
	}
	if desktopTools[toolName] {
		return "Describe what you see in this desktop screenshot. Focus on: open windows, applications, dialogs, and any important text or UI elements visible. Be concise."
	}
	return "Describe what you see in this image in detail."
}

// injectDescription appends a vision description to the tool result.
// For PageContext: injects into the Vision field.
// For everything else: wraps in a map with a vision_description field.
func injectDescription(result any, descText string) any {
	// Try to set Vision field on PageContext-like structs
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return result
	}

	var m map[string]any
	if err := json.Unmarshal(resultBytes, &m); err != nil {
		return result
	}

	// If it has a "vision" or "screenshot" field, it's a PageContext-like result
	if _, hasVision := m["vision"]; hasVision {
		if _, hasScreenshot := m["screenshot"]; hasScreenshot {
			// It's a PageContext — set the Vision field
			m["vision"] = descText
			return m
		}
	}

	// If it has image_b64, it's a DesktopFrame-like result
	if _, hasB64 := m["image_b64"]; hasB64 {
		m["vision_description"] = descText
		return m
	}

	// Generic: append description to the first string field we find
	for key, val := range m {
		if s, ok := val.(string); ok && len(s) > 100 {
			// Likely the main content field
			if !strings.Contains(s, "[Vision:") {
				m[key] = s + "\n\n[Vision: " + descText + "]"
			}
			return m
		}
	}

	return result
}
