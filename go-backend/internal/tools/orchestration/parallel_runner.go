package orchestration

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/util"
)

// SubOrchestratorConfig holds config for creating a division sub-orchestrator.
type SubOrchestratorConfig struct {
	DivisionPath string // absolute path to division directory
	ProjectDir   string // parent project directory
	SandboxID    string // shared sandbox ID
	Depth        int    // current recursion depth
	ModelID      string // optional model override
}

// SubOrchestrator is a minimal interface for running a sub-orchestrator.
type SubOrchestrator interface {
	Run(ctx context.Context, userMsg string, subscriber chan<- core.AgentEvent) error
	Close() error
}

// OrchestratorFactory creates a sub-orchestrator for recursive delegation.
type OrchestratorFactory func(ctx context.Context, cfg SubOrchestratorConfig) (SubOrchestrator, error)

// ParallelRunner implements DelegateRunner with goroutine-based parallelism.
// delegate_async spawns independent sub-agents; collect_results waits for all.
type ParallelRunner struct {
	providerFactory ProviderFactory           // creates isolated providers per sub-agent
	executor        core.ToolExecutor          // for executing sub-agent tools
	toolSpecs       []core.OpenAITool          // all available tool definitions
	baseSession     core.Session               // parent session for context inheritance
	ctxSize         int                        // context size for sub-agent
	modelResolver   ModelResolver              // resolves model ID to provider (nil = use default)
	logger          func(format string, args ...interface{})

	// Recursive delegation
	orchestratorFactory OrchestratorFactory
	projectDir          string
	depth               int
	maxDepth            int // default 3

	// Parent SSE subscriber — sub-agent events are forwarded here for TUI visibility
	subscriber chan<- core.AgentEvent

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
func NewParallelRunner(executor core.ToolExecutor, toolSpecs []core.OpenAITool, baseSession core.Session, ctxSize int, modelResolver ModelResolver) *ParallelRunner {
	return &ParallelRunner{
		executor:      executor,
		toolSpecs:     toolSpecs,
		baseSession:   baseSession,
		ctxSize:       ctxSize,
		modelResolver: modelResolver,
		logger:        func(format string, args ...interface{}) {},
		tasks:         make(map[string]*asyncTask),
	}
}

// SetProviderFactory sets the factory for creating isolated providers per sub-agent.
func (r *ParallelRunner) SetProviderFactory(factory ProviderFactory) {
	r.providerFactory = factory
}

// enrichTask prepends relevant context from the parent session to the task.
// This gives sub-agents faithful context (original user request + CTO delegation reasoning)
// instead of just a paraphrased task description.
func (r *ParallelRunner) enrichTask(ctx context.Context, task string) string {
	if r.baseSession == nil {
		return task
	}
	msgs, err := r.baseSession.BuildContext(ctx)
	if err != nil || len(msgs) == 0 {
		return task
	}

	// Find the first user message (non-system, non-tool)
	var originalRequest string
	for _, msg := range msgs {
		if msg.Role == "user" {
			originalRequest = msg.Content
			break
		}
	}

	// Find the last assistant message before delegation (CTO's reasoning context)
	var lastAssistantContext string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			lastAssistantContext = msgs[i].Content
			break
		}
	}

	// Build enriched task
	var b strings.Builder
	if originalRequest != "" && len(originalRequest) < 500 {
		b.WriteString("<original_request>\n")
		b.WriteString(originalRequest)
		b.WriteString("\n</original_request>\n\n")
	}
	if lastAssistantContext != "" && len(lastAssistantContext) < 300 {
		b.WriteString("<cto_context>\n")
		b.WriteString(lastAssistantContext)
		b.WriteString("\n</cto_context>\n\n")
	}
	b.WriteString("<task>\n")
	b.WriteString(task)
	b.WriteString("\n</task>")

	enriched := b.String()
	if len(enriched) > 2000 {
		enriched = enriched[:2000] + "...\n</task>"
	}
	return enriched
}

// SetLogger sets the logger function.
func (r *ParallelRunner) SetLogger(fn func(format string, args ...interface{})) {
	r.logger = fn
}

// SetOrchestratorFactory configures the factory for recursive delegation.
func (r *ParallelRunner) SetOrchestratorFactory(factory OrchestratorFactory) {
	r.orchestratorFactory = factory
}

// SetProjectDir sets the project root directory for resolving division paths.
func (r *ParallelRunner) SetProjectDir(dir string) {
	r.projectDir = dir
}

// SetDepth sets the current recursion depth (0 = top-level).
func (r *ParallelRunner) SetDepth(depth int) {
	r.depth = depth
	if r.maxDepth == 0 {
		r.maxDepth = 3
	}
}

// SetSubscriber sets the parent SSE subscriber channel for forwarding sub-agent events.
func (r *ParallelRunner) SetSubscriber(ch chan<- core.AgentEvent) {
	r.subscriber = ch
}

// RunDelegate runs a synchronous sub-agent.
func (r *ParallelRunner) RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32, modelID string) (map[string]any, error) {
	agentName := extractAgentName(instructions)
	r.logger("SYNC_DELEGATE: task=%q agent=%s tools=%v model=%q", task, agentName, toolNames, modelID)

	// Emit subagent_start to parent subscriber
	core.SendEvent(r.subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentStart,
		Data: core.AgentEventData{
			AgentName: agentName,
			Task:      truncateTask(task, 120),
			ToolName:  "delegate_to",
		},
	})

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
		core.SendEvent(r.subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: fmt.Sprintf("none of the requested tools were found: %v", toolNames)},
		})
		return nil, core.NewToolError("delegate_to", fmt.Sprintf("none of the requested tools were found: %v", toolNames))
	}

	// Create isolated provider for this sub-agent via factory.
	// Each sub-agent gets its own Adapter → own session → own llama-server slot.
	// If modelID is specified, use the model resolver to pick the right engine instead.
	provider := r.providerFactory()
	if modelID != "" && r.modelResolver != nil {
		if resolved := r.modelResolver(modelID); resolved != nil {
			// Close the factory-created provider, use the resolved model-specific one instead
			if closer, ok := provider.(io.Closer); ok {
				closer.Close()
			}
			provider = resolved
			r.logger("MODEL_OVERRIDE: using %s for sub-agent", modelID)
		}
	}
	defer func() {
		if closer, ok := provider.(io.Closer); ok {
			closer.Close()
		}
	}()

	// Rewrite delegation context: include original user request + CTO's delegation reasoning.
	// This gives the sub-agent faithful context instead of a paraphrased task.
	enrichedTask := r.enrichTask(ctx, task)

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
	loop := core.NewAgentLoop(provider, r.executor, sess, cfg)

	// Collect events into result
	events := make(chan core.AgentEvent, 128)
	done := make(chan struct{})
	var runErr error

	go func() {
		defer close(done)
		defer close(events)
		runErr = loop.Run(ctx, enrichedTask, events)
	}()

	// Drain events: accumulate text result + forward tool events to parent subscriber
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
			// Forward tool events to parent subscriber with agent context
			if r.subscriber != nil && (evt.Type == core.EventTypeToolStart || evt.Type == core.EventTypeToolEnd || evt.Type == core.EventTypeToolUpdate) {
				forwarded := evt
				forwarded.Data.AgentName = agentName
				core.SendEvent(r.subscriber, forwarded)
			}
		}
	}

	// Emit subagent_end
	endStatus := "completed"
	if runErr != nil {
		endStatus = "error"
	}
	core.SendEvent(r.subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName: agentName,
			Status:    endStatus,
			Task:      truncateTask(task, 120),
		},
	})

	if runErr != nil {
		return map[string]any{"error": runErr.Error(), "partial_result": finalText}, runErr
	}

	return map[string]any{"result": finalText, "status": "completed"}, nil
}

// RunDivisionDelegate creates a full sub-orchestrator for a division head.
// The division path points to a sub-directory with its own pux.yaml, roles, etc.
func (r *ParallelRunner) RunDivisionDelegate(ctx context.Context, task, divisionPath, modelID string) (map[string]any, error) {
	if r.orchestratorFactory == nil {
		return nil, fmt.Errorf("recursive delegation not available: no orchestrator factory configured")
	}
	if r.depth >= r.maxDepth {
		return nil, fmt.Errorf("max delegation depth (%d) reached", r.maxDepth)
	}

	agentName := filepath.Base(divisionPath)

	// Resolve division path relative to project root
	absPath := divisionPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(r.projectDir, divisionPath)
	}

	// Verify pux.yaml exists in the division directory
	puxPath := filepath.Join(absPath, "pux.yaml")
	if _, err := os.Stat(puxPath); err != nil {
		return nil, fmt.Errorf("division directory %q has no pux.yaml: %w", absPath, err)
	}

	r.logger("DIVISION_DELEGATE: path=%s depth=%d model=%q", absPath, r.depth+1, modelID)

	// Emit subagent_start
	core.SendEvent(r.subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentStart,
		Data: core.AgentEventData{
			AgentName: agentName,
			Task:      truncateTask(task, 120),
			ToolName:  "delegate_to",
		},
	})

	subOrch, err := r.orchestratorFactory(ctx, SubOrchestratorConfig{
		DivisionPath: absPath,
		ProjectDir:   r.projectDir,
		SandboxID:    r.baseSession.ID(),
		Depth:        r.depth + 1,
		ModelID:      modelID,
	})
	if err != nil {
		core.SendEvent(r.subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: err.Error()},
		})
		return nil, fmt.Errorf("failed to create division orchestrator: %w", err)
	}
	defer subOrch.Close()

	// Run the sub-orchestrator, collect events
	events := make(chan core.AgentEvent, 128)
	done := make(chan struct{})
	var runErr error

	go func() {
		defer close(done)
		defer close(events)
		runErr = subOrch.Run(ctx, task, events)
	}()

	var finalText string
	evtDone := false
	for !evtDone {
		select {
		case <-done:
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
			// Forward tool events to parent subscriber with agent context
			if r.subscriber != nil && (evt.Type == core.EventTypeToolStart || evt.Type == core.EventTypeToolEnd || evt.Type == core.EventTypeToolUpdate) {
				forwarded := evt
				forwarded.Data.AgentName = agentName
				core.SendEvent(r.subscriber, forwarded)
			}
		}
	}

	// Emit subagent_end
	endStatus := "completed"
	if runErr != nil {
		endStatus = "error"
	}
	core.SendEvent(r.subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName: agentName,
			Status:    endStatus,
			Task:      truncateTask(task, 120),
		},
	})

	if runErr != nil {
		return map[string]any{"error": runErr.Error(), "partial_result": finalText}, runErr
	}

	return map[string]any{"result": finalText, "status": "completed", "division": divisionPath}, nil
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

		result, err := r.RunDelegate(bgCtx, task, instructions, toolNames, 15, 0.4, "")

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

// extractAgentName derives a display name from the instructions/role.
// For role-based delegation, the instructions IS the role name (e.g. "sarah").
// For custom instructions, returns the first word or "agent".
func extractAgentName(instructions string) string {
	if instructions == "" {
		return "agent"
	}
	// If it looks like a role name (single word, lowercase), use it directly
	if len(instructions) <= 30 && !containsSpace(instructions) {
		return instructions
	}
	// Otherwise truncate to first line or first 20 chars
	for i, c := range instructions {
		if c == '\n' {
			return util.TruncateEllipsis(instructions[:i], 20)
		}
	}
	return util.TruncateEllipsis(instructions, 20)
}

func containsSpace(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' {
			return true
		}
	}
	return false
}

func truncateTask(task string, maxLen int) string {
	return util.TruncateEllipsis(task, maxLen)
}
