package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
		JSONError(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), projectName)
	if err != nil {
		JSONError(w, "Project not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(projectDir, "TASKS.md")

	// Return empty if file doesn't exist
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tasks": []Task{},
		})
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		h.logger.Error("Failed to read checklist", zap.Error(err))
		JSONError(w, "Failed to read checklist", http.StatusInternalServerError)
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		JSONError(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		// Create directory if it doesn't exist
		projectDir = filepath.Join(h.db.GetProjectsDir(), req.Project)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			h.logger.Error("Failed to create project directory", zap.Error(err))
			JSONError(w, "Project directory not found", http.StatusNotFound)
			return
		}
	}

	filePath := filepath.Join(projectDir, "TASKS.md")

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
		JSONError(w, "Failed to update checklist", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		JSONError(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		JSONError(w, "Project not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(projectDir, "TASKS.md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		JSONError(w, "Checklist not found", http.StatusNotFound)
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		h.logger.Error("Failed to read checklist", zap.Error(err))
		JSONError(w, "Failed to merge", http.StatusInternalServerError)
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
		JSONError(w, "Failed to merge", http.StatusInternalServerError)
		return
	}

	// Reset current task index
	if err := h.db.SetCurrentTaskIndex(r.Context(), req.Project, -1); err != nil {
		h.logger.Warn("Failed to reset task index", zap.Error(err))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "PR merged and task marked as completed.",
		"summary": mergedTaskText,
	})
}

// GenerateChecklistStream handles SSE streaming for checklist generation.
// Scans the project for basic structure and generates a task list.
// For full LLM-powered analysis, use the Pi agent (/api/pi/prompt).
func (h *ChecklistHandler) GenerateChecklistStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		Prompt  string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		JSONError(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		JSONError(w, "Project not found", http.StatusNotFound)
		return
	}

	// Set SSE headers
	setSSEHeaders(w)

	flusher, ok := w.(http.Flusher)
	if !ok {
		JSONError(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Generate basic tasks from project structure
	tasks := h.generateProjectTasks(projectDir, req.Prompt)

	// Write tasks to file
	filePath := filepath.Join(projectDir, "TASKS.md")
	var content strings.Builder
	for _, task := range tasks {
		content.WriteString(fmt.Sprintf("- [ ] %s\n", task))
	}
	if err := os.WriteFile(filePath, []byte(content.String()), 0644); err != nil {
		h.logger.Error("Failed to write tasks file", zap.Error(err))
	}

	// Stream events
	sseLog := func(msg string) {
		data, _ := json.Marshal(map[string]string{"event": "log", "message": msg})
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	}

	sseLog("AGENT: Scanning project structure...")
	sseLog(fmt.Sprintf("AGENT: Found project at %s", projectDir))

	for i, task := range tasks {
		sseLog(fmt.Sprintf("AGENT: Generated task %d/%d: %s", i+1, len(tasks), task))
	}

	sseLog("AGENT: Task generation complete. Refreshing...")
}

// generateProjectTasks creates a basic task list from project structure.
func (h *ChecklistHandler) generateProjectTasks(projectDir, prompt string) []string {
	tasks := []string{}

	// Walk the project directory to understand structure
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return []string{"Analyze project structure and create implementation plan"}
	}

	hasReadme := false
	hasTests := false
	hasDocker := false
	hasGitignore := false

	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if strings.HasPrefix(name, "readme") {
			hasReadme = true
		}
		if strings.Contains(name, "test") || name == "__tests__" {
			hasTests = true
		}
		if strings.HasPrefix(name, "dockerfile") || strings.HasPrefix(name, "docker-compose") {
			hasDocker = true
		}
		if name == ".gitignore" {
			hasGitignore = true
		}
	}

	if !hasReadme {
		tasks = append(tasks, "Create comprehensive README.md with setup instructions")
	}
	if !hasTests {
		tasks = append(tasks, "Add unit tests for core functionality")
	}
	if !hasDocker {
		tasks = append(tasks, "Add Docker configuration for containerized deployment")
	}
	if !hasGitignore {
		tasks = append(tasks, "Add .gitignore file")
	}

	if prompt != "" {
		tasks = append([]string{prompt}, tasks...)
	}

	if len(tasks) == 0 {
		tasks = append(tasks,
			"Review code quality and add missing documentation",
			"Add error handling improvements",
			"Optimize build and deployment pipeline",
		)
	}

	return tasks
}
