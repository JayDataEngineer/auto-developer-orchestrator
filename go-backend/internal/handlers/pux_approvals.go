package handlers

import (
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Decision handles all HITL responses.
// POST /api/pux/decision
func (h *PuxHandler) Decision(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		DecisionID string `json:"decisionId"`
		Action     string `json:"action"` // "answer", "approve", "reject", "refine", "cancel"
		Value      string `json:"value"`
	}](w, r)
	if !ok { return }
	if req.DecisionID == "" || req.Action == "" {
		JSONError(w, "decisionId and action are required", http.StatusBadRequest)
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
