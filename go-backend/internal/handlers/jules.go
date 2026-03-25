package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/jules"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// JulesHandler handles Jules API integration
type JulesHandler struct {
	db          *storage.Database
	logger      *zap.Logger
	julesClient *jules.Client
	httpClient  *http.Client
	poller      *JulesPoller
}

// JulesPoller handles polling for Jules session status
type JulesPoller struct {
	db       *storage.Database
	logger   *zap.Logger
	httpClient *http.Client
	ticker   *time.Ticker
	done     chan bool
	mu       sync.RWMutex
	running  bool
}

// NewJulesHandler creates a new JulesHandler
func NewJulesHandler(db *storage.Database, logger *zap.Logger) *JulesHandler {
	apiKey := os.Getenv("JULES_API_KEY")

	handler := &JulesHandler{
		db:     db,
		logger: logger,
		julesClient: jules.NewClient(apiKey),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}

	// Initialize and start poller
	handler.poller = NewJulesPoller(db, logger)
	handler.poller.Start()

	return handler
}

// NewJulesPoller creates a new JulesPoller
func NewJulesPoller(db *storage.Database, logger *zap.Logger) *JulesPoller {
	return &JulesPoller{
		db:     db,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		ticker:  time.NewTicker(30 * time.Second), // Poll every 30 seconds
		done:    make(chan bool),
		running: false,
	}
}

// Start begins the polling loop
func (p *JulesPoller) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return
	}

	p.running = true
	go p.pollLoop()

	p.logger.Info("Jules poller started")
}

// Stop halts the polling loop
func (p *JulesPoller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	p.done <- true
	p.ticker.Stop()
	p.running = false

	p.logger.Info("Jules poller stopped")
}

// pollLoop runs the polling loop
func (p *JulesPoller) pollLoop() {
	for {
		select {
		case <-p.ticker.C:
			p.pollActiveSessions()
		case <-p.done:
			return
		}
	}
}

// pollActiveSessions polls all active Jules sessions
func (p *JulesPoller) pollActiveSessions() {
	ctx := context.Background()

	// Get all active sessions from database
	sessions, err := p.db.GetActiveJulesSessions(ctx)
	if err != nil {
		p.logger.Error("Failed to get active sessions", zap.Error(err))
		return
	}

	if len(sessions) == 0 {
		return
	}

	apiKey := os.Getenv("JULES_API_KEY")
	if apiKey == "" {
		return
	}

	for _, session := range sessions {
		p.pollSession(session, apiKey)
	}
}

// pollSession polls a single Jules session
func (p *JulesPoller) pollSession(session storage.JulesSession, apiKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Call Jules API to get session status
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://jules.googleapis.com/v1alpha/sessions/%s", session.SessionID),
		nil)
	if err != nil {
		p.logger.Error("Failed to create Jules status request",
			zap.String("session_id", session.SessionID),
			zap.Error(err))
		return
	}

	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Error("Failed to poll Jules session",
			zap.String("session_id", session.SessionID),
			zap.Error(err))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("Failed to read Jules response",
			zap.String("session_id", session.SessionID),
			zap.Error(err))
		return
	}

	// Parse response
	var statusResp struct {
		Name       string `json:"name"`
		State      string `json:"state"`
		Plan       *struct {
			Description string `json:"description"`
		} `json:"plan,omitempty"`
		SourceContext *struct {
			Git *struct {
				PullRequest *struct {
					Url string `json:"pullRequest"`
				} `json:"git"`
			} `json:"git,omitempty"`
		} `json:"sourceContext,omitempty"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &statusResp); err != nil {
		p.logger.Error("Failed to parse Jules status",
			zap.String("session_id", session.SessionID),
			zap.Error(err))
		return
	}

	// Update session in database
	session.Status = statusResp.State
	session.UpdatedAt = time.Now()
	session.LastPolledAt = time.Now()

	// Extract PR URL if available
	if statusResp.SourceContext != nil && statusResp.SourceContext.Git != nil &&
		statusResp.SourceContext.Git.PullRequest != nil {
		session.PRURL = statusResp.SourceContext.Git.PullRequest.Url
	}

	// Extract error if available
	if statusResp.Error != nil {
		session.ErrorMessage = statusResp.Error.Message
	}

	// Check if session is complete
	if statusResp.State == "COMPLETED" || statusResp.State == "FAILED" {
		// Update task in checklist
		if err := p.markTaskComplete(session.ProjectName, session.TaskID); err != nil {
			p.logger.Error("Failed to mark task complete",
				zap.String("task_id", session.TaskID),
				zap.Error(err))
		}
	}

	// Save updated session
	if err := p.db.UpdateJulesSession(ctx, &session); err != nil {
		p.logger.Error("Failed to update session",
			zap.String("session_id", session.SessionID),
			zap.Error(err))
	}

	p.logger.Info("Polled Jules session",
		zap.String("session_id", session.SessionID),
		zap.String("status", session.Status),
		zap.String("pr_url", session.PRURL))
}

// markTaskComplete marks a task as completed in the checklist
func (p *JulesPoller) markTaskComplete(project, taskID string) error {
	// Get project directory
	projectDir, err := p.db.GetProjectDir(context.Background(), project)
	if err != nil {
		return err
	}

	// Read checklist
	checklistPath := projectDir + "/TODO_FOR_JULES.md"
	content, err := os.ReadFile(checklistPath)
	if err != nil {
		return err
	}

	// Parse task index from taskID (e.g., "task-4" -> 4)
	var index int
	fmt.Sscanf(taskID, "task-%d", &index)

	// Update checklist
	lines := bytes.Split(content, []byte("\n"))
	if index >= 0 && index < len(lines) {
		line := string(lines[index])
		if strings.Contains(line, "- [ ]") {
			lines[index] = []byte(strings.Replace(line, "- [ ]", "- [x]", 1))
			return os.WriteFile(checklistPath, bytes.Join(lines, []byte("\n")), 0644)
		}
	}

	return nil
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
				taskText = strings.TrimPrefix(strings.TrimPrefix(line, "- [ ] "), "- [x] ")
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
	julesSession := &storage.JulesSession{
		ProjectName:  req.Project,
		TaskID:       req.TaskID,
		SessionID:    session.ID,
		Status:       "PLANNING",
		PlanApproved: false,
		IssueURL:     "",
		PRURL:        "",
	}
	if err := h.db.CreateJulesSession(r.Context(), julesSession); err != nil {
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
		"success":    true,
		"message":    fmt.Sprintf("Task %s dispatched to JULES", req.TaskID),
		"task_id":    req.TaskID,
		"issue_url":  fmt.Sprintf("https://jules.google.com/session/%s", session.ID),
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

// ListSessions returns all Jules sessions
func (h *JulesHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessions, err := h.db.GetActiveJulesSessions(ctx)
	if err != nil {
		h.logger.Error("Failed to get sessions", zap.Error(err))
		http.Error(w, "Failed to get sessions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"sessions": sessions,
	})
}

// GetSession retrieves a single Jules session
func (h *JulesHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	session, err := h.db.GetJulesSession(r.Context(), sessionID)
	if err != nil {
		h.logger.Error("Failed to get session", zap.Error(err))
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"session": session,
	})
}

// ApprovePlan approves a Jules session plan
func (h *JulesHandler) ApprovePlan(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	apiKey := os.Getenv("JULES_API_KEY")
	if apiKey == "" {
		http.Error(w, "JULES_API_KEY not configured", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := h.julesClient.ApprovePlan(ctx, sessionID); err != nil {
		h.logger.Error("Failed to approve plan", zap.Error(err))
		http.Error(w, fmt.Sprintf("Failed to approve plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Update session in database
	session, err := h.db.GetJulesSession(ctx, sessionID)
	if err == nil {
		session.PlanApproved = true
		session.Status = "IN_PROGRESS"
		h.db.UpdateJulesSession(ctx, session)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Plan approved",
	})
}
