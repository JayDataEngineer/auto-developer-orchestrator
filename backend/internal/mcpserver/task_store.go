package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// TaskStatus is the per-task lifecycle state.
//
// pending  → task created, goroutine not yet started.
// running  → goroutine active; round counter advances.
// complete → CTO returned final response (FinishStop, no tool calls).
// failed   → provider error, tool error, or max-rounds exhaustion.
type TaskStatus string

const (
	StatusPending  TaskStatus = "pending"
	StatusRunning  TaskStatus = "running"
	StatusComplete TaskStatus = "complete"
	StatusFailed   TaskStatus = "failed"
)

// Task is the per-dispatch record. Read by get_task_status; mutated only
// by the dispatch goroutine + a small set of status setters.
type Task struct {
	ID         string
	Org        string
	Status     TaskStatus
	Task       string
	Result     string
	Error      string
	Round      int
	Tail       []string // last N assistant text messages (progress signal)
	StartedAt  time.Time
	FinishedAt time.Time

	cancel context.CancelFunc // wired by Dispatch; called by Shutdown
}

// TaskStore is the in-memory registry of dispatched tasks. Safe for
// concurrent use. Single-tenant — no persistence. Tasks live until the
// server restarts.
//
// observer is an optional core.TaskObserver wired in at construction. When
// non-nil, the store fires lifecycle events (pending/running/complete/failed)
// inside the same mutex critical section that mutates the Task. Nil = no
// events fire (the common case when history is not opted-in).
type TaskStore struct {
	mu        sync.RWMutex
	tasks     map[string]*Task
	observer  core.TaskObserver
}

// NewTaskStore constructs an empty store. observer may be nil — lifecycle
// events are silently dropped when no observer is wired.
func NewTaskStore(observer core.TaskObserver) *TaskStore {
	return &TaskStore{tasks: make(map[string]*Task), observer: observer}
}

// Insert registers a task under a fresh, randomly-generated ID. Returns
// the inserted task (caller can read the generated ID).
func (s *TaskStore) Insert(org, task string) *Task {
	t := &Task{
		ID:        newTaskID(),
		Org:       org,
		Status:    StatusPending,
		Task:      task,
		StartedAt: time.Now(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
	if s.observer != nil {
		s.observer.OnTaskPending(context.Background(), t.ID, org, task, t.StartedAt)
	}
	return t
}

// Get returns a snapshot copy of the named task. Returns false if not found.
// The returned Task is a deep copy of the mutable fields so callers can
// serialize it without holding the store lock.
func (s *TaskStore) Get(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, false
	}
	out := *t
	if t.Tail != nil {
		out.Tail = append([]string(nil), t.Tail...)
	}
	return out, true
}

// SetRunning marks the task as having started its goroutine.
func (s *TaskStore) SetRunning(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Status = StatusRunning
		if s.observer != nil {
			s.observer.OnTaskRunning(context.Background(), id)
		}
	}
}

// SetComplete marks the task as done with the supplied result text.
func (s *TaskStore) SetComplete(id, result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Status = StatusComplete
		t.Result = result
		t.FinishedAt = time.Now()
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		if s.observer != nil {
			s.observer.OnTaskComplete(context.Background(), id, result, t.FinishedAt)
		}
	}
}

// SetFailed marks the task as failed with the supplied error message.
func (s *TaskStore) SetFailed(id, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Status = StatusFailed
		t.Error = msg
		t.FinishedAt = time.Now()
		if t.cancel != nil {
			t.cancel()
			t.cancel = nil
		}
		if s.observer != nil {
			s.observer.OnTaskFailed(context.Background(), id, msg, t.FinishedAt)
		}
	}
}

// SetCancel wires a cancel func so Shutdown can interrupt in-flight tasks.
// Caller MUST NOT invoke the cancel func directly — the store owns its
// lifecycle.
func (s *TaskStore) SetCancel(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.cancel = cancel
	}
}

// UpdateProgress is called by the running goroutine to advance the round
// counter + transcript tail. Cheap to call (called once per round).
func (s *TaskStore) UpdateProgress(id string, round int, tail []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Round = round
		if tail != nil {
			tailCopy := append([]string(nil), tail...)
			t.Tail = tailCopy
		}
	}
}

// All returns a snapshot slice of all known tasks (sorted by StartedAt).
// Used by tests + the eventual list_tasks tool (not in v1 surface).
func (s *TaskStore) All() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		snap := *t
		if t.Tail != nil {
			snap.Tail = append([]string(nil), t.Tail...)
		}
		out = append(out, snap)
	}
	return out
}

// Shutdown cancels every in-flight task. Called by main.go's SIGTERM handler.
// Tasks already complete/failed have no-op cancel funcs.
func (s *TaskStore) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.Status == StatusPending || t.Status == StatusRunning {
			if t.cancel != nil {
				t.cancel()
				t.cancel = nil
			}
			t.Status = StatusFailed
			t.Error = "server shutdown"
			t.FinishedAt = time.Now()
		}
	}
}

// newTaskID returns a 16-hex-char random ID with a `tsk_` prefix. Collisions
// are astronomically unlikely at single-tenant scale.
func newTaskID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand should never fail on Linux. If it does, the host is
		// broken — better to crash than emit predictable IDs.
		panic(fmt.Sprintf("task_store: crypto/rand failed: %v", err))
	}
	return "tsk_" + hex.EncodeToString(buf[:])
}

// ErrTaskNotFound is returned by callers that need a typed sentinel for
// "task ID doesn't exist in the store".
var ErrTaskNotFound = errors.New("task not found")
