package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
)

func TestFsBrowseHandler(t *testing.T) {
	// Create temp dir structure for testing
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, ".hidden"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "inner.txt"), []byte("inner"), 0644)

	// Set HOME to tmpDir so the handler allows it
	t.Setenv("HOME", tmpDir)
	t.Setenv("PROJECT_ROOT", tmpDir)

	handler := handlers.NewFsBrowseHandler()

	t.Run("lists directory entries", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/fs/browse?path="+tmpDir, nil)
		w := httptest.NewRecorder()
		handler.Browse(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Path    string `json:"path"`
			Parent  string `json:"parent"`
			Entries []struct {
				Name  string `json:"name"`
				IsDir bool   `json:"isDir"`
			} `json:"entries"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.Path != tmpDir {
			t.Errorf("Expected path %s, got %s", tmpDir, resp.Path)
		}

		// Should have subdir (dir) and test.txt (file), but not .hidden or node_modules
		names := make(map[string]bool)
		for _, e := range resp.Entries {
			names[e.Name] = true
		}

		if !names["subdir"] {
			t.Error("Expected 'subdir' in entries")
		}
		if !names["test.txt"] {
			t.Error("Expected 'test.txt' in entries")
		}
		if names[".hidden"] {
			t.Error("Hidden dir '.hidden' should be filtered")
		}
		if names["node_modules"] {
			t.Error("'node_modules' should be filtered")
		}
	})

	t.Run("directories come first", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/fs/browse?path="+tmpDir, nil)
		w := httptest.NewRecorder()
		handler.Browse(w, req)

		var resp struct {
			Entries []struct {
				Name  string `json:"name"`
				IsDir bool   `json:"isDir"`
			} `json:"entries"`
		}
		json.NewDecoder(w.Body).Decode(&resp)

		foundFile := false
		for _, e := range resp.Entries {
			if !e.IsDir {
				foundFile = true
			}
			if foundFile && e.IsDir {
				t.Errorf("Directory '%s' found after files — should be dirs first", e.Name)
			}
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/fs/browse?path=/etc/passwd", nil)
		w := httptest.NewRecorder()
		handler.Browse(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 for /etc/passwd, got %d", w.Code)
		}
	})

	t.Run("rejects non-existent path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/fs/browse?path="+tmpDir+"/nonexistent", nil)
		w := httptest.NewRecorder()
		handler.Browse(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for non-existent path, got %d", w.Code)
		}
	})

	t.Run("defaults to HOME when no path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/fs/browse", nil)
		w := httptest.NewRecorder()
		handler.Browse(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Path string `json:"path"`
		}
		json.NewDecoder(w.Body).Decode(&resp)

		if resp.Path != tmpDir {
			t.Errorf("Expected path to default to HOME (%s), got %s", tmpDir, resp.Path)
		}
	})

	t.Run("parent is correct", func(t *testing.T) {
		subPath := filepath.Join(tmpDir, "subdir")
		req := httptest.NewRequest("GET", "/api/fs/browse?path="+subPath, nil)
		w := httptest.NewRecorder()
		handler.Browse(w, req)

		var resp struct {
			Parent string `json:"parent"`
		}
		json.NewDecoder(w.Body).Decode(&resp)

		if resp.Parent != tmpDir {
			t.Errorf("Expected parent %s, got %s", tmpDir, resp.Parent)
		}
	})

	t.Run("PUX_FS_ROOTS allows custom roots", func(t *testing.T) {
		t.Setenv("PUX_FS_ROOTS", "/tmp")
		customHandler := handlers.NewFsBrowseHandler()

		req := httptest.NewRequest("GET", "/api/fs/browse?path=/tmp", nil)
		w := httptest.NewRecorder()
		customHandler.Browse(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 with PUX_FS_ROOTS=/tmp, got %d", w.Code)
		}
	})
}
