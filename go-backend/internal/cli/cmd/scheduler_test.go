package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSchedulerList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scheduler/" {
			t.Errorf("expected /api/scheduler/, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jobs": []map[string]interface{}{
				{"id": "abc123456789", "name": "test-job", "project": "test-repo", "scheduleType": "manual", "enabled": true},
			},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "scheduler", "list")
	if err != nil {
		t.Fatalf("scheduler list failed: %v", err)
	}
	if !strings.Contains(stdout, "test-job") {
		t.Errorf("expected test-job in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "test-repo") {
		t.Errorf("expected test-repo in output, got: %s", stdout)
	}
}

func TestSchedulerListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jobs": []map[string]interface{}{
				{"id": "abc", "name": "my-job"},
			},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "-o", "json", "scheduler", "list")
	if err != nil {
		t.Fatalf("scheduler list json failed: %v", err)
	}
	if !strings.Contains(stdout, `"id"`) {
		t.Errorf("expected JSON output, got: %s", stdout)
	}
}

func TestSchedulerListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jobs": []interface{}{},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "scheduler", "list")
	if err != nil {
		t.Fatalf("scheduler list empty failed: %v", err)
	}
	if !strings.Contains(stdout, "No scheduled jobs") {
		t.Errorf("expected empty message, got: %s", stdout)
	}
}

func TestSchedulerCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/scheduler/" {
			t.Errorf("expected /api/scheduler/, got %s", r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "my-job" {
			t.Errorf("expected name=my-job, got %v", body["name"])
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "new123", "name": "my-job",
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "scheduler", "create",
		"--name", "my-job", "--message", "test msg")
	if err != nil {
		t.Fatalf("scheduler create failed: %v", err)
	}
	if !strings.Contains(stdout, "Created job") {
		t.Errorf("expected creation message, got: %s", stdout)
	}
}

func TestSchedulerCreateMissingFlags(t *testing.T) {
	_, _, err := runCommand(t, "http://unused:9999", "scheduler", "create")
	if err == nil {
		t.Fatal("expected error when --name is missing")
	}
}

func TestSchedulerGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scheduler/job123" {
			t.Errorf("expected /api/scheduler/job123, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "job123", "name": "test", "project": "repo",
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "scheduler", "get", "job123")
	if err != nil {
		t.Fatalf("scheduler get failed: %v", err)
	}
	if !strings.Contains(stdout, "job123") {
		t.Errorf("expected job123 in output, got: %s", stdout)
	}
}

func TestSchedulerDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "scheduler", "delete", "job123")
	if err != nil {
		t.Fatalf("scheduler delete failed: %v", err)
	}
	if !strings.Contains(stdout, "Deleted") {
		t.Errorf("expected delete message, got: %s", stdout)
	}
}

func TestSchedulerTrigger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/scheduler/job123/trigger" {
			t.Errorf("expected POST /api/scheduler/job123/trigger, got %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "scheduler", "trigger", "job123")
	if err != nil {
		t.Fatalf("scheduler trigger failed: %v", err)
	}
	if !strings.Contains(stdout, "Triggered") {
		t.Errorf("expected triggered message, got: %s", stdout)
	}
}

func TestSchedulerRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scheduler/runs" {
			t.Errorf("expected /api/scheduler/runs, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "run12345678", "jobId": "job12345678", "status": "completed", "startedAt": "2025-01-01"},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "scheduler", "runs")
	if err != nil {
		t.Fatalf("scheduler runs failed: %v", err)
	}
	if !strings.Contains(stdout, "completed") {
		t.Errorf("expected status in output, got: %s", stdout)
	}
}

func TestSchedulerRunsByJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scheduler/job123/runs" {
			t.Errorf("expected /api/scheduler/job123/runs, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]interface{}{})
	}))
	defer srv.Close()

	_, _, err := runCommand(t, srv.URL, "scheduler", "runs", "job123")
	if err != nil {
		t.Fatalf("scheduler runs by job failed: %v", err)
	}
}
