package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// TaskHandler handles HTTP endpoints for task management.
type TaskHandler struct {
	taskMgr *pi.TaskManager
	logger  *zap.Logger
}

// NewTaskHandler creates a new task handler.
func NewTaskHandler(taskMgr *pi.TaskManager, logger *zap.Logger) *TaskHandler {
	return &TaskHandler{
		taskMgr: taskMgr,
		logger:  logger,
	}
}

// RegisterRoutes registers all task routes.
func (h *TaskHandler) RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
	Put(string, http.HandlerFunc)
	Delete(string, http.HandlerFunc)
}) {
	r.Post("/", h.Create)
	r.Get("/list", h.List)
	r.Get("/{taskId}", h.Get)
	r.Put("/{taskId}", h.Update)
	r.Delete("/{taskId}", h.Delete)
	r.Post("/{taskId}/stop", h.Stop)
	r.Get("/{taskId}/canStart", h.CanStart)
	r.Post("/{taskId}/deps", h.SetDependencies)
}

// createTaskRequest is the body for task creation.
type createTaskRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	ProjectDir  string `json:"projectDir"`
	ParentAgent string `json:"parentAgent"`
	SubAgentID  string `json:"subAgentId,omitempty"`
	Model       string `json:"model,omitempty"`
}

// Create adds a new task.
// POST /api/pi/tasks/
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Title == "" {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "title is required",
		})
		return
	}

	task, err := h.taskMgr.Create(pi.Task{
		Title:       req.Title,
		Description: req.Description,
		ProjectDir:  req.ProjectDir,
		ParentAgent: req.ParentAgent,
		SubAgentID:  req.SubAgentID,
		Model:       req.Model,
	})
	if err != nil {
		writeTaskJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeTaskJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    task,
	})
}

// Get returns a single task.
// GET /api/pi/tasks/{taskId}
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "taskId is required",
		})
		return
	}

	task, err := h.taskMgr.Get(taskID)
	if err != nil {
		writeTaskJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeTaskJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    task,
	})
}

// updateTaskRequest is the body for task update.
type updateTaskRequest struct {
	Status      *string `json:"status,omitempty"`
	Output      *string `json:"output,omitempty"`
	Error       *string `json:"error,omitempty"`
	SubAgentID  *string `json:"subAgentId,omitempty"`
	Description *string `json:"description,omitempty"`
}

// Update modifies a task.
// PUT /api/pi/tasks/{taskId}
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "taskId is required",
		})
		return
	}

	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	updates := pi.TaskUpdate{
		Output:      req.Output,
		Error:       req.Error,
		SubAgentID:  req.SubAgentID,
		Description: req.Description,
	}
	if req.Status != nil {
		s := pi.TaskStatus(*req.Status)
		updates.Status = &s
	}

	task, err := h.taskMgr.Update(taskID, updates)
	if err != nil {
		writeTaskJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeTaskJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    task,
	})
}

// Stop marks a task as failed.
// POST /api/pi/tasks/{taskId}/stop
func (h *TaskHandler) Stop(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "taskId is required",
		})
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	task, err := h.taskMgr.Stop(taskID, req.Reason)
	if err != nil {
		writeTaskJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeTaskJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    task,
	})
}

// Delete removes a task.
// DELETE /api/pi/tasks/{taskId}
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "taskId is required",
		})
		return
	}

	if err := h.taskMgr.Delete(taskID); err != nil {
		writeTaskJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeTaskJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// List returns tasks filtered by project or agent.
// GET /api/pi/tasks/list?projectDir=X&parentAgent=Y
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	projectDir := r.URL.Query().Get("projectDir")
	parentAgent := r.URL.Query().Get("parentAgent")

	var tasks []pi.Task
	if parentAgent != "" {
		tasks = h.taskMgr.ListByAgent(parentAgent)
	} else {
		tasks = h.taskMgr.ListByProject(projectDir)
	}

	if tasks == nil {
		tasks = []pi.Task{}
	}

	writeTaskJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tasks":   tasks,
	})
}

// CanStart checks if a task's dependencies are met.
// GET /api/pi/tasks/{taskId}/canStart
func (h *TaskHandler) CanStart(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "taskId is required",
		})
		return
	}

	canStart, err := h.taskMgr.CanStart(taskID)
	if err != nil {
		writeTaskJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeTaskJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"canStart":  canStart,
	})
}

// setDepsRequest is the body for setting dependencies.
type setDepsRequest struct {
	Blocks    []string `json:"blocks"`
	BlockedBy []string `json:"blockedBy"`
}

// SetDependencies configures task dependencies.
// POST /api/pi/tasks/{taskId}/deps
func (h *TaskHandler) SetDependencies(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "taskId is required",
		})
		return
	}

	var req setDepsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if err := h.taskMgr.SetDependencies(taskID, req.Blocks, req.BlockedBy); err != nil {
		writeTaskJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeTaskJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// writeTaskJSON writes a JSON response.
func writeTaskJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
