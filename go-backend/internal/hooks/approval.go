package hooks

import (
	"context"
	"encoding/json"
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

// SetLogger sets a custom logger.
func (h *ApprovalHook) SetLogger(logger *log.Logger) {
	h.logger = logger
}

// ChannelApprovalHandler implements ApprovalHandler using Go channels.
// This is the simplest implementation — register a request, get a channel,
// then someone sends a response to the channel.
type ChannelApprovalHandler struct {
	mu       sync.Mutex
	channels map[string]chan ApprovalResponse
}

// NewChannelApprovalHandler creates a channel-based approval handler.
func NewChannelApprovalHandler() *ChannelApprovalHandler {
	return &ChannelApprovalHandler{
		channels: make(map[string]chan ApprovalResponse),
	}
}

// RequestApproval creates a channel and blocks until a response is received.
func (h *ChannelApprovalHandler) RequestApproval(ctx context.Context, requestID string, data map[string]any) (ApprovalResponse, error) {
	ch := make(chan ApprovalResponse, 1)

	h.mu.Lock()
	h.channels[requestID] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.channels, requestID)
		h.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return ApprovalResponse{Approved: false, Feedback: "timeout"}, ctx.Err()
	case resp := <-ch:
		return resp, nil
	}
}

// Resolve sends a response to a pending approval request.
func (h *ChannelApprovalHandler) Resolve(requestID string, resp ApprovalResponse) bool {
	h.mu.Lock()
	ch, ok := h.channels[requestID]
	h.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case ch <- resp:
		return true
	default:
		return false
	}
}

// PendingRequests returns the IDs of all pending approval requests.
func (h *ChannelApprovalHandler) PendingRequests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	ids := make([]string, 0, len(h.channels))
	for id := range h.channels {
		ids = append(ids, id)
	}
	return ids
}

// MarshalPlan extracts a plan from a tool result string.
func MarshalPlan(result string) (map[string]any, error) {
	var plan map[string]any
	if err := json.Unmarshal([]byte(result), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan JSON: %w", err)
	}
	return plan, nil
}
