package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

// mockBrowserProvider implements BrowserProvider for testing the form tools.
type mockBrowserProvider struct {
	selectOptionFn    func(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
	uploadFileFn      func(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
	saveSessionFn     func(ctx context.Context, sandboxID, path string) (map[string]interface{}, error)
	restoreSessionFn  func(ctx context.Context, sandboxID, path string) (map[string]interface{}, error)
	navigateFn        func(ctx context.Context, sandboxID string, url string) (map[string]interface{}, error)
	findElementFn     func(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error)
	a11ySnapshotFn    func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	getCookiesFn      func(ctx context.Context, sandboxID string, urls []string) (map[string]interface{}, error)
	setCookieFn       func(ctx context.Context, sandboxID string, cookie map[string]interface{}) (map[string]interface{}, error)
	clearCookiesFn    func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	getStorageFn      func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	setStorageFn      func(ctx context.Context, sandboxID, key, value string) (map[string]interface{}, error)
	clearStorageFn    func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	browserScreenshotFn func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	evaluateJSFn      func(ctx context.Context, sandboxID, code string) (map[string]interface{}, error)
	readPageFn        func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	downloadFileFn    func(ctx context.Context, sandboxID, url, path string) (map[string]interface{}, error)
}

func (m *mockBrowserProvider) Navigate(ctx context.Context, sandboxID string, url string) (map[string]interface{}, error) {
	if m.navigateFn != nil { return m.navigateFn(ctx, sandboxID, url) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) FindElement(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	if m.findElementFn != nil { return m.findElementFn(ctx, sandboxID, req) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) FindElementVisual(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "ok", "x": 100.0, "y": 100.0}, nil
}
func (m *mockBrowserProvider) A11ySnapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	if m.a11ySnapshotFn != nil { return m.a11ySnapshotFn(ctx, sandboxID) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) GetCookies(ctx context.Context, sandboxID string, urls []string) (map[string]interface{}, error) {
	if m.getCookiesFn != nil { return m.getCookiesFn(ctx, sandboxID, urls) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) SetCookie(ctx context.Context, sandboxID string, cookie map[string]interface{}) (map[string]interface{}, error) {
	if m.setCookieFn != nil { return m.setCookieFn(ctx, sandboxID, cookie) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) ClearCookies(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	if m.clearCookiesFn != nil { return m.clearCookiesFn(ctx, sandboxID) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) GetStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	if m.getStorageFn != nil { return m.getStorageFn(ctx, sandboxID) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) SetStorage(ctx context.Context, sandboxID, key, value string) (map[string]interface{}, error) {
	if m.setStorageFn != nil { return m.setStorageFn(ctx, sandboxID, key, value) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) ClearStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	if m.clearStorageFn != nil { return m.clearStorageFn(ctx, sandboxID) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) BrowserScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	if m.browserScreenshotFn != nil { return m.browserScreenshotFn(ctx, sandboxID) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) EvaluateJS(ctx context.Context, sandboxID, code string) (map[string]interface{}, error) {
	if m.evaluateJSFn != nil { return m.evaluateJSFn(ctx, sandboxID, code) }
	return map[string]interface{}{"result": "ok"}, nil
}
func (m *mockBrowserProvider) ReadPage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	if m.readPageFn != nil { return m.readPageFn(ctx, sandboxID) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) DownloadFile(ctx context.Context, sandboxID, url, path string) (map[string]interface{}, error) {
	if m.downloadFileFn != nil { return m.downloadFileFn(ctx, sandboxID, url, path) }
	return map[string]interface{}{"status": "ok"}, nil
}
func (m *mockBrowserProvider) SelectOption(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	if m.selectOptionFn != nil { return m.selectOptionFn(ctx, sandboxID, req) }
	return map[string]interface{}{"selected": "US", "selectedIndex": float64(1)}, nil
}
func (m *mockBrowserProvider) UploadFile(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	if m.uploadFileFn != nil { return m.uploadFileFn(ctx, sandboxID, req) }
	return map[string]interface{}{"uploaded": "resume.pdf", "size": float64(1234)}, nil
}
func (m *mockBrowserProvider) SaveSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error) {
	if m.saveSessionFn != nil { return m.saveSessionFn(ctx, sandboxID, path) }
	return map[string]interface{}{"session": `{"cookies":{},"storage":{}}`, "path": path}, nil
}
func (m *mockBrowserProvider) RestoreSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error) {
	if m.restoreSessionFn != nil { return m.restoreSessionFn(ctx, sandboxID, path) }
	return map[string]interface{}{"note": "restored", "path": path}, nil
}
func (m *mockBrowserProvider) InjectFile(ctx context.Context, sandboxID, destPath, contentBase64 string) (map[string]interface{}, error) {
	return map[string]interface{}{"path": destPath, "size": len(contentBase64), "injected": true}, nil
}
func (m *mockBrowserProvider) CredentialGet(ctx context.Context, sandboxID, service string) (map[string]interface{}, error) {
	return map[string]interface{}{"service": service, "username": "test@example.com", "password": "secret123", "found": true}, nil
}
func (m *mockBrowserProvider) UserProfile(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return map[string]interface{}{"name": "Test User", "email": "test@example.com", "found": true}, nil
}

func defaultMockProvider() *mockBrowserProvider {
	return &mockBrowserProvider{}
}

func sandboxIDFn(id string) func() string {
	return func() string { return id }
}

var _ BrowserProvider = (*mockBrowserProvider)(nil)

// ── SelectOptionTool Tests ──

func TestSelectOptionTool(t *testing.T) {
	p := defaultMockProvider()
	tool := NewSelectOptionTool(p, sandboxIDFn("sb-test"))
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "select_option" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "select_option")
	}
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() must not be empty")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"selector": "select#country",
		"value":    "US",
	})
	testutil.AssertNoError(t, err)
	r := result.(map[string]interface{})
	if r["selected"] != "US" {
		t.Errorf("expected selected='US', got %v", r["selected"])
	}
}

func TestSelectOptionTool_ByText(t *testing.T) {
	p := defaultMockProvider()
	tool := NewSelectOptionTool(p, sandboxIDFn("sb-test"))

	result, err := tool.Execute(context.Background(), map[string]any{
		"selector": "select#country",
		"text":     "United States",
	})
	testutil.AssertNoError(t, err)
	r := result.(map[string]interface{})
	if r["selected"] != "US" {
		t.Errorf("expected selected='US', got %v", r["selected"])
	}
}

func TestSelectOptionTool_DelegatesToProvider(t *testing.T) {
	var capturedReq map[string]interface{}
	var capturedID string
	p := defaultMockProvider()
	p.selectOptionFn = func(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
		capturedID = sandboxID
		capturedReq = req
		return map[string]interface{}{"selected": "CA"}, nil
	}
	tool := NewSelectOptionTool(p, sandboxIDFn("sb-42"))

	result, err := tool.Execute(context.Background(), map[string]any{
		"selector": "select#province",
		"value":    "CA",
	})
	testutil.AssertNoError(t, err)
	if capturedID != "sb-42" {
		t.Errorf("expected sandboxID 'sb-42', got %q", capturedID)
	}
	if capturedReq["selector"] != "select#province" {
		t.Errorf("expected selector 'select#province', got %v", capturedReq["selector"])
	}
	if capturedReq["value"] != "CA" {
		t.Errorf("expected value 'CA', got %v", capturedReq["value"])
	}
	r := result.(map[string]interface{})
	if r["selected"] != "CA" {
		t.Errorf("expected selected='CA', got %v", r["selected"])
	}
}

func TestSelectOptionTool_ProviderError(t *testing.T) {
	p := defaultMockProvider()
	p.selectOptionFn = func(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("element not found")
	}
	tool := NewSelectOptionTool(p, sandboxIDFn("sb-test"))

	_, err := tool.Execute(context.Background(), map[string]any{
		"value": "US",
	})
	if err == nil {
		t.Fatal("expected error from provider")
	}
}

// ── UploadFileTool Tests ──

func TestUploadFileTool(t *testing.T) {
	p := defaultMockProvider()
	tool := NewUploadFileTool(p, sandboxIDFn("sb-test"))
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "upload_file" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "upload_file")
	}
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() must not be empty")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "/sandbox/workspace/resume.pdf",
	})
	testutil.AssertNoError(t, err)
	r := result.(map[string]interface{})
	if r["uploaded"] != "resume.pdf" {
		t.Errorf("expected uploaded='resume.pdf', got %v", r["uploaded"])
	}
}

func TestUploadFileTool_DelegatesToProvider(t *testing.T) {
	var capturedReq map[string]interface{}
	var capturedID string
	p := defaultMockProvider()
	p.uploadFileFn = func(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
		capturedID = sandboxID
		capturedReq = req
		return map[string]interface{}{"uploaded": "cv.pdf", "size": float64(5678)}, nil
	}
	tool := NewUploadFileTool(p, sandboxIDFn("sb-99"))

	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "/sandbox/workspace/cv.pdf",
		"selector":  "input[type=file]#resume",
	})
	testutil.AssertNoError(t, err)
	if capturedID != "sb-99" {
		t.Errorf("expected sandboxID 'sb-99', got %q", capturedID)
	}
	if capturedReq["file_path"] != "/sandbox/workspace/cv.pdf" {
		t.Errorf("expected file_path '/sandbox/workspace/cv.pdf', got %v", capturedReq["file_path"])
	}
	if capturedReq["selector"] != "input[type=file]#resume" {
		t.Errorf("expected selector 'input[type=file]#resume', got %v", capturedReq["selector"])
	}
	r := result.(map[string]interface{})
	if r["uploaded"] != "cv.pdf" {
		t.Errorf("expected uploaded='cv.pdf', got %v", r["uploaded"])
	}
}

func TestUploadFileTool_ProviderError(t *testing.T) {
	p := defaultMockProvider()
	p.uploadFileFn = func(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("permission denied")
	}
	tool := NewUploadFileTool(p, sandboxIDFn("sb-test"))

	_, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "/etc/passwd",
	})
	if err == nil {
		t.Fatal("expected error from provider")
	}
}

// ── SaveSessionTool Tests ──

func TestSaveSessionTool(t *testing.T) {
	p := defaultMockProvider()
	tool := NewSaveSessionTool(p, sandboxIDFn("sb-test"))
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "save_session" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "save_session")
	}
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() must not be empty")
	}

	result, err := tool.Execute(context.Background(), map[string]any{})
	testutil.AssertNoError(t, err)
	r := result.(map[string]interface{})
	if r["path"] == nil {
		t.Error("expected path in result")
	}
}

func TestSaveSessionTool_CustomPath(t *testing.T) {
	var capturedPath string
	p := defaultMockProvider()
	p.saveSessionFn = func(ctx context.Context, sandboxID string, path string) (map[string]interface{}, error) {
		capturedPath = path
		return map[string]interface{}{"session": "{}", "path": path}, nil
	}
	tool := NewSaveSessionTool(p, sandboxIDFn("sb-test"))

	_, err := tool.Execute(context.Background(), map[string]any{
		"path": "/custom/path/session.json",
	})
	testutil.AssertNoError(t, err)
	if capturedPath != "/custom/path/session.json" {
		t.Errorf("expected path '/custom/path/session.json', got %q", capturedPath)
	}
}

func TestSaveSessionTool_DefaultPath(t *testing.T) {
	var capturedPath string
	p := defaultMockProvider()
	p.saveSessionFn = func(ctx context.Context, sandboxID string, path string) (map[string]interface{}, error) {
		capturedPath = path
		return map[string]interface{}{}, nil
	}
	tool := NewSaveSessionTool(p, sandboxIDFn("sb-test"))

	_, err := tool.Execute(context.Background(), map[string]any{})
	testutil.AssertNoError(t, err)
	if capturedPath != "/sandbox/workspace/.browser_session.json" {
		t.Errorf("expected default path, got %q", capturedPath)
	}
}

// ── RestoreSessionTool Tests ──

func TestRestoreSessionTool(t *testing.T) {
	p := defaultMockProvider()
	tool := NewRestoreSessionTool(p, sandboxIDFn("sb-test"))
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "restore_session" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "restore_session")
	}
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() must not be empty")
	}

	result, err := tool.Execute(context.Background(), map[string]any{})
	testutil.AssertNoError(t, err)
	r := result.(map[string]interface{})
	if r["note"] == nil && r["path"] == nil {
		t.Error("expected note or path in result")
	}
}

func TestRestoreSessionTool_DelegatesToProvider(t *testing.T) {
	var capturedPath string
	var capturedID string
	p := defaultMockProvider()
	p.restoreSessionFn = func(ctx context.Context, sandboxID string, path string) (map[string]interface{}, error) {
		capturedID = sandboxID
		capturedPath = path
		return map[string]interface{}{"note": "restored"}, nil
	}
	tool := NewRestoreSessionTool(p, sandboxIDFn("sb-77"))

	_, err := tool.Execute(context.Background(), map[string]any{
		"path": "/sandbox/workspace/.browser_session.json",
	})
	testutil.AssertNoError(t, err)
	if capturedID != "sb-77" {
		t.Errorf("expected sandboxID 'sb-77', got %q", capturedID)
	}
	if capturedPath != "/sandbox/workspace/.browser_session.json" {
		t.Errorf("expected path '/sandbox/workspace/.browser_session.json', got %q", capturedPath)
	}
}

// ── Registration Tests ──

func TestRegisterBrowserTools_IncludesNewFormTools(t *testing.T) {
	p := defaultMockProvider()
	tools := RegisterBrowserTools([]core.Tool{}, p, sandboxIDFn("sb-test"))

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}

	expected := []string{
		"browse_to", "browser_screenshot", "find_element", "snapshot_a11y",
		"get_cookies", "set_cookie", "clear_cookies",
		"get_storage", "set_storage", "clear_storage",
		"evaluate_js", "read_page", "download_file",
		"select_option", "upload_file", "save_session", "restore_session",
		"inject_file", "credential_get", "user_profile",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected tool %q to be registered, but it's missing", name)
		}
	}
}

func TestRegisterBrowserTools_NilProvider(t *testing.T) {
	tools := RegisterBrowserTools([]core.Tool{}, nil, sandboxIDFn("sb-test"))
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for nil provider, got %d", len(tools))
	}
}

func TestAllBrowserProviderTools_ValidSchemas(t *testing.T) {
	p := defaultMockProvider()
	tools := RegisterBrowserTools([]core.Tool{}, p, sandboxIDFn("sb-test"))
	testutil.AssertValidSchemas(t, tools)
}

func TestAllBrowserProviderTools_NamesInOrder(t *testing.T) {
	p := defaultMockProvider()
	tools := RegisterBrowserTools([]core.Tool{}, p, sandboxIDFn("sb-test"))
	expected := []string{
		"browse_to",
		"browser_screenshot",
		"find_element",
		"find_element_visual",
		"snapshot_a11y",
		"get_cookies",
		"set_cookie",
		"clear_cookies",
		"get_storage",
		"set_storage",
		"clear_storage",
		"evaluate_js",
		"read_page",
		"download_file",
		"select_option",
		"upload_file",
		"save_session",
		"restore_session",
		"inject_file",
		"credential_get",
		"user_profile",
	}
	testutil.AssertToolNames(t, tools, expected)
}

// ── ensureBrowserReady Tests ──

type mockEnsurer struct {
	mockBrowserProvider
	ensureFn func(ctx context.Context, sandboxID string) error
}

func (m *mockEnsurer) EnsureReady(ctx context.Context, sandboxID string) error {
	if m.ensureFn != nil { return m.ensureFn(ctx, sandboxID) }
	return nil
}

func TestEnsureBrowserReady_EmptySandboxID(t *testing.T) {
	p := defaultMockProvider()
	err := ensureBrowserReady(context.Background(), p, "", "select_option")
	if err == nil {
		t.Fatal("expected error for empty sandbox ID")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestEnsureBrowserReady_EnsurerReady(t *testing.T) {
	var capturedID string
	p := &mockEnsurer{}
	p.ensureFn = func(ctx context.Context, sandboxID string) error {
		capturedID = sandboxID
		return nil
	}
	err := ensureBrowserReady(context.Background(), p, "sb-ready", "select_option")
	testutil.AssertNoError(t, err)
	if capturedID != "sb-ready" {
		t.Errorf("expected sandboxID 'sb-ready', got %q", capturedID)
	}
}

func TestEnsureBrowserReady_EnsurerError(t *testing.T) {
	p := &mockEnsurer{}
	p.ensureFn = func(ctx context.Context, sandboxID string) error {
		return errors.New("sandbox not found")
	}
	err := ensureBrowserReady(context.Background(), p, "sb-missing", "select_option")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "[select_option] browser not ready: sandbox not found" {
		t.Errorf("unexpected error: %v", err)
	}
}
