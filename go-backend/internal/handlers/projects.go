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
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// ProjectHandler handles project-related HTTP requests
type ProjectHandler struct {
	db        *storage.Database
	logger    *zap.Logger
	git       *git.GitOps
	scheduler ScheduleRegisterer // optional: for auto-registering manifest schedules
	toolReg   ToolRegisterer     // optional: for auto-registering manifest tools
	sandboxIn SandboxInitializer // optional: for auto-initializing sandboxes from manifest
}

// ScheduleRegisterer is implemented by *scheduler.Scheduler to auto-register
// schedule entries from a project manifest.
type ScheduleRegisterer interface {
	CreateJobFromManifest(project, name, cronExpr, promptText, description, model string) (string, error)
	FindJobByProjectAndName(project, name string) string
	UpdateJob(jobID string, updates *scheduler.Job) error
}

// ToolRegisterer is implemented by the llama engine to auto-register
// app tools from a project manifest.
type ToolRegisterer interface {
	RegisterFromManifest(projectName, projectDir string, tools []manifest.ToolDef) []string
	UnregisterFromManifest(projectName string)
}

// SandboxInitializer initializes sandbox containers from manifest declarations.
type SandboxInitializer interface {
	InitFromManifest(ctx context.Context, projectName string, sandboxCfg *manifest.SandboxConfig, projectDir string) *SandboxInitResult
	InitIfSandboxExists(ctx context.Context, projectName string, sandboxCfg *manifest.SandboxConfig, projectDir string) *SandboxInitResult
}

// SandboxInitResult describes the outcome of sandbox initialization.
type SandboxInitResult struct {
	FilesUploaded         int      `json:"files_uploaded"`
	PipPackagesInstalled  int      `json:"pip_packages_installed"`
	EnvVarsWritten        int      `json:"env_vars_written"`
	Errors                []string `json:"errors,omitempty"`
	SandboxNotFound       bool     `json:"sandbox_not_found,omitempty"`
}

// sandboxInit is the concrete implementation of SandboxInitializer.
type sandboxInit struct {
	manager *sandbox.Manager
	logger  *zap.Logger
}

// NewSandboxInitializer creates a new SandboxInitializer.
func NewSandboxInitializer(manager *sandbox.Manager, logger *zap.Logger) SandboxInitializer {
	return &sandboxInit{manager: manager, logger: logger}
}

// InitFromManifest runs full sandbox initialization (files, pip, env) for a project.
// If no sandbox exists, returns a result with SandboxNotFound=true.
func (si *sandboxInit) InitFromManifest(ctx context.Context, projectName string, sandboxCfg *manifest.SandboxConfig, projectDir string) *SandboxInitResult {
	result := &SandboxInitResult{}

	sb := si.manager.FindSandboxByProject(projectName)
	if sb == nil {
		result.SandboxNotFound = true
		return result
	}

	return si.runInit(ctx, sb.ID, sandboxCfg, projectDir)
}

// InitIfSandboxExists runs init only if a sandbox already exists for the project.
func (si *sandboxInit) InitIfSandboxExists(ctx context.Context, projectName string, sandboxCfg *manifest.SandboxConfig, projectDir string) *SandboxInitResult {
	sb := si.manager.FindSandboxByProject(projectName)
	if sb == nil {
		return &SandboxInitResult{SandboxNotFound: true}
	}
	return si.runInit(ctx, sb.ID, sandboxCfg, projectDir)
}

func (si *sandboxInit) runInit(ctx context.Context, sandboxID string, sandboxCfg *manifest.SandboxConfig, projectDir string) *SandboxInitResult {
	result := &SandboxInitResult{}

	// Upload init_files
	for _, relPath := range sandboxCfg.InitFiles {
		localPath := filepath.Join(projectDir, relPath)
		sandboxPath := filepath.Join("/sandbox", filepath.Base(relPath))
		if err := si.manager.CopyToSandbox(ctx, sandboxID, localPath, sandboxPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("upload %s: %v", relPath, err))
			continue
		}
		result.FilesUploaded++
	}

	// Install pip packages
	if len(sandboxCfg.PipPackages) > 0 {
		if err := si.manager.PipInstall(ctx, sandboxID, sandboxCfg.PipPackages); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("pip install: %v", err))
		} else {
			result.PipPackagesInstalled = len(sandboxCfg.PipPackages)
		}
	}

	// Write .env file
	if len(sandboxCfg.Env) > 0 {
		if err := si.manager.WriteEnvFile(ctx, sandboxID, sandboxCfg.Env); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("write .env: %v", err))
		} else {
			result.EnvVarsWritten = len(sandboxCfg.Env)
		}
	}

	si.logger.Info("sandbox initialized from manifest",
		zap.String("sandbox_id", sandboxID),
		zap.Int("files_uploaded", result.FilesUploaded),
		zap.Int("pip_installed", result.PipPackagesInstalled),
		zap.Int("env_vars", result.EnvVarsWritten),
		zap.Int("errors", len(result.Errors)),
	)
	return result
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

// SetToolRegisterer sets the tool registerer for auto-registering manifest tools.
func (h *ProjectHandler) SetToolRegisterer(tr ToolRegisterer) {
	h.toolReg = tr
}

// SetSandboxInitializer sets the sandbox initializer for auto-initializing sandboxes.
func (h *ProjectHandler) SetSandboxInitializer(si SandboxInitializer) {
	h.sandboxIn = si
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

				// Idempotent: update existing job if one with same project+name exists
				existingID := h.scheduler.FindJobByProjectAndName(req.Name, schedName)
				if existingID != "" {
					updErr := h.scheduler.UpdateJob(existingID, &scheduler.Job{
						Name:        schedName,
						Description: schedDef.Description,
						Project:     req.Name,
						Message:     promptText,
						Model:       schedDef.Model,
						Schedule:    scheduler.ScheduleCron,
						CronExpr:    schedDef.Cron,
						Enabled:     true,
					})
					if updErr != nil {
						h.logger.Warn("Failed to update existing schedule",
							zap.String("schedule", schedName), zap.Error(updErr))
						continue
					}
					registered = append(registered, fmt.Sprintf("%s (%s) → job %s [updated]", schedName, schedDef.Cron, existingID))
				} else {
					jobID, schedErr := h.scheduler.CreateJobFromManifest(
						req.Name, schedName, schedDef.Cron, promptText, schedDef.Description, schedDef.Model,
					)
					if schedErr != nil {
						h.logger.Warn("Failed to register schedule",
							zap.String("schedule", schedName), zap.Error(schedErr))
						continue
					}
					registered = append(registered, fmt.Sprintf("%s (%s) → job %s", schedName, schedDef.Cron, jobID))
				}
			}
			resp["registered_schedules"] = registered
		}

		// Auto-register app tools from manifest
		if h.toolReg != nil && len(mf.Tools) > 0 {
			registered := h.toolReg.RegisterFromManifest(req.Name, req.Path, mf.Tools)
			resp["registered_tools"] = registered
		}

		// Auto-initialize sandbox from manifest (files, pip, env)
		if h.sandboxIn != nil && mf.Sandbox != nil {
			initCtx, initCancel := context.WithTimeout(r.Context(), 120*time.Second)
			defer initCancel()
			initResult := h.sandboxIn.InitFromManifest(initCtx, req.Name, mf.Sandbox, req.Path)
			if initResult.SandboxNotFound {
				resp["sandbox_init"] = "deferred (no sandbox yet — will init on first session)"
			} else {
				resp["sandbox_init"] = initResult
			}
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
