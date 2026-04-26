package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func newGitHubRouter(t *testing.T) (*chi.Mux, *handlers.GitHubTokenStore) {
	t.Helper()
	logger := zap.NewNop()
	t.Setenv("GITHUB_TOKEN", "") // Ensure no token
	tokenStore := handlers.NewGitHubTokenStore()
	h := handlers.NewGitHubHandler(logger, tokenStore)

	r := chi.NewRouter()
	r.Get("/repos", h.GetRepos)
	r.Get("/prs", h.GetPRs)
	r.Get("/stats", h.GetStats)
	r.Get("/branches", h.GetBranches)
	r.Get("/activity", h.GetActivity)
	return r, tokenStore
}

// ── GetRepos (no token) ───────────────────────────────────────

func TestGitHubGetReposNoToken(t *testing.T) {
	r, _ := newGitHubRouter(t)

	req := httptest.NewRequest("GET", "/repos", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["connected"] != false {
		t.Error("expected connected=false without token")
	}
}

// ── GetPRs validation ─────────────────────────────────────────

func TestGitHubGetPRsMissingParams(t *testing.T) {
	r, _ := newGitHubRouter(t)

	req := httptest.NewRequest("GET", "/prs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing owner/repo, got %d", w.Code)
	}
}

func TestGitHubGetPRsNoToken(t *testing.T) {
	r, _ := newGitHubRouter(t)

	req := httptest.NewRequest("GET", "/prs?owner=test&repo=repo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}
}

// ── GetStats validation ───────────────────────────────────────

func TestGitHubGetStatsMissingParams(t *testing.T) {
	r, _ := newGitHubRouter(t)

	req := httptest.NewRequest("GET", "/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── GetBranches validation ────────────────────────────────────

func TestGitHubGetBranchesMissingParams(t *testing.T) {
	r, _ := newGitHubRouter(t)

	req := httptest.NewRequest("GET", "/branches", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── GetActivity validation ────────────────────────────────────

func TestGitHubGetActivityMissingParams(t *testing.T) {
	r, _ := newGitHubRouter(t)

	req := httptest.NewRequest("GET", "/activity", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
