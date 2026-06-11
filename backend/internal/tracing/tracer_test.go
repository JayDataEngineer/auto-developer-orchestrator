package tracing

import (
	"context"
	"testing"
	"time"
)

func TestStartSpan(t *testing.T) {
	tracer := NewTracer()
	span := tracer.StartSpan("test-op")
	if span.TraceID == "" {
		t.Error("expected non-empty TraceID")
	}
	if span.SpanID == "" {
		t.Error("expected non-empty SpanID")
	}
	if span.ParentID != "" {
		t.Error("expected empty ParentID for root span")
	}
	if span.Operation != "test-op" {
		t.Errorf("expected Operation 'test-op', got %q", span.Operation)
	}
	if span.StartTime.IsZero() {
		t.Error("expected StartTime to be set")
	}
	// Verify ID is 32 hex chars (16 bytes)
	if len(span.TraceID) != 32 {
		t.Errorf("expected TraceID length 32, got %d", len(span.TraceID))
	}
	if len(span.SpanID) != 32 {
		t.Errorf("expected SpanID length 32, got %d", len(span.SpanID))
	}
}

func TestFinishTiming(t *testing.T) {
	tracer := NewTracer()
	span := tracer.StartSpan("timed-op")

	if !span.EndTime.IsZero() {
		t.Error("expected EndTime to be zero before Finish()")
	}

	span.Finish()

	if span.EndTime.IsZero() {
		t.Error("expected EndTime to be set after Finish()")
	}
	if span.EndTime.Before(span.StartTime) {
		t.Error("expected EndTime to be after StartTime")
	}
	if span.Duration() <= 0 {
		t.Error("expected Duration() to be positive")
	}
}

func TestSetAttribute(t *testing.T) {
	tracer := NewTracer()
	span := tracer.StartSpan("attr-op")

	span.SetAttribute("db.statement", "SELECT 1")
	span.SetAttribute("db.user", "admin")

	attrs := span.Attributes()
	if attrs["db.statement"] != "SELECT 1" {
		t.Errorf("expected db.statement 'SELECT 1', got %q", attrs["db.statement"])
	}
	if attrs["db.user"] != "admin" {
		t.Errorf("expected db.user 'admin', got %q", attrs["db.user"])
	}
	if len(attrs) != 2 {
		t.Errorf("expected 2 attributes, got %d", len(attrs))
	}
}

func TestParentChildRelationship(t *testing.T) {
	tracer := NewTracer()
	parent := tracer.StartSpan("parent")

	time.Sleep(time.Microsecond) // ensure measurable offset
	child := tracer.StartChild(parent, "child")

	if child.TraceID != parent.TraceID {
		t.Error("expected child to inherit parent's TraceID")
	}
	if child.ParentID != parent.SpanID {
		t.Error("expected child.ParentID to equal parent.SpanID")
	}
	if child.SpanID == parent.SpanID {
		t.Error("expected child to have a different SpanID from parent")
	}
	if child.StartTime.Before(parent.StartTime) {
		t.Error("expected child StartTime after parent StartTime")
	}
}

func TestContextRoundTrip(t *testing.T) {
	tracer := NewTracer()
	original := tracer.StartSpan("ctx-test").TraceID
	ctx := NewContext(context.Background(), original)

	extracted, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected ok to be true")
	}
	if extracted != original {
		t.Errorf("extracted trace ID %q does not match original %q", extracted, original)
	}
}

func TestFromContextMissing(t *testing.T) {
	_, ok := FromContext(context.Background())
	if ok {
		t.Error("expected ok to be false when no trace ID in context")
	}
}

func TestConcurrentAttributeAccess(t *testing.T) {
	tracer := NewTracer()
	span := tracer.StartSpan("concurrent")

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			span.SetAttribute("key", "value")
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			span.Attributes()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}