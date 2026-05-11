package hooks

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SSEHookBridge allows external subscribers (e.g., the TUI via SSE) to
// intercept agent loop events and respond. Uses the same channel-based
// pattern as the approval system: the loop emits a hook_request event,
// pauses, and waits for a response via the registered channel.
//
// Supported hook points:
//   - "tool_call": intercept before tool execution (can block/modify)
//   - "tool_result": intercept after tool execution (can modify result)
//   - "context": intercept before LLM call (can modify messages)
type SSEHookBridge struct {
	mu       sync.Mutex
	pending  map[string]chan HookResponse // hookID → response channel
	timeout  time.Duration
	disabled bool // if true, all hooks pass through without blocking
	logger   *log.Logger
}

// HookResponse is the response from the TUI to a hook request.
type HookResponse struct {
	// Action: "allow" (proceed), "block" (prevent execution), "modify" (change args/result)
	Action string `json:"action"`
	// ModifiedData contains modified args (for tool_call) or modified result (for tool_result)
	ModifiedData map[string]any `json:"modifiedData,omitempty"`
	// Reason is an optional explanation for the action
	Reason string `json:"reason,omitempty"`
}

// NewSSEHookBridge creates a new SSE hook bridge.
func NewSSEHookBridge(timeout time.Duration) *SSEHookBridge {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &SSEHookBridge{
		pending: make(map[string]chan HookResponse),
		timeout: timeout,
		logger:  log.Default(),
	}
}

// SetDisabled controls whether hooks block or pass through.
func (b *SSEHookBridge) SetDisabled(disabled bool) {
	b.mu.Lock()
	b.disabled = disabled
	b.mu.Unlock()
}

// Register creates a response channel for a hook ID and returns it.
// The caller (HTTP handler) uses this to deliver responses from the TUI.
func (b *SSEHookBridge) Register(hookID string) chan HookResponse {
	ch := make(chan HookResponse, 1)
	b.mu.Lock()
	b.pending[hookID] = ch
	b.mu.Unlock()
	return ch
}

// Cleanup removes a pending hook.
func (b *SSEHookBridge) Cleanup(hookID string) {
	b.mu.Lock()
	delete(b.pending, hookID)
	b.mu.Unlock()
}

// Respond delivers a response to a pending hook. Called by the HTTP handler
// when the TUI sends POST /api/pux/hook-response.
func (b *SSEHookBridge) Respond(hookID string, resp HookResponse) bool {
	b.mu.Lock()
	ch, ok := b.pending[hookID]
	b.mu.Unlock()
	if !ok {
		return false
	}
	ch <- resp
	return true
}

// waitForResponse waits for a hook response with timeout.
func (b *SSEHookBridge) waitForResponse(ctx context.Context, hookID string) (HookResponse, error) {
	b.mu.Lock()
	ch, ok := b.pending[hookID]
	b.mu.Unlock()
	if !ok {
		return HookResponse{Action: "allow"}, fmt.Errorf("no pending hook for %s", hookID)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		b.Cleanup(hookID)
		return HookResponse{Action: "allow"}, timeoutCtx.Err()
	case resp := <-ch:
		b.Cleanup(hookID)
		return resp, nil
	}
}

// ── LoopHook interface (partial — only implements tool_call interception) ──

// OnAfterToolCall intercepts tool results when hooked. Emits hook_request
// via the subscriber channel and blocks until the TUI responds.
func (b *SSEHookBridge) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	b.mu.Lock()
	skip := b.disabled
	b.mu.Unlock()
	if skip {
		return nil
	}
	// tool_result hook — not yet wired to SSE (future: emit hook_request for tool_result)
	return nil
}

// ── Noop stubs for unused hook methods ──

func (b *SSEHookBridge) Name() string { return "sse_hook_bridge" }
func (b *SSEHookBridge) OnAgentStart(_ context.Context, _ *core.LoopState) error {
	b.mu.Lock()
	b.disabled = false
	b.mu.Unlock()
	return nil
}
func (b *SSEHookBridge) OnBeforeTurn(_ context.Context, _ *core.LoopState) ([]string, error) {
	return nil, nil
}
func (b *SSEHookBridge) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) {
	return msgs, nil
}
func (b *SSEHookBridge) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error {
	return nil
}
func (b *SSEHookBridge) OnAgentEnd(_ context.Context, _ *core.LoopState) error { return nil }
