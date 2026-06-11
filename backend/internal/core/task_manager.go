package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// ProgressThreshold is how long a foreground task runs before it can be backgrounded.
	ProgressThresholdMs = 2000

	// AutoBackgroundMs is how long a foreground bash command runs in the main agent
	// before it is automatically backgrounded to keep the agent responsive.
	AutoBackgroundMs = 15000
)

// TaskState represents the lifecycle state of a background task.
type TaskState string

const (
	TaskRunning     TaskState = "running"
	TaskCompleted   TaskState = "completed"
	TaskFailed      TaskState = "failed"
	TaskBackgrounded TaskState = "backgrounded" // foreground task sent to background
)

// BackgroundTask tracks a command running in the background.
type BackgroundTask struct {
	ID           string
	Command      string
	Description  string
	Status       TaskState
	Output       strings.Builder
	OutputFile   string // file path for large outputs
	ExitCode     int
	Error        string
	StartTime    time.Time
	EndTime      time.Time
	Done         chan struct{}
	IsBackgrounded bool

	// BackgroundReq is signaled when a foreground task should detach
	// (user pressed Ctrl+B or auto-background timer fired).
	BackgroundReq chan struct{}

	// onComplete is called when the task finishes. Used to emit SSE events.
	onComplete func(task *BackgroundTask)

	mu sync.Mutex
}

// AppendOutput appends a chunk of output to the task buffer and file.
func (t *BackgroundTask) AppendOutput(chunk string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Output.WriteString(chunk)
	if t.OutputFile != "" {
		// Best-effort write to file
		_ = os.WriteFile(t.OutputFile, []byte(t.Output.String()), 0644)
	}
}

// GetOutput returns the current output.
func (t *BackgroundTask) GetOutput() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Output.String()
}

// TaskStatus is a snapshot of a task's state for external consumers.
type TaskStatus struct {
	ID          string    `json:"id"`
	Command     string    `json:"command"`
	Description string    `json:"description,omitempty"`
	Status      TaskState `json:"status"`
	OutputLen   int       `json:"outputLen"`
	OutputFile  string    `json:"outputFile,omitempty"`
	ExitCode    int       `json:"exitCode,omitempty"`
	Error       string    `json:"error,omitempty"`
	Duration    string    `json:"duration,omitempty"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
}

// TaskManager manages background and foreground-convertible tasks.
type TaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*BackgroundTask

	// outputDir is where task output files are stored.
	outputDir string

	// subscriber is the SSE event channel for emitting task lifecycle events.
	subscriber chan<- AgentEvent
}

// TaskManagerKey is the context key for the TaskManager.
type TaskManagerKey struct{}

// NewTaskManager creates a new task manager.
func NewTaskManager(outputDir string) *TaskManager {
	if outputDir == "" {
		outputDir = filepath.Join(os.TempDir(), "pux-tasks")
	}
	_ = os.MkdirAll(outputDir, 0755)
	return &TaskManager{
		tasks:     make(map[string]*BackgroundTask),
		outputDir: outputDir,
	}
}

// SetSubscriber sets the SSE subscriber channel for emitting task events.
func (m *TaskManager) SetSubscriber(ch chan<- AgentEvent) {
	m.subscriber = ch
}

// StartTracked creates a tracked task without running a command.
// Used for delegations: the caller manages the lifecycle (calls CompleteTracked
// when done). The task gets the same foreground→background signal mechanism.
func (m *TaskManager) StartTracked(command, description string) (*BackgroundTask, error) {
	taskID := "bg_" + uuid.New().String()[:8]

	task := &BackgroundTask{
		ID:             taskID,
		Command:        command,
		Description:    description,
		Status:         TaskRunning,
		StartTime:      time.Now(),
		Done:           make(chan struct{}),
		IsBackgrounded: false,
		BackgroundReq:  make(chan struct{}, 1),
	}

	m.mu.Lock()
	m.tasks[taskID] = task
	m.mu.Unlock()

	m.emitEvent(EventTypeTaskStarted, TaskStartedData{
		TaskID:  taskID,
		Command: command,
	})

	return task, nil
}

// CompleteTracked marks a tracked task as complete. Used by the parallel runner
// when a backgrounded delegation finishes.
func (m *TaskManager) CompleteTracked(taskID string, output string, exitCode int, errStr string) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return
	}

	task.mu.Lock()
	if errStr != "" {
		task.Status = TaskFailed
		task.Error = errStr
	} else {
		task.Status = TaskCompleted
	}
	task.ExitCode = exitCode
	task.Output.WriteString(output)
	task.EndTime = time.Now()
	task.mu.Unlock()

	close(task.Done)

	m.emitEvent(EventTypeTaskCompleted, TaskCompletedData{
		TaskID:   taskID,
		ExitCode: exitCode,
	})
}

// Start begins executing a command. If runInBG is true, the task runs in the
// background and the method returns immediately. If runInBG is false, the
// command still runs in a goroutine but the caller should wait on task.Done
// or watch task.BackgroundReq.
func (m *TaskManager) Start(ctx context.Context, command, description string, runInBG bool, workDir string) (*BackgroundTask, error) {
	taskID := "bg_" + uuid.New().String()[:8]
	outputFile := filepath.Join(m.outputDir, taskID+".txt")

	task := &BackgroundTask{
		ID:            taskID,
		Command:       command,
		Description:   description,
		Status:        TaskRunning,
		OutputFile:    outputFile,
		StartTime:     time.Now(),
		Done:          make(chan struct{}),
		IsBackgrounded: runInBG,
		BackgroundReq: make(chan struct{}, 1),
	}

	m.mu.Lock()
	m.tasks[taskID] = task
	m.mu.Unlock()

	// Emit task started event
	m.emitEvent(EventTypeTaskStarted, TaskStartedData{
		TaskID:  taskID,
		Command: command,
	})

	// Start the command in a goroutine
	go m.runCommand(task, command, workDir)

	return task, nil
}

// runCommand executes the shell command and updates the task on completion.
func (m *TaskManager) runCommand(task *BackgroundTask, command, workDir string) {
	cmd := exec.Command("sh", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Pipe stdout and stderr to the task's output
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		task.mu.Lock()
		task.Status = TaskFailed
		task.Error = err.Error()
		task.ExitCode = 1
		task.EndTime = time.Now()
		task.mu.Unlock()
		m.onTaskDone(task)
		return
	}

	// Read stdout and stderr
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				task.AppendOutput(chunk)
				// Emit streaming output event for foreground tasks
				if !task.IsBackgrounded {
					m.emitEvent(EventTypeToolUpdate, ToolUpdate{
						ToolID:   task.ID,
						ToolName: "bash",
						Text:     chunk,
					})
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				task.AppendOutput(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	wg.Wait()
	err := cmd.Wait()

	task.mu.Lock()
	if err != nil {
		task.Status = TaskFailed
		task.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			task.ExitCode = exitErr.ExitCode()
		} else {
			task.ExitCode = 1
		}
	} else {
		task.Status = TaskCompleted
		task.ExitCode = 0
	}
	task.EndTime = time.Now()
	task.mu.Unlock()

	m.onTaskDone(task)
}

// onTaskDone handles task completion: emit events and clean up.
func (m *TaskManager) onTaskDone(task *BackgroundTask) {
	close(task.Done)

	duration := task.EndTime.Sub(task.StartTime).Round(time.Millisecond)

	m.emitEvent(EventTypeTaskCompleted, TaskCompletedData{
		TaskID:   task.ID,
		ExitCode: task.ExitCode,
	})

	// Store duration in the event data
	_ = duration

	if task.onComplete != nil {
		task.onComplete(task)
	}
}

// Background signals a foreground task to detach and continue running in the background.
// This is called when the user presses Ctrl+B or when the auto-background timer fires.
func (m *TaskManager) Background(taskID string) error {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	task.mu.Lock()
	if task.IsBackgrounded {
		task.mu.Unlock()
		return fmt.Errorf("task %s already backgrounded", taskID)
	}
	task.IsBackgrounded = true
	task.Status = TaskBackgrounded
	task.mu.Unlock()

	// Signal the foreground Execute to return
	select {
	case task.BackgroundReq <- struct{}{}:
	default:
	}

	m.emitEvent(EventTypeTaskBackground, TaskBackgroundData{
		TaskID: taskID,
	})

	return nil
}

// Wait blocks until the task completes or the timeout expires.
func (m *TaskManager) Wait(taskID string, timeout time.Duration) (*TaskStatus, error) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-task.Done:
		return m.Status(taskID)
	case <-timer.C:
		return nil, fmt.Errorf("timeout waiting for task %s", taskID)
	}
}

// Status returns the current status of a task.
func (m *TaskManager) Status(taskID string) (*TaskStatus, error) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	task.mu.Lock()
	defer task.mu.Unlock()

	status := &TaskStatus{
		ID:          task.ID,
		Command:     task.Command,
		Description: task.Description,
		Status:      task.Status,
		OutputLen:   task.Output.Len(),
		OutputFile:  task.OutputFile,
		ExitCode:    task.ExitCode,
		Error:       task.Error,
		StartTime:   task.StartTime,
		EndTime:     task.EndTime,
	}
	if !task.EndTime.IsZero() {
		status.Duration = task.EndTime.Sub(task.StartTime).Round(time.Millisecond).String()
	}
	return status, nil
}

// List returns all tasks.
func (m *TaskManager) List() []*TaskStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*TaskStatus, 0, len(m.tasks))
	for _, task := range m.tasks {
		task.mu.Lock()
		s := &TaskStatus{
			ID:          task.ID,
			Command:     task.Command,
			Description: task.Description,
			Status:      task.Status,
			OutputLen:   task.Output.Len(),
			OutputFile:  task.OutputFile,
			ExitCode:    task.ExitCode,
			Error:       task.Error,
			StartTime:   task.StartTime,
			EndTime:     task.EndTime,
		}
		if !task.EndTime.IsZero() {
			s.Duration = task.EndTime.Sub(task.StartTime).Round(time.Millisecond).String()
		}
		task.mu.Unlock()
		result = append(result, s)
	}
	return result
}

// GetOutput reads the task's output from its file.
func (m *TaskManager) GetOutput(taskID string) (string, error) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("task %s not found", taskID)
	}

	// Try reading from file first (handles large outputs)
	if task.OutputFile != "" {
		if data, err := os.ReadFile(task.OutputFile); err == nil {
			return string(data), nil
		}
	}

	// Fall back to in-memory buffer
	return task.GetOutput(), nil
}

// emitEvent sends an SSE event to the subscriber channel.
func (m *TaskManager) emitEvent(eventType AgentEventType, data EventPayload) {
	if m.subscriber == nil {
		return
	}
	SendEvent(m.subscriber, AgentEvent{
		Type: eventType,
		Data: data,
	})
}
