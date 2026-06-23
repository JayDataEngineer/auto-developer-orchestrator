package safeguard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SafeguardHook is a LoopHook + ToolCallWrapper that runs the safeguard
// router against every tool call's arguments. When a pattern matches it
// emits a safeguard_fallback SSE event before passing through to next().
//
// The hook does NOT block the call — blocking is the permission hook's job.
// The safeguard's job is to surface destructive patterns in the audit trail
// and (eventually) trigger engine re-routing. For now, the audit signal and
// frontend banner are the deliverables; engine swap is deferred.
type SafeguardHook struct {
	router     *Router
	subscriber chan<- core.AgentEvent
	// AgentName is set by the orchestrator at construction time so emitted
	// events carry the correct source.
	AgentName string
	// ModelInFlight is the model the current request is using. Captured at
	// OnBeforeModel so emitted events can report OriginalModel.
	ModelInFlight string
}

// NewSafeguardHook constructs a hook. Pass nil for subscriber to disable
// SSE emission (checks still run; useful for tests).
func NewSafeguardHook(router *Router, subscriber chan<- core.AgentEvent) *SafeguardHook {
	return &SafeguardHook{
		router:     router,
		subscriber: subscriber,
	}
}

// --- LoopHook ---

func (h *SafeguardHook) Name() string { return "safeguard" }

func (h *SafeguardHook) OnAgentStart(_ context.Context, _ *core.LoopState) error { return nil }

func (h *SafeguardHook) OnBeforeTurn(_ context.Context, _ *core.LoopState) ([]string, error) {
	return nil, nil
}

func (h *SafeguardHook) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) {
	// Capture the latest user message text so Check can run against it too.
	// This catches "I'm going to run git push --force" in the prompt itself
	// before the model emits a tool call.
	if h.router == nil {
		return msgs, nil
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && msgs[i].Content != "" {
			matches := h.router.Check(msgs[i].Content)
			if len(matches) > 0 {
				h.emit(msgs[i].Content, matches, "", "")
			}
			break
		}
	}
	return msgs, nil
}

func (h *SafeguardHook) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error {
	return nil
}

func (h *SafeguardHook) OnAfterToolCall(_ context.Context, _ *core.LoopState, _ string, _ map[string]any, _ string, _ error) error {
	return nil
}

func (h *SafeguardHook) OnAgentEnd(_ context.Context, _ *core.LoopState) error { return nil }

// --- ToolCallWrapper ---

// WrapToolCall runs the router against every string value in args. If any
// match, emits safeguard_fallback. Passes through to next() regardless —
// the permission hook blocks, the safeguard observes.
func (h *SafeguardHook) WrapToolCall(ctx context.Context, toolName string, args map[string]any, next func(context.Context, string, map[string]any) (any, error)) (any, error) {
	if h.router == nil {
		return next(ctx, toolName, args)
	}
	texts := extractArgStrings(args)
	if len(texts) == 0 {
		return next(ctx, toolName, args)
	}
	matches := h.router.CheckAny(texts)
	if len(matches) > 0 {
		h.emit(strings.Join(texts, "\n"), matches, toolName, h.ModelInFlight)
	}
	return next(ctx, toolName, args)
}

// emit sends a safeguard_fallback event to the subscriber (if any).
// sourceText is kept in the signature for future use (e.g. logging full
// context); only the matched snippet is shipped in the event payload to
// keep SSE traffic bounded.
func (h *SafeguardHook) emit(_ string, matches []Match, toolName, originalModel string) {
	if h.subscriber == nil {
		return
	}
	first := matches[0]
	matched := first.MatchedText
	if len(matched) > 200 {
		matched = matched[:200] + "..."
	}
	data := core.SafeguardFallbackData{
		PatternID:     first.ID,
		Description:   first.Description,
		MatchedText:   matched,
		OriginalModel: originalModel,
		FallbackModel: originalModel, // MVP: no engine swap. Future PR rewrites this field.
		AgentName:     h.AgentName,
		ToolName:      toolName,
	}
	h.subscriber <- core.AgentEvent{
		Type: core.EventTypeSafeguardFallback,
		Data: data,
	}
}

// extractArgStrings returns every string value in the args map, including
// nested strings one level deep (e.g. `{"args": {"command": "..."}}`).
// Deeper nesting is ignored — the patterns we care about live at the top
// of tool args, not buried in opaque blobs.
func extractArgStrings(args map[string]any) []string {
	var out []string
	for _, v := range args {
		switch sv := v.(type) {
		case string:
			out = append(out, sv)
		case map[string]any:
			for _, inner := range sv {
				if s, ok := inner.(string); ok {
					out = append(out, s)
				}
			}
		case []any:
			for _, item := range sv {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
		default:
			// Numbers, bools, nil — not interesting. Marshaling to JSON
			// and re-scanning would catch the rare case where a number
			// somehow contains the pattern, but the cost isn't worth it.
			// The patterns are text-anchored.
			_ = sv
		}
	}
	return out
}

// Describe is a debug helper that formats matches for logging. Not used in
// production code paths; useful in tests and CLI debugging.
func Describe(matches []Match) string {
	if len(matches) == 0 {
		return "(no matches)"
	}
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		parts = append(parts, fmt.Sprintf("%s: %s", m.ID, m.Description))
	}
	b, _ := json.Marshal(parts)
	return string(b)
}
