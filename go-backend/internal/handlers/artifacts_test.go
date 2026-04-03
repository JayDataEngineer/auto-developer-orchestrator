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

func TestArtifactHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := handlers.NewArtifactHandler(logger)

	t.Run("CreateOrUpdate - creates artifact", func(t *testing.T) {
		body := handlers.CreateOrUpdateArtifactRequest{
			AgentID: "agent-1",
			Type:    "plan",
			Title:   "Implementation Plan",
			Content: "## Step 1\nDo the thing",
		}
		jsonBody, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/pi/artifacts", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateOrUpdate(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var artifact handlers.Artifact
		json.NewDecoder(w.Body).Decode(&artifact)

		if artifact.ID != "agent-1:plan" {
			t.Errorf("Expected ID 'agent-1:plan', got '%s'", artifact.ID)
		}
		if artifact.AgentID != "agent-1" {
			t.Errorf("Expected AgentID 'agent-1', got '%s'", artifact.AgentID)
		}
		if artifact.Type != "plan" {
			t.Errorf("Expected Type 'plan', got '%s'", artifact.Type)
		}
		if artifact.Title != "Implementation Plan" {
			t.Errorf("Expected Title 'Implementation Plan', got '%s'", artifact.Title)
		}
		if artifact.Content != "## Step 1\nDo the thing" {
			t.Errorf("Expected content, got '%s'", artifact.Content)
		}
		if artifact.UpdatedAt.IsZero() {
			t.Error("Expected UpdatedAt to be set")
		}
	})

	t.Run("CreateOrUpdate - updates existing artifact", func(t *testing.T) {
		body := handlers.CreateOrUpdateArtifactRequest{
			AgentID: "agent-1",
			Type:    "plan",
			Title:   "Updated Plan",
			Content: "## Step 1 (revised)",
		}
		jsonBody, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/pi/artifacts", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateOrUpdate(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		var artifact handlers.Artifact
		json.NewDecoder(w.Body).Decode(&artifact)

		if artifact.Title != "Updated Plan" {
			t.Errorf("Expected title 'Updated Plan', got '%s'", artifact.Title)
		}
		if artifact.Content != "## Step 1 (revised)" {
			t.Errorf("Expected updated content, got '%s'", artifact.Content)
		}
	})

	t.Run("CreateOrUpdate - missing agentId", func(t *testing.T) {
		body := handlers.CreateOrUpdateArtifactRequest{
			Type:    "plan",
			Title:   "No Agent",
			Content: "content",
		}
		jsonBody, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/pi/artifacts", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateOrUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("CreateOrUpdate - missing type", func(t *testing.T) {
		body := handlers.CreateOrUpdateArtifactRequest{
			AgentID: "agent-1",
			Title:   "No Type",
			Content: "content",
		}
		jsonBody, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/pi/artifacts", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateOrUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("CreateOrUpdate - multiple artifact types", func(t *testing.T) {
		for _, artifactType := range []string{"todo", "notes"} {
			body := handlers.CreateOrUpdateArtifactRequest{
				AgentID: "agent-1",
				Type:    artifactType,
				Title:   artifactType + " title",
				Content: artifactType + " content",
			}
			jsonBody, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/api/pi/artifacts", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.CreateOrUpdate(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200 for type '%s', got %d", artifactType, w.Code)
			}
		}
	})

	t.Run("List - returns artifacts for agent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/pi/artifacts?agentId=agent-1", nil)
		w := httptest.NewRecorder()

		handler.List(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		var response map[string]any
		json.NewDecoder(w.Body).Decode(&response)

		artifacts, ok := response["artifacts"].([]interface{})
		if !ok {
			t.Fatal("Expected artifacts array")
		}

		// Should have plan, todo, notes = 3 artifacts
		if len(artifacts) != 3 {
			t.Errorf("Expected 3 artifacts, got %d", len(artifacts))
		}
	})

	t.Run("List - empty for unknown agent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/pi/artifacts?agentId=unknown-agent", nil)
		w := httptest.NewRecorder()

		handler.List(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		var response map[string]any
		json.NewDecoder(w.Body).Decode(&response)

		artifacts, ok := response["artifacts"].([]interface{})
		if !ok {
			t.Fatal("Expected artifacts array")
		}
		if len(artifacts) != 0 {
			t.Errorf("Expected 0 artifacts for unknown agent, got %d", len(artifacts))
		}
	})

	t.Run("List - missing agentId parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/pi/artifacts", nil)
		w := httptest.NewRecorder()

		handler.List(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("CreateOrUpdate - multiple agents are independent", func(t *testing.T) {
		body := handlers.CreateOrUpdateArtifactRequest{
			AgentID: "agent-2",
			Type:    "plan",
			Title:   "Agent 2 Plan",
			Content: "Different content",
		}
		jsonBody, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/pi/artifacts", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateOrUpdate(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		// agent-2 should have 1 artifact
		req2 := httptest.NewRequest("GET", "/api/pi/artifacts?agentId=agent-2", nil)
		w2 := httptest.NewRecorder()
		handler.List(w2, req2)

		var response map[string]any
		json.NewDecoder(w2.Body).Decode(&response)

		artifacts, _ := response["artifacts"].([]interface{})
		if len(artifacts) != 1 {
			t.Errorf("Expected 1 artifact for agent-2, got %d", len(artifacts))
		}

		// agent-1 should still have 3 artifacts (not affected)
		req3 := httptest.NewRequest("GET", "/api/pi/artifacts?agentId=agent-1", nil)
		w3 := httptest.NewRecorder()
		handler.List(w3, req3)

		json.NewDecoder(w3.Body).Decode(&response)

		artifacts, _ = response["artifacts"].([]interface{})
		if len(artifacts) != 3 {
			t.Errorf("Expected 3 artifacts for agent-1, got %d", len(artifacts))
		}
	})

	t.Run("CreateOrUpdate - invalid JSON body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/pi/artifacts", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateOrUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}
