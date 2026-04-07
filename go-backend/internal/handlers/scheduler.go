package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SchedulerHandler handles scheduled job HTTP endpoints.
type SchedulerHandler struct {
	scheduler *scheduler.Scheduler
	log       *zap.Logger
}

// NewSchedulerHandler creates a new scheduler handler.
func NewSchedulerHandler(s *scheduler.Scheduler, logger *zap.Logger) *SchedulerHandler {
	return &SchedulerHandler{
		scheduler: s,
		log:       logger,
	}
}

// RegisterRoutes registers all scheduler routes on the given router.
func (h *SchedulerHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.CreateJob)
	r.Get("/", h.ListJobs)
	r.Get("/{id}", h.GetJob)
	r.Put("/{id}", h.UpdateJob)
	r.Delete("/{id}", h.DeleteJob)
	r.Post("/{id}/trigger", h.TriggerJob)
	r.Get("/{id}/executions", h.ListExecutions)
	r.Get("/{id}/runs", h.ListRuns)
	r.Get("/runs", h.ListAllRuns)
}

// createJobRequest is the request body for creating a job.
type createJobRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Project      string `json:"project"`
	AgentID      string `json:"agentId,omitempty"`
	Message      string `json:"message"`
	Model        string `json:"model,omitempty"`
	ScheduleType string `json:"scheduleType"`
	CronExpr     string `json:"cronExpr,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	EverySeconds int64  `json:"everySeconds,omitempty"`
	AtTime       string `json:"atTime,omitempty"`
	AutoBranch   bool   `json:"autoBranch,omitempty"`
	AutoMerge    bool   `json:"autoMerge,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
	// Phase 4: Delivery
	DeliveryMode       string `json:"deliveryMode,omitempty"`
	DeliveryWebhookURL string `json:"deliveryWebhookUrl,omitempty"`
	// Phase 3/4: Failure alerts
	FailureAlertAfter      int    `json:"failureAlertAfter,omitempty"`
	FailureAlertWebhookURL string `json:"failureAlertWebhookUrl,omitempty"`
}

// CreateJob creates a new scheduled job.
func (h *SchedulerHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	job := &scheduler.Job{
		Name:                   req.Name,
		Description:            req.Description,
		Project:                req.Project,
		AgentID:                req.AgentID,
		Message:                req.Message,
		Model:                  req.Model,
		Schedule:               scheduler.ScheduleType(req.ScheduleType),
		CronExpr:               req.CronExpr,
		Timezone:               req.Timezone,
		EverySeconds:           req.EverySeconds,
		AtTime:                 req.AtTime,
		AutoBranch:             req.AutoBranch,
		AutoMerge:              req.AutoMerge,
		Enabled:                enabled,
		DeliveryMode:           scheduler.DeliveryMode(req.DeliveryMode),
		DeliveryWebhookURL:     req.DeliveryWebhookURL,
		FailureAlertAfter:      req.FailureAlertAfter,
		FailureAlertWebhookURL: req.FailureAlertWebhookURL,
	}

	if err := h.scheduler.CreateJob(job); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"job":     job,
	})
}

// ListJobs returns all scheduled jobs.
func (h *SchedulerHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := h.scheduler.ListJobs()
	if jobs == nil {
		jobs = []*scheduler.Job{}
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": jobs,
	})
}

// GetJob returns a single job.
func (h *SchedulerHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	job, err := h.scheduler.GetJob(jobID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}
	h.writeJSON(w, http.StatusOK, job)
}

// UpdateJob updates an existing job.
func (h *SchedulerHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	updates := &scheduler.Job{
		Name:                   req.Name,
		Description:            req.Description,
		Project:                req.Project,
		AgentID:                req.AgentID,
		Message:                req.Message,
		Model:                  req.Model,
		Schedule:               scheduler.ScheduleType(req.ScheduleType),
		CronExpr:               req.CronExpr,
		Timezone:               req.Timezone,
		EverySeconds:           req.EverySeconds,
		AtTime:                 req.AtTime,
		AutoBranch:             req.AutoBranch,
		AutoMerge:              req.AutoMerge,
		DeliveryMode:           scheduler.DeliveryMode(req.DeliveryMode),
		DeliveryWebhookURL:     req.DeliveryWebhookURL,
		FailureAlertAfter:      req.FailureAlertAfter,
		FailureAlertWebhookURL: req.FailureAlertWebhookURL,
	}
	if req.Enabled != nil {
		updates.Enabled = *req.Enabled
	} else {
		updates.Enabled = true // default
	}

	if err := h.scheduler.UpdateJob(jobID, updates); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	job, _ := h.scheduler.GetJob(jobID)
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"job":     job,
	})
}

// DeleteJob deletes a job.
func (h *SchedulerHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if err := h.scheduler.DeleteJob(jobID); err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// TriggerJob manually triggers a job execution.
func (h *SchedulerHandler) TriggerJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if err := h.scheduler.TriggerJob(jobID); err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Job triggered",
	})
}

// ListExecutions returns execution history for a job.
func (h *SchedulerHandler) ListExecutions(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	executions := h.scheduler.ListExecutions(jobID, 50)
	if executions == nil {
		executions = []*scheduler.JobExecution{}
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"executions": executions,
	})
}

// ListRuns returns persistent run log entries for a job.
func (h *SchedulerHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	statusFilter := r.URL.Query().Get("status")

	entries, err := h.scheduler.ListRuns(jobID, limit, statusFilter)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}
	if entries == nil {
		entries = []scheduler.RunLogEntry{}
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs": entries,
	})
}

// ListAllRuns returns run log entries across all jobs.
func (h *SchedulerHandler) ListAllRuns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	statusFilter := r.URL.Query().Get("status")
	jobIDFilter := r.URL.Query().Get("jobId")

	entries, err := h.scheduler.ListAllRuns(limit, statusFilter, jobIDFilter)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}
	if entries == nil {
		entries = []scheduler.RunLogEntry{}
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs": entries,
	})
}

// writeJSON writes a JSON response.
func (h *SchedulerHandler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// NewSchedulerPromptSender creates a PromptSender that sends prompts through the Pi system.
// This is the bridge between the scheduler and the Pi agent pool.
func NewSchedulerPromptSender(sendFn func(ctx context.Context, project, agentID, message, model string) (string, error)) scheduler.PromptSender {
	return func(ctx context.Context, project, agentID, message, model string, autoBranch, autoMerge bool) (string, error) {
		if sendFn == nil {
			return "", fmt.Errorf("prompt sender not configured")
		}
		output, err := sendFn(ctx, project, agentID, message, model)
		if err != nil {
			return "", err
		}
		return output, nil
	}
}

// Keep the import for time
var _ = time.Second
