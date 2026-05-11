package hooks

import (
	"context"

	ctxpkg "github.com/auto-developer-orchestrator/backend/internal/context"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ScratchpadHook injects the current scratch pad state before each model call.
// Notes survive compaction because they are re-injected every turn.
type ScratchpadHook struct {
	store *ctxpkg.ScratchStore
}

func NewScratchpadHook(store *ctxpkg.ScratchStore) *ScratchpadHook {
	return &ScratchpadHook{store: store}
}

func (h *ScratchpadHook) Name() string { return "scratchpad" }

func (h *ScratchpadHook) OnAgentStart(_ context.Context, _ *core.LoopState) error { return nil }

func (h *ScratchpadHook) OnBeforeTurn(_ context.Context, _ *core.LoopState) ([]string, error) {
	notes := h.store.FormatForContext()
	if notes == "" {
		return nil, nil
	}
	return []string{notes}, nil
}

func (h *ScratchpadHook) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) {
	return msgs, nil
}

func (h *ScratchpadHook) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error {
	return nil
}

func (h *ScratchpadHook) OnAfterToolCall(_ context.Context, _ *core.LoopState, _ string, _ map[string]any, _ string, _ error) error {
	return nil
}

func (h *ScratchpadHook) OnAgentEnd(_ context.Context, _ *core.LoopState) error { return nil }
