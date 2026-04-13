package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func setupSandboxRouter(t *testing.T) (*chi.Mux, *sandbox.Manager) {
	t.Helper()
	mgr := sandbox.NewTestManager()
	handler := NewSandboxHandler(mgr, zap.NewNop())

	r := chi.NewRouter()
	r.Route("/api/sandbox", func(r chi.Router) {
		r.Get("/", handler.ListSandboxes)
		r.Get("/{id}", handler.GetSandbox)
		r.Get("/{id}/viewer", handler.GetDesktopViewer)
		r.Post("/{id}/browser-mode", handler.EnableBrowserMode)
		r.Post("/{id}/desktop-mode", handler.EnableDesktopMode)
		r.Delete("/{id}/mode", handler.DisableMode)
	})

	return r, mgr
}

// --- GetDesktopViewer (the /viewer endpoint the frontend calls) ---

func TestGetViewerNoSandbox(t *testing.T) {
	r, _ := setupSandboxRouter(t)

	req := httptest.NewRequest("GET", "/api/sandbox/no-exist/viewer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing sandbox, got %d", w.Code)
	}
}

func TestGetViewerNoDesktopSession(t *testing.T) {
	r, mgr := setupSandboxRouter(t)
	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     "sb-1",
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeCLI,
	})

	req := httptest.NewRequest("GET", "/api/sandbox/sb-1/viewer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Sandbox exists but no desktop session → 404
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for sandbox without desktop session, got %d", w.Code)
	}
}

func TestGetViewerBrowserMode(t *testing.T) {
	r, mgr := setupSandboxRouter(t)

	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     "sb-browser",
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeBrowser,
	})
	mgr.AddTestDesktopSession("sb-browser", &sandbox.DesktopSession{
		SandboxID: "sb-browser",
		Mode:      sandbox.ModeBrowser,
		CDPPort:   19222,
		NoVNCPort: 6080,
		VNCPort:   5900,
	})

	req := httptest.NewRequest("GET", "/api/sandbox/sb-browser/viewer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// These are the fields the frontend checks to avoid "VNC viewer not available"
	novncUrl, _ := resp["novncUrl"].(string)
	if novncUrl == "" {
		t.Error("expected non-empty novncUrl — frontend shows 'VNC viewer not available' without this")
	}
	cdpUrl, _ := resp["cdpUrl"].(string)
	if cdpUrl == "" {
		t.Error("expected non-empty cdpUrl")
	}
	if resp["mode"] != "browser" {
		t.Errorf("expected mode=browser, got %v", resp["mode"])
	}
}

func TestGetViewerDesktopMode(t *testing.T) {
	r, mgr := setupSandboxRouter(t)

	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     "sb-desktop",
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeDesktop,
	})
	mgr.AddTestDesktopSession("sb-desktop", &sandbox.DesktopSession{
		SandboxID:  "sb-desktop",
		Mode:       sandbox.ModeDesktop,
		DisplayNum: 1,
		CDPPort:    9222,
		NoVNCPort:  6081,
		VNCPort:    5901,
	})

	req := httptest.NewRequest("GET", "/api/sandbox/sb-desktop/viewer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["mode"] != "desktop" {
		t.Errorf("expected mode=desktop, got %v", resp["mode"])
	}
	novncUrl, _ := resp["novncUrl"].(string)
	if novncUrl == "" {
		t.Error("expected non-empty novncUrl for desktop mode")
	}
	vncUrl, _ := resp["vncUrl"].(string)
	if vncUrl == "" {
		t.Error("expected non-empty vncUrl for desktop mode")
	}
}

// --- URL format helpers (verify frontend-compatible URLs) ---

func TestNoVNCURLFormat(t *testing.T) {
	session := &sandbox.DesktopSession{NoVNCPort: 6080}
	url := getNoVNCURL(session)
	want := "http://localhost:6080/vnc.html"
	if url != want {
		t.Errorf("getNoVNCURL = %q, want %q", url, want)
	}
}

func TestCDPURLFormat(t *testing.T) {
	session := &sandbox.DesktopSession{CDPPort: 19222}
	url := getCDPURL(session)
	want := "http://localhost:19222"
	if url != want {
		t.Errorf("getCDPURL = %q, want %q", url, want)
	}
}

func TestVNCURLFormat(t *testing.T) {
	session := &sandbox.DesktopSession{VNCPort: 5901}
	url := getVNCURL(session)
	want := "vnc://localhost:5901"
	if url != want {
		t.Errorf("getVNCURL = %q, want %q", url, want)
	}
}

// --- Sandbox CRUD ---

func TestListSandboxesEmpty(t *testing.T) {
	r, _ := setupSandboxRouter(t)

	req := httptest.NewRequest("GET", "/api/sandbox/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListSandboxesWithData(t *testing.T) {
	r, mgr := setupSandboxRouter(t)
	mgr.AddTestSandbox(&sandbox.Sandbox{ID: "sb-a", Status: sandbox.StatusRunning})
	mgr.AddTestSandbox(&sandbox.Sandbox{ID: "sb-b", Status: sandbox.StatusRunning})

	req := httptest.NewRequest("GET", "/api/sandbox/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var list []*sandbox.Sandbox
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Errorf("expected 2 sandboxes, got %d", len(list))
	}
}

func TestGetSandboxNotFound(t *testing.T) {
	r, _ := setupSandboxRouter(t)

	req := httptest.NewRequest("GET", "/api/sandbox/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetSandboxFound(t *testing.T) {
	r, mgr := setupSandboxRouter(t)
	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     "sb-1",
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeCLI,
	})

	req := httptest.NewRequest("GET", "/api/sandbox/sb-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var s sandbox.Sandbox
	json.Unmarshal(w.Body.Bytes(), &s)
	if s.ID != "sb-1" {
		t.Errorf("expected ID sb-1, got %s", s.ID)
	}
}

func TestDisableModeNoSandbox(t *testing.T) {
	r, _ := setupSandboxRouter(t)

	req := httptest.NewRequest("DELETE", "/api/sandbox/nope/mode", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestDisableModeCLISandbox(t *testing.T) {
	r, mgr := setupSandboxRouter(t)
	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     "sb-cli",
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeCLI,
	})

	req := httptest.NewRequest("DELETE", "/api/sandbox/sb-cli/mode", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Disabling CLI mode (no-op) should succeed
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

// --- E2E flow: the exact sequence the frontend performs ---

func TestE2EViewerFlow(t *testing.T) {
	// Simulates the full frontend flow:
	// 1. Frontend generates sandboxId from project name
	// 2. Fetches GET /api/sandbox/{id}/viewer → 404 (no session yet)
	// 3. Calls POST /api/sandbox/{id}/computer-use/enable (creates sandbox + session)
	// 4. Fetches GET /api/sandbox/{id}/viewer → 200 with novncUrl
	//
	// This test verifies step 4 produces a response that prevents
	// "VNC viewer not available" on the frontend.

	r, mgr := setupSandboxRouter(t)
	sandboxID := "sandbox-myproject-default"

	// Step 1: Viewer returns 404 (no sandbox)
	req := httptest.NewRequest("GET", "/api/sandbox/"+sandboxID+"/viewer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("step 1: expected 404, got %d", w.Code)
	}

	// Step 2: Simulate what EnableBrowserMode does — add sandbox + session
	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     sandboxID,
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeBrowser,
	})
	mgr.AddTestDesktopSession(sandboxID, &sandbox.DesktopSession{
		SandboxID: sandboxID,
		Mode:      sandbox.ModeBrowser,
		CDPPort:   19222,
		NoVNCPort: 6080,
		VNCPort:   5900,
		ViewerURL: "/sandbox/" + sandboxID + "/viewer",
		IsActive:  true,
	})

	// Step 3: Viewer now returns 200 with all required fields
	req = httptest.NewRequest("GET", "/api/sandbox/"+sandboxID+"/viewer", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("step 3: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// The frontend checks: session?.novncUrl
	// If this is missing/empty → "VNC viewer not available"
	novncUrl, _ := resp["novncUrl"].(string)
	if novncUrl == "" {
		t.Error("E2E FAILURE: novncUrl is empty — frontend would show 'VNC viewer not available'")
	}
	cdpUrl, _ := resp["cdpUrl"].(string)
	if cdpUrl == "" {
		t.Error("E2E FAILURE: cdpUrl is empty — frontend can't show Chrome CDP")
	}
	viewerUrl, _ := resp["viewerUrl"].(string)
	if viewerUrl == "" {
		t.Error("E2E FAILURE: viewerUrl is empty — frontend can't open desktop viewer popup")
	}
	if resp["mode"] != "browser" {
		t.Errorf("E2E FAILURE: expected mode=browser, got %v", resp["mode"])
	}

	t.Logf("Viewer response OK: novncUrl=%s cdpUrl=%s viewerUrl=%s mode=%s",
		novncUrl, cdpUrl, viewerUrl, resp["mode"])
}
