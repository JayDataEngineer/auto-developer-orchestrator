package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("expected /api/health, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "health")
	if err != nil {
		t.Fatalf("health failed: %v", err)
	}
	if !strings.Contains(stdout, "OK") {
		t.Errorf("expected OK in output, got: %s", stdout)
	}
}

func TestHealthFail(t *testing.T) {
	// Use a server that returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, _, err := runCommand(t, srv.URL, "health")
	if err == nil {
		t.Fatal("expected error for unhealthy backend")
	}
}
