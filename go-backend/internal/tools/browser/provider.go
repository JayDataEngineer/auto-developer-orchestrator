package browser

import "context"

// BrowserProvider abstracts browser automation for tool wrappers.
// Implemented by handlers.ComputerUseBridge.
type BrowserProvider interface {
	// Navigate navigates the browser to a URL and returns page info.
	Navigate(ctx context.Context, sandboxID string, url string) (map[string]interface{}, error)
	// FindElement finds an element by semantic criteria and optionally performs an action.
	FindElement(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
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
}
