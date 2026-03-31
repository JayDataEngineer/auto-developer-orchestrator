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
	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// PiHandler handles Pi agent HTTP endpoints.
type PiHandler struct {
	pool *pi.PiPool
	db   *storage.Database
	git  *git.GitOps
	log  *zap.Logger
}

// NewPiHandler creates a new Pi handler.
func NewPiHandler(pool *pi.PiPool, db *storage.Database, gitOps *git.GitOps, logger *zap.Logger) *PiHandler {
	return &PiHandler{
		pool: pool,
		db:   db,
		git:  gitOps,
		log:  logger,
	}
}

// RegisterRoutes registers all Pi routes on the given router.
func (h *PiHandler) RegisterRoutes(r chi.Router) {
	r.Post("/prompt", h.Prompt)
	r.Post("/abort", h.Abort)
	r.Get("/state", h.GetState)
	r.Get("/messages", h.GetMessages)
	r.Get("/models", h.GetModels)
	r.Put("/model", h.SetModel)
	r.Post("/compact", h.Compact)
	r.Get("/sessions", h.ListSessions)
	r.Put("/session", h.SwitchSession)
	r.Get("/active", h.ListActive)
	r.Post("/agent/spawn", h.SpawnAgent)
	r.Post("/agent/destroy", h.DestroyAgent)
}

// resolveAgent reads ?agentId= from the query string, defaulting to "default".
func resolveAgent(r *http.Request) string {
	aid := r.URL.Query().Get("agentId")
	if aid == "" {
		return "default"
	}
	return aid
}

// promptRequest is the request body for the prompt endpoint.
type promptRequest struct {
	Message       string `json:"message"`
	Project       string `json:"project"`
	AgentId       string `json:"agentId,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	AutoBranch    bool   `json:"autoBranch,omitempty"`
}

// Prompt sends a coding task to Pi and streams events back via SSE.
func (h *PiHandler) Prompt(w http.ResponseWriter, r *http.Request) {
	var req promptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Message == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Message is required",
		})
		return
	}

	// Auto-generate agentId if not provided
	if req.AgentId == "" {
		req.AgentId = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}

	projectPath := h.resolveProjectPath(req.Project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client, err := h.pool.GetOrCreateWithID(projectPath, req.AgentId)
	if err != nil {
		h.log.Error("Failed to get Pi client", zap.Error(err))
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("Failed to start Pi agent: %v", err),
		})
		return
	}

	// Auto-branch if requested
	var autoBranchName string
	if req.AutoBranch && h.git != nil {
		autoBranchName = fmt.Sprintf("pi/task-%d", time.Now().Unix())
		if err := h.git.Checkout(r.Context(), git.CheckoutOptions{
			Dir:       projectPath,
			Branch:    autoBranchName,
			CreateNew: true,
		}); err != nil {
			h.log.Warn("Auto-branch failed (non-fatal)", zap.Error(err), zap.String("branch", autoBranchName))
			autoBranchName = ""
		}
	}

	// Set up SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	// Send agent_spawned event
	spawnData, _ := json.Marshal(map[string]string{"agentId": req.AgentId})
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", pi.EventAgentSpawned, string(spawnData))
	if canFlush {
		flusher.Flush()
	}

	// Send branch_created event if auto-branched
	if autoBranchName != "" {
		data, _ := json.Marshal(map[string]string{"branch": autoBranchName})
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", pi.EventBranchCreated, string(data))
		if canFlush {
			flusher.Flush()
		}
	}

	// Subscribe to events
	subId := fmt.Sprintf("sse-%d", r.Context().Value("requestID"))
	events := client.Subscribe(subId)
	defer client.Unsubscribe(subId)

	// Send prompt command
	if err := client.SendPrompt(req.Message, req.Model, req.ThinkingLevel); err != nil {
		h.log.Error("Failed to send prompt to Pi", zap.Error(err))
		http.Error(w, "Failed to send prompt", http.StatusInternalServerError)
		return
	}

	// Stream events to SSE
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			sseEvent := h.mapEventToSSE(event)
			if sseEvent == nil {
				continue
			}
			data, err := json.Marshal(sseEvent.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEvent.Type, string(data))
			if canFlush {
				flusher.Flush()
			}
		}
	}
}

// sseEvent is a simplified event for the frontend.
type sseEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// mapEventToSSE converts a Pi RPC event to an SSE event for the frontend.
func (h *PiHandler) mapEventToSSE(event pi.AgentEvent) *sseEvent {
	switch event.Type {
	case pi.RpcEventMessageUpdate:
		// Pi sends message updates with text deltas
		return &sseEvent{
			Type: pi.EventTextDelta,
			Data: map[string]string{"text": event.Data.Text},
		}
	case pi.RpcEventToolStart:
		return &sseEvent{
			Type: pi.EventToolStart,
			Data: map[string]interface{}{
				"toolName": event.Data.ToolName,
				"args":     event.Data.ToolArgs,
				"toolId":   event.Data.ToolId,
			},
		}
	case pi.RpcEventToolEnd:
		return &sseEvent{
			Type: pi.EventToolEnd,
			Data: map[string]interface{}{
				"toolName": event.Data.ToolName,
				"toolId":   event.Data.ToolId,
				"result":   event.Data.Result,
				"error":    event.Data.Error,
			},
		}
	case pi.RpcEventAgentStart:
		return &sseEvent{
			Type: pi.EventAgentStart,
			Data: map[string]interface{}{},
		}
	case pi.RpcEventAgentEnd:
		return &sseEvent{
			Type: pi.EventAgentEnd,
			Data: map[string]interface{}{
				"input":  event.Data.Input,
				"output": event.Data.Output,
				"cache":  event.Data.Cache,
			},
		}
	case pi.RpcEventCompactionStart:
		return &sseEvent{
			Type: pi.EventCompactionStart,
			Data: map[string]interface{}{},
		}
	case pi.RpcEventCompactionEnd:
		return &sseEvent{
			Type: pi.EventCompactionEnd,
			Data: map[string]interface{}{
				"compactedMessages": event.Data.CompactedMessages,
				"keptMessages":      event.Data.KeptMessages,
			},
		}
	case pi.RpcEventError:
		return &sseEvent{
			Type: pi.EventError,
			Data: map[string]string{"error": event.Data.Error},
		}
	case pi.RpcEventResponse:
		// Response events contain state data
		return &sseEvent{
			Type: pi.EventStateUpdate,
			Data: map[string]interface{}{
				"model":  event.Data.Model,
				"input":  event.Data.Input,
				"output": event.Data.Output,
				"cache":  event.Data.Cache,
			},
		}
	default:
		return nil
	}
}

// Abort cancels the current Pi operation.
func (h *PiHandler) Abort(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := h.resolveProjectPath(project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session for project",
		})
		return
	}

	if err := client.Abort(); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GetState returns the current Pi session state.
func (h *PiHandler) GetState(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := h.resolveProjectPath(project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		h.writeJSON(w, http.StatusOK, pi.SessionState{})
		return
	}

	h.writeJSON(w, http.StatusOK, client.GetState())
}

// GetMessages returns the conversation history.
func (h *PiHandler) GetMessages(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := h.resolveProjectPath(project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		h.writeJSON(w, http.StatusOK, []pi.AgentMessage{})
		return
	}

	// Request messages from Pi - they'll come as events
	// For now return empty, frontend subscribes to SSE for updates
	if err := client.GetMessages(); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, []pi.AgentMessage{})
}

// GetModels lists available models.
func (h *PiHandler) GetModels(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := h.resolveProjectPath(project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client, err := h.pool.GetOrCreateWithID(projectPath, agentId)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	// Request models from Pi
	if err := client.GetAvailableModels(); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": []pi.ModelInfo{},
	})
}

// setModelRequest is the request body for SetModel.
type setModelRequest struct {
	Project  string `json:"project"`
	Provider string `json:"provider"`
	ModelId  string `json:"modelId"`
	AgentId  string `json:"agentId,omitempty"`
}

// SetModel switches the active model.
func (h *PiHandler) SetModel(w http.ResponseWriter, r *http.Request) {
	var req setModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	agentId := req.AgentId
	if agentId == "" {
		agentId = "default"
	}

	projectPath := h.resolveProjectPath(req.Project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session - send a prompt first",
		})
		return
	}

	if err := client.SetModel(req.Provider, req.ModelId); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Compact triggers context compaction.
func (h *PiHandler) Compact(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := h.resolveProjectPath(project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session - send a prompt first",
		})
		return
	}

	if err := client.Compact(); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ListSessions lists saved Pi sessions.
func (h *PiHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := h.resolveProjectPath(project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		h.writeJSON(w, http.StatusOK, []pi.SessionInfo{})
		return
	}

	if err := client.ListSessions(); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, []pi.SessionInfo{})
}

// switchSessionRequest is the request body for SwitchSession.
type switchSessionRequest struct {
	Project   string `json:"project"`
	SessionId string `json:"sessionId"`
	AgentId   string `json:"agentId,omitempty"`
}

// SwitchSession switches to a different Pi session.
func (h *PiHandler) SwitchSession(w http.ResponseWriter, r *http.Request) {
	var req switchSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	agentId := req.AgentId
	if agentId == "" {
		agentId = "default"
	}

	projectPath := h.resolveProjectPath(req.Project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session - send a prompt first",
		})
		return
	}

	if err := client.SwitchSession(req.SessionId); err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ListActive returns all active Pi sessions grouped by project.
func (h *PiHandler) ListActive(w http.ResponseWriter, r *http.Request) {
	allActive := h.pool.ListAllActive()

	type agentInfo struct {
		AgentId   string          `json:"agentId"`
		State     pi.SessionState `json:"state"`
	}

	type projectGroup struct {
		Project string      `json:"project"`
		Agents  []agentInfo `json:"agents"`
	}

	groups := make([]projectGroup, 0, len(allActive))
	for projectPath, agents := range allActive {
		agentInfos := make([]agentInfo, 0, len(agents))
		for _, a := range agents {
			agentInfos = append(agentInfos, agentInfo{
				AgentId: a.AgentId,
				State:   a.State,
			})
		}
		groups = append(groups, projectGroup{
			Project: filepath.Base(projectPath),
			Agents:  agentInfos,
		})
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects": groups,
	})
}

// spawnAgentRequest is the request body for SpawnAgent.
type spawnAgentRequest struct {
	Project string `json:"project"`
	AgentId string `json:"agentId,omitempty"`
}

// SpawnAgent starts a new Pi subprocess for a project and returns its agentId.
func (h *PiHandler) SpawnAgent(w http.ResponseWriter, r *http.Request) {
	var req spawnAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Project == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Project is required",
		})
		return
	}

	// Auto-generate agentId if not provided
	if req.AgentId == "" {
		req.AgentId = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}

	projectPath := h.resolveProjectPath(req.Project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client, err := h.pool.GetOrCreateWithID(projectPath, req.AgentId)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	_ = client // Client is started, kept alive in pool

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"agentId": req.AgentId,
	})
}

// destroyAgentRequest is the request body for DestroyAgent.
type destroyAgentRequest struct {
	Project string `json:"project"`
	AgentId string `json:"agentId"`
}

// DestroyAgent shuts down a specific agent.
func (h *PiHandler) DestroyAgent(w http.ResponseWriter, r *http.Request) {
	var req destroyAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Project == "" || req.AgentId == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Project and agentId are required",
		})
		return
	}

	projectPath := h.resolveProjectPath(req.Project)
	if projectPath == "" {
		h.writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	h.pool.RemoveAgent(projectPath, req.AgentId)

	h.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// resolveProjectPath resolves a project name to its filesystem path.
func (h *PiHandler) resolveProjectPath(project string) string {
	if project == "" {
		return ""
	}

	// First check database for custom project paths
	if h.db != nil {
		ctx := context.Background()
		customProjects, err := h.db.GetCustomProjects(ctx)
		if err == nil {
			for _, p := range customProjects {
				if p.Name == project {
					// Skip non-filesystem paths (e.g. jules:// URLs from legacy data)
					if strings.HasPrefix(p.Path, "jules://") || strings.Contains(p.Path, "://") {
						break
					}
					return p.Path
				}
			}
		}
	}

	// Default: use projects dir
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

// writeJSON writes a JSON response with the given status code.
func (h *PiHandler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
