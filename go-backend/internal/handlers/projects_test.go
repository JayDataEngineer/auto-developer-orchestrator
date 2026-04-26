package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func newProjectRouter(t *testing.T) (*chi.Mux, *storage.Database) {
	t.Helper()
	logger := zap.NewNop()
	gitOps := git.NewGitOps(logger)

	// Temp projects dir
	projectsDir := t.TempDir()
	t.Setenv("PROJECT_ROOT", projectsDir)

	// Create a project dir inside
	os.MkdirAll(filepath.Join(projectsDir, "default-proj"), 0755)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Register default-proj as a custom project so GetProjectDir returns the temp path
	db.AddCustomProject(context.Background(), "default-proj", filepath.Join(projectsDir, "default-proj"))

	h := handlers.NewProjectHandler(db, logger, gitOps)

	r := chi.NewRouter()
	r.Get("/list", h.List)
	r.Post("/add", h.Add)
	r.Get("/status", h.GetStatus)
	r.Put("/mode", h.SetMode)

	return r, db
}

// ── List ──────────────────────────────────────────────────────

func TestProjectList(t *testing.T) {
	r, _ := newProjectRouter(t)

	req := httptest.NewRequest("GET", "/list", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	projects, ok := resp["projects"].([]interface{})
	if !ok {
		t.Fatal("expected projects array")
	}
	if len(projects) < 1 {
		t.Error("expected at least 1 project")
	}
}

// ── Add validation ────────────────────────────────────────────

func TestProjectAddInvalidJSON(t *testing.T) {
	r, _ := newProjectRouter(t)

	req := httptest.NewRequest("POST", "/add", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestProjectAddMissingName(t *testing.T) {
	r, _ := newProjectRouter(t)

	body, _ := json.Marshal(map[string]string{"path": "/tmp/some"})
	req := httptest.NewRequest("POST", "/add", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestProjectAddMissingPath(t *testing.T) {
	r, _ := newProjectRouter(t)

	body, _ := json.Marshal(map[string]string{"name": "my-proj"})
	req := httptest.NewRequest("POST", "/add", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing path, got %d", w.Code)
	}
}

func TestProjectAddNonexistentDir(t *testing.T) {
	r, _ := newProjectRouter(t)

	body, _ := json.Marshal(map[string]string{"name": "my-proj", "path": "/nonexistent/path"})
	req := httptest.NewRequest("POST", "/add", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nonexistent dir, got %d", w.Code)
	}
}

func TestProjectAddSuccess(t *testing.T) {
	r, _ := newProjectRouter(t)

	dir := t.TempDir()
	body, _ := json.Marshal(map[string]string{"name": "new-proj", "path": dir})
	req := httptest.NewRequest("POST", "/add", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != true {
		t.Error("expected success=true")
	}
}

// ── GetStatus ─────────────────────────────────────────────────

func TestProjectGetStatusMissingProject(t *testing.T) {
	r, _ := newProjectRouter(t)

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestProjectGetStatus(t *testing.T) {
	r, _ := newProjectRouter(t)

	req := httptest.NewRequest("GET", "/status?project=default-proj", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["project"] != "default-proj" {
		t.Errorf("expected project=default-proj, got %v", resp["project"])
	}
}

// ── SetMode ───────────────────────────────────────────────────

func TestProjectSetModeInvalidJSON(t *testing.T) {
	r, _ := newProjectRouter(t)

	req := httptest.NewRequest("PUT", "/mode", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestProjectSetModeMissingProject(t *testing.T) {
	r, _ := newProjectRouter(t)

	body, _ := json.Marshal(map[string]string{"mode": "auto"})
	req := httptest.NewRequest("PUT", "/mode", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestProjectSetModeAuto(t *testing.T) {
	r, _ := newProjectRouter(t)

	body, _ := json.Marshal(map[string]string{"mode": "auto", "project": "default-proj"})
	req := httptest.NewRequest("PUT", "/mode", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["is_auto_mode"] != true {
		t.Error("expected is_auto_mode=true")
	}
}

func TestProjectSetModeManual(t *testing.T) {
	r, _ := newProjectRouter(t)

	body, _ := json.Marshal(map[string]string{"mode": "manual", "project": "default-proj"})
	req := httptest.NewRequest("PUT", "/mode", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["is_auto_mode"] != false {
		t.Error("expected is_auto_mode=false for manual mode")
	}
}
