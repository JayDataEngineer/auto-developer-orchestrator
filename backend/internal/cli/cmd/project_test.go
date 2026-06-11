package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects" {
			t.Errorf("expected /api/projects, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"projects": []string{"test-repo", "my-project"},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "project", "list")
	if err != nil {
		t.Fatalf("project list failed: %v", err)
	}
	if !strings.Contains(stdout, "test-repo") {
		t.Errorf("expected test-repo in output, got: %s", stdout)
	}
}

func TestProjectListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"projects": []string{"test-repo"},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "-o", "json", "project", "list")
	if err != nil {
		t.Fatalf("project list json failed: %v", err)
	}
	if !strings.Contains(stdout, `"test-repo"`) {
		t.Errorf("expected JSON with test-repo, got: %s", stdout)
	}
}

func TestProjectAdd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/projects/add" {
			t.Errorf("expected /api/projects/add, got %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "new-proj" {
			t.Errorf("expected name=new-proj, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "project", "add", "new-proj")
	if err != nil {
		t.Fatalf("project add failed: %v", err)
	}
	if !strings.Contains(stdout, "Added project") {
		t.Errorf("expected add message, got: %s", stdout)
	}
}

func TestProjectStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"branch": "main", "modified": 2, "staged": 1,
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "project", "status")
	if err != nil {
		t.Fatalf("project status failed: %v", err)
	}
	if !strings.Contains(stdout, "main") {
		t.Errorf("expected branch in output, got: %s", stdout)
	}
}

func TestProjectBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/branch" {
			t.Errorf("expected /api/branch, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"branch": "feature/test"})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "project", "branch")
	if err != nil {
		t.Fatalf("project branch failed: %v", err)
	}
	if !strings.Contains(stdout, "feature/test") {
		t.Errorf("expected feature/test, got: %s", stdout)
	}
}

func TestProjectCheckout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/branch/checkout" {
			t.Errorf("expected /api/branch/checkout, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "project", "checkout", "--branch", "main")
	if err != nil {
		t.Fatalf("project checkout failed: %v", err)
	}
	if !strings.Contains(stdout, "Checked out") {
		t.Errorf("expected checkout message, got: %s", stdout)
	}
}
