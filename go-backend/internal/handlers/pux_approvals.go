package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Decision handles all HITL responses.
// POST /api/pux/decision
func (h *PuxHandler) Decision(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DecisionID string `json:"decisionId"`
		Action     string `json:"action"` // "answer", "approve", "reject", "refine", "cancel"
		Value      string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}
	if req.DecisionID == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "decisionId and action are required",
		})
		return
	}

	if ok := core.GlobalDecisions.Resolve(req.DecisionID, core.DecisionResponse{
		Action: req.Action,
		Value:  req.Value,
	}); !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "No pending decision found (may have timed out)",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
