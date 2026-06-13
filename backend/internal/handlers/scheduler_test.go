package handlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/go-chi/chi/v5"
)

// setupSchedulerRouter builds an isolated scheduler router backed by a temp DB.
func setupSchedulerRouter(t *testing.T) (*chi.Mux, *scheduler.Scheduler) {
	t.Helper()
	db := testutil.NewTempDB(t)
	logger := testutil.NopLogger()

	s := scheduler.NewScheduler(db, func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge, sandboxOnly bool) (string, error) {
		return "test output", nil
	}, logger)

	handler := handlers.NewSchedulerHandler(s, logger)

	r := chi.NewRouter()
	r.Route("/api/scheduler", handler.RegisterRoutes)

	return r, s
}

// validJobBody returns a job payload that passes validation.
func validJobBody(name string) map[string]any {
	if name == "" {
		name = "Job"
	}
	return map[string]any{
		"name":         name,
		"project":      "test-project",
		"message":      "Run tests",
		"scheduleType": "every",
		"everySeconds": 300,
		"enabled":      true,
	}
}

func TestSchedulerCreateJob(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	var resp map[string]any
	code := testutil.DoJSON(t, r, "POST", "/api/scheduler/", validJobBody("Test Job"), &resp)
	testutil.AssertStatus(t, code, http.StatusCreated)
	testutil.AssertEqual(t, resp["success"], true)
	job, ok := resp["job"].(map[string]any)
	if !ok {
		t.Fatal("Expected job in response")
	}
	testutil.AssertEqual(t, job["name"], "Test Job")
}

func TestSchedulerListJobsEmpty(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	var resp map[string]any
	code := testutil.DoJSON(t, r, "GET", "/api/scheduler/", nil, &resp)
	testutil.AssertStatus(t, code, http.StatusOK)
	jobs, ok := resp["jobs"].([]any)
	if !ok {
		t.Fatal("Expected jobs array in response")
	}
	testutil.AssertEqual(t, len(jobs), 0)
}

func TestSchedulerCreateAndListJobs(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	code := testutil.DoJSON(t, r, "POST", "/api/scheduler/", validJobBody("List Test"), nil)
	testutil.AssertStatus(t, code, http.StatusCreated)

	var resp map[string]any
	code = testutil.DoJSON(t, r, "GET", "/api/scheduler/", nil, &resp)
	testutil.AssertStatus(t, code, http.StatusOK)
	jobs := resp["jobs"].([]any)
	testutil.AssertEqual(t, len(jobs), 1)
}

// Table-driven: all the not-found endpoints return 404.
func TestSchedulerNotFoundEndpoints(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GetJobNotFound", "GET", "/api/scheduler/nonexistent"},
		{"DeleteJobNotFound", "DELETE", "/api/scheduler/nonexistent"},
		{"TriggerJobNotFound", "POST", "/api/scheduler/nonexistent/trigger"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := testutil.DoJSON(t, r, tc.method, tc.path, nil, nil)
			testutil.AssertStatus(t, code, http.StatusNotFound)
		})
	}
}

func TestSchedulerCreateGetDelete(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	var createResp map[string]any
	code := testutil.DoJSON(t, r, "POST", "/api/scheduler/", validJobBody("CRUD Test"), &createResp)
	testutil.AssertStatus(t, code, http.StatusCreated)
	jobID := createResp["job"].(map[string]any)["id"].(string)

	// Get
	code = testutil.DoJSON(t, r, "GET", "/api/scheduler/"+jobID, nil, nil)
	testutil.AssertStatus(t, code, http.StatusOK)

	// Delete
	code = testutil.DoJSON(t, r, "DELETE", "/api/scheduler/"+jobID, nil, nil)
	testutil.AssertStatus(t, code, http.StatusOK)

	// Get again — should be 404
	code = testutil.DoJSON(t, r, "GET", "/api/scheduler/"+jobID, nil, nil)
	testutil.AssertStatus(t, code, http.StatusNotFound)
}

func TestSchedulerCronJob(t *testing.T) {
	r, _ := setupSchedulerRouter(t)
	body := map[string]any{
		"name": "Cron Job", "project": "p", "message": "tick",
		"scheduleType": "cron", "cronExpr": "0 */5 * * * *", "enabled": true,
	}
	code := testutil.DoJSON(t, r, "POST", "/api/scheduler/", body, nil)
	testutil.AssertStatus(t, code, http.StatusCreated)
}

// Table-driven: validation failures all return 400.
func TestSchedulerValidationFailures(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{"MissingRequiredFields", map[string]any{"scheduleType": "every"}},
		{"InvalidCronExpr", map[string]any{
			"name": "Bad Cron", "project": "p", "message": "m",
			"scheduleType": "cron", "cronExpr": "invalid", "enabled": true,
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := testutil.DoJSON(t, r, "POST", "/api/scheduler/", tc.body, nil)
			testutil.AssertStatus(t, code, http.StatusBadRequest)
		})
	}
}

// Invalid JSON body (malformed, not just missing fields) → 400.
func TestSchedulerInvalidJSON(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	req := httptest.NewRequest("POST", "/api/scheduler/", bytes.NewBufferString("{bad json"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	testutil.AssertStatus(t, w.Code, http.StatusBadRequest)
}

func TestSchedulerUpdateJob(t *testing.T) {
	r, _ := setupSchedulerRouter(t)

	var createResp map[string]any
	code := testutil.DoJSON(t, r, "POST", "/api/scheduler/", validJobBody("Original"), &createResp)
	testutil.AssertStatus(t, code, http.StatusCreated)
	jobID := createResp["job"].(map[string]any)["id"].(string)

	updateBody := map[string]any{
		"name": "Updated", "project": "p", "message": "new message",
		"scheduleType": "every", "everySeconds": 120,
	}
	var updateResp map[string]any
	code = testutil.DoJSON(t, r, "PUT", "/api/scheduler/"+jobID, updateBody, &updateResp)
	testutil.AssertStatus(t, code, http.StatusOK)
	updatedJob := updateResp["job"].(map[string]any)
	testutil.AssertEqual(t, updatedJob["name"], "Updated")
}

func TestSchedulerListRuns(t *testing.T) {
	r, _ := setupSchedulerRouter(t)
	code := testutil.DoJSON(t, r, "GET", "/api/scheduler/some-job/runs?limit=10", nil, nil)
	testutil.AssertStatus(t, code, http.StatusOK)
}

func TestSchedulerListAllRuns(t *testing.T) {
	r, _ := setupSchedulerRouter(t)
	code := testutil.DoJSON(t, r, "GET", "/api/scheduler/runs?limit=10", nil, nil)
	testutil.AssertStatus(t, code, http.StatusOK)
}
