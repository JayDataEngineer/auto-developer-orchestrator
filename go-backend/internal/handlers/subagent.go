package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SubAgentHandler handles HTTP endpoints for sub-agent operations.
type SubAgentHandler struct {
	manager *pi.SubAgentManager
	pool    *pi.PiPool
	logger  *zap.Logger
}

// NewSubAgentHandler creates a new sub-agent handler.
func NewSubAgentHandler(manager *pi.SubAgentManager, pool *pi.PiPool, logger *zap.Logger) *SubAgentHandler {
	return &SubAgentHandler{
		manager: manager,
		pool:    pool,
		logger:  logger,
	}
}

// RegisterRoutes registers all sub-agent routes on the given router.
func (h *SubAgentHandler) RegisterRoutes(r chi.Router) {
	r.Post("/spawn", h.Spawn)
	r.Get("/status", h.Status)
	r.Get("/result", h.Result)
	r.Post("/abort", h.Abort)
	r.Get("/list", h.ListByParent)
}

// spawnRequest is the request body for the spawn endpoint.
type spawnRequest struct {
	Project       string `json:"project"`
	ParentAgentId string `json:"parentAgentId"`
	Type          string `json:"type"`
	Task          string `json:"task"`
	Model         string `json:"model,omitempty"`
}

// Spawn creates a new sub-agent and returns its ID immediately.
// POST /api/pi/subagent/spawn
func (h *SubAgentHandler) Spawn(w http.ResponseWriter, r *http.Request) {
	var req spawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeSubJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Project == "" || req.ParentAgentId == "" || req.Task == "" {
		h.writeSubJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "project, parentAgentId, and task are required",
		})
		return
	}

	projectPath := h.resolveProjectPath(req.Project)
	if projectPath == "" {
		h.writeSubJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	cfg := pi.SubAgentConfig{
		ProjectDir: projectPath,
		ParentID:   req.ParentAgentId,
		Type:       pi.SubAgentType(req.Type),
		Task:       req.Task,
		Model:      req.Model,
	}

	subAgentID, err := h.manager.Spawn(r.Context(), cfg)
	if err != nil {
		h.logger.Error("Failed to spawn sub-agent", zap.Error(err))
		h.writeSubJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeSubJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"subAgentId": subAgentID,
	})
}

// Status returns the current non-blocking status of a sub-agent.
// GET /api/pi/subagent/status?subAgentId=X
func (h *SubAgentHandler) Status(w http.ResponseWriter, r *http.Request) {
	subAgentID := r.URL.Query().Get("subAgentId")
	if subAgentID == "" {
		h.writeSubJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "subAgentId is required",
		})
		return
	}

	status, err := h.manager.GetStatus(subAgentID)
	if err != nil {
		h.writeSubJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeSubJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"subAgentId": subAgentID,
		"status":     string(status),
	})
}

// Result streams sub-agent events via SSE and sends the final result.
// GET /api/pi/subagent/result?subAgentId=X
func (h *SubAgentHandler) Result(w http.ResponseWriter, r *http.Request) {
	subAgentID := r.URL.Query().Get("subAgentId")
	if subAgentID == "" {
		h.writeSubJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "subAgentId is required",
		})
		return
	}

	inst, err := h.manager.GetInstance(subAgentID)
	if err != nil {
		h.writeSubJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	// Check if already complete
	if inst.IsTerminalState() {
		result := inst.GetResult()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		data, _ := json.Marshal(result)
		fmt.Fprintf(w, "event: subagent_result\ndata: %s\n\n", string(data))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// Set up SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	// Subscribe to the sub-agent's PiClient events
	subID := fmt.Sprintf("sub-sse-%d", time.Now().UnixNano())
	events := inst.Client.Subscribe(subID)
	defer inst.Client.Unsubscribe(subID)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-inst.Done:
			// Agent finished — send final result
			result := inst.GetResult()
			if result != nil {
				data, _ := json.Marshal(result)
				fmt.Fprintf(w, "event: subagent_result\ndata: %s\n\n", string(data))
				if canFlush {
					flusher.Flush()
				}
			}
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			// Map and forward events (reuse the same mapping pattern as PiHandler)
			sseData, _ := json.Marshal(map[string]interface{}{
				"subAgentId": subAgentID,
				"event":      event.Type,
			})
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(sseData))
			if canFlush {
				flusher.Flush()
			}
		}
	}
}

// abortRequest is the request body for the abort endpoint.
type abortSubAgentRequest struct {
	SubAgentId string `json:"subAgentId"`
}

// Abort cancels a running sub-agent.
// POST /api/pi/subagent/abort
func (h *SubAgentHandler) Abort(w http.ResponseWriter, r *http.Request) {
	var req abortSubAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeSubJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.SubAgentId == "" {
		h.writeSubJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "subAgentId is required",
		})
		return
	}

	if err := h.manager.Abort(req.SubAgentId); err != nil {
		h.writeSubJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeSubJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ListByParent lists all sub-agents for a parent agent.
// GET /api/pi/subagent/list?parentAgentId=X&project=Y
func (h *SubAgentHandler) ListByParent(w http.ResponseWriter, r *http.Request) {
	parentAgentId := r.URL.Query().Get("parentAgentId")
	if parentAgentId == "" {
		h.writeSubJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "parentAgentId is required",
		})
		return
	}

	results := h.manager.ListByParent(parentAgentId)
	if results == nil {
		results = []pi.SubAgentResult{}
	}

	h.writeSubJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"subAgents":  results,
	})
}

// resolveProjectPath resolves a project name to its filesystem path.
func (h *SubAgentHandler) resolveProjectPath(project string) string {
	if project == "" {
		return ""
	}

	projectsDir := os.Getenv("PROJECT_ROOT")
	if projectsDir == "" {
		projectsDir = "/app/projects"
	}

	candidate := filepath.Join(projectsDir, project)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}

	return ""
}

// writeSubJSON writes a JSON response.
func (h *SubAgentHandler) writeSubJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
