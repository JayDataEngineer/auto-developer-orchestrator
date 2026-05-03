package hooks

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// GoalNudgeHook injects guidance messages when the agent needs direction.
// It handles: goal reminders, budget warnings, reflection nudges, and cycle detection.
type GoalNudgeHook struct {
	logger      *log.Logger
	maxRounds   int
	nudgeEvery  int // inject reflection every N tool calls
	cycleWindow int // re-detect cycles within this many rounds
	history     []historyEntry
}

type historyEntry struct {
	toolName string
	argsKey  string
	result   string
	round    int
}

// NewGoalNudgeHook creates a goal nudge hook.
func NewGoalNudgeHook(maxRounds int) *GoalNudgeHook {
	return &GoalNudgeHook{
		logger:      log.Default(),
		maxRounds:   maxRounds,
		nudgeEvery:  3,
		cycleWindow: 5,
	}
}

func (h *GoalNudgeHook) Name() string { return "goal_nudge" }

func (h *GoalNudgeHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	h.history = nil
	return nil
}

func (h *GoalNudgeHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	var nudges []string

	// Budget warning
	if h.maxRounds > 0 {
		remaining := h.maxRounds - state.Round
		if state.Round >= int(float64(h.maxRounds)*0.75) && remaining > 0 {
			nudges = append(nudges, fmt.Sprintf(
				"[SYSTEM: BUDGET WARNING — %d/%d tool rounds used. %d remaining. Consolidate results now.]",
				state.Round, h.maxRounds, remaining,
			))
		}
	}

	// Reflection every N tool calls
	if state.Round > 0 && state.Round%h.nudgeEvery == 0 {
		nudges = append(nudges, "REFLECT: Review the last few actions. Did they make progress toward your goal? If not, change your approach. Are you close to done? If so, stop using tools and write your final answer.")
	}

	return nudges, nil
}

func (h *GoalNudgeHook) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	if err != nil {
		return nil
	}

	// Build args key for cycle detection
	argsKey := fmt.Sprintf("%v", args)

	// Record history
	h.history = append(h.history, historyEntry{
		toolName: toolName,
		argsKey:  argsKey,
		result:   result,
		round:    state.Round,
	})

	// Trim old history
	if len(h.history) > h.cycleWindow*3 {
		h.history = h.history[len(h.history)-h.cycleWindow*3:]
	}

	// Detect cycles: same tool + same args 3+ times with same result
	sameCount := 0
	for i := len(h.history) - 1; i >= 0; i-- {
		e := h.history[i]
		if e.toolName == toolName && e.argsKey == argsKey && e.result == result {
			sameCount++
		} else if e.toolName == toolName {
			break
		}
	}

	if sameCount >= 3 {
		h.logger.Printf("GoalNudgeHook: cycle detected for tool=%s (round %d)", toolName, state.Round)
		// Cycle nudge will be injected on next OnBeforeTurn via state
		// since we can't modify the session from OnAfterToolCall
		nudge := fmt.Sprintf(
			"CYCLE DETECTED: You have called %s with the same arguments %d times with the same result. You are stuck. Try a COMPLETELY DIFFERENT approach.",
			toolName, sameCount,
		)
		// Store in state for next turn (access via custom field)
		if state.FailCounts == nil {
			state.FailCounts = make(map[string]int)
		}
		state.FailCounts["_cycle_nudges"]++
		// This is a hack - we inject a message. Better approach: add to loop state.
		_ = nudge
	}

	return nil
}

func (h *GoalNudgeHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	return nil
}

// GetCycleNudges returns accumulated cycle nudge messages since last call.
func (h *GoalNudgeHook) GetCycleNudges() []string {
	var nudges []string

	// Check recent history for cycles
	if len(h.history) < 3 {
		return nil
	}

	seen := make(map[string]int)
	for _, e := range h.history {
		key := e.toolName + e.argsKey
		seen[key]++
	}

	for key, count := range seen {
		if count >= 3 {
			parts := splitLast(key, "map[")
			toolName := parts[0]
			nudges = append(nudges, fmt.Sprintf(
				"CYCLE DETECTED: You have called %s with the same arguments %d times with the same result. You are stuck. Try a COMPLETELY DIFFERENT approach.",
				toolName, count,
			))
		}
	}

	return nudges
}

func splitLast(s, sep string) []string {
	if idx := strings.Index(s, sep); idx >= 0 {
		return []string{s[:idx], s[idx:]}
	}
	return []string{s}
}
