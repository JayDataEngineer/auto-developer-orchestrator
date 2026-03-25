package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"go.uber.org/zap"
)

// TestCLIHandler tests the CLI handler endpoints
func TestCLIHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	tmpDir := t.TempDir()
	handler := handlers.NewCLIHandler(logger, tmpDir)

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("Hello World"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "file.txt"), []byte("Subdir file"), 0644)

	t.Run("ListAllowedCommands", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/cli/commands", nil)
		w := httptest.NewRecorder()

		handler.ListAllowedCommands(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if response["success"] != true {
			t.Error("Expected success to be true")
		}

		commands, ok := response["commands"].([]interface{})
		if !ok || len(commands) == 0 {
			t.Error("Expected commands array to have items")
		}
	})

	t.Run("ExecuteCommand - ls", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"command": "ls",
			"args":    []string{"-la"},
		}
		jsonBody, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/cli/execute", bytes.NewBuffer(jsonBody))
		w := httptest.NewRecorder()

		handler.ExecuteCommand(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response handlers.CLIResponse
		json.NewDecoder(w.Body).Decode(&response)

		if !response.Success {
			t.Error("Expected success to be true")
		}

		if response.Command != "ls" {
			t.Errorf("Expected command 'ls', got '%s'", response.Command)
		}
	})

	t.Run("ExecuteCommand - pwd", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"command": "pwd",
		}
		jsonBody, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/cli/execute", bytes.NewBuffer(jsonBody))
		w := httptest.NewRecorder()

		handler.ExecuteCommand(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response handlers.CLIResponse
		json.NewDecoder(w.Body).Decode(&response)

		if !response.Success {
			t.Error("Expected success to be true")
		}
	})

	t.Run("ExecuteCommand - Blocked command", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"command": "rm",
			"args":    []string{"-rf", "/"},
		}
		jsonBody, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/cli/execute", bytes.NewBuffer(jsonBody))
		w := httptest.NewRecorder()

		handler.ExecuteCommand(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", w.Code)
		}
	})

	t.Run("ReadFile", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/cli/cat?path=test.txt", nil)
		w := httptest.NewRecorder()

		handler.ReadFile(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if response["success"] != true {
			t.Error("Expected success to be true")
		}

		content, ok := response["content"].(string)
		if !ok || content != "Hello World" {
			t.Error("Expected content 'Hello World'")
		}
	})

	t.Run("ListDirectory", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/cli/ls?path=.", nil)
		w := httptest.NewRecorder()

		handler.ListDirectory(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if response["success"] != true {
			t.Error("Expected success to be true")
		}

		entries, ok := response["entries"].([]interface{})
		if !ok || len(entries) == 0 {
			t.Error("Expected entries array to have items")
		}
	})

	t.Run("ReadFile - Directory traversal blocked", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/cli/cat?path=../../../etc/passwd", nil)
		w := httptest.NewRecorder()

		handler.ReadFile(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("ExecuteCommand - Shell injection blocked", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"command": "ls",
			"args":    []string{"; rm -rf /"},
		}
		jsonBody, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/cli/execute", bytes.NewBuffer(jsonBody))
		w := httptest.NewRecorder()

		handler.ExecuteCommand(w, req)

		// Injection is blocked - command will fail because sanitized arg is invalid
		// But the important part is the dangerous chars are removed
		var response handlers.CLIResponse
		json.NewDecoder(w.Body).Decode(&response)

		// Args should be sanitized (semicolon removed)
		for _, arg := range response.Args {
			if strings.Contains(arg, ";") {
				t.Error("Expected semicolon to be removed from args")
			}
		}
	})
}
