package handlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
)

func newChecklistRouter(t *testing.T) (*chi.Mux, *storage.Database, string) {
	t.Helper()
	logger := testutil.NopLogger()
	db := testutil.NewTempDB(t)

	projectsDir := t.TempDir()
	projectDir := filepath.Join(projectsDir, "test-proj")
	os.MkdirAll(projectDir, 0o755)

	// Register project in DB so GetProjectDir finds it
	if err := db.AddCustomProject(context.Background(), "test-proj", projectDir); err != nil {
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

// doRaw sends a raw body (no JSON marshalling) through the router. Used for
// malformed-JSON tests that must send invalid bytes.
func doRaw(t *testing.T, r http.Handler, method, path string, body []byte) (int, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w
}

// ── Get ───────────────────────────────────────────────────────

func TestChecklistGetMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)
	code := testutil.DoJSON(t, r, "GET", "/checklist", nil, nil)
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestChecklistGetEmpty(t *testing.T) {
	r, _, _ := newChecklistRouter(t)

	var resp map[string]any
	code := testutil.DoJSON(t, r, "GET", "/checklist?project=test-proj", nil, &resp)
	testutil.AssertStatus(t, code, http.StatusOK)
	tasks, ok := resp["tasks"].([]any)
	if !ok {
		t.Fatal("expected tasks array")
	}
	testutil.AssertEqual(t, len(tasks), 0)
}

func TestChecklistGetWithTasks(t *testing.T) {
	r, _, dir := newChecklistRouter(t)

	os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte("- [ ] Task one\n- [x] Task two\n"), 0o644)

	var resp map[string]any
	code := testutil.DoJSON(t, r, "GET", "/checklist?project=test-proj", nil, &resp)
	testutil.AssertStatus(t, code, http.StatusOK)
	tasks := resp["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	task0 := tasks[0].(map[string]any)
	if task0["completed"] != false {
		t.Error("task 0 should not be completed")
	}
	task1 := tasks[1].(map[string]any)
	if task1["completed"] != true {
		t.Error("task 1 should be completed")
	}
}

func TestChecklistGetInProgress(t *testing.T) {
	r, db, dir := newChecklistRouter(t)

	os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte("- [ ] First\n- [ ] Second\n"), 0o644)
	db.SetCurrentTaskIndex(context.Background(), "test-proj", 1)

	var resp map[string]any
	code := testutil.DoJSON(t, r, "GET", "/checklist?project=test-proj", nil, &resp)
	testutil.AssertStatus(t, code, http.StatusOK)
	tasks := resp["tasks"].([]any)

	task0 := tasks[0].(map[string]any)
	if task0["status"] != "pending" {
		t.Errorf("task 0 should be pending, got %v", task0["status"])
	}
	task1 := tasks[1].(map[string]any)
	if task1["status"] != "in-progress" {
		t.Errorf("task 1 should be in-progress, got %v", task1["status"])
	}
}

// ── Update ────────────────────────────────────────────────────

func TestChecklistUpdateInvalidJSON(t *testing.T) {
	r, _, _ := newChecklistRouter(t)
	code, _ := doRaw(t, r, "PUT", "/checklist", []byte("{bad"))
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestChecklistUpdateMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)
	body := map[string]any{
		"tasks": []handlers.Task{{ID: "1", Text: "Test", Completed: false}},
	}
	code := testutil.DoJSON(t, r, "PUT", "/checklist", body, nil)
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestChecklistUpdateSuccess(t *testing.T) {
	r, _, dir := newChecklistRouter(t)

	body := map[string]any{
		"project": "test-proj",
		"tasks": []handlers.Task{
			{ID: "0", Text: "Build feature", Completed: false},
			{ID: "1", Text: "Write tests", Completed: true},
		},
	}
	code := testutil.DoJSON(t, r, "PUT", "/checklist", body, nil)
	testutil.AssertStatus(t, code, http.StatusOK)

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
	code, _ := doRaw(t, r, "POST", "/checklist/merge", []byte("{bad"))
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestChecklistMergeMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)
	code := testutil.DoJSON(t, r, "POST", "/checklist/merge", map[string]string{}, nil)
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestChecklistMergeNoChecklist(t *testing.T) {
	r, _, _ := newChecklistRouter(t)
	code := testutil.DoJSON(t, r, "POST", "/checklist/merge", map[string]string{"project": "test-proj"}, nil)
	testutil.AssertStatus(t, code, http.StatusNotFound)
}

func TestChecklistMergeSuccess(t *testing.T) {
	r, db, dir := newChecklistRouter(t)

	os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte("- [ ] Implement login\n- [ ] Write docs\n"), 0o644)
	db.SetCurrentTaskIndex(context.Background(), "test-proj", 0)

	code := testutil.DoJSON(t, r, "POST", "/checklist/merge", map[string]string{"project": "test-proj"}, nil)
	testutil.AssertStatus(t, code, http.StatusOK)

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
	code, _ := doRaw(t, r, "POST", "/checklist/generate", []byte("{bad"))
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestChecklistGenerateMissingProject(t *testing.T) {
	r, _, _ := newChecklistRouter(t)
	code := testutil.DoJSON(t, r, "POST", "/checklist/generate", map[string]string{"prompt": "test"}, nil)
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestChecklistGenerateSuccess(t *testing.T) {
	r, _, dir := newChecklistRouter(t)

	// SSE endpoint: we need the recorder to inspect the Content-Type header.
	req := testutil.NewJSONRequest(t, "POST", "/checklist/generate", map[string]string{"project": "test-proj", "prompt": "Custom task"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	testutil.AssertStatus(t, w.Code, http.StatusOK)

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
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
