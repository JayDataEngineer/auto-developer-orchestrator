package core

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DecisionHint tells the frontend which UI component to render.
type DecisionHint string

const (
	HintQuestion   DecisionHint = "question"    // text input, optional option list
	HintApproval   DecisionHint = "approval"    // approve/reject binary
	HintPlanReview DecisionHint = "plan_review" // approve/refine/cancel with feedback
)

// DecisionRequest is the payload sent to the frontend via SSE.
type DecisionRequest struct {
	ID            string         // unique decision ID
	SourceTool    string         // tool that created this decision (e.g., "ask_user", "create_plan")
	Title         string         // primary text (question text, plan name, approval title)
	Description   string         // secondary text (plan content, approval description)
	Hint          DecisionHint   // which UI component to render
	Options       []string       // for HintQuestion: multiple-choice options
	AllowFreeText bool           // for HintQuestion: can user type a custom answer?
	Metadata      map[string]any // tool-specific data (e.g., filePath for plans)
}

// DecisionResponse is what the frontend sends back.
type DecisionResponse struct {
	Action string // "answer", "approve", "reject", "refine", "cancel"
	Value  string // user's text answer, selected option, or feedback
}

// DecisionRegistry is the single HITL registry. All tools that need human input
// register here, and the single POST /api/pux/decision endpoint resolves.
type DecisionRegistry struct {
	mu      sync.Mutex
	pending map[string]chan DecisionResponse
}

// GlobalDecisions is the process-wide decision registry.
var GlobalDecisions = &DecisionRegistry{
	pending: make(map[string]chan DecisionResponse),
}

// NewDecisionRegistry constructs a fresh registry. Use this in tests instead
// of mutating GlobalDecisions, and in production code that needs isolated
// decision state (e.g., sub-agent workers that may run in parallel).
func NewDecisionRegistry() *DecisionRegistry {
	return &DecisionRegistry{pending: make(map[string]chan DecisionResponse)}
}

// Register creates a pending decision and returns a channel that receives the response.
func (r *DecisionRegistry) Register(id string) chan DecisionResponse {
	ch := make(chan DecisionResponse, 1)
	r.mu.Lock()
	r.pending[id] = ch
	r.mu.Unlock()
	return ch
}

// Resolve delivers a response to a pending decision. Returns false if not found.
func (r *DecisionRegistry) Resolve(id string, resp DecisionResponse) bool {
	r.mu.Lock()
	ch, ok := r.pending[id]
	if ok {
		delete(r.pending, id)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- resp
	return true
}

// Cleanup removes a pending decision without resolving (e.g. on context cancel).
func (r *DecisionRegistry) Cleanup(id string) {
	r.mu.Lock()
	ch, ok := r.pending[id]
	if ok {
		delete(r.pending, id)
	}
	r.mu.Unlock()
	if ok {
		select {
		case ch <- DecisionResponse{Action: "cancel"}:
		default:
		}
	}
}

// WaitForDecision is a convenience: register, emit SSE event, block until response.
// Returns the user's response or an error on timeout/cancellation.
func (r *DecisionRegistry) WaitForDecision(
	ctx context.Context,
	req DecisionRequest,
	subscriber chan<- AgentEvent,
	timeout time.Duration,
) (DecisionResponse, error) {
	ch := r.Register(req.ID)

	// Emit decision_request SSE event with full payload
	if subscriber != nil {
		SendEvent(subscriber, AgentEvent{
			Type: EventTypeDecisionRequest,
			Data: DecisionRequestData{
				ID:            req.ID,
				SourceTool:    req.SourceTool,
				Title:         req.Title,
				Description:   req.Description,
				Hint:          string(req.Hint),
				Options:       req.Options,
				AllowFreeText: req.AllowFreeText,
				Metadata:      req.Metadata,
			},
		})
	}

	if timeout > 0 {
		select {
		case resp := <-ch:
			return resp, nil
		case <-ctx.Done():
			r.Cleanup(req.ID)
			return DecisionResponse{}, ctx.Err()
		case <-time.After(timeout):
			r.Cleanup(req.ID)
			return DecisionResponse{}, fmt.Errorf("decision timed out after %v", timeout)
		}
	}

	// No timeout — wait indefinitely for user response
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		r.Cleanup(req.ID)
		return DecisionResponse{}, ctx.Err()
	}
}
