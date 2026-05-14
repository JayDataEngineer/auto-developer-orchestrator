package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func setupSchedulerRouter(t *testing.T) (*chi.Mux, *scheduler.Scheduler) {
	t.Helper()
	logger := zap.NewNop()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "scheduler.json")

	s := scheduler.NewScheduler(storePath, func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge bool) (string, error) {
		return "test output", nil
	}, logger)

	handler := handlers.NewSchedulerHandler(s, logger)

	r := chi.NewRouter()
	r.Route("/api/scheduler", handler.RegisterRoutes)

	return r, s
}

func TestSchedulerCreateJob(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	body := map[string]interface{}{
		"name":         "Test Job",
		"project":      "test-project",
		"message":      "Run tests",
		"scheduleType": "every",
		"everySeconds": 300,
		"enabled":      true,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBuffer(jsonBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	job, ok := resp["job"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected job in response")
	}
	if job["name"] != "Test Job" {
		t.Errorf("Expected name 'Test Job', got %v", job["name"])
	}
}

func TestSchedulerCreateJobValidation(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	// Missing required fields
	body := map[string]interface{}{
		"scheduleType": "every",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBuffer(jsonBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid job, got %d", w.Code)
	}
}

func TestSchedulerListJobsEmpty(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("GET", "/api/scheduler/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	jobs, ok := resp["jobs"].([]interface{})
	if !ok {
		t.Fatal("Expected jobs array in response")
	}
	if len(jobs) != 0 {
		t.Errorf("Expected 0 jobs, got %d", len(jobs))
	}
}

func TestSchedulerCreateAndListJobs(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	// Create a job
	body := map[string]interface{}{
		"name":         "List Test",
		"project":      "p",
		"message":      "m",
		"scheduleType": "every",
		"everySeconds": 60,
		"enabled":      true,
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBuffer(jsonBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Create failed: %d %s", w.Code, w.Body.String())
	}

	// List jobs
	req = httptest.NewRequest("GET", "/api/scheduler/", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	jobs := resp["jobs"].([]interface{})
	if len(jobs) != 1 {
		t.Errorf("Expected 1 job, got %d", len(jobs))
	}
}

func TestSchedulerGetJobNotFound(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("GET", "/api/scheduler/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestSchedulerDeleteJobNotFound(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("DELETE", "/api/scheduler/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestSchedulerTriggerJobNotFound(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("POST", "/api/scheduler/nonexistent/trigger", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestSchedulerCreateGetDelete(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	// Create
	body := map[string]interface{}{
		"name":         "CRUD Test",
		"project":      "p",
		"message":      "m",
		"scheduleType": "every",
		"everySeconds": 60,
		"enabled":      true,
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBuffer(jsonBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var createResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&createResp)
	job := createResp["job"].(map[string]interface{})
	jobID := job["id"].(string)

	// Get
	req = httptest.NewRequest("GET", "/api/scheduler/"+jobID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Get job expected 200, got %d", w.Code)
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/api/scheduler/"+jobID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Delete job expected 200, got %d", w.Code)
	}

	// Get again - should be 404
	req = httptest.NewRequest("GET", "/api/scheduler/"+jobID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Get deleted job expected 404, got %d", w.Code)
	}
}

func TestSchedulerCronJob(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	body := map[string]interface{}{
		"name":         "Cron Job",
		"project":      "p",
		"message":      "tick",
		"scheduleType": "cron",
		"cronExpr":     "0 */5 * * * *",
		"enabled":      true,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBuffer(jsonBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201 for cron job, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSchedulerInvalidCronExpr(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	body := map[string]interface{}{
		"name":         "Bad Cron",
		"project":      "p",
		"message":      "m",
		"scheduleType": "cron",
		"cronExpr":     "invalid",
		"enabled":      true,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBuffer(jsonBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid cron, got %d", w.Code)
	}
}

func TestSchedulerUpdateJob(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	// Create
	body := map[string]interface{}{
		"name":         "Original",
		"project":      "p",
		"message":      "m",
		"scheduleType": "every",
		"everySeconds": 60,
		"enabled":      true,
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBuffer(jsonBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var createResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&createResp)
	jobID := createResp["job"].(map[string]interface{})["id"].(string)

	// Update
	updateBody := map[string]interface{}{
		"name":         "Updated",
		"project":      "p",
		"message":      "new message",
		"scheduleType": "every",
		"everySeconds": 120,
	}
	jsonBody, _ = json.Marshal(updateBody)
	req = httptest.NewRequest("PUT", "/api/scheduler/"+jobID, bytes.NewBuffer(jsonBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Update expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updateResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&updateResp)
	updatedJob := updateResp["job"].(map[string]interface{})
	if updatedJob["name"] != "Updated" {
		t.Errorf("Expected name 'Updated', got %v", updatedJob["name"])
	}
}

func TestSchedulerListRuns(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("GET", "/api/scheduler/some-job/runs?limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for runs, got %d", w.Code)
	}
}

func TestSchedulerListAllRuns(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("GET", "/api/scheduler/runs?limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for all runs, got %d", w.Code)
	}
}

func TestSchedulerInvalidJSON(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBufferString("{bad json"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}
