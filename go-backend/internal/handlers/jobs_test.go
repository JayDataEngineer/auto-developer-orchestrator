package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func setupJobsRouter(t *testing.T, apiKey string) (*chi.Mux, *scheduler.Scheduler) {
	t.Helper()
	logger := zap.NewNop()
	dir := t.TempDir()
	db, err := storage.NewDatabase(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	s := scheduler.NewScheduler(db, func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge, sandboxOnly bool) (string, error) {
		return "test output from agent", nil
	}, logger)

	handler := handlers.NewJobsHandler(s, "http://localhost:3847", logger)
	if apiKey != "" {
		handler.SetAPIKey(apiKey)
	}

	r := chi.NewRouter()
	r.Route("/api/jobs", handler.RegisterRoutes)

	return r, s
}

func TestJobsSubmitAsync(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	body := map[string]interface{}{
		"task":    "List files in the project",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if resp["jobId"] == "" {
		t.Error("Expected jobId in response")
	}
	if resp["status"] != "running" {
		t.Errorf("Expected status 'running', got %v", resp["status"])
	}
	pollUrl, ok := resp["pollUrl"].(string)
	if !ok || pollUrl == "" {
		t.Error("Expected pollUrl in response")
	}
}

func TestJobsSubmitValidation(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	// Missing task
	body := map[string]interface{}{
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing task, got %d", w.Code)
	}
}

func TestJobsSubmitAutoName(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	body := map[string]interface{}{
		"task":    "This is a very long task description that should be truncated to forty characters",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d: %s", w.Code, w.Body.String())
	}
	// Just verify it works — the auto-name is internal
}

func TestJobsSubmitAndGetStatus(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	// Submit
	body := map[string]interface{}{
		"task":    "Do something",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var submitResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&submitResp)
	jobID := submitResp["jobId"].(string)

	// Wait a moment for the async trigger to execute
	time.Sleep(200 * time.Millisecond)

	// Get status
	req = httptest.NewRequest("GET", "/api/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for status, got %d: %s", w.Code, w.Body.String())
	}

	var statusResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&statusResp)

	if statusResp["jobId"] != jobID {
		t.Errorf("Expected jobId %s, got %v", jobID, statusResp["jobId"])
	}
	// Job should have completed (success or still running)
	status, _ := statusResp["status"].(string)
	if status != "disabled" && status != "idle" && status != "running" && status != "success" && status != "error" {
		t.Errorf("Unexpected status: %v", status)
	}
}

func TestJobsGetStatusNotFound(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	req := httptest.NewRequest("GET", "/api/jobs/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestJobsDeleteOneShot(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	// Submit a one-shot job
	body := map[string]interface{}{
		"task":    "Temporary task",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var submitResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&submitResp)
	jobID := submitResp["jobId"].(string)

	// Delete it
	req = httptest.NewRequest("DELETE", "/api/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for delete, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's gone
	req = httptest.NewRequest("GET", "/api/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d", w.Code)
	}
}

func TestJobsDeleteRejectsNonOneShot(t *testing.T) {
	r, s := setupJobsRouter(t, "")

	// Create a regular (non-one-shot) manual job directly via scheduler
	job := &scheduler.Job{
		Name:        "Regular Job",
		Description: "", // NOT "oneshot"
		Project:     ".",
		Message:     "test",
		Schedule:    scheduler.ScheduleManual,
		Enabled:     false,
	}
	if err := s.CreateJob(job); err != nil {
		t.Fatal(err)
	}

	// Try to delete via jobs endpoint
	req := httptest.NewRequest("DELETE", "/api/jobs/"+job.ID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for non-one-shot delete, got %d", w.Code)
	}
}

func TestJobsAuthRequired(t *testing.T) {
	r, _ := setupJobsRouter(t, "secret-key-123")

	body := map[string]interface{}{
		"task":    "Auth test",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	// Request without API key
	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 without API key, got %d", w.Code)
	}
}

func TestJobsAuthValid(t *testing.T) {
	r, _ := setupJobsRouter(t, "secret-key-123")

	body := map[string]interface{}{
		"task":    "Auth test",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	// Request with correct API key in header
	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret-key-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202 with valid API key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJobsAuthViaQuery(t *testing.T) {
	r, _ := setupJobsRouter(t, "secret-key-123")

	body := map[string]interface{}{
		"task":    "Auth test",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	// Request with API key in query param
	req := httptest.NewRequest("POST", "/api/jobs/?api_key=secret-key-123", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected 202 with valid query API key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJobsAuthWrongKey(t *testing.T) {
	r, _ := setupJobsRouter(t, "secret-key-123")

	body := map[string]interface{}{
		"task":    "Auth test",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 with wrong API key, got %d", w.Code)
	}
}

func TestJobsInvalidJSON(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestJobsCleanupOneShots(t *testing.T) {
	r, s := setupJobsRouter(t, "")

	// Submit a one-shot job
	body := map[string]interface{}{
		"task":    "Cleanup test",
		"project": ".",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d", w.Code)
	}

	// Cleanup with 0 duration (everything should be cleaned)
	// But first wait for the job to finish
	time.Sleep(200 * time.Millisecond)

	// Cleanup with a very short maxAge (should clean completed jobs)
	cleaned := s.CleanupExpiredOneShots(1 * time.Nanosecond)
	if cleaned < 1 {
		t.Errorf("Expected at least 1 cleaned job, got %d", cleaned)
	}
}

func TestJobsCustomName(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	body := map[string]interface{}{
		"task":    "Named task",
		"project": ".",
		"name":    "my-custom-job",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJobsFullSandbox(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	body := map[string]interface{}{
		"task":         "Sandbox task",
		"project":      ".",
		"full_sandbox": true,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/jobs/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d: %s", w.Code, w.Body.String())
	}
}
