package approval

import (
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/llama"
)

func TestNewManager_DefaultTimeout(t *testing.T) {
	m := NewManager(0)
	if m.Timeout() != 5*time.Minute {
		t.Errorf("expected default timeout 5m, got %v", m.Timeout())
	}
}

func TestNewManager_CustomTimeout(t *testing.T) {
	m := NewManager(30 * time.Second)
	if m.Timeout() != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", m.Timeout())
	}
}

func TestManager_Register(t *testing.T) {
	m := NewManager(5 * time.Minute)
	ch := m.Register("req-1")
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestManager_Resolve(t *testing.T) {
	m := NewManager(5 * time.Minute)
	ch := m.Register("req-1")

	ok := m.Resolve("req-1", llama.ApprovalResponse{Action: "approve", Message: "ok"})
	if !ok {
		t.Fatal("expected Resolve to return true")
	}

	resp := <-ch
	if resp.Action != "approve" {
		t.Errorf("expected action 'approve', got %q", resp.Action)
	}
	if resp.Message != "ok" {
		t.Errorf("expected message 'ok', got %q", resp.Message)
	}
}

func TestManager_Resolve_NotFound(t *testing.T) {
	m := NewManager(5 * time.Minute)
	ok := m.Resolve("nonexistent", llama.ApprovalResponse{})
	if ok {
		t.Fatal("expected Resolve to return false for unknown ID")
	}
}

func TestManager_Resolve_Twice(t *testing.T) {
	m := NewManager(5 * time.Minute)
	m.Register("req-1")

	if !m.Resolve("req-1", llama.ApprovalResponse{}) {
		t.Fatal("first resolve should succeed")
	}
	if m.Resolve("req-1", llama.ApprovalResponse{}) {
		t.Fatal("second resolve should fail (already resolved)")
	}
}

func TestManager_Cleanup(t *testing.T) {
	m := NewManager(5 * time.Minute)
	ch := m.Register("req-1")
	m.Cleanup("req-1")

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after cleanup")
	}
}

func TestManager_Cleanup_NotFound(t *testing.T) {
	m := NewManager(5 * time.Minute)
	// Should not panic
	m.Cleanup("nonexistent")
}

func TestManager_Resolve_DeliversToChannel(t *testing.T) {
	m := NewManager(5 * time.Minute)
	ch1 := m.Register("req-1")
	ch2 := m.Register("req-2")

	m.Resolve("req-1", llama.ApprovalResponse{Action: "approve"})
	m.Resolve("req-2", llama.ApprovalResponse{Action: "deny"})

	resp1 := <-ch1
	if resp1.Action != "approve" {
		t.Errorf("ch1: expected 'approve', got %q", resp1.Action)
	}
	resp2 := <-ch2
	if resp2.Action != "deny" {
		t.Errorf("ch2: expected 'deny', got %q", resp2.Action)
	}
}
