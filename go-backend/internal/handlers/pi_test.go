package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// setupPiRouter creates a PiHandler with an empty pool and optional real DB.
// PROJECT_ROOT is set to a temp dir so resolveProjectPath can find projects.
func setupPiRouter(t *testing.T) (*chi.Mux, *storage.Database) {
	t.Helper()
	logger := zap.NewNop()

	// Create a temp projects dir and a subdirectory named "test-project"
	projectsDir := t.TempDir()
	projectDir := filepath.Join(projectsDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROJECT_ROOT", projectsDir)

	// Real SQLite DB in temp dir
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Empty pool (no Pi subprocesses)
	pool := pi.NewPiPool(logger, 0)

	handler := handlers.NewPiHandler(pool, db, nil, nil, logger)

	r := chi.NewRouter()
	r.Route("/api/pi", handler.RegisterRoutes)

	return r, db
}

// ── Prompt Validation ─────────────────────────────────────────

func TestPiPromptInvalidJSON(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/prompt", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPiPromptMissingMessage(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{"project": "test-project"})
	req := httptest.NewRequest("POST", "/api/pi/prompt", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing message, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "Message is required" {
		t.Errorf("Expected 'Message is required' error, got %v", resp["error"])
	}
}

func TestPiPromptProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{
		"message": "hello",
		"project": "nonexistent",
	})
	req := httptest.NewRequest("POST", "/api/pi/prompt", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown project, got %d", w.Code)
	}
}

// ── Respond Validation ────────────────────────────────────────

func TestPiRespondInvalidJSON(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/respond", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPiRespondMissingRequiredFields(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{"project": "test-project"})
	req := httptest.NewRequest("POST", "/api/pi/respond", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing requestId/action, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "requestId and action are required" {
		t.Errorf("Expected 'requestId and action are required', got %v", resp["error"])
	}
}

func TestPiRespondProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{
		"requestId": "req-1",
		"action":    "approve",
		"project":   "nonexistent",
	})
	req := httptest.NewRequest("POST", "/api/pi/respond", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown project, got %d", w.Code)
	}
}

func TestPiRespondAgentNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{
		"requestId": "req-1",
		"action":    "approve",
		"project":   "test-project",
		"agentId":   "default",
	})
	req := httptest.NewRequest("POST", "/api/pi/respond", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for no agent, got %d", w.Code)
	}
}

// ── Abort ─────────────────────────────────────────────────────

func TestPiAbortProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/abort?project=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown project, got %d", w.Code)
	}
}

func TestPiAbortNoActiveSession(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/abort?project=test-project", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Empty pool returns 200 with success=false
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for no active session, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != false {
		t.Error("Expected success=false for no active session")
	}
}

// ── GetState ──────────────────────────────────────────────────

func TestPiGetStateProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/state?project=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestPiGetStateEmptySession(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/state?project=test-project", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Empty pool returns 200 with empty SessionState
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for empty state, got %d", w.Code)
	}
}

// ── GetMessages ───────────────────────────────────────────────

func TestPiGetMessagesMissingProject(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/messages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing project, got %d", w.Code)
	}
}

func TestPiGetMessagesEmpty(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/messages?project=test-project", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var msgs []storage.StoredMessage
	json.NewDecoder(w.Body).Decode(&msgs)
	// DB has no messages, should return empty array
	if len(msgs) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(msgs))
	}
}

func TestPiGetMessagesReturnsSaved(t *testing.T) {
	r, db := setupPiRouter(t)

	// Save a user message directly to the DB
	_, err := db.SaveUserMessage(context.TODO(), "test-project", "default", "Hello agent")
	if err != nil {
		t.Fatalf("Failed to save user message: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/pi/messages?project=test-project", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var msgs []storage.StoredMessage
	json.NewDecoder(w.Body).Decode(&msgs)
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello agent" {
		t.Errorf("Expected content 'Hello agent', got %q", msgs[0].Content)
	}
	if msgs[0].Role != "user" {
		t.Errorf("Expected role 'user', got %q", msgs[0].Role)
	}
}

// ── GetHistory ────────────────────────────────────────────────

func TestPiGetHistoryEmpty(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/history", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	convs, ok := resp["conversations"].([]interface{})
	if !ok {
		t.Fatal("Expected conversations array")
	}
	if len(convs) != 0 {
		t.Errorf("Expected 0 conversations, got %d", len(convs))
	}
}

func TestPiGetHistoryWithMessages(t *testing.T) {
	r, db := setupPiRouter(t)

	// Save messages to create a conversation
	db.SaveUserMessage(context.TODO(), "test-project", "default", "First prompt")
	db.SaveAssistantMessage(context.TODO(), "test-project", "default", "First response", "", "[]")

	req := httptest.NewRequest("GET", "/api/pi/history", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	convs := resp["conversations"].([]interface{})
	if len(convs) != 1 {
		t.Fatalf("Expected 1 conversation, got %d", len(convs))
	}
	conv := convs[0].(map[string]interface{})
	if conv["project"] != "test-project" {
		t.Errorf("Expected project 'test-project', got %v", conv["project"])
	}
}

// ── DeleteConversation ────────────────────────────────────────

func TestPiDeleteConversationMissingFields(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("DELETE", "/api/pi/conversation?project=test-project", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing agentId, got %d", w.Code)
	}
}

func TestPiDeleteConversationDeletesMessages(t *testing.T) {
	r, db := setupPiRouter(t)

	// Create messages
	db.SaveUserMessage(context.TODO(), "test-project", "default", "Hello")
	db.SaveAssistantMessage(context.TODO(), "test-project", "default", "Hi", "", "[]")

	// Verify they exist
	msgs, _ := db.GetConversationHistory(context.TODO(), "test-project", "default", 100)
	if len(msgs) != 2 {
		t.Fatalf("Expected 2 messages before delete, got %d", len(msgs))
	}

	// Delete
	req := httptest.NewRequest("DELETE", "/api/pi/conversation?project=test-project&agentId=default", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify they're gone
	msgs, _ = db.GetConversationHistory(context.TODO(), "test-project", "default", 100)
	if len(msgs) != 0 {
		t.Errorf("Expected 0 messages after delete, got %d", len(msgs))
	}
}

// ── RenameConversation ────────────────────────────────────────

func TestPiRenameConversationMissingFields(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{"title": "New Title"})
	req := httptest.NewRequest("PUT", "/api/pi/conversation/rename?project=test-project", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing agentId, got %d", w.Code)
	}
}

func TestPiRenameConversationInvalidJSON(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("PUT", "/api/pi/conversation/rename?project=test-project&agentId=default", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPiRenameConversationSetsTitle(t *testing.T) {
	r, db := setupPiRouter(t)

	// Create a conversation so there's something to rename
	db.SaveUserMessage(context.TODO(), "test-project", "default", "Hello")

	body, _ := json.Marshal(map[string]string{"title": "My Conversation"})
	req := httptest.NewRequest("PUT", "/api/pi/conversation/rename?project=test-project&agentId=default", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify via history
	msgs, _ := db.GetConversationHistory(context.TODO(), "test-project", "default", 100)
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(msgs))
	}
}

// ── SpawnAgent ────────────────────────────────────────────────

func TestPiSpawnAgentInvalidJSON(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/agent/spawn", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPiSpawnAgentMissingProject(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("POST", "/api/pi/agent/spawn", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing project, got %d", w.Code)
	}
}

// ── DestroyAgent ──────────────────────────────────────────────

func TestPiDestroyAgentInvalidJSON(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/agent/destroy", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPiDestroyAgentMissingFields(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{"project": "test-project"})
	req := httptest.NewRequest("POST", "/api/pi/agent/destroy", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing agentId, got %d", w.Code)
	}
}

func TestPiDestroyAgentProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{
		"project": "nonexistent",
		"agentId": "default",
	})
	req := httptest.NewRequest("POST", "/api/pi/agent/destroy", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown project, got %d", w.Code)
	}
}

// ── SetModel ──────────────────────────────────────────────────

func TestPiSetModelInvalidJSON(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("PUT", "/api/pi/model", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPiSetModelProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{
		"project":  "nonexistent",
		"provider": "litellm",
		"modelId":  "gemma-4-26b",
	})
	req := httptest.NewRequest("PUT", "/api/pi/model", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown project, got %d", w.Code)
	}
}

func TestPiSetModelNoActiveSession(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{
		"project":  "test-project",
		"provider": "litellm",
		"modelId":  "gemma-4-26b",
	})
	req := httptest.NewRequest("PUT", "/api/pi/model", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Empty pool returns 200 with success=false
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for no active session, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != false {
		t.Error("Expected success=false for no active session")
	}
}

// ── Compact ───────────────────────────────────────────────────

func TestPiCompactProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/compact?project=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestPiCompactNoActiveSession(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("POST", "/api/pi/compact?project=test-project", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for no active session, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != false {
		t.Error("Expected success=false for no active session")
	}
}

// ── ListActive ────────────────────────────────────────────────

func TestPiListActiveEmpty(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/active", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	projects, ok := resp["projects"].([]interface{})
	if !ok {
		t.Fatal("Expected projects array")
	}
	if len(projects) != 0 {
		t.Errorf("Expected 0 active projects, got %d", len(projects))
	}
}

// ── GetModels ─────────────────────────────────────────────────

func TestPiGetModelsReturnsEmptyWithoutLiteLLM(t *testing.T) {
	r, _ := setupPiRouter(t)

	// No LITELLM_PROXY_URL set, and no models.json file
	t.Setenv("LITELLM_PROXY_URL", "")

	req := httptest.NewRequest("GET", "/api/pi/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	models, ok := resp["models"]
	if !ok {
		t.Fatal("Expected models field")
	}
	// models will be nil/null since there's no models.json
	_ = models
}

// ── SwitchSession ─────────────────────────────────────────────

func TestPiSwitchSessionInvalidJSON(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("PUT", "/api/pi/session", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestPiSwitchSessionProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	body, _ := json.Marshal(map[string]string{
		"project":   "nonexistent",
		"sessionId": "sess-123",
	})
	req := httptest.NewRequest("PUT", "/api/pi/session", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown project, got %d", w.Code)
	}
}

// ── ListSessions ──────────────────────────────────────────────

func TestPiListSessionsProjectNotFound(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/sessions?project=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown project, got %d", w.Code)
	}
}

func TestPiListSessionsEmpty(t *testing.T) {
	r, _ := setupPiRouter(t)

	req := httptest.NewRequest("GET", "/api/pi/sessions?project=test-project", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}
