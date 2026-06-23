package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/auto-developer-orchestrator/backend/internal/util"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// JobsHandler handles one-shot job submission for external agents.
type JobsHandler struct {
	scheduler *scheduler.Scheduler
	log       *zap.Logger
	apiKey    string // optional API key (empty = no auth)
	baseURL   string // local server base URL (e.g. "http://localhost:3847")
}

// NewJobsHandler creates a new jobs handler.
func NewJobsHandler(sched *scheduler.Scheduler, baseURL string, logger *zap.Logger) *JobsHandler {
	return &JobsHandler{
		scheduler: sched,
		baseURL:   baseURL,
		log:       logger,
	}
}

// SetAPIKey configures optional API key authentication.
func (h *JobsHandler) SetAPIKey(key string) {
	h.apiKey = key
}

// RegisterRoutes registers all jobs routes on the given router.
func (h *JobsHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.SubmitJob)
	r.Get("/{id}", h.GetJobStatus)
	r.Delete("/{id}", h.DeleteJob)
}

// submitJobRequest is the simplified request body for POST /api/jobs.
type submitJobRequest struct {
	Task           string `json:"task"`                    // required — the agent prompt
	Project        string `json:"project,omitempty"`       // optional — project path or name
	Org            string `json:"org,omitempty"`           // optional — org name
	Model          string `json:"model,omitempty"`         // optional — model override
	FullSandbox    bool   `json:"full_sandbox,omitempty"`  // sandboxOnly mode
	Wait           bool   `json:"wait,omitempty"`          // block with SSE stream until completion
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"` // max execution time (default: 600)
	Name           string `json:"name,omitempty"`          // descriptive name
}

// SubmitJob creates a one-shot manual job and triggers it.
// With wait=false (default), returns 202 Accepted immediately.
// With wait=true, proxies the SSE stream from /api/pux/prompt.
func (h *JobsHandler) SubmitJob(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false, "error": "Invalid or missing API key",
		})
		return
	}

	req, ok := decodeReq[submitJobRequest](w, r)
	if !ok {
		return
	}

	if req.Task == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "task is required",
		})
		return
	}

	// Auto-generate name from task if not provided
	name := req.Name
	if name == "" {
		name = "oneshot: " + util.Truncate(req.Task, 40)
	}

	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = 600
	}
	if timeout > 3600 {
		timeout = 3600
	}

	// Resolve org to project path
	effectiveProject := req.Project
	if req.Org != "" {
		if resolved, err := resolveOrgPathForJob(req.Org); err == nil {
			effectiveProject = resolved
		} else {
			h.log.Warn("failed to resolve org, using project as-is",
				zap.String("org", req.Org), zap.Error(err))
		}
	}

	job := &scheduler.Job{
		Name:        name,
		Description: "oneshot", // marker for cleanup
		Project:     effectiveProject,
		Message:     req.Task,
		Model:       req.Model,
		Org:         req.Org,
		Schedule:    scheduler.ScheduleManual,
		Enabled:     false,
		SandboxOnly: req.FullSandbox,
			TimeoutSeconds: timeout,
	}

	if err := h.scheduler.CreateJob(job); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	// Trigger the job (runs async via goroutine)
	if err := h.scheduler.TriggerJob(job.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	// Async mode: return immediately with job ID
	if !req.Wait {
		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"success": true,
			"jobId":   job.ID,
			"status":  "running",
			"pollUrl": "/api/jobs/" + job.ID,
		})
		return
	}

	// Sync mode: proxy SSE stream from /api/pux/prompt
	h.proxySSEStream(w, r, job, effectiveProject, timeout)
}

// proxySSEStream forwards the agent's SSE stream to the caller.
func (h *JobsHandler) proxySSEStream(w http.ResponseWriter, r *http.Request, job *scheduler.Job, project string, timeout int) {
	payload := map[string]interface{}{
		"message":     job.Message,
		"project":     project,
		"autoBranch":  false,
		"autoMerge":   false,
		"sandboxOnly": job.SandboxOnly,
	}
	if job.Model != "" {
		payload["model"] = job.Model
	}

	data, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": "Failed to build prompt request",
		})
		return
	}

	ctx := r.Context()
	promptURL := h.baseURL + "/api/pux/prompt"
	promptReq, err := http.NewRequestWithContext(ctx, http.MethodPost, promptURL, bytes.NewReader(data))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": "Failed to create prompt request",
		})
		return
	}
	promptReq.Header.Set("Content-Type", "application/json")
	promptReq.Header.Set("Accept", "text/event-stream")
	// Mark as non-interactive: the job's CTO has no human watching, so the
	// permission hook should auto-approve "ask" patterns instead of hanging 5min.
	promptReq.Header.Set("X-Pux-Non-Interactive", "1")

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Do(promptReq)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("Prompt request failed: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			writeJSON(w, resp.StatusCode, map[string]interface{}{
				"success": false, "error": errResp.Error,
			})
		} else {
			writeJSON(w, resp.StatusCode, map[string]interface{}{
				"success": false, "error": fmt.Sprintf("Prompt returned %d", resp.StatusCode),
			})
		}
		return
	}

	// Set SSE headers and pipe the stream
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(w, line)

		// Flush on empty lines (SSE event boundary)
		if canFlush && line == "" {
			flusher.Flush()
		}
	}

	if canFlush {
		flusher.Flush()
	}
}

// GetJobStatus returns the current status of a one-shot job.
func (h *JobsHandler) GetJobStatus(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false, "error": "Invalid or missing API key",
		})
		return
	}

	jobID := chi.URLParam(r, "id")
	job, err := h.scheduler.GetJob(jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Job not found",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobId":        job.ID,
		"status":       string(job.Status),
		"output":       job.LastOutput,
		"error":        job.LastError,
		"createdAt":    job.CreatedAt.Format(time.RFC3339),
		"completedAt":  formatJobTime(job.LastRunAt),
		"durationMs":   job.DurationMs,
		"inputTokens":  job.InputTokens,
		"outputTokens": job.OutputTokens,
	})
}

// DeleteJob removes a one-shot job. Only works on jobs with Description == "oneshot".
func (h *JobsHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false, "error": "Invalid or missing API key",
		})
		return
	}

	jobID := chi.URLParam(r, "id")
	job, err := h.scheduler.GetJob(jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Job not found",
		})
		return
	}

	if job.Description != "oneshot" {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success": false, "error": "Only one-shot jobs can be deleted via this endpoint",
		})
		return
	}

	if err := h.scheduler.DeleteJob(jobID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// checkAuth validates the API key if one is configured.
func (h *JobsHandler) checkAuth(r *http.Request) bool {
	if h.apiKey == "" {
		return true
	}
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = r.URL.Query().Get("api_key")
	}
	return subtleCompare(key, h.apiKey)
}

// subtleCompare does a constant-time string comparison.
func subtleCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// resolveOrgPathForJob resolves an org name to a filesystem path.
func resolveOrgPathForJob(name string) (string, error) {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".pux", "orgs", name),
		filepath.Join(home, "Documents", "programs", "dev", name),
		filepath.Join(home, "Documents", "projects", name),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "pux.yaml")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("organization '%s' not found", name)
}

// formatJobTime returns RFC3339 or empty string for zero time.
func formatJobTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
