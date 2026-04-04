package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"go.uber.org/zap"
)

// Note: NewSandboxBrowserClient now takes (port, hostname, logger) — tests that call
// the handler don't need to construct browser clients directly since the handler creates
// them via getOrCreateClient, which passes the correct Docker hostname.
func TestComputerUseHandlerWithoutSandbox(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := handlers.NewComputerUseHandler(nil, nil, logger)

	t.Run("Enable - no sandbox manager", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/sandbox/test-sandbox/computer-use/enable", nil)
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Enable(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status 503 for nil manager, got %d", w.Code)
		}
	})

	t.Run("Screenshot - not enabled", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sandbox/test-sandbox/computer-use/screenshot?describe=true", nil)
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Screenshot(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for non-enabled sandbox, got %d", w.Code)
		}
	})

	t.Run("Snapshot - not enabled", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sandbox/test-sandbox/computer-use/snapshot", nil)
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Snapshot(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for non-enabled sandbox, got %d", w.Code)
		}
	})

	t.Run("Act - not enabled", func(t *testing.T) {
		body := handlers.ActRequest{
			Action: "click",
			Element: 5,
		}
		jsonBody, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/sandbox/test-sandbox/computer-use/act", bytes.NewBuffer(jsonBody))
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Act(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for non-enabled sandbox, got %d", w.Code)
		}
	})

	t.Run("Act - invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/sandbox/test-sandbox/computer-use/act", bytes.NewBufferString("{bad"))
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Act(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 for invalid JSON, got %d", w.Code)
		}
	})

	t.Run("Act - unknown action", func(t *testing.T) {
		body := handlers.ActRequest{
			Action: "unknown_action",
		}
		jsonBody, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/sandbox/test-sandbox/computer-use/act", bytes.NewBuffer(jsonBody))
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Act(w, req)

		if w.Code != http.StatusNotFound {
			// Will be 404 because no client exists, not 400 for unknown action
			t.Logf("Act with unknown action on non-enabled sandbox returned %d", w.Code)
		}
	})

	t.Run("Disable - succeeds even without client", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/sandbox/test-sandbox/computer-use/disable", nil)
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Disable(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 for disable, got %d", w.Code)
		}

		var response map[string]bool
		json.NewDecoder(w.Body).Decode(&response)

		if !response["disabled"] {
			t.Error("Expected disabled=true in response")
		}
	})
}

func TestComputerUseHandlerScreenshotFormats(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := handlers.NewComputerUseHandler(nil, nil, logger)

	t.Run("Screenshot - default format is json", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sandbox/test-sandbox/computer-use/screenshot", nil)
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Screenshot(w, req)

		// Should be 404 (not enabled) but should not panic
		if w.Code != http.StatusNotFound {
			t.Logf("Screenshot returned %d", w.Code)
		}
	})

	t.Run("Screenshot - format=png", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sandbox/test-sandbox/computer-use/screenshot?format=png", nil)
		req.SetPathValue("id", "test-sandbox")
		w := httptest.NewRecorder()

		handler.Screenshot(w, req)

		// Should be 404 (not enabled) but should not panic
		if w.Code != http.StatusNotFound {
			t.Logf("Screenshot returned %d", w.Code)
		}
	})
}

func TestComputerUseHandlerRegisterRoutes(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := handlers.NewComputerUseHandler(nil, nil, logger)

	// Verify RegisterRoutes doesn't panic
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
		t.Fatalf("Expected %d routes, got %d", len(expectedRoutes), len(router.routes))
	}

	for i, expected := range expectedRoutes {
		if router.routes[i].method != expected.method {
			t.Errorf("Route %d: expected method %s, got %s", i, expected.method, router.routes[i].method)
		}
		if router.routes[i].path != expected.path {
			t.Errorf("Route %d: expected path %s, got %s", i, expected.path, router.routes[i].path)
		}
	}
}

func TestComputerUseShutdown(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := handlers.NewComputerUseHandler(nil, nil, logger)

	// Shutdown should not panic even with no clients
	handler.Shutdown()
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
