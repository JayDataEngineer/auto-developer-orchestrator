package handlers

import (
	"fmt"
	"net/http"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
)

// EventStreamer wraps an HTTP response writer for SSE output.
// It handles SSE formatting, keepalives, and flushing.
type EventStreamer struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	canFlush bool
}

// NewEventStreamer creates a streamer for the given response writer.
func NewEventStreamer(w http.ResponseWriter) *EventStreamer {
	flusher, canFlush := w.(http.Flusher)
	return &EventStreamer{w: w, flusher: flusher, canFlush: canFlush}
}

// WriteEvent converts a core event to the frontend SSE format and writes it.
func (s *EventStreamer) WriteEvent(evt llamaeng.AgentEvent) {
	mapped := mapEventToSSE(evt)
	if mapped == nil {
		return
	}
	writeSSE(s.w, mapped.Type, mapped.Data, s.canFlush, s.flusher)
}

// WriteKeepalive sends an SSE keepalive comment.
func (s *EventStreamer) WriteKeepalive() {
	fmt.Fprintf(s.w, ": keepalive\n\n")
	if s.canFlush {
		s.flusher.Flush()
	}
}

// WriteDone sends the terminal SSE done signal.
func (s *EventStreamer) WriteDone() {
	fmt.Fprintf(s.w, "data: [DONE]\n\n")
	if s.canFlush {
		s.flusher.Flush()
	}
}
