package browser

import "context"

// BrowserProvider abstracts browser automation for tool wrappers.
// Implemented by handlers.ComputerUseBridge.
type BrowserProvider interface {
	// Navigate navigates the browser to a URL and returns page info.
	Navigate(ctx context.Context, sandboxID string, url string) (map[string]interface{}, error)
	// FindElement finds an element by semantic criteria and optionally performs an action.
	FindElement(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
	// FindElementVisual locates an element via the MCP ground_ui vision tool
	// for canvas/WebGL/obfuscated interfaces where DOM tools fail.
	// Returns ground_ui's coordinate payload; optionally clicks the located coords.
	FindElementVisual(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
	// A11ySnapshot returns the accessibility tree.
	A11ySnapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	// GetCookies returns browser cookies.
	GetCookies(ctx context.Context, sandboxID string, urls []string) (map[string]interface{}, error)
	// SetCookie sets a browser cookie.
	SetCookie(ctx context.Context, sandboxID string, cookie map[string]interface{}) (map[string]interface{}, error)
	// ClearCookies clears all browser cookies.
	ClearCookies(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	// GetStorage returns localStorage data.
	GetStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	// SetStorage sets a localStorage item.
	SetStorage(ctx context.Context, sandboxID string, key, value string) (map[string]interface{}, error)
	// ClearStorage clears localStorage.
	ClearStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	// BrowserScreenshot takes a screenshot of the current browser page via CDP.
	BrowserScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	// EvaluateJS executes JavaScript in the browser and returns the result.
	EvaluateJS(ctx context.Context, sandboxID, code string) (map[string]interface{}, error)
	// ReadPage extracts structured content from the current page.
	ReadPage(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	// DownloadFile downloads a file to the sandbox via curl.
	DownloadFile(ctx context.Context, sandboxID, url, path string) (map[string]interface{}, error)
	// SelectOption selects an option in a <select> dropdown.
	SelectOption(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
	// UploadFile uploads a file to an <input type="file"> element.
	UploadFile(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
	// SaveSession saves cookies + localStorage to a file for persistence.
	SaveSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error)
	// RestoreSession restores cookies + localStorage from a saved file.
	RestoreSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error)
	// InjectFile writes a file (base64-encoded content) into the sandbox filesystem.
	// Use this to materialize any file the page needs that isn't already in the sandbox.
	InjectFile(ctx context.Context, sandboxID, destPath, contentBase64 string) (map[string]interface{}, error)
	// CredentialGet reads credentials for a service from environment variables.
	// Looks up {SERVICE}_USERNAME and {SERVICE}_PASSWORD (or _EMAIL and _PASS).
	// Use this to get login credentials without hardcoding them in prompts.
	CredentialGet(ctx context.Context, sandboxID, service string) (map[string]interface{}, error)
	// UserProfile reads the user's profile information from a JSON config file.
	// The profile is read from PROFILE_PATH, ~/.pux/user_profile.json, or PROJECT_ROOT/user_profile.json.
	UserProfile(ctx context.Context, sandboxID string) (map[string]interface{}, error)
}

// SandboxEnsurer is an optional interface that BrowserProvider implementations
// can satisfy to auto-provision the browser sandbox on first tool call.
// ComputerUseBridge implements this via EnsureReady().
type SandboxEnsurer interface {
	EnsureReady(ctx context.Context, sandboxID string) error
}
