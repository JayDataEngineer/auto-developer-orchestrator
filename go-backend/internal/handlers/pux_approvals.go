package handlers

import (
	"encoding/json"
	"net/http"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/tools/ask"
	"github.com/auto-developer-orchestrator/backend/internal/tools/plan"
)

// respondRequest is the request body for the approval response endpoint.
type respondRequest struct {
	Project   string `json:"project"`
	AgentId   string `json:"agentId"`
	RequestID string `json:"requestId"`
	Action    string `json:"action"` // "approve", "deny", "answer"
	Message   string `json:"message,omitempty"`
}

// Respond handles user approval/denial responses for pending agent approvals.
func (h *PuxHandler) Respond(w http.ResponseWriter, r *http.Request) {
	var req respondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.RequestID == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "requestId and action are required",
		})
		return
	}

	resp := llamaeng.ApprovalResponse{
		Action:  req.Action,
		Message: req.Message,
	}

	if ok := h.approvalMgr.Resolve(req.RequestID, resp); !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "No pending approval found for this request",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// UserResponse handles responses to ask_user questions from the TUI.
// POST /api/pux/user-response
func (h *PuxHandler) UserResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuestionID string `json:"questionId"`
		Response   string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}
	if req.QuestionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "questionId is required",
		})
		return
	}

	if ok := ask.PendingQuestions.Resolve(req.QuestionID, req.Response); !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "No pending question found for this ID (may have timed out)",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// PlanResponse handles responses to create_plan approval requests from the TUI.
// POST /api/pux/plan-response
func (h *PuxHandler) PlanResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanID   string `json:"planId"`
		Action   string `json:"action"`   // "approve", "refine", "cancel"
		Feedback string `json:"feedback"` // optional, for refine
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}
	if req.PlanID == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "planId and action are required",
		})
		return
	}
	if req.Action != "approve" && req.Action != "refine" && req.Action != "cancel" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "action must be 'approve', 'refine', or 'cancel'",
		})
		return
	}

	if ok := plan.PendingPlans.Resolve(req.PlanID, plan.PlanResponse{
		Action:   req.Action,
		Feedback: req.Feedback,
	}); !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "No pending plan found for this ID (may have timed out)",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
