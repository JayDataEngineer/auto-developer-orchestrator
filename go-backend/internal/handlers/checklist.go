package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// ChecklistHandler handles checklist-related HTTP requests
type ChecklistHandler struct {
	db     *storage.Database
	logger *zap.Logger
}

// NewChecklistHandler creates a new ChecklistHandler
func NewChecklistHandler(db *storage.Database, logger *zap.Logger) *ChecklistHandler {
	return &ChecklistHandler{
		db:     db,
		logger: logger,
	}
}

// Task represents a single task in the checklist
type Task struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
	Status    string `json:"status"` // completed, in-progress, pending
}

// Get returns the checklist for a project
func (h *ChecklistHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectName := r.URL.Query().Get("project")
	if projectName == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), projectName)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(projectDir, "TODO_FOR_JULES.md")

	// Return empty if file doesn't exist
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []Task{},
		})
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		h.logger.Error("Failed to read checklist", zap.Error(err))
		http.Error(w, "Failed to read checklist", http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(content), "\n")
	tasks := []Task{}

	// Get current task index
	currentTaskIndex, _ := h.db.GetCurrentTaskIndex(r.Context(), projectName)

	taskCounter := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- [") {
			continue
		}

		completed := strings.Contains(line, "[x]")
		text := strings.TrimPrefix(line, "- [ ] ")
		text = strings.TrimPrefix(text, "- [x] ")

		status := "pending"
		if completed {
			status = "completed"
		} else if taskCounter == currentTaskIndex {
			status = "in-progress"
		}

		tasks = append(tasks, Task{
			ID:        fmt.Sprintf("task-%d", taskCounter),
			Text:      text,
			Completed: completed,
			Status:    status,
		})

		taskCounter++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": tasks,
	})
}

// Update updates the checklist for a project
func (h *ChecklistHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tasks   []Task `json:"tasks"`
		Project string `json:"project"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		// Create directory if it doesn't exist
		projectDir = filepath.Join(h.db.GetProjectsDir(), req.Project)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			h.logger.Error("Failed to create project directory", zap.Error(err))
			http.Error(w, "Project directory not found", http.StatusNotFound)
			return
		}
	}

	filePath := filepath.Join(projectDir, "TODO_FOR_JULES.md")

	// Generate markdown content
	var content strings.Builder
	for _, task := range req.Tasks {
		checkbox := " "
		if task.Completed {
			checkbox = "x"
		}
		content.WriteString(fmt.Sprintf("- [%s] %s\n", checkbox, task.Text))
	}

	if err := os.WriteFile(filePath, []byte(content.String()), 0644); err != nil {
		h.logger.Error("Failed to write checklist", zap.Error(err))
		http.Error(w, "Failed to update checklist", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Checklist updated successfully",
	})
}

// Merge marks the current task as completed and adds a test task
func (h *ChecklistHandler) Merge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(projectDir, "TODO_FOR_JULES.md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "Checklist not found", http.StatusNotFound)
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		h.logger.Error("Failed to read checklist", zap.Error(err))
		http.Error(w, "Failed to merge", http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(content), "\n")

	// Get current task index
	currentTaskIndex, _ := h.db.GetCurrentTaskIndex(r.Context(), req.Project)

	var mergedTaskText string
	taskCounter := 0
	updatedLines := []string{}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [") {
			if taskCounter == currentTaskIndex {
				// Mark as completed
				line = strings.Replace(line, "- [ ]", "- [x]", 1)
				// Extract task text
				mergedTaskText = strings.TrimPrefix(trimmed, "- [ ] ")
				mergedTaskText = strings.TrimPrefix(mergedTaskText, "- [x] ")
			}
			taskCounter++
		}
		updatedLines = append(updatedLines, line)
	}

	// Add test task
	if mergedTaskText != "" {
		updatedLines = append(updatedLines, fmt.Sprintf("- [ ] Debug / enhance testing around: %s", mergedTaskText))
	}

	// Write updated content
	if err := os.WriteFile(filePath, []byte(strings.Join(updatedLines, "\n")), 0644); err != nil {
		h.logger.Error("Failed to write checklist", zap.Error(err))
		http.Error(w, "Failed to merge", http.StatusInternalServerError)
		return
	}

	// Reset current task index
	if err := h.db.SetCurrentTaskIndex(r.Context(), req.Project, -1); err != nil {
		h.logger.Warn("Failed to reset task index", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "PR merged and task marked as completed.",
		"summary": mergedTaskText,
	})
}

// GenerateChecklistStream handles SSE streaming for deep agent checklist generation
func (h *ChecklistHandler) GenerateChecklistStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		Prompt  string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Create SSE channel
	sseChan := make(chan string, 100)
	errChan := make(chan error, 1)

	// Get Python service URL from environment
	pythonServiceURL := os.Getenv("PYTHON_SERVICE_URL")
	if pythonServiceURL == "" {
		pythonServiceURL = "http://localhost:8080"
	}

	// Call Python microservice
	go func() {
		defer close(sseChan)

		// Send initialization event
		sseChan <- fmt.Sprintf(`data: {"event": "log", "message": "DEEP AGENT: Initializing connection to Python service..."}`)
		flusher.Flush()

		// Prepare request to Python service
		pythonReqBody := map[string]interface{}{
			"project_path": projectDir,
			"prompt":       req.Prompt,
		}
		pythonReqJSON, _ := json.Marshal(pythonReqBody)

		// Call Python service
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		pythonReq, err := http.NewRequestWithContext(
			ctx,
			"POST",
			pythonServiceURL+"/api/v1/checklist/generate",
			bytes.NewReader(pythonReqJSON),
		)
		if err != nil {
			errChan <- fmt.Errorf("failed to create Python request: %w", err)
			return
		}
		pythonReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 120 * time.Second}
		resp, err := client.Do(pythonReq)
		if err != nil {
			errChan <- fmt.Errorf("failed to call Python service: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errChan <- fmt.Errorf("Python service error: %d %s", resp.StatusCode, string(body))
			return
		}

		// Stream events from Python service to frontend
		sseChan <- fmt.Sprintf(`data: {"event": "log", "message": "DEEP AGENT: Connected to Python service, starting analysis..."}`)
		flusher.Flush()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				sseChan <- line
				flusher.Flush()
			}
		}

		sseChan <- fmt.Sprintf(`data: {"event": "log", "message": "DEEP AGENT: Analysis complete!"}`)
		flusher.Flush()
	}()

	// Stream events to client
	for {
		select {
		case event, ok := <-sseChan:
			if !ok {
				return
			}
			fmt.Fprintln(w, event)
			flusher.Flush()
		case err := <-errChan:
			h.logger.Error("SSE stream error", zap.Error(err))
			fmt.Fprintf(w, `data: {"error": "%s"}\n`, err.Error())
			flusher.Flush()
			return
		case <-r.Context().Done():
			return
		}
	}
}

// Helper function to convert to JSON
func toJSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// readChecklistFile reads and parses a checklist file
func readChecklistFile(filePath string) ([]Task, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	tasks := []Task{}
	scanner := bufio.NewScanner(file)
	taskCounter := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "- [") {
			continue
		}

		completed := strings.Contains(line, "[x]")
		text := strings.TrimPrefix(line, "- [ ] ")
		text = strings.TrimPrefix(text, "- [x] ")

		tasks = append(tasks, Task{
			ID:        fmt.Sprintf("task-%d", taskCounter),
			Text:      text,
			Completed: completed,
			Status:    "pending",
		})

		taskCounter++
	}

	return tasks, scanner.Err()
}
