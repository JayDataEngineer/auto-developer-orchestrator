package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubConnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["token"] != "ghp_test123" {
			t.Errorf("expected token, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "github", "connect", "--token", "ghp_test123")
	if err != nil {
		t.Fatalf("github connect failed: %v", err)
	}
	if !strings.Contains(stdout, "connected") {
		t.Errorf("expected connected message, got: %s", stdout)
	}
}

func TestGitHubConnectMissingToken(t *testing.T) {
	_, _, err := runCommand(t, "http://unused:9999", "github", "connect")
	if err == nil {
		t.Fatal("expected error when --token is missing")
	}
}

func TestGitHubUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"login": "testuser", "name": "Test User",
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "github", "user")
	if err != nil {
		t.Fatalf("github user failed: %v", err)
	}
	if !strings.Contains(stdout, "testuser") {
		t.Errorf("expected testuser in output, got: %s", stdout)
	}
}

func TestGitHubRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"full_name": "user/repo1", "private": false},
			{"full_name": "user/repo2", "private": true},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "github", "repos")
	if err != nil {
		t.Fatalf("github repos failed: %v", err)
	}
	if !strings.Contains(stdout, "user/repo1") {
		t.Errorf("expected repo1 in output, got: %s", stdout)
	}
}

func TestGitHubPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("owner") != "myorg" || r.URL.Query().Get("repo") != "myrepo" {
			t.Errorf("expected owner=myorg repo=myrepo, got %s", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"number": 42, "title": "Fix bug", "state": "open"},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "github", "prs", "--owner", "myorg", "--repo", "myrepo")
	if err != nil {
		t.Fatalf("github prs failed: %v", err)
	}
	if !strings.Contains(stdout, "Fix bug") {
		t.Errorf("expected PR title in output, got: %s", stdout)
	}
}

func TestGitHubPRsMissingFlags(t *testing.T) {
	_, _, err := runCommand(t, "http://unused:9999", "github", "prs")
	if err == nil {
		t.Fatal("expected error when --owner --repo missing")
	}
}
