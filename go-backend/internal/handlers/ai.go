package handlers

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// AIHandler handles AI-related requests
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

// GenerateTests generates test cases
// TODO: migrate to Pi agent for real LLM-powered test generation
func (h *AIHandler) GenerateTests(w http.ResponseWriter, r *http.Request) {
	var req GenerateTestsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"engine":    "stub",
		"summary":   req.Summary,
		"tests":     []string{},
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

	results := make([]map[string]interface{}, len(req.Tests))
	for i, test := range req.Tests {
		status := "PASSED"
		if rand.Float64() < 0.1 {
			status = "FAILED"
		}
		results[i] = map[string]interface{}{
			"test":     test,
			"status":   status,
			"duration": rand.Intn(500) + 100,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"results":   results,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// GenerateChecklist generates a checklist
func (h *AIHandler) GenerateChecklist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		Prompt  string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.logger.Info("Generating checklist",
		zap.String("project", req.Project),
		zap.String("prompt", req.Prompt))

	w.Header().Set("Content-Type", "text/event-stream")
}
