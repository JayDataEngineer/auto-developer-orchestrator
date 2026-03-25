package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// ProjectHandler handles project-related HTTP requests
type ProjectHandler struct {
	db     *storage.Database
	logger *zap.Logger
}

// NewProjectHandler creates a new ProjectHandler
func NewProjectHandler(db *storage.Database, logger *zap.Logger) *ProjectHandler {
	return &ProjectHandler{
		db:     db,
		logger: logger,
	}
}

// List returns all projects (default + custom)
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	projectsDir := h.db.GetProjectsDir()

	// Read default projects from filesystem
	defaultProjects := []string{}
	if entries, err := os.ReadDir(projectsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				defaultProjects = append(defaultProjects, entry.Name())
			}
		}
	}

	// Get custom projects from database
	customProjects, err := h.db.GetCustomProjects(r.Context())
	if err != nil {
		h.logger.Error("Failed to get custom projects", zap.Error(err))
		http.Error(w, "Failed to list projects", http.StatusInternalServerError)
		return
	}

	// Merge and deduplicate
	projectSet := make(map[string]bool)
	for _, p := range defaultProjects {
		projectSet[p] = true
	}
	for _, p := range customProjects {
		projectSet[p.Name] = true
	}

	projects := make([]string, 0, len(projectSet))
	for project := range projectSet {
		projects = append(projects, project)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"projects": projects,
	})
}

// Add registers a new custom project
func (h *ProjectHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Path == "" {
		http.Error(w, "Name and path are required", http.StatusBadRequest)
		return
	}

	// Verify directory exists
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		http.Error(w, "Directory does not exist", http.StatusBadRequest)
		return
	}

	// Store in database
	if err := h.db.AddCustomProject(r.Context(), req.Name, req.Path); err != nil {
		h.logger.Error("Failed to add custom project", zap.Error(err))
		http.Error(w, "Failed to add project", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Project " + req.Name + " added from " + req.Path,
	})
}

// Clone simulates cloning a repository (actual git clone via CLI)
func (h *ProjectHandler) Clone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Extract project name from URL
	projectName := filepath.Base(req.URL)
	projectName = filepath.TrimSuffix(projectName, ".git")

	projectsDir := h.db.GetProjectsDir()
	projectDir := filepath.Join(projectsDir, projectName)

	// Check if already exists
	if _, err := os.Stat(projectDir); err == nil {
		http.Error(w, "Project already exists locally", http.StatusBadRequest)
		return
	}

	// TODO: Execute git clone via CLI
	// For now, create directory and initial checklist
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		h.logger.Error("Failed to create project directory", zap.Error(err))
		http.Error(w, "Failed to clone repository", http.StatusInternalServerError)
		return
	}

	checklistPath := filepath.Join(projectDir, "TODO_FOR_JULES.md")
	initialChecklist := `- [ ] Initial codebase analysis
- [ ] Configure CI/CD pipeline
- [ ] Audit existing test suite
- [ ] Identify architectural bottlenecks`

	if err := os.WriteFile(checklistPath, []byte(initialChecklist), 0644); err != nil {
		h.logger.Error("Failed to create checklist", zap.Error(err))
		http.Error(w, "Failed to create checklist", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"message":      "Repository '" + projectName + "' cloned successfully to " + projectDir,
		"project_name": projectName,
	})
}

// GetStatus returns the status of a project
func (h *ProjectHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
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

	// Get automation mode
	isAutoMode, err := h.db.GetAutomationMode(r.Context(), projectName)
	if err != nil {
		isAutoMode = false
	}

	// TODO: Get actual git status
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"git_state":     "clean",
		"working_tree":  "main",
		"is_auto_mode":  isAutoMode,
		"agent_status":  map[bool]string{true: "running", false: "paused"}[isAutoMode],
		"last_commit":   "1a2b3c4",
		"project":       projectName,
		"project_dir":   projectDir,
	})
}

// SetMode toggles automation mode for a project
func (h *ProjectHandler) SetMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode    string `json:"mode"`
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

	isAutoMode := req.Mode == "auto"
	if err := h.db.SetAutomationMode(r.Context(), req.Project, isAutoMode); err != nil {
		h.logger.Error("Failed to set automation mode", zap.Error(err))
		http.Error(w, "Failed to update mode", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"is_auto_mode": isAutoMode,
	})
}
