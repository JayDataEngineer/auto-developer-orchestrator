package core

import (
	"testing"
)

func TestDoomLoopDetection(t *testing.T) {
	d := NewDoomLoopDetector(6, 3)

	// First two identical calls — no trigger
	if d.Record("bash", map[string]any{"command": "ls"}) {
		t.Fatal("should not trigger on first call")
	}
	if d.Record("bash", map[string]any{"command": "ls"}) {
		t.Fatal("should not trigger on second call")
	}

	// Third identical call — triggers doom loop
	if !d.Record("bash", map[string]any{"command": "ls"}) {
		t.Fatal("should trigger on third identical call")
	}

	msg := d.WarningMessage()
	if msg == "" {
		t.Fatal("should have a warning message")
	}
	if len(msg) < 50 {
		t.Fatalf("warning too short: %q", msg)
	}
}

func TestDoomLoopDifferentTools(t *testing.T) {
	d := NewDoomLoopDetector(6, 3)

	// Alternating tools — each appears at most 2 times, below trigger threshold
	d.Record("bash", map[string]any{"command": "ls"})
	d.Record("file_read", map[string]any{"path": "/tmp/test"})
	d.Record("bash", map[string]any{"command": "pwd"})
	d.Record("file_read", map[string]any{"path": "/tmp/other"})

	if d.Triggered() {
		t.Fatal("different tools/args should not trigger doom loop")
	}
}

func TestDoomLoopResetOnSuccess(t *testing.T) {
	d := NewDoomLoopDetector(6, 3)

	// Two identical calls
	d.Record("bash", map[string]any{"command": "ls"})
	d.Record("bash", map[string]any{"command": "ls"})

	// Reset clears triggered state (but window keeps history)
	d.Reset()
	if d.Triggered() {
		t.Fatal("reset should clear triggered state")
	}

	// A successful different tool call doesn't retrigger
	d.Record("file_read", map[string]any{"path": "other"})
	if d.Triggered() {
		t.Fatal("different tool after reset should not trigger")
	}
}

func TestDoomLoopWindowSliding(t *testing.T) {
	d := NewDoomLoopDetector(4, 3)

	// Fill window with different calls
	d.Record("bash", map[string]any{"command": "ls"})
	d.Record("file_read", map[string]any{"path": "a"})
	d.Record("bash", map[string]any{"command": "ls"})
	d.Record("file_read", map[string]any{"path": "b"})

	// Window is full (4). Old "bash ls" entries should start sliding out.
	// Two more "bash ls" — but the first one slid out, so only 2 in window
	d.Record("bash", map[string]any{"command": "ls"})
	d.Record("bash", map[string]any{"command": "ls"})

	// Should trigger: window=[bash(ls), bash(ls), bash(ls)] (one slid out, three added)
	if !d.Triggered() {
		t.Fatal("should trigger after sliding window accumulates 3 identical calls")
	}
}

func TestDoomLoopSimilarArgs(t *testing.T) {
	d := NewDoomLoopDetector(6, 3)

	// Same tool, slightly different args — should still catch structural similarity
	// because args are truncated to 200 chars for fingerprinting
	d.Record("file_read", map[string]any{"path": "/very/long/path/file1.go"})
	d.Record("file_read", map[string]any{"path": "/very/long/path/file2.go"})
	d.Record("file_read", map[string]any{"path": "/very/long/path/file3.go"})

	// These are different signatures (different arg values), so should NOT trigger
	if d.Triggered() {
		t.Fatal("different arg values should not trigger doom loop")
	}

	// But truly identical args should trigger
	d2 := NewDoomLoopDetector(6, 3)
	args := map[string]any{"path": "/exact/same/path.go"}
	d2.Record("file_read", args)
	d2.Record("file_read", args)
	if !d2.Record("file_read", args) {
		t.Fatal("identical args should trigger")
	}
}
