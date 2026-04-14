package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func newConfigRouter(t *testing.T) (*chi.Mux, *handlers.GitHubTokenStore) {
	t.Helper()
	logger := zap.NewNop()
	tokenStore := handlers.NewGitHubTokenStore()
	h := handlers.NewConfigHandler(logger, tokenStore, nil)

	r := chi.NewRouter()
	r.Get("/api/config/ai", h.GetAI)
	r.Put("/api/config/ai", h.SetAI)
	r.Get("/api/config/system", h.GetSystem)
	r.Put("/api/config/system", h.SetSystem)
	r.Get("/api/config/github", h.GetGitHubUser)
	r.Post("/api/config/github", h.ConnectGitHub)
	return r, tokenStore
}

// ── GetAI ─────────────────────────────────────────────────────

func TestConfigGetAI(t *testing.T) {
	r, _ := newConfigRouter(t)

	req := httptest.NewRequest("GET", "/api/config/ai", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cfg handlers.Config
	json.NewDecoder(w.Body).Decode(&cfg)
	if !cfg.AutoTask {
		t.Error("expected AutoTask=true by default")
	}
	if cfg.FullAutomationMode {
		t.Error("expected FullAutomationMode=false by default")
	}
}

// ── SetAI ─────────────────────────────────────────────────────

func TestConfigSetAI(t *testing.T) {
	r, _ := newConfigRouter(t)

	newCfg := handlers.Config{
		AutoTask:           false,
		AutoTest:           true,
		FullAutomationMode: true,
		TestGenPrompt:      "test prompt",
	}
	body, _ := json.Marshal(newCfg)

	req := httptest.NewRequest("PUT", "/api/config/ai", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the update stuck
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}

	// Get to verify
	req2 := httptest.NewRequest("GET", "/api/config/ai", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var got handlers.Config
	json.NewDecoder(w2.Body).Decode(&got)
	if got.AutoTask {
		t.Error("AutoTask should be false after update")
	}
	if !got.FullAutomationMode {
		t.Error("FullAutomationMode should be true after update")
	}
}

func TestConfigSetAIInvalidJSON(t *testing.T) {
	r, _ := newConfigRouter(t)

	req := httptest.NewRequest("PUT", "/api/config/ai", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── GetSystem ─────────────────────────────────────────────────

func TestConfigGetSystem(t *testing.T) {
	r, _ := newConfigRouter(t)

	req := httptest.NewRequest("GET", "/api/config/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cfg map[string]string
	json.NewDecoder(w.Body).Decode(&cfg)
	if cfg["projectsDir"] != "/app/projects" {
		t.Errorf("expected /app/projects, got %q", cfg["projectsDir"])
	}
}

// ── SetSystem ─────────────────────────────────────────────────

func TestConfigSetSystem(t *testing.T) {
	r, _ := newConfigRouter(t)

	body, _ := json.Marshal(map[string]string{"projectsDir": "/custom/path"})
	req := httptest.NewRequest("PUT", "/api/config/system", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestConfigSetSystemInvalidJSON(t *testing.T) {
	r, _ := newConfigRouter(t)

	req := httptest.NewRequest("PUT", "/api/config/system", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── GetGitHubUser ─────────────────────────────────────────────

func TestConfigGetGitHubUserNoToken(t *testing.T) {
	r, _ := newConfigRouter(t)

	req := httptest.NewRequest("GET", "/api/config/github", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp handlers.GitHubUserResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Connected {
		t.Error("expected connected=false without token")
	}
}

// ── ConnectGitHub ─────────────────────────────────────────────

func TestConnectGitHubInvalidJSON(t *testing.T) {
	r, _ := newConfigRouter(t)

	req := httptest.NewRequest("POST", "/api/config/github", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestConnectGitHubEmptyToken(t *testing.T) {
	r, _ := newConfigRouter(t)

	body, _ := json.Marshal(map[string]string{"token": ""})
	req := httptest.NewRequest("POST", "/api/config/github", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestConnectGitHubInvalidToken(t *testing.T) {
	r, _ := newConfigRouter(t)

	body, _ := json.Marshal(map[string]string{"token": "fake-token"})
	req := httptest.NewRequest("POST", "/api/config/github", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should return 200 with success=false (GitHub API rejects fake token)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != false {
		t.Error("expected success=false for invalid token")
	}
}
