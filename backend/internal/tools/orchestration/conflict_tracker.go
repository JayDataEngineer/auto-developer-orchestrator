// Package orchestration conflict_tracker.go — resource-conflict detection
// for parallel sub-agents.
//
// Implements the Fable/Mythos §8.10 "turf war" pattern: when two parallel
// sub-agents write to the same file, surface the conflict to the CTO via
// an SSE event so the orchestrator can re-plan instead of letting the
// agents race to clobber each other's output.
//
// The detector is intentionally narrow:
//
//   - Tracks `file_write`, `file_edit`, `edit`, `write` tool calls per agent.
//   - Stores the path argument verbatim — normalization is the agent's job.
//   - Emits one event per overlap pair; does NOT block the write. Blocking
//     would be safer but breaks the "agents work in parallel" contract.
//   - Drops entries on agent unregister.
package orchestration

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ConflictTracker records which paths each agent is currently writing.
// Use Record(agentID, path) at the start of a file-modifying tool call;
// Clear(agentID) when the agent exits.
type ConflictTracker struct {
	mu      sync.Mutex
	writes  map[string]map[string]bool // agentID -> set of paths
	subscriber chan<- core.AgentEvent
}

// NewConflictTracker constructs a tracker. Pass the SSE subscriber so
// conflicts surface as `resource_conflict` events in the TUI / web.
func NewConflictTracker(subscriber chan<- core.AgentEvent) *ConflictTracker {
	return &ConflictTracker{
		writes:     make(map[string]map[string]bool),
		subscriber: subscriber,
	}
}

// Record marks `path` as being written by `agentID`. If another agent
// already holds the same path, emits a resource_conflict event naming
// both agents. Idempotent per (agent, path).
//
// Returns the list of conflicting peer agent IDs (empty if no conflict).
func (c *ConflictTracker) Record(agentID, path string) []string {
	if agentID == "" || path == "" {
		return nil
	}
	norm := normalize(path)
	c.mu.Lock()
	defer c.mu.Unlock()

	set, ok := c.writes[agentID]
	if !ok {
		set = make(map[string]bool)
		c.writes[agentID] = set
	}
	if set[norm] {
		// Already recorded by this agent — not a conflict.
		return nil
	}
	set[norm] = true

	// Scan for peers holding the same path.
	var conflicts []string
	for other, otherSet := range c.writes {
		if other == agentID {
			continue
		}
		if otherSet[norm] {
			conflicts = append(conflicts, other)
		}
	}
	if len(conflicts) > 0 {
		c.emit(agentID, conflicts, path)
	}
	return conflicts
}

// Clear drops all paths held by agentID. Call on agent exit so future
// agents don't see stale conflicts.
func (c *ConflictTracker) Clear(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.writes, agentID)
}

// emit ships a resource_conflict event for each conflicting peer.
func (c *ConflictTracker) emit(agentID string, peers []string, path string) {
	if c.subscriber == nil {
		return
	}
	for _, peer := range peers {
		core.SendEvent(c.subscriber, core.AgentEvent{
			Type: core.EventTypeResourceConflict,
			Data: core.ResourceConflictData{
				Path:   path,
				AgentA: agentID,
				AgentB: peer,
			},
		})
	}
}

// normalize lowercases and strips "." / ".." segments so equivalent paths
// match. Not a full canonicalization (no symlink resolution) — the goal is
// to catch obvious collisions, not defeat adversarial obfuscation.
func normalize(p string) string {
	clean := filepath.Clean(p)
	return strings.ToLower(clean)
}
