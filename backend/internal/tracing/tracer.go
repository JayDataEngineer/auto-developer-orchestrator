package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// TraceID uniquely identifies a trace.
type TraceID string

// SpanID uniquely identifies a span within a trace.
type SpanID string

// Span represents a timed operation within a trace.
type Span struct {
	TraceID    TraceID
	SpanID     SpanID
	ParentID   SpanID
	Operation  string
	StartTime  time.Time
	EndTime    time.Time
	attributes map[string]string
	mu         sync.Mutex
}

// Duration returns the elapsed time of the span.
func (s *Span) Duration() time.Duration {
	return s.EndTime.Sub(s.StartTime)
}

// Finish sets the span's end time to now.
func (s *Span) Finish() {
	s.EndTime = time.Now()
}

// SetAttribute adds or updates a key-value attribute on the span.
func (s *Span) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attributes == nil {
		s.attributes = make(map[string]string)
	}
	s.attributes[key] = value
}

// Attributes returns a copy of the span's attributes.
func (s *Span) Attributes() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]string, len(s.attributes))
	for k, v := range s.attributes {
		cp[k] = v
	}
	return cp
}

// generateID creates a hex-encoded 16-byte random ID.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Tracer creates and manages spans.
type Tracer struct{}

// NewTracer creates a new Tracer.
func NewTracer() Tracer {
	return Tracer{}
}

// StartSpan creates a new root span with an auto-generated trace and span ID.
func (t Tracer) StartSpan(operation string) Span {
	traceID := TraceID(generateID())
	spanID := SpanID(generateID())
	return Span{
		TraceID:   traceID,
		SpanID:    spanID,
		Operation: operation,
		StartTime: time.Now(),
	}
}

// StartChild creates a child span under the given parent span.
func (t Tracer) StartChild(parent Span, operation string) Span {
	spanID := SpanID(generateID())
	return Span{
		TraceID:   parent.TraceID,
		SpanID:    spanID,
		ParentID:  parent.SpanID,
		Operation: operation,
		StartTime: time.Now(),
	}
}

// contextKey is an unexported type used as a context key to avoid collisions.
type contextKey struct{}

// NewContext stores a trace ID in the context.
func NewContext(ctx context.Context, traceID TraceID) context.Context {
	return context.WithValue(ctx, contextKey{}, traceID)
}

// FromContext extracts a trace ID from the context.
func FromContext(ctx context.Context) (TraceID, bool) {
	traceID, ok := ctx.Value(contextKey{}).(TraceID)
	return traceID, ok
}