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
	return "Select an option in a dropdown (<select>) element. Find the select by CSS selector, role, or label, then choose an option by value or visible text. Use this for any picker that exposes a fixed list of choices (region, language, category, preference, etc.)."
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
	return "Upload a file to a file input element (<input type='file'>). The file must already exist in the sandbox workspace (e.g., /sandbox/workspace/document.pdf). Use this whenever a page asks for a file you already have on hand."
}
func (t *UploadFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"selector": {"type": "string", "description": "CSS selector for the file input (default: 'input[type=file]')"},
			"file_path": {"type": "string", "description": "Path to the file in the sandbox (e.g., '/sandbox/workspace/document.pdf')"}
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

// ── Inject File Tool ──
// Writes a base64-encoded file into the sandbox filesystem.

type InjectFileTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewInjectFileTool(p BrowserProvider, sandboxID func() string) *InjectFileTool {
	return &InjectFileTool{provider: p, sandboxID: sandboxID}
}

func (t *InjectFileTool) Name() string { return "inject_file" }
func (t *InjectFileTool) Description() string {
	return "Write a file (base64-encoded) into the sandbox filesystem. Use this to materialize any file the page needs (e.g., an image, PDF, CSV, or archive) that isn't already in the sandbox. The file will be decoded and saved at the specified path."
}
func (t *InjectFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"dest_path": {"type": "string", "description": "Destination path in the sandbox (e.g., '/sandbox/workspace/document.pdf')"},
			"content_base64": {"type": "string", "description": "Base64-encoded file content"}
		},
		"required": ["dest_path", "content_base64"]
	}`)
}

func (t *InjectFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	destPath, _ := args["dest_path"].(string)
	contentB64, _ := args["content_base64"].(string)
	if destPath == "" {
		return nil, fmt.Errorf("inject_file: dest_path is required")
	}
	if contentB64 == "" {
		return nil, fmt.Errorf("inject_file: content_base64 is required")
	}
	return t.provider.InjectFile(ctx, sbID, destPath, contentB64)
}

// ── Credential Get Tool ──
// Reads credentials from environment variables for a given service.

type CredentialGetTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewCredentialGetTool(p BrowserProvider, sandboxID func() string) *CredentialGetTool {
	return &CredentialGetTool{provider: p, sandboxID: sandboxID}
}

func (t *CredentialGetTool) Name() string { return "credential_get" }
func (t *CredentialGetTool) Description() string {
	return "Get saved login credentials for a service. Looks up service-specific environment variables (e.g., <SERVICE>_USERNAME, <SERVICE>_PASSWORD). Use this to log into any service without hardcoding credentials in prompts."
}
func (t *CredentialGetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"service": {"type": "string", "description": "Service name. The tool looks up <SERVICE>_USERNAME and <SERVICE>_PASSWORD (uppercase). Case-insensitive."}
		},
		"required": ["service"]
	}`)
}

func (t *CredentialGetTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	service, _ := args["service"].(string)
	if service == "" {
		return nil, fmt.Errorf("credential_get: service is required")
	}
	return t.provider.CredentialGet(ctx, sbID, service)
}

// ── User Profile Tool ──
// Reads the user's profile information from a JSON config file.

type UserProfileTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewUserProfileTool(p BrowserProvider, sandboxID func() string) *UserProfileTool {
	return &UserProfileTool{provider: p, sandboxID: sandboxID}
}

func (t *UserProfileTool) Name() string { return "user_profile" }
func (t *UserProfileTool) Description() string {
	return "Read the user's profile information (name, email, phone, and any other fields they've chosen to save) from a JSON config file. The config is loaded from PROFILE_PATH env var, ~/.pux/user_profile.json, or the project root. Use this whenever a page asks for information about the user that they've already chosen to persist."
}
func (t *UserProfileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t *UserProfileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	return t.provider.UserProfile(ctx, sbID)
}
