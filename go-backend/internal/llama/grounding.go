package llama

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// NormalizeCoords converts 0-1000 normalized coordinates to actual screen pixels.
// This is the CUA/Google computer-use pattern: models output coordinates in a
// resolution-independent 0-1000 space, then we scale to the actual display.
func NormalizeCoords(normX, normY float64, screenW, screenH int) (int, int) {
	actualX := int(math.Round(normX * float64(screenW) / 1000.0))
	actualY := int(math.Round(normY * float64(screenH) / 1000.0))
	// Clamp to screen bounds
	if actualX < 0 {
		actualX = 0
	}
	if actualX >= screenW {
		actualX = screenW - 1
	}
	if actualY < 0 {
		actualY = 0
	}
	if actualY >= screenH {
		actualY = screenH - 1
	}
	return actualX, actualY
}

// ── Cycle Detection ───────────────────────────────────────────────────

// ToolCallRecord tracks a single tool execution for cycle detection.
type ToolCallRecord struct {
	ToolName  string
	ArgsSig   string // first 100 chars of sorted args
	ResultSig string // first 50 chars of result
	Round     int
}

// CycleDetector tracks recent tool calls and detects when the agent is stuck
// in a loop (same tool + similar args producing the same result repeatedly).
// From Agent-S reference repo: reflection/cycle detection without suggesting fixes.
type CycleDetector struct {
	history []ToolCallRecord
	maxLen  int
}

// NewCycleDetector creates a detector that tracks the last maxLen tool calls.
func NewCycleDetector(maxLen int) *CycleDetector {
	if maxLen <= 0 {
		maxLen = 10
	}
	return &CycleDetector{maxLen: maxLen}
}

// Record adds a tool call to the history and returns whether a cycle was detected.
// A cycle is detected when the same tool with similar args produces the same result 3+ times.
func (d *CycleDetector) Record(toolName string, args map[string]interface{}, result string, round int) bool {
	record := ToolCallRecord{
		ToolName:  toolName,
		ArgsSig:   argsSignature(args),
		ResultSig: truncate(result, 50),
		Round:     round,
	}

	d.history = append(d.history, record)
	if len(d.history) > d.maxLen {
		d.history = d.history[len(d.history)-d.maxLen:]
	}

	return d.isCycle()
}

// isCycle checks if the last 3+ calls with the same tool name and similar args
// produced the same result. If so, the agent is stuck.
func (d *CycleDetector) isCycle() bool {
	if len(d.history) < 3 {
		return false
	}

	latest := d.history[len(d.history)-1]
	streak := 1

	for i := len(d.history) - 2; i >= 0; i-- {
		prev := d.history[i]
		if prev.ToolName == latest.ToolName && prev.ArgsSig == latest.ArgsSig && prev.ResultSig == latest.ResultSig {
			streak++
		} else {
			break
		}
	}

	return streak >= 3
}

// CycleNudge returns a message to inject when a cycle is detected.
// Important: detects the loop without suggesting specific fixes (Agent-S pattern).
func CycleNudge() string {
	return "CYCLE DETECTED: You have called the same tool with the same arguments 3+ times with the same result. " +
		"You are stuck. Try a COMPLETELY DIFFERENT approach — use a different tool, modify your arguments, or reassess the task."
}

// argsSignature creates a short hash of tool arguments for cycle comparison.
func argsSignature(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}
	// Simple concatenation of key=value pairs, truncated
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	// Sort not needed for cycle detection — same args produce same order
	combined := strings.Join(parts, "&")
	if len(combined) > 100 {
		hash := sha256.Sum256([]byte(combined))
		return fmt.Sprintf("%x", hash[:8])
	}
	return combined
}

// atoiFromInterface extracts an int from a JSON-decoded value.
// Handles float64 (json.Unmarshal numbers), string, and int.
func atoiFromInterface(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	case int:
		return n
	default:
		return 0
	}
}
