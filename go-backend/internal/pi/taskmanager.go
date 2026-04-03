package pi

import (
	"fmt"
	"sync"
	"time"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
)

// TaskIsTerminal returns true if the task status is terminal.
func TaskIsTerminal(s TaskStatus) bool {
	return s == TaskCompleted || s == TaskFailed
}

// Task represents a unit of work tracked by the task management system.
type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      TaskStatus `json:"status"`
	ProjectDir  string     `json:"projectDir"`
	ParentAgent string     `json:"parentAgent"`
	SubAgentID  string     `json:"subAgentId,omitempty"` // linked sub-agent
	Model       string     `json:"model,omitempty"`
	// Dependencies
	Blocks   []string `json:"blocks,omitempty"`   // task IDs this blocks
	BlockedBy []string `json:"blockedBy,omitempty"` // task IDs blocking this
	// Result
	Output    string  `json:"output,omitempty"`
	Error     string  `json:"error,omitempty"`
	// Timing
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
	// Metrics
	InputTokens  float64 `json:"inputTokens,omitempty"`
	OutputTokens float64 `json:"outputTokens,omitempty"`
	DurationMs   int64   `json:"durationMs,omitempty"`
}

// TaskManager provides CRUD operations for tasks with dependency tracking.
type TaskManager struct {
	mu    sync.Mutex
	tasks map[string]*Task // taskID → task
	counter int64
}

// NewTaskManager creates a new task manager.
func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks: make(map[string]*Task),
		counter: time.Now().UnixMilli(),
	}
}

func (tm *TaskManager) nextID() string {
	tm.counter++
	return fmt.Sprintf("task-%d", tm.counter)
}

// Create adds a new task. Auto-generates ID if empty.
func (tm *TaskManager) Create(task Task) (*Task, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if task.ID == "" {
		task.ID = tm.nextID()
	}
	if task.Status == "" {
		task.Status = TaskPending
	}
	now := time.Now().UnixMilli()
	task.CreatedAt = now
	task.UpdatedAt = now

	tm.tasks[task.ID] = &task
	return &task, nil
}

// Get returns a task by ID.
func (tm *TaskManager) Get(taskID string) (*Task, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	return t, nil
}

// ListByProject returns all tasks for a project directory.
func (tm *TaskManager) ListByProject(projectDir string) []Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var result []Task
	for _, t := range tm.tasks {
		if t.ProjectDir == projectDir || projectDir == "" {
			result = append(result, *t)
		}
	}
	return result
}

// ListByAgent returns all tasks for a parent agent.
func (tm *TaskManager) ListByAgent(parentAgent string) []Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var result []Task
	for _, t := range tm.tasks {
		if t.ParentAgent == parentAgent || parentAgent == "" {
			result = append(result, *t)
		}
	}
	return result
}

// Update modifies a task's status and fields.
func (tm *TaskManager) Update(taskID string, updates TaskUpdate) (*Task, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	if updates.Status != nil {
		t.Status = *updates.Status
	}
	if updates.Output != nil {
		t.Output = *updates.Output
	}
	if updates.Error != nil {
		t.Error = *updates.Error
	}
	if updates.SubAgentID != nil {
		t.SubAgentID = *updates.SubAgentID
	}
	if updates.Description != nil {
		t.Description = *updates.Description
	}
	t.UpdatedAt = time.Now().UnixMilli()

	return t, nil
}

// Stop marks a task as failed.
func (tm *TaskManager) Stop(taskID string, reason string) (*Task, error) {
	return tm.Update(taskID, TaskUpdate{
		Status: statusPtr(TaskFailed),
		Error:  strPtr(reason),
	})
}

// SetDependencies configures task dependencies.
func (tm *TaskManager) SetDependencies(taskID string, blocks []string, blockedBy []string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	t.Blocks = blocks
	t.BlockedBy = blockedBy
	t.UpdatedAt = time.Now().UnixMilli()

	// Validate: check for cycles
	if err := tm.validateNoCycleLocked(taskID); err != nil {
		t.Blocks = nil
		t.BlockedBy = nil
		return err
	}

	return nil
}

// CanStart returns true if a task's dependencies are all complete.
func (tm *TaskManager) CanStart(taskID string) (bool, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tasks[taskID]
	if !ok {
		return false, fmt.Errorf("task %s not found", taskID)
	}

	for _, depID := range t.BlockedBy {
		dep, ok := tm.tasks[depID]
		if !ok {
			return false, nil // dependency doesn't exist yet
		}
		if dep.Status != TaskCompleted {
			return false, nil // dependency not complete
		}
	}
	return true, nil
}

// Delete removes a task.
func (tm *TaskManager) Delete(taskID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, ok := tm.tasks[taskID]; !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	delete(tm.tasks, taskID)
	return nil
}

// Shutdown clears all tasks.
func (tm *TaskManager) Shutdown() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tasks = make(map[string]*Task)
}

// validateNoCycleLocked checks for dependency cycles using DFS.
func (tm *TaskManager) validateNoCycleLocked(startID string) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(id string) error
	dfs = func(id string) error {
		visited[id] = true
		inStack[id] = true

		t, ok := tm.tasks[id]
		if !ok {
			return nil
		}
		for _, depID := range t.BlockedBy {
			if inStack[depID] {
				return fmt.Errorf("dependency cycle detected: %s → %s", id, depID)
			}
			if !visited[depID] {
				if err := dfs(depID); err != nil {
					return err
				}
			}
		}

		inStack[id] = false
		return nil
	}

	return dfs(startID)
}

// TaskUpdate represents partial updates to a task.
type TaskUpdate struct {
	Status      *TaskStatus `json:"status,omitempty"`
	Output      *string     `json:"output,omitempty"`
	Error       *string     `json:"error,omitempty"`
	SubAgentID  *string     `json:"subAgentId,omitempty"`
	Description *string     `json:"description,omitempty"`
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string { return &s }

// statusPtr returns a pointer to the given TaskStatus.
func statusPtr(s TaskStatus) *TaskStatus { return &s }
