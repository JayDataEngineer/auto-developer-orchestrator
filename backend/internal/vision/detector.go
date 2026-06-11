package vision

import (
	"encoding/json"
	"strings"
)

// ImageDetection is the result of scanning a tool result for image data.
type ImageDetection struct {
	HasImage          bool
	Base64Data        string
	MIMEType          string
	AlreadyDescribed  bool // true if vision description already present
}

// browserTools produce PageContext results with screenshots.
var browserTools = map[string]bool{
	"browse_to":      true,
	"navigate":       true,
	"click_element":  true,
	"click":          true,
	"type":           true,
	"type_text":      true,
	"read_page":      true,
	"observe":        true,
	"scroll":         true,
	"scroll_page":    true,
	"search_web":     true,
	"scrape":         true,
	"find_element":   true,
	"snapshot_a11y":  true,
	"get_cookies":    true,
	"set_cookie":     true,
	"clear_cookies":  true,
	"get_storage":    true,
	"set_storage":    true,
	"clear_storage":  true,
}

// desktopTools produce DesktopFrame results with image_b64.
var desktopTools = map[string]bool{
	"desktop_screenshot": true,
	"browser_screenshot": true,
	"desktop_click":      true,
	"desktop_type":       true,
	"desktop_key":        true,
	"desktop_observe":    true,
}

// DetectImage scans a tool result for embedded image data.
// Returns nil if no image is found.
func DetectImage(toolName string, resultJSON string) *ImageDetection {
	if browserTools[toolName] {
		return detectPageContext(resultJSON)
	}
	if desktopTools[toolName] {
		return detectDesktopFrame(resultJSON)
	}
	return nil
}

// detectPageContext extracts screenshot data from a PageContext JSON.
func detectPageContext(jsonStr string) *ImageDetection {
	// Fast check: does it even look like it has screenshot data?
	if !strings.Contains(jsonStr, "screenshot") && !strings.Contains(jsonStr, "Screenshot") {
		return nil
	}

	var pc struct {
		Vision     string `json:"vision"`
		Screenshot string `json:"screenshot"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &pc); err != nil {
		return nil
	}

	if pc.Screenshot == "" {
		return nil
	}

	return &ImageDetection{
		HasImage:         true,
		Base64Data:       pc.Screenshot,
		MIMEType:         "image/png",
		AlreadyDescribed: pc.Vision != "",
	}
}

// detectDesktopFrame extracts image data from a DesktopFrame JSON.
func detectDesktopFrame(jsonStr string) *ImageDetection {
	if !strings.Contains(jsonStr, "image_b64") {
		return nil
	}

	var df struct {
		ImageB64 string `json:"image_b64"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &df); err != nil {
		return nil
	}

	if df.ImageB64 == "" {
		return nil
	}

	return &ImageDetection{
		HasImage:  true,
		Base64Data: df.ImageB64,
		MIMEType:  "image/png",
	}
}
