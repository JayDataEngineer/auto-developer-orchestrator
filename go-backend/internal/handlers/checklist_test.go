package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func newChecklistRouter(t *testing.T) (*chi.Mux, *storage.Database, string) {
	t.Helper()
	logger := zap.NewNop()

	projectsDir := t.TempDir()
	projectDir := filepath.Join(projectsDir, "test-proj")
	os.MkdirAll(projectDir, 0755)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Register project in DB so GetProjectDir finds it
	ctx := context.Background()
	if err := db.AddCustomProject(ctx, "test-proj", projectDir); err != nil {
		t.Fatal(err)
	}

	h := handlers.NewChecklistHandler(db, logger)

	r := chi.NewRouter()
	r.Get("/checklist", h.Get)
	r.Put("/checklist", h.Update)
	r.Post("/checklist/merge", h.Merge)
	r.Post("/checklist/generate", h.GenerateChecklistStream)

	return r, db, projectDir
}

// ── Get ───────────────────────────────────────────────────────

func TestChecklistGetMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	req := httptest.NewRequest("GET", "/checklist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChecklistGetEmpty(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	req := httptest.NewRequest("GET", "/checklist?project=test-proj", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tasks, ok := resp["tasks"].([]interface{})
	if !ok {
		t.Fatal("expected tasks array")
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestChecklistGetWithTasks(t *testing.T) {
	r, _, dir := newChecklistRouter(t)

	os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte("- [ ] Task one\n- [x] Task two\n"), 0644)

	req := httptest.NewRequest("GET", "/checklist?project=test-proj", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tasks := resp["tasks"].([]interface{})
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	task0 := tasks[0].(map[string]interface{})
	if task0["completed"] != false {
		t.Error("task 0 should not be completed")
	}
	task1 := tasks[1].(map[string]interface{})
	if task1["completed"] != true {
		t.Error("task 1 should be completed")
	}
}

func TestChecklistGetInProgress(t *testing.T) {
	r, db, dir := newChecklistRouter(t)

	os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte("- [ ] First\n- [ ] Second\n"), 0644)
	db.SetCurrentTaskIndex(context.Background(), "test-proj", 1)

	req := httptest.NewRequest("GET", "/checklist?project=test-proj", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tasks := resp["tasks"].([]interface{})

	task0 := tasks[0].(map[string]interface{})
	if task0["status"] != "pending" {
		t.Errorf("task 0 should be pending, got %v", task0["status"])
	}
	task1 := tasks[1].(map[string]interface{})
	if task1["status"] != "in-progress" {
		t.Errorf("task 1 should be in-progress, got %v", task1["status"])
	}
}

// ── Update ────────────────────────────────────────────────────

func TestChecklistUpdateInvalidJSON(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	req := httptest.NewRequest("PUT", "/checklist", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChecklistUpdateMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"tasks": []handlers.Task{{ID: "1", Text: "Test", Completed: false}},
	})
	req := httptest.NewRequest("PUT", "/checklist", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChecklistUpdateSuccess(t *testing.T) {
	r, _, dir := newChecklistRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"project": "test-proj",
		"tasks": []handlers.Task{
			{ID: "0", Text: "Build feature", Completed: false},
			{ID: "1", Text: "Write tests", Completed: true},
		},
	})
	req := httptest.NewRequest("PUT", "/checklist", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	content, err := os.ReadFile(filepath.Join(dir, "TASKS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "- [ ] Build feature") {
		t.Errorf("expected task in file, got %q", string(content))
	}
	if !strings.Contains(string(content), "- [x] Write tests") {
		t.Errorf("expected completed task in file, got %q", string(content))
	}
}

// ── Merge ─────────────────────────────────────────────────────

func TestChecklistMergeInvalidJSON(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	req := httptest.NewRequest("POST", "/checklist/merge", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChecklistMergeMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("POST", "/checklist/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChecklistMergeNoChecklist(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	body, _ := json.Marshal(map[string]string{"project": "test-proj"})
	req := httptest.NewRequest("POST", "/checklist/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestChecklistMergeSuccess(t *testing.T) {
	r, db, dir := newChecklistRouter(t)

	os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte("- [ ] Implement login\n- [ ] Write docs\n"), 0644)
	db.SetCurrentTaskIndex(context.Background(), "test-proj", 0)

	body, _ := json.Marshal(map[string]string{"project": "test-proj"})
	req := httptest.NewRequest("POST", "/checklist/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	content, _ := os.ReadFile(filepath.Join(dir, "TASKS.md"))
	if !strings.Contains(string(content), "[x] Implement login") {
		t.Errorf("expected task marked complete, got %q", string(content))
	}
	if !strings.Contains(string(content), "Debug / enhance testing around: Implement login") {
		t.Errorf("expected test task added, got %q", string(content))
	}
}

// ── GenerateChecklistStream ───────────────────────────────────

func TestChecklistGenerateInvalidJSON(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	req := httptest.NewRequest("POST", "/checklist/generate", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChecklistGenerateMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	body, _ := json.Marshal(map[string]string{"prompt": "test"})
	req := httptest.NewRequest("POST", "/checklist/generate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChecklistGenerateSuccess(t *testing.T) {
	r, _, dir := newChecklistRouter(t)

	body, _ := json.Marshal(map[string]string{"project": "test-proj", "prompt": "Custom task"})
	req := httptest.NewRequest("POST", "/checklist/generate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	content, err := os.ReadFile(filepath.Join(dir, "TASKS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Custom task") {
		t.Errorf("expected custom task in file, got %q", string(content))
	}
}
