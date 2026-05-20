package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/autoconfig"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func newTestWorkerHandler(t *testing.T) (*WorkerHandler, chi.Router) {
	t.Helper()
	dir := t.TempDir()
	store := autoconfig.NewWorkerStore(dir)
	log := zap.NewNop()
	h := NewWorkerHandler(store, log)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return h, r
}

func TestWorkersListEmpty(t *testing.T) {
	_, r := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	workers := resp["workers"].([]any)
	// May include org workers from ~/.pux/orgs/ — check no kernel workers
	for _, w := range workers {
		m := w.(map[string]any)
		if m["source"] == "kernel" {
			t.Errorf("unexpected kernel worker: %v", m)
		}
	}
}

func TestWorkersCreateAndList(t *testing.T) {
	_, r := newTestWorkerHandler(t)

	// Create
	body := map[string]any{
		"name":         "test-worker",
		"persona":      "Test worker",
		"capabilities": []string{"shell"},
		"maxRounds":    10,
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", w.Code, w.Body.String())
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	workers := resp["workers"].([]any)
	// Find our created worker among potentially many org workers
	var found *map[string]any
	for _, w := range workers {
		m := w.(map[string]any)
		if m["name"] == "test-worker" && m["source"] == "kernel" {
			found = &m
			break
		}
	}
	if found == nil {
		t.Fatalf("test-worker not found among %d workers", len(workers))
	}
	if (*found)["name"] != "test-worker" {
		t.Errorf("name = %q, want test-worker", (*found)["name"])
	}
}

func TestWorkersCreateMissingName(t *testing.T) {
	_, r := newTestWorkerHandler(t)

	body := map[string]any{"persona": "No name"}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestWorkersDelete(t *testing.T) {
	dir := t.TempDir()
	store := autoconfig.NewWorkerStore(dir)
	log := zap.NewNop()
	h := NewWorkerHandler(store, log)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Create worker directly
	store.Put(context.Background(), "delete-me", map[string]any{
		"persona":      "Delete me",
		"capabilities": []string{"shell"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/delete-me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", w.Code)
	}
}

func TestWorkersCapabilities(t *testing.T) {
	// Need PROJECT_ROOT for LoadToolPackages
	setupTestEnvForWorkers(t)
	_, r := newTestWorkerHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	caps := resp["capabilities"].([]any)
	if len(caps) == 0 {
		t.Error("expected capabilities, got none")
	}
}

func TestWorkersUpdate(t *testing.T) {
	dir := t.TempDir()
	store := autoconfig.NewWorkerStore(dir)
	log := zap.NewNop()
	h := NewWorkerHandler(store, log)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Create first
	store.Put(context.Background(), "update-me", map[string]any{
		"persona":      "Original",
		"capabilities": []string{"shell"},
	})

	// Update
	body := map[string]any{
		"persona":      "Updated persona",
		"capabilities": []string{"shell", "code"},
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/update-me", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["persona"] != "Updated persona" {
		t.Errorf("persona = %q, want Updated persona", resp["persona"])
	}
}

func setupTestEnvForWorkers(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(filepath.Join(configDir, "capabilities", "shell"), 0755)
	os.WriteFile(filepath.Join(configDir, "prompt.md"), []byte("# test"), 0644)
	os.WriteFile(filepath.Join(configDir, "capabilities", "shell", "capability.yaml"), []byte("tools:\n  - bash\n"), 0644)
	os.Setenv("PROJECT_ROOT", dir)
	t.Cleanup(func() { os.Unsetenv("PROJECT_ROOT") })
}

func TestKernelWorkerProtection(t *testing.T) {
	_, r := newTestWorkerHandler(t)

	// Override kernel names AFTER handler construction (constructor calls common.KernelWorkerNames)
	kernelWorkerNames = map[string]bool{
		"browser_ops": true,
		"code_ops":    true,
		"sarah":       true,
	}
	t.Cleanup(func() { kernelWorkerNames = nil })

	t.Run("create kernel worker returns 403", func(t *testing.T) {
		body := map[string]any{
			"name":    "browser_ops",
			"persona": "Hijacked",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	t.Run("update kernel worker returns 403", func(t *testing.T) {
		body := map[string]any{"persona": "Hijacked"}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/sarah", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	t.Run("delete kernel worker returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/code_ops", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	t.Run("revert kernel worker returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/browser_ops/revert", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	t.Run("custom worker still works", func(t *testing.T) {
		body := map[string]any{
			"name":         "custom-worker",
			"persona":      "My custom worker",
			"capabilities": []string{"shell"},
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
		}
	})
}
