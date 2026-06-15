package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// ── Select Option Tool ──
// Selects an option in a <select> dropdown by value or visible text.

type SelectOptionTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewSelectOptionTool(p BrowserProvider, sandboxID func() string) *SelectOptionTool {
	return &SelectOptionTool{provider: p, sandboxID: sandboxID}
}

func (t *SelectOptionTool) Name() string { return "select_option" }
func (t *SelectOptionTool) Description() string {
	return "Select an option in a dropdown (<select>) element. Find the select by CSS selector, role, or label, then choose an option by value or visible text. Use this for country/state/language pickers on forms."
}
func (t *SelectOptionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"selector": {"type": "string", "description": "CSS selector for the <select> element (e.g., 'select#country')"},
			"role": {"type": "string", "description": "ARIA role to find the select (e.g., 'combobox', 'listbox')"},
			"label": {"type": "string", "description": "Label text to find the select element"},
			"value": {"type": "string", "description": "Option value to select (the value attribute, e.g., 'US')"},
			"text": {"type": "string", "description": "Visible option text to select (e.g., 'United States'). Used if value is not provided."}
		},
		"required": ["value"]
	}`)
}

func (t *SelectOptionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "select_option"); err != nil {
		return nil, err
	}
	return t.provider.SelectOption(ctx, sbID, args)
}

// ── Upload File Tool ──
// Uploads a file to an <input type="file"> element.

type UploadFileTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewUploadFileTool(p BrowserProvider, sandboxID func() string) *UploadFileTool {
	return &UploadFileTool{provider: p, sandboxID: sandboxID}
}

func (t *UploadFileTool) Name() string { return "upload_file" }
func (t *UploadFileTool) Description() string {
	return "Upload a file to a file input element (<input type='file'>). The file must exist in the sandbox workspace (e.g., /sandbox/workspace/resume.pdf). Use this for resume/CV uploads on job application forms."
}
func (t *UploadFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"selector": {"type": "string", "description": "CSS selector for the file input (default: 'input[type=file]')"},
			"file_path": {"type": "string", "description": "Path to the file in the sandbox (e.g., '/sandbox/workspace/resume.pdf')"}
		},
		"required": ["file_path"]
	}`)
}

func (t *UploadFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "upload_file"); err != nil {
		return nil, err
	}
	return t.provider.UploadFile(ctx, sbID, args)
}

// ── Save Session Tool ──
// Saves cookies + localStorage to a JSON file for persistence between sessions.

type SaveSessionTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewSaveSessionTool(p BrowserProvider, sandboxID func() string) *SaveSessionTool {
	return &SaveSessionTool{provider: p, sandboxID: sandboxID}
}

func (t *SaveSessionTool) Name() string { return "save_session" }
func (t *SaveSessionTool) Description() string {
	return "Save the current browser session (cookies + localStorage) to a file. Use this after logging into a website so you can restore the session later without re-authenticating."
}
func (t *SaveSessionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path to save the session file (default: '/sandbox/workspace/.browser_session.json')"}
		}
	}`)
}

func (t *SaveSessionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "save_session"); err != nil {
		return nil, err
	}
	path := "/sandbox/workspace/.browser_session.json"
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}
	return t.provider.SaveSession(ctx, sbID, path)
}

// ── Restore Session Tool ──

type RestoreSessionTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewRestoreSessionTool(p BrowserProvider, sandboxID func() string) *RestoreSessionTool {
	return &RestoreSessionTool{provider: p, sandboxID: sandboxID}
}

func (t *RestoreSessionTool) Name() string { return "restore_session" }
func (t *RestoreSessionTool) Description() string {
	return "Restore a previously saved browser session (cookies + localStorage). Use this at the start of a browser task to resume an authenticated session."
}
func (t *RestoreSessionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path to the saved session file (default: '/sandbox/workspace/.browser_session.json')"}
		}
	}`)
}

func (t *RestoreSessionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "restore_session"); err != nil {
		return nil, err
	}
	path := "/sandbox/workspace/.browser_session.json"
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}
	return t.provider.RestoreSession(ctx, sbID, path)
}

// formatError wraps an error with context about which tool failed.
func formatError(tool string, err error) error {
	return fmt.Errorf("%s failed: %w", tool, err)
}
