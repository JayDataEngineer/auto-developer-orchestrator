package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/manifest"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// ProjectHandler handles project-related HTTP requests
type ProjectHandler struct {
	db        *storage.Database
	logger    *zap.Logger
	git       *git.GitOps
	scheduler ScheduleRegisterer // optional: for auto-registering manifest schedules
}

// ScheduleRegisterer is implemented by *scheduler.Scheduler to auto-register
// schedule entries from a project manifest.
type ScheduleRegisterer interface {
	CreateJobFromManifest(project, name, cronExpr, promptText, description string) (string, error)
}

// NewProjectHandler creates a new ProjectHandler
func NewProjectHandler(db *storage.Database, logger *zap.Logger, gitOps *git.GitOps) *ProjectHandler {
	return &ProjectHandler{
		db:     db,
		logger: logger,
		git:    gitOps,
	}
}

// SetScheduler sets the scheduler for auto-registering manifest schedules.
func (h *ProjectHandler) SetScheduler(s ScheduleRegisterer) {
	h.scheduler = s
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
		JSONError(w, "Failed to list projects", http.StatusInternalServerError)
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

	// Filter to only projects that resolve to an actual directory
	type projectInfo struct {
		Name         string `json:"name"`
		HasManifest  bool   `json:"has_manifest"`
		Description  string `json:"description,omitempty"`
		Version      string `json:"version,omitempty"`
	}
	projects := make([]projectInfo, 0, len(projectSet))
	for project := range projectSet {
		dir := resolveProjectPath(project, h.db)
		if dir == "" {
			continue
		}
		info := projectInfo{Name: project}
		mf, _ := manifest.LoadManifest(dir)
		if mf != nil {
			info.HasManifest = true
			info.Description = mf.Description
			info.Version = mf.Version
		}
		projects = append(projects, info)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects": projects,
	})
}

// Add registers a new custom project. If repoUrl is provided without a local
// path, the repo is cloned first then registered.
func (h *ProjectHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		RepoURL string `json:"repoUrl"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		JSONError(w, "Name is required", http.StatusBadRequest)
		return
	}

	// GitHub flow: clone then register
	if req.Path == "" && req.RepoURL != "" {
		projectsDir := os.Getenv("PROJECT_ROOT")
		if projectsDir == "" {
			projectsDir = h.db.GetProjectsDir()
		}
		projectDir := filepath.Join(projectsDir, req.Name)

		// Already cloned?
		if _, err := os.Stat(projectDir); err == nil {
			req.Path = projectDir
		} else {
			// Clone in background
			ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
			defer cancel()

			cloneOpts := git.CloneOptions{
				URL:   req.RepoURL,
				Dir:   projectDir,
				Depth: 1,
			}

			if err := h.git.Clone(ctx, cloneOpts); err != nil {
				h.logger.Error("Failed to clone repository", zap.Error(err))
				JSONError(w, fmt.Sprintf("Failed to clone repository: %v", err), http.StatusInternalServerError)
				return
			}
			req.Path = projectDir
			h.logger.Info("Repository cloned", zap.String("project", req.Name), zap.String("dir", projectDir))
		}
	}

	if req.Path == "" {
		JSONError(w, "Path or repoUrl is required", http.StatusBadRequest)
		return
	}

	// Verify directory exists
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		JSONError(w, "Directory does not exist", http.StatusBadRequest)
		return
	}

	// Store in database
	if err := h.db.AddCustomProject(r.Context(), req.Name, req.Path); err != nil {
		h.logger.Error("Failed to add custom project", zap.Error(err))
		JSONError(w, "Failed to add project", http.StatusInternalServerError)
		return
	}

	// Try to load manifest
	resp := map[string]interface{}{
		"success": true,
		"message": "Project " + req.Name + " added",
	}

	mf, err := manifest.LoadManifest(req.Path)
	if err != nil {
		h.logger.Warn("Failed to parse pux.yaml", zap.Error(err))
		resp["manifest_error"] = err.Error()
	} else if mf != nil {
		resp["manifest"] = mf
		resp["brief"] = mf.Brief()

		// Auto-register schedules from manifest
		if h.scheduler != nil && mf.ScheduleCount() > 0 {
			registered := []string{}
			for schedName, schedDef := range mf.Schedule {
				promptText, _ := mf.ResolvePrompt(req.Path, schedDef.Prompt)
				jobID, schedErr := h.scheduler.CreateJobFromManifest(
					req.Name, schedName, schedDef.Cron, promptText, schedDef.Description,
				)
				if schedErr != nil {
					h.logger.Warn("Failed to register schedule",
						zap.String("schedule", schedName), zap.Error(schedErr))
					continue
				}
				registered = append(registered, fmt.Sprintf("%s (%s) → job %s", schedName, schedDef.Cron, jobID))
			}
			resp["registered_schedules"] = registered
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// Clone clones a repository via git CLI
func (h *ProjectHandler) Clone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		JSONError(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Extract project name from URL
	projectName := filepath.Base(req.URL)
	projectName = strings.TrimSuffix(projectName, ".git")

	projectsDir := h.db.GetProjectsDir()
	projectDir := filepath.Join(projectsDir, projectName)

	// Check if already exists
	if _, err := os.Stat(projectDir); err == nil {
		JSONError(w, "Project already exists locally", http.StatusBadRequest)
		return
	}

	// Clone repository via git CLI
	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second) // 5 minute timeout
	defer cancel()

	cloneOpts := git.CloneOptions{
		URL:   req.URL,
		Dir:   projectDir,
		Depth: 1, // Shallow clone for speed
	}

	if err := h.git.Clone(ctx, cloneOpts); err != nil {
		h.logger.Error("Failed to clone repository", zap.Error(err))
		JSONError(w, fmt.Sprintf("Failed to clone repository: %v", err), http.StatusInternalServerError)
		return
	}

	h.logger.Info("Repository cloned successfully",
		zap.String("project", projectName),
		zap.String("dir", projectDir))

	// Create initial checklist
	checklistPath := filepath.Join(projectDir, "TASKS.md")
	initialChecklist := `- [ ] Initial codebase analysis
- [ ] Configure CI/CD pipeline
- [ ] Audit existing test suite
- [ ] Identify architectural bottlenecks`

	if err := os.WriteFile(checklistPath, []byte(initialChecklist), 0644); err != nil {
		h.logger.Error("Failed to create checklist", zap.Error(err))
		// Don't fail the request, just log the error
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"message":      "Repository '" + projectName + "' cloned successfully to " + projectDir,
		"project_name": projectName,
		"project_dir":  projectDir,
	})
}

// CheckoutBranchRequest represents a branch checkout request
type CheckoutBranchRequest struct {
	Project string `json:"project"`
	Branch  string `json:"branch"`
}

// CheckoutBranch checks out a branch in a project
func (h *ProjectHandler) CheckoutBranch(w http.ResponseWriter, r *http.Request) {
	var req CheckoutBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Project == "" || req.Branch == "" {
		JSONError(w, "Project and branch are required", http.StatusBadRequest)
		return
	}

	projectDir, err := h.db.GetProjectDir(r.Context(), req.Project)
	if err != nil {
		JSONError(w, "Project not found", http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	checkoutOpts := git.CheckoutOptions{
		Dir:    projectDir,
		Branch: req.Branch,
	}

	if err := h.git.Checkout(ctx, checkoutOpts); err != nil {
		h.logger.Error("Failed to checkout branch",
			zap.String("project", req.Project),
			zap.String("branch", req.Branch),
			zap.Error(err))
		JSONError(w, fmt.Sprintf("Failed to checkout branch: %v", err), http.StatusInternalServerError)
		return
	}

	h.logger.Info("Branch checked out successfully",
		zap.String("project", req.Project),
		zap.String("branch", req.Branch))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"message":     fmt.Sprintf("Switched to branch '%s' in project '%s'", req.Branch, req.Project),
		"project":     req.Project,
		"branch":      req.Branch,
		"project_dir": projectDir,
	})
}

// GetBranchRequest represents a branch info request
type GetBranchRequest struct {
	Project string `json:"project"`
}

// GetBranch returns the current branch for a project
func (h *ProjectHandler) GetBranch(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	currentBranch, err := h.git.GetCurrentBranch(ctx, projectDir)
	if err != nil {
		h.logger.Error("Failed to get current branch",
			zap.String("project", projectName),
			zap.Error(err))
		JSONError(w, fmt.Sprintf("Failed to get branch: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"project": projectName,
		"branch":  currentBranch,
	})
}

// GetStatus returns the status of a project
func (h *ProjectHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
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

	// Get automation mode
	isAutoMode, err := h.db.GetAutomationMode(r.Context(), projectName)
	if err != nil {
		isAutoMode = false
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"gitState":    "clean",
		"workingTree": "main",
		"isAutoMode":  isAutoMode,
		"agentStatus": map[bool]string{true: "running", false: "paused"}[isAutoMode],
		"lastCommit":  "1a2b3c4",
		"project":     projectName,
		"projectDir":  projectDir,
	})
}

// SetMode toggles automation mode for a project
func (h *ProjectHandler) SetMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode    string `json:"mode"`
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

	isAutoMode := req.Mode == "auto"
	if err := h.db.SetAutomationMode(r.Context(), req.Project, isAutoMode); err != nil {
		h.logger.Error("Failed to set automation mode", zap.Error(err))
		JSONError(w, "Failed to update mode", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"is_auto_mode": isAutoMode,
	})
}
