package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSandboxList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sandbox/" {
			t.Errorf("expected /api/sandbox/, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "test-repo", "status": "running"},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "sandbox", "list")
	if err != nil {
		t.Fatalf("sandbox list failed: %v", err)
	}
	if !strings.Contains(stdout, "test-repo") {
		t.Errorf("expected test-repo in output, got: %s", stdout)
	}
}

func TestSandboxListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]interface{}{})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "sandbox", "list")
	if err != nil {
		t.Fatalf("sandbox list empty failed: %v", err)
	}
	if !strings.Contains(stdout, "No sandboxes") {
		t.Errorf("expected empty message, got: %s", stdout)
	}
}

func TestSandboxCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["id"] != "my-box" {
			t.Errorf("expected id=my-box, got %v", body)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "my-box", "status": "creating",
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "sandbox", "create", "--id", "my-box")
	if err != nil {
		t.Fatalf("sandbox create failed: %v", err)
	}
	if !strings.Contains(stdout, "Created sandbox") {
		t.Errorf("expected creation message, got: %s", stdout)
	}
}

func TestSandboxExec(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sandbox/test-repo/exec" {
			t.Errorf("expected /api/sandbox/test-repo/exec, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"stdout": "file1.txt\nfile2.txt\n", "stderr": "", "exitCode": 0,
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "sandbox", "exec", "test-repo", "--", "ls")
	if err != nil {
		t.Fatalf("sandbox exec failed: %v", err)
	}
	if !strings.Contains(stdout, "file1.txt") {
		t.Errorf("expected file listing, got: %s", stdout)
	}
}

func TestSandboxExecExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"stdout": "", "stderr": "command not found", "exitCode": 127,
		})
	}))
	defer srv.Close()

	_, _, err := runCommand(t, srv.URL, "sandbox", "exec", "test-repo", "--", "badcmd")
	if err == nil {
		t.Fatal("expected error for non-zero exit code")
	}
	if !strings.Contains(err.Error(), "127") {
		t.Errorf("expected exit code 127 in error, got: %v", err)
	}
}

func TestSandboxDestroy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "sandbox", "destroy", "test-repo")
	if err != nil {
		t.Fatalf("sandbox destroy failed: %v", err)
	}
	if !strings.Contains(stdout, "Destroyed") {
		t.Errorf("expected destroy message, got: %s", stdout)
	}
}

func TestSandboxReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "sandbox", "ready", "test-repo")
	if err != nil {
		t.Fatalf("sandbox ready failed: %v", err)
	}
	if !strings.Contains(stdout, "Ready") {
		t.Errorf("expected Ready, got: %s", stdout)
	}
}
