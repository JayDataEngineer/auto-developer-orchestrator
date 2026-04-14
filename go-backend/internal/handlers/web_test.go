package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func newWebRouter(t *testing.T) (*chi.Mux, *browser.BrowserClient) {
	t.Helper()
	logger := zap.NewNop()
	bc, _ := browser.NewBrowserClient("ws://localhost:3000", logger)
	vc := browser.NewVisionClient("", "", nil)
	h := handlers.NewWebHandler(bc, vc, logger)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, bc
}

// ── GetScreenshot ─────────────────────────────────────────────

func TestWebGetScreenshotMissingSession(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("GET", "/screenshot", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing sessionId, got %d", w.Code)
	}
}

func TestWebGetScreenshotNoSession(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("GET", "/screenshot?sessionId=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing session, got %d", w.Code)
	}
}

// ── GetState ──────────────────────────────────────────────────

func TestWebGetStateMissingSession(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("GET", "/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing sessionId, got %d", w.Code)
	}
}

func TestWebGetStateNoSession(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("GET", "/state?sessionId=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing session, got %d", w.Code)
	}
}

// ── Navigate ──────────────────────────────────────────────────

func TestWebNavigateInvalidJSON(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("POST", "/navigate", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWebNavigateNoSession(t *testing.T) {
	r, _ := newWebRouter(t)

	body, _ := json.Marshal(map[string]string{"url": "http://example.com", "sessionId": "nonexistent"})
	req := httptest.NewRequest("POST", "/navigate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing session, got %d", w.Code)
	}
}

// ── Click ─────────────────────────────────────────────────────

func TestWebClickInvalidJSON(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("POST", "/click", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── Type ──────────────────────────────────────────────────────

func TestWebTypeInvalidJSON(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("POST", "/type", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── Scroll ────────────────────────────────────────────────────

func TestWebScrollInvalidJSON(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("POST", "/scroll", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── CreateSession ─────────────────────────────────────────────

func TestWebCreateSessionNoBrowser(t *testing.T) {
	r, _ := newWebRouter(t)

	// BrowserClient with no actual connection - CreateSession will fail
	// because there's no remote Browserless instance
	body, _ := json.Marshal(map[string]string{"sessionId": "test-session"})
	req := httptest.NewRequest("POST", "/session", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Will fail since no actual WS connection, but should not panic
	// Status may be 500 (failed to create) or 200 if idempotent
}

// ── CloseSession ──────────────────────────────────────────────

func TestWebCloseSessionInvalidJSON(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("DELETE", "/session", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── DescribePage ──────────────────────────────────────────────

func TestWebDescribePageInvalidJSON(t *testing.T) {
	r, _ := newWebRouter(t)

	req := httptest.NewRequest("POST", "/describe", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
