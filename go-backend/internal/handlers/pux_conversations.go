package handlers

import (
	"fmt"
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

// GetHistory returns conversation history for a project+agent.
// GET /api/pux/history?project=...&agentId=...&limit=...
func (h *PuxHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentID := r.URL.Query().Get("agentId")
	if project == "" {
		JSONError(w, "project query parameter is required", http.StatusBadRequest)
		return
	}
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err != nil || n != 1 {
			limit = 200
		}
	}

	if h.db == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	msgs, err := h.db.GetConversationHistory(r.Context(), project, agentID, limit)
	if err != nil {
		JSONError(w, "Failed to get history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// GetConversations returns a summary list of all conversations.
// GET /api/pux/conversations
func (h *PuxHandler) GetConversations(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	project := r.URL.Query().Get("project")
	summaries, err := h.db.GetConversationSummaries(r.Context())
	if err != nil {
		JSONError(w, "Failed to get conversations", http.StatusInternalServerError)
		return
	}
	if summaries == nil {
		summaries = []storage.ConversationSummary{}
	}
	if project != "" {
		filtered := make([]storage.ConversationSummary, 0, len(summaries))
		for _, s := range summaries {
			if s.Project == project {
				filtered = append(filtered, s)
			}
		}
		summaries = filtered
	}
	// Enrich with running status
	for i := range summaries {
		if h.registry.IsRunning(summaries[i].Project, summaries[i].AgentID) {
			summaries[i].Status = "running"
		}
	}
	writeJSON(w, http.StatusOK, summaries)
}

// DeleteConversation deletes all messages for a project+agent.
// DELETE /api/pux/conversation?project=...&agentId=...
func (h *PuxHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentID := r.URL.Query().Get("agentId")
	if project == "" {
		JSONError(w, "project query parameter is required", http.StatusBadRequest)
		return
	}

	if h.db == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	if err := h.db.ClearConversationHistory(r.Context(), project, agentID); err != nil {
		JSONError(w, "Failed to delete conversation", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// RenameConversation sets a custom title for a conversation.
// PUT /api/pux/conversation/rename
func (h *PuxHandler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		AgentID string `json:"agentId"`
		Title   string `json:"title"`
	}](w, r)
	if !ok {
		return
	}
	if req.Project == "" || req.Title == "" {
		JSONError(w, "project and title are required", http.StatusBadRequest)
		return
	}

	if h.db == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	if err := h.db.SetConversationTitle(r.Context(), req.Project, req.AgentID, req.Title); err != nil {
		JSONError(w, "Failed to rename conversation", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
