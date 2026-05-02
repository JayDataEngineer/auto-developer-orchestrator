package llama

import (
	"context"
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

// PersistEvents wraps event persistence around the subscriber channel.
// It returns an input channel (for the orchestrator to write to). Non-delta
// events are persisted to the EventStore as they flow through to downstream.
func PersistEvents(
	ctx context.Context,
	store *storage.EventStore,
	sessionID string,
	downstream chan<- AgentEvent,
) chan<- AgentEvent {
	in := make(chan AgentEvent, 256)

	go func() {
		defer close(downstream)
		for evt := range in {
			// Persist non-delta events (skip high-volume text/thinking deltas)
			if evt.Type != EventTypeTextDelta && evt.Type != EventTypeThinkingDelta && store != nil {
				data, _ := json.Marshal(evt.Data)
				// Best-effort persistence — don't block SSE on DB errors
				_, _ = store.Append(ctx, sessionID, string(evt.Type), evt.Data.ToolID, data)
			}
			// Forward to downstream (SSE handler)
			downstream <- evt
		}
	}()

	return in
}
