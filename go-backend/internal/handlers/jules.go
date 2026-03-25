package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/jules"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// JulesHandler handles Jules API integration
type JulesHandler struct {
	db        *storage.Database
	logger    *zap.Logger
	julesClient *jules.Client
	httpClient  *http.Client
}

// NewJulesHandler creates a new JulesHandler
func NewJulesHandler(db *storage.Database, logger *zap.Logger) *JulesHandler {
	apiKey := os.Getenv("JULES_API_KEY")
	
	return &JulesHandler{
		db:     db,
		logger: logger,
		julesClient: jules.NewClient(apiKey),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// DispatchRequest represents a task dispatch request
type DispatchRequest struct {
	TaskID    string `json:"taskId"`
	Project   string `json:"project"`
	RepoOwner string `json:"repoOwner"`
	RepoName  string `json:"repoName"`
}

// Dispatch sends a single task to Jules API
func (h *JulesHandler) Dispatch(w http.ResponseWriter, r *http.Request) {
	var req DispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	// Get task text from checklist
	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	taskText := fmt.Sprintf("Task %s", req.TaskID)
	checklistPath := projectDir + "/TODO_FOR_JULES.md"
	if content, err := os.ReadFile(checklistPath); err == nil {
		lines := bytes.Split(content, []byte("\n"))
		// Extract index from taskId (e.g., "task-4" -> 4)
		var index int
		fmt.Sscanf(req.TaskID, "task-%d", &index)
		if index >= 0 && index < len(lines) {
			line := string(lines[index])
			if len(line) > 0 {
				taskText = line
				taskText = bytes.TrimPrefix([]byte(taskText), []byte("- [ ] ")).String()
				taskText = bytes.TrimPrefix([]byte(taskText), []byte("- [x] ")).String()
			}
		}
	}

	// Check if API key exists
	apiKey := os.Getenv("JULES_API_KEY")
	if apiKey == "" {
		h.logger.Warn("JULES_API_KEY not found, returning mock response")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"message":  fmt.Sprintf("Task %s dispatched to JULES (mock)", req.TaskID),
			"task_id":  req.TaskID,
			"issue_url": "https://github.com/user/repo/issues/102",
		})
		return
	}

	// Call Jules API
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	session, err := h.julesClient.CreateSession(ctx, jules.CreateSessionRequest{
		Prompt: taskText,
		SourceContext: jules.SourceContext{
			Source: fmt.Sprintf("sources/github/%s/%s", req.RepoOwner, req.RepoName),
		},
		AutomationMode: "AUTO_CREATE_PR",
		Title:          truncateString(taskText, 50),
	})

	if err != nil {
		h.logger.Error("Failed to call Jules API", zap.Error(err))
		http.Error(w, fmt.Sprintf("Failed to call Jules API: %v", err), http.StatusInternalServerError)
		return
	}

	// Store session in database for polling
	if err := h.db.StoreJulesSession(r.Context(), req.Project, req.TaskID, session.ID); err != nil {
		h.logger.Warn("Failed to store session", zap.Error(err))
	}

	// Update current task index
	taskIndex := 0
	fmt.Sscanf(req.TaskID, "task-%d", &taskIndex)
	if err := h.db.SetCurrentTaskIndex(r.Context(), req.Project, taskIndex); err != nil {
		h.logger.Warn("Failed to update task index", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   fmt.Sprintf("Task %s dispatched to JULES", req.TaskID),
		"task_id":   req.TaskID,
		"issue_url": fmt.Sprintf("https://jules.google.com/session/%s", session.ID),
		"session_id": session.ID,
	})
}

// DispatchAllRequest represents a bulk dispatch request
type DispatchAllRequest struct {
	Project   string                   `json:"project"`
	Todos     []map[string]interface{} `json:"todos"`
	RepoOwner string                   `json:"repoOwner"`
	RepoName  string                   `json:"repoName"`
}

// DispatchAll sends all tasks to Jules API
func (h *JulesHandler) DispatchAll(w http.ResponseWriter, r *http.Request) {
	var req DispatchAllRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" || len(req.Todos) == 0 || req.RepoOwner == "" || req.RepoName == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	h.logger.Info("Dispatching tasks", 
		zap.Int("count", len(req.Todos)),
		zap.String("project", req.Project),
		zap.String("repo", fmt.Sprintf("%s/%s", req.RepoOwner, req.RepoName)))

	apiKey := os.Getenv("JULES_API_KEY")
	if apiKey == "" {
		h.logger.Warn("JULES_API_KEY not found, returning mock response")
		results := make([]map[string]interface{}, len(req.Todos))
		for i, todo := range req.Todos {
			results[i] = map[string]interface{}{
				"content":  todo["content"],
				"status":   "dispatched",
				"issue_url": fmt.Sprintf("https://github.com/%s/%s/issues/%d", req.RepoOwner, req.RepoName, 1000+i),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"results": results,
		})
		return
	}

	// Dispatch all tasks sequentially
	results := make([]map[string]interface{}, len(req.Todos))
	for i, todo := range req.Todos {
		taskText, ok := todo["content"].(string)
		if !ok {
			taskText = fmt.Sprintf("Task %d", i)
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		session, err := h.julesClient.CreateSession(ctx, jules.CreateSessionRequest{
			Prompt: taskText,
			SourceContext: jules.SourceContext{
				Source: fmt.Sprintf("sources/github/%s/%s", req.RepoOwner, req.RepoName),
			},
			AutomationMode: "AUTO_CREATE_PR",
			Title:          truncateString(taskText, 50),
		})
		cancel()

		if err != nil {
			h.logger.Error("Failed to dispatch task", 
				zap.Error(err),
				zap.String("task", taskText))
			results[i] = map[string]interface{}{
				"content": taskText,
				"status":  "failed",
				"error":   err.Error(),
			}
			continue
		}

		results[i] = map[string]interface{}{
			"content":   taskText,
			"status":    "dispatched",
			"issue_url": fmt.Sprintf("https://jules.google.com/session/%s", session.ID),
			"session_id": session.ID,
		}

		// Small delay to avoid rate limiting
		time.Sleep(500 * time.Millisecond)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"results": results,
	})
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// JulesClient handles communication with the Jules API
type JulesClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewJulesClient creates a new Jules API client
func NewJulesClient(apiKey string) *JulesClient {
	return &JulesClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    "https://jules.googleapis.com/v1alpha",
	}
}

// CreateSessionRequest represents a request to create a Jules session
type CreateSessionRequest struct {
	Prompt        string        `json:"prompt"`
	SourceContext SourceContext `json:"sourceContext"`
	AutomationMode string       `json:"automationMode"`
	Title         string        `json:"title"`
}

// SourceContext represents the source code context for Jules
type SourceContext struct {
	Source string `json:"source"`
}

// Session represents a Jules session
type Session struct {
	ID    string `json:"id"`
	State string `json:"state"` // PLANNING, IN_PROGRESS, COMPLETED
	Plan  *Plan  `json:"plan,omitempty"`
}

// Plan represents a Jules execution plan
type Plan struct {
	Files       []string `json:"files"`
	Description string   `json:"description"`
	Changes     string   `json:"changes"`
}

// CreateSession creates a new Jules session
func (c *JulesClient) CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	url := c.baseURL + "/sessions"

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jules API error: %d %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &session, nil
}

// GetSession retrieves a Jules session by ID
func (c *JulesClient) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	url := fmt.Sprintf("%s/sessions/%s", c.baseURL, sessionID)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jules API error: %d %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &session, nil
}

// ApprovePlan approves a Jules session plan
func (c *JulesClient) ApprovePlan(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/sessions/%s:approvePlan", c.baseURL, sessionID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Jules API error: %d %s", resp.StatusCode, string(body))
	}

	return nil
}
