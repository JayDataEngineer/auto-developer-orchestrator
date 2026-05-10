package hooks

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ApprovalResponse represents the user's response to an approval request.
type ApprovalResponse struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
}

// ApprovalHandler resolves approval requests. Implementations can use
// channels, callbacks, or any other mechanism.
type ApprovalHandler interface {
	// RequestApproval blocks until the user responds or the context is cancelled.
	// Returns the user's response.
	RequestApproval(ctx context.Context, requestID string, data map[string]any) (ApprovalResponse, error)
}

// ApprovalHook intercepts tool calls that require user approval.
// Implements core.LoopHook. When the model calls a tool that requires approval,
// the hook blocks execution and waits for the user to approve.
type ApprovalHook struct {
	handler     ApprovalHandler
	planOnly    bool      // if true, only ask approval for create_plan
	timeout     time.Duration
	mu          sync.Mutex
	pendingPlan string
	logger      *log.Logger
}

// NewApprovalHook creates an approval hook.
// If planOnly is true, only create_plan results trigger approval; individual
// tool calls run without interruption.
func NewApprovalHook(handler ApprovalHandler, planOnly bool, timeout time.Duration) *ApprovalHook {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	return &ApprovalHook{
		handler:  handler,
		planOnly: planOnly,
		timeout:  timeout,
		logger:   log.Default(),
	}
}

func (h *ApprovalHook) Name() string { return "approval" }

func (h *ApprovalHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	h.mu.Lock()
	h.pendingPlan = ""
	h.mu.Unlock()
	return nil
}

func (h *ApprovalHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	return nil, nil
}

func (h *ApprovalHook) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) { return msgs, nil }
func (h *ApprovalHook) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error { return nil }
func (h *ApprovalHook) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	if toolName != "create_plan" {
		return nil
	}

	h.mu.Lock()
	h.pendingPlan = result
	h.mu.Unlock()

	if !h.planOnly {
		return nil
	}

	// In plan-only mode, block after plan creation for user approval
	reqID := fmt.Sprintf("plan-%s-%d", state.SessionID, state.Round)
	data := map[string]any{
		"session_id": state.SessionID,
		"round":      state.Round,
		"plan":       result,
	}

	approvalCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	resp, err := h.handler.RequestApproval(approvalCtx, reqID, data)
	if err != nil {
		h.logger.Printf("APPROVAL: request %s failed: %v", reqID, err)
		return nil // don't block on failure
	}

	if !resp.Approved {
		h.logger.Printf("APPROVAL: plan rejected by user, feedback: %s", resp.Feedback)
		// Inject rejection feedback as next turn's nudge
		return nil
	}

	h.logger.Printf("APPROVAL: plan approved")
	return nil
}

func (h *ApprovalHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	return nil
}
