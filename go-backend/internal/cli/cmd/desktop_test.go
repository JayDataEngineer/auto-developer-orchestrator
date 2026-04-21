package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopEnable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/sandbox/test-repo/desktop-mode" {
			t.Errorf("expected desktop-mode path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]bool{"enabled": true})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "desktop", "enable", "test-repo")
	if err != nil {
		t.Fatalf("desktop enable failed: %v", err)
	}
	if !strings.Contains(stdout, "enabled") {
		t.Errorf("expected enabled message, got: %s", stdout)
	}
}

func TestDesktopDisable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "desktop", "disable", "test-repo")
	if err != nil {
		t.Fatalf("desktop disable failed: %v", err)
	}
	if !strings.Contains(stdout, "disabled") {
		t.Errorf("expected disabled message, got: %s", stdout)
	}
}

func TestDesktopActClick(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sandbox/test-repo/computer-use/act" {
			t.Errorf("expected act path, got %s", r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["action"] != "click" {
			t.Errorf("expected action=click, got %v", body)
		}
		if body["element"] != "btn-1" {
			t.Errorf("expected element=btn-1, got %v", body)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "desktop", "act", "test-repo",
		"--action", "click", "--element", "btn-1")
	if err != nil {
		t.Fatalf("desktop act failed: %v", err)
	}
	if !strings.Contains(stdout, "performed") {
		t.Errorf("expected performed message, got: %s", stdout)
	}
}

func TestDesktopActNavigate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["action"] != "navigate" {
			t.Errorf("expected action=navigate, got %v", body)
		}
		if body["url"] != "https://example.com" {
			t.Errorf("expected url, got %v", body)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "desktop", "act", "test-repo",
		"--action", "navigate", "--url", "https://example.com")
	if err != nil {
		t.Fatalf("desktop act navigate failed: %v", err)
	}
	if !strings.Contains(stdout, "performed") {
		t.Errorf("expected performed, got: %s", stdout)
	}
}

func TestDesktopMouse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sandbox/test-repo/x11/mouse" {
			t.Errorf("expected mouse path, got %s", r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["action"] != "click" {
			t.Errorf("expected action=click, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "desktop", "mouse", "test-repo",
		"--action", "click", "--x", "100", "--y", "200")
	if err != nil {
		t.Fatalf("desktop mouse failed: %v", err)
	}
	if !strings.Contains(stdout, "Mouse action") {
		t.Errorf("expected mouse message, got: %s", stdout)
	}
}

func TestDesktopKeyboardType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["type"] != "type" {
			t.Errorf("expected type=type, got %v", body)
		}
		if body["text"] != "hello world" {
			t.Errorf("expected text, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "desktop", "keyboard", "test-repo",
		"--type", "hello world")
	if err != nil {
		t.Fatalf("desktop keyboard type failed: %v", err)
	}
	if !strings.Contains(stdout, "Keyboard") {
		t.Errorf("expected keyboard message, got: %s", stdout)
	}
}

func TestDesktopKeyboardKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["type"] != "key" {
			t.Errorf("expected type=key, got %v", body)
		}
		if body["key"] != "Return" {
			t.Errorf("expected key=Return, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, _, err := runCommand(t, srv.URL, "desktop", "keyboard", "test-repo",
		"--key", "Return")
	if err != nil {
		t.Fatalf("desktop keyboard key failed: %v", err)
	}
}
