package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config/ai" {
			t.Errorf("expected /api/config/ai, got %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"autoTask": true, "autoTest": false,
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "config", "ai")
	if err != nil {
		t.Fatalf("config ai failed: %v", err)
	}
	if !strings.Contains(stdout, "autoTask") {
		t.Errorf("expected autoTask in output, got: %s", stdout)
	}
}

func TestConfigAISet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["autoTask"] != "true" {
			t.Errorf("expected autoTask=true, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "config", "ai", "--set", "autoTask=true")
	if err != nil {
		t.Fatalf("config ai set failed: %v", err)
	}
	if !strings.Contains(stdout, "Updated") {
		t.Errorf("expected Updated, got: %s", stdout)
	}
}

func TestConfigModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"mainModel": map[string]string{"modelId": "gemma-4-26b"},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "config", "models")
	if err != nil {
		t.Fatalf("config models failed: %v", err)
	}
	if !strings.Contains(stdout, "gemma-4-26b") {
		t.Errorf("expected gemma model in output, got: %s", stdout)
	}
}

func TestConfigModelsSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "config", "models", "--set", "mainModel=gemma-4")
	if err != nil {
		t.Fatalf("config models set failed: %v", err)
	}
	if !strings.Contains(stdout, "Updated") {
		t.Errorf("expected Updated, got: %s", stdout)
	}
}

func TestConfigSystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"projectsDir": "/app/projects",
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "config", "system")
	if err != nil {
		t.Fatalf("config system failed: %v", err)
	}
	if !strings.Contains(stdout, "projectsDir") {
		t.Errorf("expected projectsDir in output, got: %s", stdout)
	}
}
