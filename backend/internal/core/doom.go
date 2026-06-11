package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DoomLoopDetector detects when the agent is calling the same tool with
// similar arguments repeatedly, indicating it's stuck in a loop.
type DoomLoopDetector struct {
	// window stores the last N tool call signatures (name + arg fingerprint).
	window []string
	size   int

	// triggerCount is how many times the same signature must appear
	// in the window to trigger a doom loop warning.
	triggerCount int

	// triggered tracks whether a doom loop was detected this turn.
	triggered bool
	triggerSig string
}

// NewDoomLoopDetector creates a detector with the given window size and trigger threshold.
func NewDoomLoopDetector(windowSize, triggerCount int) *DoomLoopDetector {
	if windowSize <= 0 {
		windowSize = 6
	}
	if triggerCount <= 0 {
		triggerCount = 3
	}
	return &DoomLoopDetector{
		window:       make([]string, 0, windowSize),
		size:         windowSize,
		triggerCount: triggerCount,
	}
}

// toolSignature creates a fingerprint for a tool call: name + sorted arg keys + truncated values.
// This catches both exact repeats and near-repeats (same tool, slightly different args).
func toolSignature(name string, args map[string]any) string {
	var b strings.Builder
	b.WriteString(name)
	b.WriteString("(")

	// Use arg values truncated to catch "same tool, slightly different args" patterns
	argsJSON, err := json.Marshal(args)
	if err != nil {
		argsJSON = []byte("{}")
	}

	// Truncate args to first 200 chars — catches structural similarity
	argStr := string(argsJSON)
	if len(argStr) > 200 {
		argStr = argStr[:200]
	}
	b.WriteString(argStr)
	b.WriteString(")")
	return b.String()
}

// Record adds a tool call to the sliding window and checks for doom loops.
// Returns true if a doom loop pattern was detected.
func (d *DoomLoopDetector) Record(name string, args map[string]any) bool {
	sig := toolSignature(name, args)

	d.window = append(d.window, sig)
	if len(d.window) > d.size {
		d.window = d.window[len(d.window)-d.size:]
	}

	// Count occurrences of this signature in the window
	count := 0
	for _, s := range d.window {
		if s == sig {
			count++
		}
	}

	d.triggered = count >= d.triggerCount
	if d.triggered {
		d.triggerSig = sig
	}
	return d.triggered
}

// Triggered returns whether a doom loop was detected in the last Record call.
func (d *DoomLoopDetector) Triggered() bool {
	return d.triggered
}

// WarningMessage returns the nudge to inject when a doom loop is detected.
func (d *DoomLoopDetector) WarningMessage() string {
	if !d.triggered {
		return ""
	}
	return fmt.Sprintf(
		"[SYSTEM WARNING: Doom loop detected. You have called the same tool with similar arguments %d+ times in the last %d calls (pattern: %s). "+
			"Stop repeating this approach. Use a completely different tool or strategy. "+
			"If you are stuck, explain the problem to the user instead of retrying.]",
		d.triggerCount, d.size, truncate(d.triggerSig, 100),
	)
}

// Reset clears the doom loop state (e.g., after a successful tool call).
func (d *DoomLoopDetector) Reset() {
	d.triggered = false
	d.triggerSig = ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
