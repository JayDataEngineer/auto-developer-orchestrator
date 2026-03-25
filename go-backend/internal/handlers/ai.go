package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// AIHandler handles AI-related requests (test generation, etc.)
type AIHandler struct {
	logger *zap.Logger
}

// NewAIHandler creates a new AIHandler
func NewAIHandler(logger *zap.Logger) *AIHandler {
	return &AIHandler{logger: logger}
}

// GenerateTestsRequest represents a test generation request
type GenerateTestsRequest struct {
	Summary string `json:"summary"`
	Engine  string `json:"engine"`
	Prompt  string `json:"prompt"`
}

// GenerateTests generates test cases using AI
func (h *AIHandler) GenerateTests(w http.ResponseWriter, r *http.Request) {
	var req GenerateTestsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// For Gemini, return mock tests (client-side handling)
	// In production, this would call LiteLLM proxy
	tests := []string{
		"Verify that " + req.Summary + " handles null inputs correctly.",
		"Check for race conditions in " + req.Summary + " during high concurrency.",
		"Ensure " + req.Summary + " doesn't leak memory on repeated calls.",
		"Validate that " + req.Summary + " respects existing security permissions.",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"engine":    req.Engine,
		"summary":   req.Summary,
		"tests":     tests,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// RunTestsRequest represents a test execution request
type RunTestsRequest struct {
	Tests []string `json:"tests"`
}

// RunTests simulates test execution
func (h *AIHandler) RunTests(w http.ResponseWriter, r *http.Request) {
	var req RunTestsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Simulate test execution
	results := make([]map[string]interface{}, len(req.Tests))
	for i, test := range req.Tests {
		status := "PASSED"
		if rng.Float64() < 0.1 { // 10% failure rate
			status = "FAILED"
		}

		results[i] = map[string]interface{}{
			"test":     test,
			"status":   status,
			"duration": rng.Intn(500) + 100,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"results":   results,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// GenerateChecklist generates a checklist using the Python deep agent service
func (h *AIHandler) GenerateChecklist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		Prompt  string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Call Python microservice via gRPC
	// For now, return mock response
	h.logger.Info("Generating checklist", 
		zap.String("project", req.Project),
		zap.String("prompt", req.Prompt))

	w.Header().Set("Content-Type", "text/event-stream")
	// SSE streaming handled in checklist.go
}
