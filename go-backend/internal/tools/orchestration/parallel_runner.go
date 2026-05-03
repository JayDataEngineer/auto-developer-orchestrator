package orchestration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ParallelRunner implements DelegateRunner with goroutine-based parallelism.
// delegate_async spawns independent sub-agents; collect_results waits for all.
type ParallelRunner struct {
	provider    core.LLMProvider           // for creating sub-agent sessions
	executor    core.ToolExecutor          // for executing sub-agent tools
	toolSpecs   []core.OpenAITool          // all available tool definitions
	baseSession core.Session               // parent session for context inheritance
	ctxSize     int                        // context size for sub-agent
	logger      func(format string, args ...interface{})

	mu      sync.Mutex
	tasks   map[string]*asyncTask
	wg      sync.WaitGroup
}

// asyncTask is a single in-flight async sub-agent.
type asyncTask struct {
	ID           string
	Task         string
	Instructions string
	Tools        []string
	Result       map[string]any
	Err          error
	Done         chan struct{}
	StartedAt    time.Time
}

// NewParallelRunner creates a runner that fans out sub-agents in parallel.
func NewParallelRunner(provider core.LLMProvider, executor core.ToolExecutor, toolSpecs []core.OpenAITool, baseSession core.Session, ctxSize int) *ParallelRunner {
	return &ParallelRunner{
		provider:    provider,
		executor:    executor,
		toolSpecs:   toolSpecs,
		baseSession: baseSession,
		ctxSize:     ctxSize,
		logger:      func(format string, args ...interface{}) {},
		tasks:       make(map[string]*asyncTask),
	}
}

// SetLogger sets the logger function.
func (r *ParallelRunner) SetLogger(fn func(format string, args ...interface{})) {
	r.logger = fn
}

// RunDelegate runs a synchronous sub-agent.
func (r *ParallelRunner) RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32) (map[string]any, error) {
	r.logger("SYNC_DELEGATE: task=%q tools=%v", task, toolNames)

	// Filter tools to only those requested
	var selectedTools []core.OpenAITool
	toolSet := make(map[string]bool)
	for _, name := range toolNames {
		toolSet[name] = true
	}
	for _, t := range r.toolSpecs {
		if toolSet[t.Function.Name] {
			selectedTools = append(selectedTools, t)
		}
	}
	if len(selectedTools) == 0 {
		return nil, core.NewToolError("delegate_to", fmt.Sprintf("none of the requested tools were found: %v", toolNames))
	}

	// Create sub-agent with a very minimal loop config
	cfg := core.AgentLoopConfig{
		SystemPrompt:   instructions,
		MaxToolRounds:  maxRounds,
		MaxTokens:      8192,
		ContextSize:    r.ctxSize,
		Tools:          selectedTools,
		Opts: core.GenerateOptions{
			MaxTokens:   8192,
			Temperature: temperature,
			TopP:        0.95,
			TopK:        20,
		},
	}

	sess := &subSession{parent: r.baseSession, msgCount: 0}
	loop := core.NewAgentLoop(r.provider, r.executor, sess, cfg)

	// Collect events into result
	events := make(chan core.AgentEvent, 128)
	done := make(chan struct{})
	var runErr error

	go func() {
		defer close(done)
		defer close(events)
		runErr = loop.Run(ctx, task, events)
	}()

	// Drain events and build result
	var finalText string
	evtDone := false
	for !evtDone {
		select {
		case <-done:
			// Drain remaining events
			for evt := range events {
				if evt.Type == core.EventTypeTextDelta || evt.Type == core.EventTypeAgentEnd {
					finalText += evt.Data.Text
				}
			}
			evtDone = true
		case evt, ok := <-events:
			if !ok {
				evtDone = true
				break
			}
			if evt.Type == core.EventTypeTextDelta || evt.Type == core.EventTypeAgentEnd {
				finalText += evt.Data.Text
			}
		}
	}

	if runErr != nil {
		return map[string]any{"error": runErr.Error(), "partial_result": finalText}, runErr
	}

	return map[string]any{"result": finalText, "status": "completed"}, nil
}

// RunDelegateAsync launches a sub-agent in a goroutine. Returns immediately.
func (r *ParallelRunner) RunDelegateAsync(ctx context.Context, taskID, task, instructions string, toolNames []string) (map[string]any, error) {
	r.mu.Lock()
	if _, exists := r.tasks[taskID]; exists {
		r.mu.Unlock()
		return nil, fmt.Errorf("async task %q already running", taskID)
	}

	t := &asyncTask{
		ID:           taskID,
		Task:         task,
		Instructions: instructions,
		Tools:        toolNames,
		Done:         make(chan struct{}),
		StartedAt:    time.Now(),
	}
	r.tasks[taskID] = t
	r.wg.Add(1)
	r.mu.Unlock()

	r.logger("ASYNC_DELEGATE: task_id=%s task=%q tools=%v", taskID, task, toolNames)

	// Launch in background goroutine with DETACHED context.
	// Using context.Background() ensures async sub-agents survive parent request
	// cancellation (SSE disconnect, browser close, etc.).
	go func() {
		defer r.wg.Done()
		defer close(t.Done)

		// Wrap in a long timeout for safety (30 minutes per async agent)
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		result, err := r.RunDelegate(bgCtx, task, instructions, toolNames, 15, 0.4)

		r.mu.Lock()
		t.Result = result
		t.Err = err
		r.mu.Unlock()
	}()

	return map[string]any{
		"task_id":   taskID,
		"status":    "dispatched",
		"started_at": t.StartedAt.Format(time.RFC3339),
	}, nil
}

// CollectAsyncResults waits for all pending async tasks and returns results.
func (r *ParallelRunner) CollectAsyncResults(ctx context.Context) (map[string]any, error) {
	r.logger("COLLECT_ASYNC: waiting for %d pending tasks", r.pendingCount())

	// Create a channel that closes when all tasks complete
	allDone := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(allDone)
	}()

	// Wait with timeout
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out waiting for async tasks: %w", ctx.Err())
	case <-allDone:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	results := make(map[string]any)
	errorCount := 0

	for taskID, t := range r.tasks {
		if t.Err != nil {
			results[taskID] = map[string]any{
				"error":  t.Err.Error(),
				"result": t.Result,
				"duration_sec": time.Since(t.StartedAt).Seconds(),
			}
			errorCount++
		} else {
			results[taskID] = map[string]any{
				"result":       t.Result,
				"duration_sec": time.Since(t.StartedAt).Seconds(),
			}
		}
	}

	// Clear completed tasks
	r.tasks = make(map[string]*asyncTask)

	return map[string]any{
		"tasks_completed": len(results),
		"tasks_error":     errorCount,
		"results":         results,
	}, nil
}

func (r *ParallelRunner) pendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tasks)
}

// subSession is a minimal session that stores messages in memory.
// It wraps a parent session for context inheritance in sub-agents.
type subSession struct {
	parent   core.Session
	messages []core.Message
	msgCount int
}

func (s *subSession) ID() string          { return s.parent.ID() + "-sub" }
func (s *subSession) Close() error         { return nil }
func (s *subSession) AppendMessage(msg core.Message) error {
	s.messages = append(s.messages, msg)
	s.msgCount++
	return nil
}
func (s *subSession) BuildContext(ctx context.Context) ([]core.Message, error) {
	return s.messages, nil
}
func (s *subSession) GetTree() *core.TreeNode    { return nil }
func (s *subSession) Navigate(nodeID string) error { return fmt.Errorf("sub-sessions do not support navigation") }
func (s *subSession) Branch(label string) (string, error) { return "", fmt.Errorf("sub-sessions do not support branching") }
func (s *subSession) Fork(nodeID string) (core.Session, error) { return nil, fmt.Errorf("sub-sessions do not support forking") }
func (s *subSession) Compact(ctx context.Context, llmProvider interface{}) (string, error) { return "", nil }
func (s *subSession) TruncateToolResults(keep int) (int, error) { return 0, nil } // no-op for ephemeral sub-sessions
func (s *subSession) GetCurrentNode() string { return s.parent.ID() + "-sub" }
