package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/test" {
			t.Errorf("expected /api/test, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	var result map[string]string
	err := client.Get("/api/test", &result)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", result)
	}
}

func TestClientGetNilOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.Get("/api/test", nil)
	if err != nil {
		t.Fatalf("Get with nil out failed: %v", err)
	}
}

func TestClientGetError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.Get("/api/test", nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention 404: %v", err)
	}
}

func TestClientPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content-type")
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "test" {
			t.Errorf("expected name=test, got %v", body)
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "123"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	var result map[string]string
	err := client.Post("/api/test", map[string]string{"name": "test"}, &result)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}
	if result["id"] != "123" {
		t.Errorf("expected id=123, got %v", result)
	}
}

func TestClientPut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]bool{"updated": true})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	var result map[string]bool
	err := client.Put("/api/test", map[string]string{"key": "val"}, &result)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if !result["updated"] {
		t.Error("expected updated=true")
	}
}

func TestClientDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.Delete("/api/test/123", nil)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestClientStreamPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		fmt.Fprintf(w, "event: text_delta\ndata: {\"text\":\"hello\"}\n\n")
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	resp, err := client.StreamPost("/api/test", map[string]string{"msg": "hi"})
	if err != nil {
		t.Fatalf("StreamPost failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "text_delta") {
		t.Errorf("expected SSE data in body, got: %s", string(body))
	}
}

func TestClientStreamGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("PNG-DATA"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	resp, err := client.StreamGet("/api/screenshot")
	if err != nil {
		t.Fatalf("StreamGet failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "PNG-DATA" {
		t.Errorf("expected PNG-DATA, got: %s", string(body))
	}
}

func TestClientStreamGetError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.StreamGet("/api/test")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}
