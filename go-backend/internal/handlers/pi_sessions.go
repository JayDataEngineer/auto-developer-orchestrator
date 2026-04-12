package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// Abort cancels the current Pi operation.
func (h *PiHandler) Abort(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := resolveProjectPath(project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session for project",
		})
		return
	}

	if err := client.Abort(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GetState returns the current Pi session state.
func (h *PiHandler) GetState(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := resolveProjectPath(project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		writeJSON(w, http.StatusOK, pi.SessionState{})
		return
	}

	writeJSON(w, http.StatusOK, client.GetState())
}

// GetMessages returns the conversation history from the database.
func (h *PiHandler) GetMessages(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)

	if project == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Project name is required",
		})
		return
	}

	if h.db == nil {
		writeJSON(w, http.StatusOK, []storage.StoredMessage{})
		return
	}

	msgs, err := h.db.GetConversationHistory(r.Context(), project, agentId, 500)
	if err != nil {
		h.log.Error("Failed to load conversation history", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	if msgs == nil {
		msgs = []storage.StoredMessage{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// GetHistory returns conversation summaries grouped by project.
func (h *PiHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeJSON(w, http.StatusOK, []storage.ConversationSummary{})
		return
	}

	summaries, err := h.db.GetConversationSummaries(r.Context())
	if err != nil {
		h.log.Error("Failed to get conversation summaries", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	if summaries == nil {
		summaries = []storage.ConversationSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"conversations": summaries,
	})
}

// DeleteConversation deletes all messages for a project+agent conversation.
func (h *PiHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := r.URL.Query().Get("agentId")
	if project == "" || agentId == "" {
		JSONError(w, "project and agentId required", http.StatusBadRequest)
		return
	}

	if h.db == nil {
		JSONError(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.db.ClearConversationHistory(r.Context(), project, agentId); err != nil {
		h.log.Error("Failed to delete conversation", zap.Error(err))
		JSONError(w, "failed to delete conversation", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// RenameConversation sets a custom title for a conversation.
func (h *PiHandler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := r.URL.Query().Get("agentId")
	if project == "" || agentId == "" {
		JSONError(w, "project and agentId required", http.StatusBadRequest)
		return
	}

	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if h.db == nil {
		JSONError(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.db.SetConversationTitle(r.Context(), project, agentId, body.Title); err != nil {
		h.log.Error("Failed to rename conversation", zap.Error(err))
		JSONError(w, "failed to rename conversation", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"renamed": true})
}

// ListSessions lists saved Pi sessions.
func (h *PiHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := resolveProjectPath(project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		writeJSON(w, http.StatusOK, []pi.SessionInfo{})
		return
	}

	if err := client.ListSessions(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, []pi.SessionInfo{})
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
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	agentId := req.AgentId
	if agentId == "" {
		agentId = "default"
	}

	projectPath := resolveProjectPath(req.Project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session - send a prompt first",
		})
		return
	}

	if err := client.SwitchSession(req.SessionId); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ListActive returns all active Pi sessions grouped by project.
func (h *PiHandler) ListActive(w http.ResponseWriter, r *http.Request) {
	allActive := h.pool.ListAllActive()

	type agentInfo struct {
		AgentId string          `json:"agentId"`
		State   pi.SessionState `json:"state"`
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Project == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Project is required",
		})
		return
	}

	// Auto-generate agentId if not provided
	if req.AgentId == "" {
		req.AgentId = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}

	projectPath := resolveProjectPath(req.Project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client, err := h.pool.GetOrCreateWithID(projectPath, req.AgentId)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	_ = client // Client is started, kept alive in pool

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Project == "" || req.AgentId == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Project and agentId are required",
		})
		return
	}

	projectPath := resolveProjectPath(req.Project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	h.pool.RemoveAgent(projectPath, req.AgentId)

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
