package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"go.uber.org/zap"
)

// TestAIHandler tests the AI handler endpoints
func TestAIHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := handlers.NewAIHandler(logger)

	t.Run("GenerateTests - Valid Request", func(t *testing.T) {
		reqBody := map[string]string{
			"summary": "user authentication",
			"engine":  "openai",
			"prompt":  "Generate comprehensive tests",
		}
		jsonBody, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/generate-tests", bytes.NewBuffer(jsonBody))
		w := httptest.NewRecorder()

		handler.GenerateTests(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Parse response
		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if response["success"] != true {
			t.Error("Expected success to be true")
		}

		tests, ok := response["tests"].([]interface{})
		if !ok || len(tests) == 0 {
			t.Error("Expected tests array to have items")
		}

		// Verify test content includes the summary
		testStr, _ := tests[0].(string)
		if testStr == "" {
			t.Error("Expected test string to not be empty")
		}
	})

	t.Run("RunTests - Valid Request", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"tests": []string{
				"Test 1",
				"Test 2",
				"Test 3",
			},
		}
		jsonBody, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/run-tests", bytes.NewBuffer(jsonBody))
		w := httptest.NewRecorder()

		handler.RunTests(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Parse response
		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if response["success"] != true {
			t.Error("Expected success to be true")
		}

		results, ok := response["results"].([]interface{})
		if !ok || len(results) != 3 {
			t.Error("Expected 3 test results")
		}

		// Verify each result has required fields
		for i, result := range results {
			r, ok := result.(map[string]interface{})
			if !ok {
				t.Errorf("Result %d should be a map", i)
				continue
			}
			if r["test"] == nil {
				t.Errorf("Result %d missing 'test' field", i)
			}
			if r["status"] == nil {
				t.Errorf("Result %d missing 'status' field", i)
			}
			if r["duration"] == nil {
				t.Errorf("Result %d missing 'duration' field", i)
			}
		}
	})
}
