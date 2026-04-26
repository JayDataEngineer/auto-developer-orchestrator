package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func TestRecoverer_NoPanic(t *testing.T) {
	logger := zap.NewNop()
	r := chi.NewRouter()
	r.Use(handlers.Recoverer(logger))
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRecoverer_Panic(t *testing.T) {
	logger := zap.NewNop()
	r := chi.NewRouter()
	r.Use(handlers.Recoverer(logger))
	r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", w.Code)
	}

	var resp handlers.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.Error != "Internal Server Error" {
		t.Errorf("expected 'Internal Server Error', got %q", resp.Error)
	}
}

func TestJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	handlers.JSONError(w, "something went wrong", http.StatusNotFound)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}

	var resp handlers.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.Error != "Not Found" {
		t.Errorf("expected 'Not Found', got %q", resp.Error)
	}
	if resp.Message != "something went wrong" {
		t.Errorf("expected 'something went wrong', got %q", resp.Message)
	}
}
