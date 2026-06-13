package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/go-chi/chi/v5"
)

func setupJobsRouter(t *testing.T, apiKey string) (*chi.Mux, *scheduler.Scheduler) {
	t.Helper()
	logger := testutil.NopLogger()
	db := testutil.NewTempDB(t)

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

// submitJob POSTs a job body and returns (jobID, statusCode). Fails the test
// if the status is not 202 (use a lower helper for non-202 cases).
func submitJobAccepted(t *testing.T, r http.Handler, body map[string]any) string {
	t.Helper()
	var resp map[string]any
	code := testutil.DoJSON(t, r, "POST", "/api/jobs/", body, &resp)
	testutil.AssertStatus(t, code, http.StatusAccepted)
	id, ok := resp["jobId"].(string)
	if !ok || id == "" {
		t.Fatalf("expected jobId in response, got %v", resp)
	}
	return id
}

func TestJobsSubmitAsync(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	var resp map[string]any
	code := testutil.DoJSON(t, r, "POST", "/api/jobs/", map[string]any{
		"task": "List files in the project", "project": ".",
	}, &resp)
	testutil.AssertStatus(t, code, http.StatusAccepted)
	testutil.AssertEqual(t, resp["success"], true)
	if resp["jobId"] == "" {
		t.Error("Expected jobId in response")
	}
	testutil.AssertEqual(t, resp["status"], "running")
	if pollUrl, ok := resp["pollUrl"].(string); !ok || pollUrl == "" {
		t.Error("Expected pollUrl in response")
	}
}

func TestJobsSubmitValidation(t *testing.T) {
	r, _ := setupJobsRouter(t, "")
	// Missing required "task" field.
	code := testutil.DoJSON(t, r, "POST", "/api/jobs/", map[string]any{"project": "."}, nil)
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

// Table-driven: variations of a valid submit that should all be accepted.
func TestJobsSubmitAcceptedVariants(t *testing.T) {
	r, _ := setupJobsRouter(t, "")

	tests := []struct {
		name string
		body map[string]any
	}{
		{"AutoName", map[string]any{
			"task": "This is a very long task description that should be truncated to forty characters",
			"project": "."}},
		{"CustomName", map[string]any{"task": "Named task", "project": ".", "name": "my-custom-job"}},
		{"FullSandbox", map[string]any{"task": "Sandbox task", "project": ".", "full_sandbox": true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			submitJobAccepted(t, r, tc.body)
		})
	}
}

func TestJobsSubmitAndGetStatus(t *testing.T) {
	r, _ := setupJobsRouter(t, "")
	jobID := submitJobAccepted(t, r, map[string]any{"task": "Do something", "project": "."})

	// Wait a moment for the async trigger to execute.
	time.Sleep(200 * time.Millisecond)

	var statusResp map[string]any
	code := testutil.DoJSON(t, r, "GET", "/api/jobs/"+jobID, nil, &statusResp)
	testutil.AssertStatus(t, code, http.StatusOK)
	if got := statusResp["jobId"]; got != jobID {
		t.Errorf("jobId = %v, want %s", got, jobID)
	}
	// Job should have completed (success or still running).
	status, _ := statusResp["status"].(string)
	switch status {
	case "disabled", "idle", "running", "success", "error":
		// ok
	default:
		t.Errorf("Unexpected status: %v", status)
	}
}

func TestJobsGetStatusNotFound(t *testing.T) {
	r, _ := setupJobsRouter(t, "")
	code := testutil.DoJSON(t, r, "GET", "/api/jobs/nonexistent", nil, nil)
	testutil.AssertStatus(t, code, http.StatusNotFound)
}

func TestJobsDeleteOneShot(t *testing.T) {
	r, _ := setupJobsRouter(t, "")
	jobID := submitJobAccepted(t, r, map[string]any{"task": "Temporary task", "project": "."})

	code := testutil.DoJSON(t, r, "DELETE", "/api/jobs/"+jobID, nil, nil)
	testutil.AssertStatus(t, code, http.StatusOK)

	// Verify it's gone.
	code = testutil.DoJSON(t, r, "GET", "/api/jobs/"+jobID, nil, nil)
	testutil.AssertStatus(t, code, http.StatusNotFound)
}

func TestJobsDeleteRejectsNonOneShot(t *testing.T) {
	r, s := setupJobsRouter(t, "")

	// Create a regular (non-one-shot) manual job directly via scheduler.
	job := &scheduler.Job{
		Name: "Regular Job", Project: ".", Message: "test",
		Schedule: scheduler.ScheduleManual, Enabled: false,
	}
	if err := s.CreateJob(job); err != nil {
		t.Fatal(err)
	}

	code := testutil.DoJSON(t, r, "DELETE", "/api/jobs/"+job.ID, nil, nil)
	testutil.AssertStatus(t, code, http.StatusForbidden)
}

// Table-driven: auth scenarios differ only in key placement and expected code.
func TestJobsAuth(t *testing.T) {
	body := map[string]any{"task": "Auth test", "project": "."}

	tests := []struct {
		name       string
		apiKey     string
		headerKey  string // value for X-API-Key ("" = omit)
		queryKey   string // value for ?api_key= ("" = omit)
		wantStatus int
	}{
		{"RequiredNoKey", "secret-key-123", "", "", http.StatusUnauthorized},
		{"WrongKey", "secret-key-123", "wrong-key", "", http.StatusUnauthorized},
		{"ValidHeader", "secret-key-123", "secret-key-123", "", http.StatusAccepted},
		{"ValidQuery", "secret-key-123", "", "secret-key-123", http.StatusAccepted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupJobsRouter(t, tc.apiKey)

			path := "/api/jobs/"
			if tc.queryKey != "" {
				path += "?api_key=" + tc.queryKey
			}
			req := testutil.NewJSONRequest(t, "POST", path, body)
			if tc.headerKey != "" {
				req.Header.Set("X-API-Key", tc.headerKey)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			testutil.AssertStatus(t, w.Code, tc.wantStatus)
		})
	}
}

func TestJobsInvalidJSON(t *testing.T) {
	r, _ := setupJobsRouter(t, "")
	code, _ := doRaw(t, r, "POST", "/api/jobs/", []byte("{bad json"))
	testutil.AssertStatus(t, code, http.StatusBadRequest)
}

func TestJobsCleanupOneShots(t *testing.T) {
	r, s := setupJobsRouter(t, "")
	submitJobAccepted(t, r, map[string]any{"task": "Cleanup test", "project": "."})

	// Wait for the job to finish before cleanup.
	time.Sleep(200 * time.Millisecond)

	// Very short maxAge cleans completed one-shot jobs.
	if cleaned := s.CleanupExpiredOneShots(1 * time.Nanosecond); cleaned < 1 {
		t.Errorf("Expected at least 1 cleaned job, got %d", cleaned)
	}
}
