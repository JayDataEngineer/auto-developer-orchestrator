package desktop

import "context"

// DesktopProvider abstracts desktop/X11 automation for tool wrappers.
// Implemented by handlers.ComputerUseBridge.
type DesktopProvider interface {
	DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error)
	DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error)
	DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error)
	Resolution(ctx context.Context, sandboxID string) (map[string]interface{}, error)
}
