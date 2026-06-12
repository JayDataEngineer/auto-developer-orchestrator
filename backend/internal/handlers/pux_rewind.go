package handlers

import (
	"fmt"
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/session"
)

// GetRewindCheckpoints handles GET /api/pux/rewind
// Returns user message checkpoints from the session tree for the rewind overlay.
func (h *PuxHandler) GetRewindCheckpoints(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentID := r.URL.Query().Get("agentId")
	if project == "" {
		project = "default"
	}
	if agentID == "" {
		agentID = "default"
	}

	sessionPath := fmt.Sprintf("%s/.pux/sessions/%s.jsonl", project, agentID)
	tree, err := session.Load(sessionPath)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	defer tree.Close()

	checkpoints := tree.GetUserCheckpoints()
	if checkpoints == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, checkpoints)
}

// RewindSession handles POST /api/pux/rewind
// Navigates the session tree to a previous user message, truncating the conversation.
func (h *PuxHandler) RewindSession(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		AgentID string `json:"agentId"`
		NodeID  string `json:"nodeId"`
	}](w, r)
	if !ok {
		return
	}
	if req.Project == "" {
		req.Project = "default"
	}
	if req.AgentID == "" {
		req.AgentID = "default"
	}
	if req.NodeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nodeId is required"})
		return
	}

	sessionPath := fmt.Sprintf("%s/.pux/sessions/%s.jsonl", req.Project, req.AgentID)
	tree, err := session.Load(sessionPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	defer tree.Close()

	if err := tree.Navigate(req.NodeID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"sessionId":   tree.ID(),
		"currentNode": tree.GetCurrentNode(),
	})
}
