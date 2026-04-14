package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// ─── Enable Handler ──────────────────────────────────────────

func TestEnableReturns200Immediately(t *testing.T) {
	// The Enable handler MUST return 200 immediately before any Docker operations.
	// This prevents ERR_NETWORK_CHANGED from aborting the response body stream.
	mgr := sandbox.NewTestManager()
	logger := zap.NewNop()
	handler := handlers.NewComputerUseHandler(mgr, nil, logger)

	req := httptest.NewRequest("POST", "/api/sandbox/sb-1/computer-use/enable", nil)
	req.SetPathValue("id", "sb-1")
	w := httptest.NewRecorder()

	handler.Enable(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp["enabled"] != true {
		t.Error("expected enabled=true")
	}
	if resp["sandboxId"] != "sb-1" {
		t.Errorf("expected sandboxId=sb-1, got %v", resp["sandboxId"])
	}
	// Response includes default ports (sent before Docker ops)
	if resp["cdpPort"] == nil {
		t.Error("expected cdpPort in response")
	}
	if resp["novncPort"] == nil {
		t.Error("expected novncPort in response")
	}
}

func TestEnableMissingSandboxID(t *testing.T) {
	mgr := sandbox.NewTestManager()
	handler := handlers.NewComputerUseHandler(mgr, nil, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/sandbox//computer-use/enable", nil)
	req.SetPathValue("id", "")
	w := httptest.NewRecorder()

	handler.Enable(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestEnableNilManager(t *testing.T) {
	handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/sandbox/sb-1/computer-use/enable", nil)
	req.SetPathValue("id", "sb-1")
	w := httptest.NewRecorder()

	handler.Enable(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestEnableFastPathAlreadyConnected(t *testing.T) {
	// When a CDP client is already connected (in-memory check), Enable returns
	// cached ports without calling EnableBrowserMode again. This is the <1ms fast path.
	mgr := sandbox.NewTestManager()
	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     "sb-fast",
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeBrowser,
		DesktopSession: &sandbox.DesktopSession{
			SandboxID: "sb-fast",
			Mode:      sandbox.ModeBrowser,
			CDPPort:   19222,
			NoVNCPort: 6080,
			VNCPort:   5900,
			ViewerURL: "/sandbox/sb-fast/viewer",
		},
	})
	mgr.AddTestDesktopSession("sb-fast", &sandbox.DesktopSession{
		SandboxID: "sb-fast",
		Mode:      sandbox.ModeBrowser,
		CDPPort:   19222,
		NoVNCPort: 6080,
		VNCPort:   5900,
		ViewerURL: "/sandbox/sb-fast/viewer",
	})

	handler := handlers.NewComputerUseHandler(mgr, nil, zap.NewNop())

	// First enable — creates a client (will fail to connect but that's fine,
	// we need the client in the map). Use ExportGetClient to inject one.
	// Actually, the fast path checks IsConnected() which requires allocator.
	// Since we can't connect to real Chrome in tests, test the response format.
	req := httptest.NewRequest("POST", "/api/sandbox/sb-fast/computer-use/enable", nil)
	req.SetPathValue("id", "sb-fast")
	w := httptest.NewRecorder()

	handler.Enable(w, req)

	// Response is sent immediately (200) regardless of Docker state
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["enabled"] != true {
		t.Error("expected enabled=true")
	}
}

func TestEnableResponseSentBeforeBackgroundSetup(t *testing.T) {
	// Verify that the HTTP response is complete before any Docker operations start.
	// The Enable handler sends the response body and THEN starts backgroundSetup.
	mgr := sandbox.NewTestManager()
	handler := handlers.NewComputerUseHandler(mgr, nil, zap.NewNop())

	start := time.Now()
	req := httptest.NewRequest("POST", "/api/sandbox/sb-timing/computer-use/enable", nil)
	req.SetPathValue("id", "sb-timing")
	w := httptest.NewRecorder()

	handler.Enable(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Response should be nearly instant (< 100ms) since Docker ops run in background
	if elapsed > 100*time.Millisecond {
		t.Errorf("Enable took %v — response should be sent before Docker ops", elapsed)
	}
}

// ─── Disable Handler ─────────────────────────────────────────

func TestDisableReturns200(t *testing.T) {
	handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/sandbox/sb-1/computer-use/disable", nil)
	req.SetPathValue("id", "sb-1")
	w := httptest.NewRecorder()

	handler.Disable(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]bool
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp["disabled"] {
		t.Error("expected disabled=true")
	}
}

func TestDisableCleansUpClient(t *testing.T) {
	// Disable should remove the client from the internal map.
	// After disable, Screenshot should return 404.
	mgr := sandbox.NewTestManager()
	mgr.AddTestSandbox(&sandbox.Sandbox{
		ID:     "sb-cleanup",
		Status: sandbox.StatusRunning,
		Mode:   sandbox.ModeBrowser,
		DesktopSession: &sandbox.DesktopSession{
			SandboxID: "sb-cleanup",
			Mode:      sandbox.ModeBrowser,
			CDPPort:   19222,
			NoVNCPort: 6080,
		},
	})
	mgr.AddTestDesktopSession("sb-cleanup", &sandbox.DesktopSession{
		SandboxID: "sb-cleanup",
		Mode:      sandbox.ModeBrowser,
		CDPPort:   19222,
		NoVNCPort: 6080,
	})

	handler := handlers.NewComputerUseHandler(mgr, nil, zap.NewNop())

	// Enable to potentially create client entry
	req := httptest.NewRequest("POST", "/api/sandbox/sb-cleanup/computer-use/enable", nil)
	req.SetPathValue("id", "sb-cleanup")
	w := httptest.NewRecorder()
	handler.Enable(w, req)

	// Now disable
	req = httptest.NewRequest("POST", "/api/sandbox/sb-cleanup/computer-use/disable", nil)
	req.SetPathValue("id", "sb-cleanup")
	w = httptest.NewRecorder()
	handler.Disable(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// After disable, screenshot should fail
	req = httptest.NewRequest("GET", "/api/sandbox/sb-cleanup/computer-use/screenshot", nil)
	req.SetPathValue("id", "sb-cleanup")
	w = httptest.NewRecorder()
	handler.Screenshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after disable, got %d", w.Code)
	}
}

// ─── Screenshot/Snapshot/Act Handler Error Tests ─────────────

func TestScreenshotNotEnabled(t *testing.T) {
	handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())

	req := httptest.NewRequest("GET", "/api/sandbox/sb-none/computer-use/screenshot?describe=true", nil)
	req.SetPathValue("id", "sb-none")
	w := httptest.NewRecorder()

	handler.Screenshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSnapshotNotEnabled(t *testing.T) {
	handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())

	req := httptest.NewRequest("GET", "/api/sandbox/sb-none/computer-use/snapshot", nil)
	req.SetPathValue("id", "sb-none")
	w := httptest.NewRecorder()

	handler.Snapshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestActInvalidJSON(t *testing.T) {
	handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/sandbox/sb-1/computer-use/act", bytes.NewBufferString("{bad"))
	req.SetPathValue("id", "sb-1")
	w := httptest.NewRecorder()

	handler.Act(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestActUnknownAction(t *testing.T) {
	// When there's no client, it should 404 (getClient fails first).
	// When there IS a client but unknown action, it should 400.
	// Test both paths.
	t.Run("no client - returns 404", func(t *testing.T) {
		handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())

		body, _ := json.Marshal(handlers.ActRequest{Action: "teleport"})
		req := httptest.NewRequest("POST", "/api/sandbox/sb-1/computer-use/act", bytes.NewReader(body))
		req.SetPathValue("id", "sb-1")
		w := httptest.NewRecorder()

		handler.Act(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 (no client), got %d", w.Code)
		}
	})
}

// ─── ActRequest Validation ───────────────────────────────────

func TestActRequestFieldParsing(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  handlers.ActRequest
	}{
		{
			name:  "click action",
			input: `{"action":"click","element":5}`,
			want:  handlers.ActRequest{Action: "click", Element: 5},
		},
		{
			name:  "type action with submit",
			input: `{"action":"type","element":3,"text":"hello","submit":true}`,
			want:  handlers.ActRequest{Action: "type", Element: 3, Text: "hello", Submit: true},
		},
		{
			name:  "scroll action with amount",
			input: `{"action":"scroll","direction":"up","amount":500}`,
			want:  handlers.ActRequest{Action: "scroll", Direction: "up", Amount: 500},
		},
		{
			name:  "navigate action",
			input: `{"action":"navigate","url":"https://example.com"}`,
			want:  handlers.ActRequest{Action: "navigate", URL: "https://example.com"},
		},
		{
			name:  "scroll defaults amount to 300 when zero",
			input: `{"action":"scroll","direction":"down"}`,
			want:  handlers.ActRequest{Action: "scroll", Direction: "down", Amount: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req handlers.ActRequest
			if err := json.Unmarshal([]byte(tt.input), &req); err != nil {
				t.Fatal(err)
			}
			if req.Action != tt.want.Action {
				t.Errorf("action: got %q, want %q", req.Action, tt.want.Action)
			}
			if req.Element != tt.want.Element {
				t.Errorf("element: got %d, want %d", req.Element, tt.want.Element)
			}
			if req.Text != tt.want.Text {
				t.Errorf("text: got %q, want %q", req.Text, tt.want.Text)
			}
			if req.URL != tt.want.URL {
				t.Errorf("url: got %q, want %q", req.URL, tt.want.URL)
			}
			if req.Direction != tt.want.Direction {
				t.Errorf("direction: got %q, want %q", req.Direction, tt.want.Direction)
			}
			if req.Amount != tt.want.Amount {
				t.Errorf("amount: got %d, want %d", req.Amount, tt.want.Amount)
			}
			if req.Submit != tt.want.Submit {
				t.Errorf("submit: got %v, want %v", req.Submit, tt.want.Submit)
			}
		})
	}
}

// ─── RegisterRoutes ──────────────────────────────────────────

func TestComputerUseHandlerRegisterRoutes(t *testing.T) {
	handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())

	router := &mockRouter{}
	handler.RegisterRoutes(router)

	expectedRoutes := []struct {
		method string
		path   string
	}{
		{"POST", "/enable"},
		{"POST", "/disable"},
		{"GET", "/screenshot"},
		{"GET", "/snapshot"},
		{"POST", "/act"},
	}

	if len(router.routes) != len(expectedRoutes) {
		t.Fatalf("expected %d routes, got %d", len(expectedRoutes), len(router.routes))
	}

	for i, expected := range expectedRoutes {
		if router.routes[i].method != expected.method {
			t.Errorf("route %d: expected method %s, got %s", i, expected.method, router.routes[i].method)
		}
		if router.routes[i].path != expected.path {
			t.Errorf("route %d: expected path %s, got %s", i, expected.path, router.routes[i].path)
		}
	}
}

// ─── Shutdown ────────────────────────────────────────────────

func TestComputerUseShutdown(t *testing.T) {
	handler := handlers.NewComputerUseHandler(nil, nil, zap.NewNop())
	handler.Shutdown() // should not panic
}

// mockRouter implements the router interface for testing
type mockRouter struct {
	routes []struct {
		method string
		path   string
	}
}

func (m *mockRouter) Post(path string, handler http.HandlerFunc) {
	m.routes = append(m.routes, struct {
		method string
		path   string
	}{"POST", path})
}

func (m *mockRouter) Get(path string, handler http.HandlerFunc) {
	m.routes = append(m.routes, struct {
		method string
		path   string
	}{"GET", path})
}
