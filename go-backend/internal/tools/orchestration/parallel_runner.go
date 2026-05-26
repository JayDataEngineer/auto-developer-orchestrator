package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/agents"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	ctxpkg "github.com/auto-developer-orchestrator/backend/internal/context"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/auto-developer-orchestrator/backend/internal/util"
	"github.com/auto-developer-orchestrator/backend/internal/vision"
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

// SummarizerFunc summarizes text (typically a sub-agent result) to a target length.
type SummarizerFunc func(ctx context.Context, text string, targetChars int) (string, error)

// RunnerConfig holds all configuration for a ParallelRunner.
// Passed to NewParallelRunner — replaces the old 14 setter methods.
type RunnerConfig struct {
	ProviderFactory ProviderFactory
	ToolSpecs       []core.OpenAITool
	Executor        core.ToolExecutor
	BaseSession     core.Session
	ContextSize     int
	ModelResolver   ModelResolver
	Logger          func(format string, args ...interface{})

	// Recursive delegation
	OrchestratorFactory OrchestratorFactory
	ProjectDir          string
	Depth, MaxDepth     int // MaxDepth defaults to 3

	// File change tracking
	Snapshotter Snapshotter

	// Per-tier executor override
	ExecutorFactory func(tier string) core.ToolExecutor

	// Vision caching for sub-agent executors
	VisualContext vision.VisualContext
	VisionChain  *vision.FallbackChain
	NativeVision bool // true if sub-agent models support native image_url
	VisionLogger func(format string, args ...interface{})

	// Auto-director: raise browser window for VNC visibility
	RaiseBrowserFunc func(ctx context.Context)

	// Scoped delegation tools
	RoleProvider RoleProvider
	MCPResolver  MCPResolver

	// Sub-agent result summarization
	Summarizer SummarizerFunc

	// Ctrl+B backgrounding support
	TaskMgr *core.TaskManager

	// Scratch pad re-injection
	ScratchStore *ctxpkg.ScratchStore

	// Sub-agent transcript persistence
	DB      *storage.Database
	Project string
}

// ParallelRunner implements DelegateRunner with goroutine-based parallelism.
// delegate_async spawns independent sub-agents; collect_results waits for all.
type ParallelRunner struct {
	cfg RunnerConfig

	mu                 sync.Mutex
	tasks              map[string]*asyncTask
	wg                 sync.WaitGroup
	liveAgents         map[string]*liveAgent // kept-alive sub-agents for continuation
	completedSnapshots map[string]string     // agentRef → snapshotID for auto-accepted delegates (enables revert)
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

// liveAgent holds a kept-alive subagent session for continuation (feedback loop).
type liveAgent struct {
	ID        string
	Role      string
	Task      string
	Session   *subSession
	Provider  core.LLMProvider
	Config    core.AgentLoopConfig
	Snapshot  string // git stash SHA for change tracking
	StartedAt time.Time
}

// NewParallelRunner creates a runner that fans out sub-agents in parallel.
func NewParallelRunner(cfg RunnerConfig) *ParallelRunner {
	// Enforce minimum context size for sub-agents
	if cfg.ContextSize < 50000 {
		cfg.ContextSize = 50000
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = func(format string, args ...interface{}) {}
	}
	return &ParallelRunner{
		cfg:                cfg,
		tasks:              make(map[string]*asyncTask),
		liveAgents:         make(map[string]*liveAgent),
		completedSnapshots: make(map[string]string),
	}
}

// enrichTask prepends relevant context from the parent session to the task.
// This gives sub-agents faithful context (original user request + CTO delegation reasoning)
// instead of just a paraphrased task description.
func (r *ParallelRunner) enrichTask(ctx context.Context, task string) string {
	if r.cfg.BaseSession == nil {
		return task
	}
	msgs, err := r.cfg.BaseSession.BuildContext(ctx)
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

	// Normalize host paths in the task to /sandbox/workspace/ so the sub-agent
	// never sees contradictory paths (host path in task vs /sandbox/ in workspace_note).
	normalizedTask := task
	if r.cfg.ProjectDir != "" {
		normalizedTask = strings.ReplaceAll(normalizedTask, r.cfg.ProjectDir, "/sandbox/workspace")
	}

	b.WriteString("<task>\n")
	b.WriteString(normalizedTask)
	b.WriteString("\n</task>")

	// Append workspace path awareness so sub-agents always write to the bind-mounted directory
	if r.cfg.ProjectDir != "" {
		b.WriteString("\n\n<workspace_note>\n")
		b.WriteString("Your working directory is /sandbox/workspace/ — this IS the project root. ")
		b.WriteString("All file operations (read, write, edit, build) target /sandbox/workspace/. ")
		b.WriteString("Ignore any other paths mentioned in the task — /sandbox/workspace/ is the ONLY path you need.\n")
		b.WriteString("</workspace_note>")

		// Inject environment info (platform, date) for platform-aware code
		b.WriteString(fmt.Sprintf("\n\n<env>\nPlatform: %s\nDate: %s\n</env>", runtime.GOOS, time.Now().Format("2006-01-02")))

		// Inject project context — CLAUDE.md, build system hints
		projectCtx := discoverProjectContext(r.cfg.ProjectDir)
		if projectCtx != "" {
			b.WriteString("\n\n<project_context>\n")
			b.WriteString(projectCtx)
			b.WriteString("\n</project_context>")
		}
	}

	enriched := b.String()
	// Truncate at the task content level, not the workspace/project metadata.
	// Keep metadata intact — it's essential for the sub-agent.
	if len(enriched) > 4000 {
		// Find </task> and truncate after it, keeping the rest
		taskEnd := strings.Index(enriched, "</task>")
		if taskEnd > 0 && taskEnd < 4000 {
			// Keep task + everything after (workspace_note, env, project_context)
			return enriched
		}
		enriched = enriched[:4000] + "...\n</task>"
	}
	return enriched
}

// SetRoleProviders configures role resolution for scoped delegation.
// Called after construction when role providers become available.
func (r *ParallelRunner) SetRoleProviders(roleProvider RoleProvider, mcpResolver MCPResolver) {
	r.cfg.RoleProvider = roleProvider
	r.cfg.MCPResolver = mcpResolver
}

// makeTranscriptID builds a deterministic composite ID for subagent transcript persistence.
// Returns empty string if baseSession is nil (e.g. in tests).
func (r *ParallelRunner) makeTranscriptID(agentName string) string {
	if r.cfg.BaseSession == nil {
		return ""
	}
	return r.cfg.BaseSession.ID() + ":sub:" + agentName + "-" + fmt.Sprintf("%d", time.Now().UnixMilli())
}

// subscriberFromCtx extracts the SSE subscriber channel from the context.
// Contract 3.4 compliance: subscriber is retrieved from context (set by AgentLoop),
// not held as a struct field. This keeps ParallelRunner a pure tool with no direct
// reference to the event stream.
func subscriberFromCtx(ctx context.Context) chan<- core.AgentEvent {
	// Accept both chan<- AgentEvent (from loop.go) and chan AgentEvent (from tests)
	if ch, ok := ctx.Value(core.SubscriberKey{}).(chan<- core.AgentEvent); ok {
		return ch
	}
	if ch, ok := ctx.Value(core.SubscriberKey{}).(chan core.AgentEvent); ok {
		return ch
	}
	return nil
}

// drainResult holds the output of an event drain loop.
type drainResult struct {
	FinalText    string
	FirstError   string
	Backgrounded bool
	RunErr       error
}

// drainAndForward drains events from a running sub-agent loop.
// Accumulates text, tracks first error, forwards tool/text/thinking events
// to the parent subscriber, and watches for Ctrl+B background signal.
// If backgrounded, spins up a goroutine to collect remaining events and
// emit subagent_end when done.
func (r *ParallelRunner) drainAndForward(
	ctx context.Context,
	subscriber chan<- core.AgentEvent,
	agentName string,
	events <-chan core.AgentEvent,
	done <-chan struct{},
	bgTask *core.BackgroundTask,
	transcriptID string,
	task string,
) drainResult {
	var finalText string
	var firstError string
	var backgrounded bool
	evtDone := false
	for !evtDone {
		select {
		case <-done:
			for evt := range events {
				if evt.Type == core.EventTypeTextDelta || evt.Type == core.EventTypeAgentEnd {
					finalText += evt.Data.Text
				}
				if evt.Type == core.EventTypeError && firstError == "" {
					firstError = evt.Data.Error
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
			if evt.Type == core.EventTypeError && firstError == "" {
				firstError = evt.Data.Error
				r.cfg.Logger("DELEGATE_LOOP_ERROR: agent=%s error=%s", agentName, firstError)
			}
			if subscriber != nil {
				switch evt.Type {
				case core.EventTypeToolStart, core.EventTypeToolEnd, core.EventTypeToolUpdate,
					core.EventTypeTextDelta, core.EventTypeThinkingDelta:
					forwarded := evt
					forwarded.Data.AgentName = agentName
					core.SendEvent(subscriber, forwarded)
				}
			}
		case <-func() <-chan struct{} {
			if bgTask != nil {
				return bgTask.BackgroundReq
			}
			return nil
		}():
			backgrounded = true
			evtDone = true
		}
	}
	return drainResult{FinalText: finalText, FirstError: firstError, Backgrounded: backgrounded}
}

// handleBackgrounded drains remaining events from a backgrounded sub-agent,
// completes the task, and emits subagent_end.
func (r *ParallelRunner) handleBackgrounded(
	subscriber chan<- core.AgentEvent,
	agentName string,
	events <-chan core.AgentEvent,
	done <-chan struct{},
	bgTask *core.BackgroundTask,
	transcriptID string,
	task string,
	prefixText string,
	runErrPtr *error,
) {
	var bgText string
	for evt := range events {
		if evt.Type == core.EventTypeTextDelta || evt.Type == core.EventTypeAgentEnd {
			bgText += evt.Data.Text
		}
		if subscriber != nil {
			switch evt.Type {
			case core.EventTypeToolStart, core.EventTypeToolEnd, core.EventTypeToolUpdate,
				core.EventTypeTextDelta, core.EventTypeThinkingDelta:
				forwarded := evt
				forwarded.Data.AgentName = agentName
				core.SendEvent(subscriber, forwarded)
			}
		}
	}
	<-done
	var errStr string
	if *runErrPtr != nil {
		errStr = (*runErrPtr).Error()
	}
	r.cfg.TaskMgr.CompleteTracked(bgTask.ID, prefixText+bgText, 0, errStr)
	endStatus := "completed"
	if *runErrPtr != nil {
		endStatus = "error"
	}
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName:    agentName,
			Status:       endStatus,
			Task:         truncateTask(task, 120),
			TranscriptID: transcriptID,
			Text:         prefixText + bgText,
		},
	})
}

// jsonToToolSpec converts a core.Tool's schema into an OpenAITool spec.
func jsonToToolSpec(t core.Tool) core.OpenAITool {
	return core.OpenAITool{
		Type: "function",
		Function: core.FunctionDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		},
	}
}

// Close cleans up all live agents and pending async tasks.
func (r *ParallelRunner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for ref, la := range r.liveAgents {
		if closer, ok := la.Provider.(io.Closer); ok {
			closer.Close()
		}
		delete(r.liveAgents, ref)
	}
}

// RunDelegate runs a synchronous sub-agent.
// Delegates to RunDelegateTracked with empty agentName and nil delegatesTo.
func (r *ParallelRunner) RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32, modelID, sandboxTier string) (map[string]any, error) {
	return r.RunDelegateTracked(ctx, task, instructions, "", toolNames, maxRounds, temperature, modelID, sandboxTier, nil)
}

// RunDivisionDelegate creates a full sub-orchestrator for a division head.
// The division path points to a sub-directory with its own pux.yaml, roles, etc.
func (r *ParallelRunner) RunDivisionDelegate(ctx context.Context, task, divisionPath, modelID string) (map[string]any, error) {
	subscriber := subscriberFromCtx(ctx)
	if r.cfg.OrchestratorFactory == nil {
		return nil, fmt.Errorf("recursive delegation not available: no orchestrator factory configured")
	}
	if r.cfg.Depth >= r.cfg.MaxDepth {
		return nil, fmt.Errorf("max delegation depth (%d) reached", r.cfg.MaxDepth)
	}

	agentName := filepath.Base(divisionPath)

	// Resolve division path relative to project root
	absPath := divisionPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(r.cfg.ProjectDir, divisionPath)
	}

	// Verify pux.yaml exists in the division directory
	puxPath := filepath.Join(absPath, "pux.yaml")
	if _, err := os.Stat(puxPath); err != nil {
		return nil, fmt.Errorf("division directory %q has no pux.yaml: %w", absPath, err)
	}

	r.cfg.Logger("DIVISION_DELEGATE: path=%s depth=%d model=%q", absPath, r.cfg.Depth+1, modelID)

	// Emit subagent_start
	transcriptID := r.makeTranscriptID(agentName)
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentStart,
		Data: core.AgentEventData{
			AgentName:    agentName,
			Task:         truncateTask(task, 120),
			ToolName:     "delegate_to",
			TranscriptID: transcriptID,
		},
	})

	subOrch, err := r.cfg.OrchestratorFactory(ctx, SubOrchestratorConfig{
		DivisionPath: absPath,
		ProjectDir:   r.cfg.ProjectDir,
		SandboxID:    r.cfg.BaseSession.ID(),
		Depth:        r.cfg.Depth + 1,
		ModelID:      modelID,
	})
	if err != nil {
		core.SendEvent(subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: err.Error(), TranscriptID: transcriptID},
		})
		return nil, fmt.Errorf("failed to create division orchestrator: %w", err)
	}
	defer subOrch.Close()

	// Register as foreground task for Ctrl+B backgrounding
	var bgTask *core.BackgroundTask
	if r.cfg.TaskMgr != nil {
		bgTask, _ = r.cfg.TaskMgr.StartTracked(
			fmt.Sprintf("division %s", agentName),
			truncateTask(task, 120),
		)
	}

	// Run the sub-orchestrator, collect events
	events := make(chan core.AgentEvent, 128)
	done := make(chan struct{})
	var runErr error

	go func() {
		defer close(done)
		defer close(events)
		runErr = subOrch.Run(ctx, task, events)
	}()

	dr := r.drainAndForward(ctx, subscriber, agentName, events, done, bgTask, transcriptID, task)

	// Handle backgrounded division delegation
	if dr.Backgrounded && bgTask != nil {
		go r.handleBackgrounded(subscriber, agentName, events, done, bgTask, transcriptID, task, dr.FinalText, &runErr)
		return map[string]any{
			"status":    "backgrounded",
			"task_id":   bgTask.ID,
			"division":  divisionPath,
			"message":   fmt.Sprintf("Division %s sent to background. Use task_output with task_id '%s' to check results.", agentName, bgTask.ID),
		}, nil
	}

	// Emit subagent_end
	endStatus := "completed"
	if runErr != nil {
		endStatus = "error"
	}
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName:    agentName,
			Status:       endStatus,
			Task:         truncateTask(task, 120),
			TranscriptID: transcriptID,
			Text:         dr.FinalText,
		},
	})

	if runErr != nil {
		return map[string]any{"error": runErr.Error(), "partial_result": dr.FinalText}, runErr
	}

	// Summarize long sub-agent results before returning to CTO
	finalText := r.maybeSummarize(ctx, dr.FinalText)

	return map[string]any{"result": finalText, "status": "completed", "division": divisionPath}, nil
}

// RunDelegateAsync launches a sub-agent in a goroutine. Returns immediately.

// RunDelegateTracked runs a sub-agent with file change tracking.
// Returns the result, an agent reference for continuation, and file changes.
func (r *ParallelRunner) RunDelegateTracked(ctx context.Context, task, instructions, agentName string, toolNames []string, maxRounds int, temperature float32, modelID string, sandboxTier string, delegatesTo []string) (map[string]any, error) {
	subscriber := subscriberFromCtx(ctx)
	// Take pre-snapshot
	var snapshotID string
	if r.cfg.Snapshotter != nil && r.cfg.ProjectDir != "" {
		var err error
		snapshotID, err = r.cfg.Snapshotter.Snapshot(ctx, r.cfg.ProjectDir)
		if err != nil {
			r.cfg.Logger("SNAPSHOT_WARN: failed to snapshot: %v", err)
		}
	}

	// Use caller-provided agentName (original role name like "shell_ops"),
	// fall back to extracting from instructions if empty
	if agentName == "" {
		agentName = extractAgentName(instructions)
	}

	// Generate deterministic transcript ID before emitting event
	transcriptID := r.makeTranscriptID(agentName)

	// Emit subagent_start to parent subscriber
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentStart,
		Data: core.AgentEventData{
			AgentName:    agentName,
			Task:         truncateTask(task, 120),
			ToolName:     "delegate_to",
			TranscriptID: transcriptID,
		},
	})

	// Filter tools to only those requested
	var selectedTools []core.OpenAITool
	toolSet := make(map[string]bool)
	for _, name := range toolNames {
		toolSet[name] = true
	}
	for _, t := range r.cfg.ToolSpecs {
		if toolSet[t.Function.Name] {
			selectedTools = append(selectedTools, t)
		}
	}
	if len(selectedTools) == 0 {
		r.cfg.Logger("DELEGATE_FAIL: agent=%s no tools found — requested=%v available=%v", agentName, toolNames, toolNamesFromSpecs(r.cfg.ToolSpecs))
		core.SendEvent(subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: fmt.Sprintf("none of the requested tools were found: %v", toolNames), TranscriptID: transcriptID},
		})
		return nil, core.NewToolError("delegate_to", fmt.Sprintf("none of the requested tools were found: %v", toolNames))
	}

	// Inject scoped delegation tools if the sub-agent's role has delegates_to
	var scopedDelegate, scopedAsync, scopedCollect core.Tool
	if len(delegatesTo) > 0 && r.cfg.Depth < r.cfg.MaxDepth && r.cfg.RoleProvider != nil {
		scopedDelegate = NewDelegateToToolDynamic(r, r.cfg.MCPResolver, r.cfg.RoleProvider, func() []string { return delegatesTo })
		scopedAsync = NewDelegateAsyncToolDynamic(r, r.cfg.MCPResolver, r.cfg.RoleProvider, func() []string { return delegatesTo })
		scopedCollect = NewCollectResultsTool(r)
		selectedTools = append(selectedTools,
			jsonToToolSpec(scopedDelegate),
			jsonToToolSpec(scopedAsync),
			jsonToToolSpec(scopedCollect),
		)
		r.cfg.Logger("SCOPED_DELEGATION: sub-agent %q can delegate to %v", agentName, delegatesTo)
	}

	// Create isolated provider — kept alive for delegate_continue
	provider := r.cfg.ProviderFactory()
	if provider == nil {
		r.cfg.Logger("DELEGATE_FAIL: agent=%s providerFactory returned nil", agentName)
		core.SendEvent(subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: "provider factory returned nil", TranscriptID: transcriptID},
		})
		return nil, core.NewToolError("delegate_to", "provider factory returned nil for sub-agent "+agentName)
	}
	if modelID != "" && r.cfg.ModelResolver != nil {
		if resolved := r.cfg.ModelResolver(modelID); resolved != nil {
			if closer, ok := provider.(io.Closer); ok {
				closer.Close()
			}
			provider = resolved
			r.cfg.Logger("MODEL_OVERRIDE: using %s for sub-agent", modelID)
		}
	}

	// Enrich task with parent context
	enrichedTask := r.enrichTask(ctx, task)

	// Auto-director: raise Chrome for VNC visibility when browser tools are detected.
	if r.cfg.RaiseBrowserFunc != nil && hasBrowserTools(toolNames) {
		r.cfg.RaiseBrowserFunc(ctx)
	}

	// Build sub-agent config
	cfg := core.AgentLoopConfig{
		SystemPrompt:   instructions,
		MaxToolRounds:  maxRounds,
		MaxTokens:      8192,
		ContextSize:    r.cfg.ContextSize,
		Tools:          selectedTools,
		ToolResultProcessor: subAgentResultProcessor(r.cfg.ProjectDir),
		Opts: core.GenerateOptions{
			MaxTokens:   8192,
			Temperature: temperature,
			TopP:        0.95,
			TopK:        20,
		},
	}

	sess := &subSession{
		Session:   r.cfg.BaseSession,
		msgCount:  0,
		db:        r.cfg.DB,
		project:   r.cfg.Project,
		dbAgentID: transcriptID,
	}
	executor := r.cfg.Executor
	if r.cfg.ExecutorFactory != nil && sandboxTier != "" {
		executor = r.cfg.ExecutorFactory(sandboxTier)
	}
	// If scoped delegation tools were injected, wrap the executor so it can find them
	if len(delegatesTo) > 0 && r.cfg.Depth < r.cfg.MaxDepth {
		executor = &scopedDelegationExecutor{
			parent:   executor,
			delegate: scopedDelegate,
			async:    scopedAsync,
			collect:  scopedCollect,
		}
	}
	// Wrap sub-agent executor with vision processing (always)
	{
		vExec := vision.NewVisionAwareExecutor(executor, r.cfg.VisionChain, log.New(io.Discard, "", 0))
		vExec.SetNativeVision(r.cfg.NativeVision)
		if r.cfg.VisualContext != nil {
			vExec.SetVisualContext(r.cfg.VisualContext)
		}
		executor = vExec
	}
	// Use BaseAgent so sub-agents get common hooks (scratch pad re-injection, etc.)
	subAgent := agents.NewBaseAgent(agents.BaseConfig{
		Provider:        provider,
		Session:         sess,
		SystemPrompt:    instructions,
		ToolSpecs:       selectedTools,
		Executor:        executor,
		MaxToolRounds:   maxRounds,
		MaxTokens:       8192,
		ContextSize:     r.cfg.ContextSize,
		ProjectDir:      r.cfg.ProjectDir,
		GenerateOptions: core.GenerateOptions{MaxTokens: 8192, Temperature: temperature, TopP: 0.95, TopK: 20},
		ScratchStore:    r.cfg.ScratchStore,
		ToolResultProcessor: subAgentResultProcessor(r.cfg.ProjectDir),
	})
	loop := subAgent.Loop()

	r.cfg.Logger("DELEGATE_START: agent=%s tools=%v maxRounds=%d model=%q sandboxTier=%q", agentName, toolNamesFromSpecs(selectedTools), maxRounds, modelID, sandboxTier)

	// Register as a foreground task so Ctrl+B can background the delegation
	var bgTask *core.BackgroundTask
	if r.cfg.TaskMgr != nil {
		bgTask, _ = r.cfg.TaskMgr.StartTracked(
			fmt.Sprintf("delegate_to %s", agentName),
			truncateTask(task, 120),
		)
	}

	// Run the loop
	events := make(chan core.AgentEvent, 128)
	done := make(chan struct{})
	var runErr error

	go func() {
		defer close(done)
		defer close(events)
		runErr = loop.Run(ctx, enrichedTask, events)
		if runErr != nil {
			r.cfg.Logger("DELEGATE_LOOP_ERR: agent=%s err=%v", agentName, runErr)
		}
	}()

	// Drain events: accumulate text result + forward tool events to parent subscriber
	// Also watch for background signal (Ctrl+B) if TaskManager is wired in.
	dr := r.drainAndForward(ctx, subscriber, agentName, events, done, bgTask, transcriptID, task)

	// If backgrounded, spin up a goroutine to collect the final result and
	// store it in the TaskManager so the user can check on it later.
	if dr.Backgrounded && bgTask != nil {
		go r.handleBackgrounded(subscriber, agentName, events, done, bgTask, transcriptID, task, dr.FinalText, &runErr)

		agentRef := fmt.Sprintf("%s-%d", agentName, time.Now().UnixMilli())
		return map[string]any{
			"status":    "backgrounded",
			"task_id":   bgTask.ID,
			"agent_ref": agentRef,
			"message":   fmt.Sprintf("Delegation to %s sent to background. Use task_output with task_id '%s' to check results.", agentName, bgTask.ID),
		}, nil
	}

	// Emit subagent_end
	endStatus := "completed"
	if runErr != nil {
		endStatus = "error"
	}
	// Log final outcome with error details for debugging insta-failures
	if runErr != nil {
		r.cfg.Logger("DELEGATE_DONE: agent=%s status=error err=%v firstLoopError=%s", agentName, runErr, dr.FirstError)
	} else {
		r.cfg.Logger("DELEGATE_DONE: agent=%s status=ok textLen=%d", agentName, len(dr.FinalText))
	}
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName:    agentName,
			Status:       endStatus,
			Task:         truncateTask(task, 120),
			TranscriptID: transcriptID,
			Error:        dr.FirstError,
			Text:         dr.FinalText,
		},
	})

	// Generate agent_ref
	agentRef := fmt.Sprintf("%s-%d", agentName, time.Now().UnixMilli())

	// Compute diff
	var changes *ChangeSet
	if r.cfg.Snapshotter != nil && snapshotID != "" && r.cfg.ProjectDir != "" {
		var diffErr error
		changes, diffErr = r.cfg.Snapshotter.Diff(ctx, r.cfg.ProjectDir, snapshotID)
		if diffErr != nil {
			r.cfg.Logger("DIFF_WARN: failed to diff: %v", diffErr)
		}
	}

	// Store in liveAgents for delegate_continue, and completedSnapshots for delegate_revert
	r.mu.Lock()
	r.completedSnapshots[agentRef] = snapshotID
	if runErr == nil {
		r.liveAgents[agentRef] = &liveAgent{
			ID:        agentRef,
			Role:      agentName,
			Task:      task,
			Session:   sess,
			Provider:  provider,
			Config:    cfg,
			Snapshot:  snapshotID,
			StartedAt: time.Now(),
		}
	} else {
		// On error, don't keep alive — close the provider
		if closer, ok := provider.(io.Closer); ok {
			closer.Close()
		}
	}
	r.mu.Unlock()

	// Build result — return only the sub-agent's final summary, not its entire context.
	// The full transcript is persisted to DB. The CTO only needs the artifact (outcome).
	if runErr != nil {
		// On error, include partial text for debugging
		result := map[string]any{"error": runErr.Error(), "partial_result": dr.FinalText, "agent_ref": agentRef}
		if changes != nil {
			result["changes"] = map[string]any{
				"files":   changes.Files,
				"summary": changes.Summary,
			}
		}
		return result, runErr
	}

	// Extract the sub-agent's final message (not its entire accumulated output).
	// This is the artifact — the concise summary the CTO needs.
	artifact := extractLastAssistantFromSession(sess)
	if artifact == "" {
		artifact = dr.FinalText // fallback if session has no assistant messages
	}
	artifact = r.maybeSummarize(ctx, artifact)

	enriched := map[string]any{"result": artifact, "status": "completed", "agent_ref": agentRef}
	if changes != nil {
		enriched["changes"] = map[string]any{
			"files":   changes.Files,
			"summary": changes.Summary,
		}
	}

	return enriched, nil
}

// RunDelegateContinue sends feedback to an existing sub-agent and continues its work.
// The subagent's session is compacted first, then the feedback is added as a new user message.
func (r *ParallelRunner) RunDelegateContinue(ctx context.Context, agentRef, feedback string) (map[string]any, error) {
	subscriber := subscriberFromCtx(ctx)
	r.mu.Lock()
	la, ok := r.liveAgents[agentRef]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no live agent with ref %q — agent may have been reverted or expired. Use delegate_to to start a new delegation instead", agentRef)
	}

	r.cfg.Logger("CONTINUE: agent=%s feedback_len=%d", la.Role, len(feedback))

	// Compact the session to make room for the feedback
	la.Session.Compact(ctx, "Context compacted for continuation")

	// Create a new agent loop with the SAME session (has full history)
	continueExecutor := core.ToolExecutor(r.cfg.Executor)
	{
		vExec := vision.NewVisionAwareExecutor(continueExecutor, r.cfg.VisionChain, log.New(io.Discard, "", 0))
		vExec.SetNativeVision(r.cfg.NativeVision)
		if r.cfg.VisualContext != nil {
			vExec.SetVisualContext(r.cfg.VisualContext)
		}
		continueExecutor = vExec
	}
	// Use BaseAgent for continuation so sub-agents keep getting common hooks
	continueAgent := agents.NewBaseAgent(agents.BaseConfig{
		Provider:        la.Provider,
		Session:         la.Session,
		SystemPrompt:    la.Config.SystemPrompt,
		ToolSpecs:       la.Config.Tools,
		Executor:        continueExecutor,
		MaxToolRounds:   la.Config.MaxToolRounds,
		ContextSize:     la.Config.ContextSize,
		ProjectDir:      r.cfg.ProjectDir,
		GenerateOptions: la.Config.Opts,
		ScratchStore:    r.cfg.ScratchStore,
		ToolResultProcessor: la.Config.ToolResultProcessor,
	})
	loop := continueAgent.Loop()

	// Emit subagent_start (continuation) — reuse existing session's transcript ID
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentStart,
		Data: core.AgentEventData{
			AgentName:    la.Role,
			Task:         truncateTask(fmt.Sprintf("continuation: %s", feedback), 120),
			ToolName:     "delegate_continue",
			TranscriptID: la.Session.dbAgentID,
		},
	})

	events := make(chan core.AgentEvent, 128)
	done := make(chan struct{})
	var runErr error

	go func() {
		defer close(done)
		defer close(events)
		// Continue uses the existing session + new feedback as user message
		runErr = loop.Continue(ctx, feedback, events)
	}()

	dr := r.drainAndForward(ctx, subscriber, la.Role, events, done, nil, la.Session.dbAgentID, feedback)

	// Emit subagent_end
	endStatus := "completed"
	if runErr != nil {
		endStatus = "error"
	}
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName:    la.Role,
			Status:       endStatus,
			Task:         truncateTask(feedback, 120),
			TranscriptID: la.Session.dbAgentID,
			Text:         dr.FinalText,
		},
	})

	// Compute updated diff
	var changes *ChangeSet
	if r.cfg.Snapshotter != nil && la.Snapshot != "" && r.cfg.ProjectDir != "" {
		var diffErr error
		changes, diffErr = r.cfg.Snapshotter.Diff(ctx, r.cfg.ProjectDir, la.Snapshot)
		if diffErr != nil {
			r.cfg.Logger("DIFF_WARN: failed to diff after continue: %v", diffErr)
		}
	}

	enriched := map[string]any{"agent_ref": agentRef}
	if runErr != nil {
		enriched["error"] = runErr.Error()
		enriched["partial_result"] = dr.FinalText
		return enriched, runErr
	}

	enriched["result"] = r.maybeSummarize(ctx, dr.FinalText)
	enriched["status"] = "continued"
	if changes != nil {
		enriched["changes"] = map[string]any{
			"files":   changes.Files,
			"summary": changes.Summary,
		}
	}
	return enriched, nil
}

// RevertAgent reverts file changes made by a sub-agent.
// Works with auto-accepted delegates via completedSnapshots.
func (r *ParallelRunner) RevertAgent(ctx context.Context, agentRef string) (map[string]any, error) {
	// Try live agents first (for delegate_continue sessions still alive)
	r.mu.Lock()
	la, isLive := r.liveAgents[agentRef]
	snapshotID := ""
	agentRole := ""
	if isLive {
		snapshotID = la.Snapshot
		agentRole = la.Role
		delete(r.liveAgents, agentRef)
	} else {
		// Try completed snapshots (auto-accepted delegates)
		snapshotID, isLive = r.completedSnapshots[agentRef]
		if isLive {
			delete(r.completedSnapshots, agentRef)
			agentRole = extractAgentName(agentRef)
		}
	}
	r.mu.Unlock()

	if !isLive {
		return nil, fmt.Errorf("no agent with ref %q (already reverted or expired)", agentRef)
	}

	// Close the provider if live agent
	if la != nil {
		if closer, ok := la.Provider.(io.Closer); ok {
			closer.Close()
		}
	}

	// Revert file changes
	var revertedFiles []string
	if r.cfg.Snapshotter != nil && snapshotID != "" && r.cfg.ProjectDir != "" {
		if err := r.cfg.Snapshotter.Revert(ctx, r.cfg.ProjectDir, snapshotID); err != nil {
			r.cfg.Logger("REVERT_WARN: failed to revert: %v", err)
			return map[string]any{
				"status":    "revert_failed",
				"agent_ref": agentRef,
				"error":     err.Error(),
			}, err
		}
		// Get the list of files that were reverted
		changes, _ := r.cfg.Snapshotter.Diff(ctx, r.cfg.ProjectDir, snapshotID)
		if changes != nil {
			revertedFiles = changes.Files
		}
	}

	r.cfg.Logger("REVERT: agent=%s ref=%s files=%v", agentRole, agentRef, revertedFiles)

	return map[string]any{
		"status":         "reverted",
		"agent_ref":      agentRef,
		"files_restored": revertedFiles,
	}, nil
}

// RunDelegateAsync launches a sub-agent in a goroutine. Returns immediately.
func (r *ParallelRunner) RunDelegateAsync(ctx context.Context, taskID, task, instructions, agentName string, toolNames []string, maxRounds int, temperature float32, modelID string, delegatesTo []string) (map[string]any, error) {
	// Capture subscriber from parent ctx before spawning goroutine.
	// The goroutine uses context.Background() to survive parent cancellation,
	// so we re-inject the subscriber into the background context.
	parentSubscriber := subscriberFromCtx(ctx)

	if r.cfg.Depth >= r.cfg.MaxDepth {
		return nil, fmt.Errorf("max delegation depth (%d) reached", r.cfg.MaxDepth)
	}

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

	r.cfg.Logger("ASYNC_DELEGATE: task_id=%s task=%q tools=%v", taskID, task, toolNames)

	// Launch in background goroutine with DETACHED context.
	// Using context.Background() ensures async sub-agents survive parent request
	// cancellation (SSE disconnect, browser close, etc.).
	go func() {
		defer r.wg.Done()
		defer close(t.Done)

		// Wrap in a long timeout for safety (30 minutes per async agent)
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		// Re-inject subscriber into background context so RunDelegateTracked can emit events
		if parentSubscriber != nil {
			bgCtx = context.WithValue(bgCtx, core.SubscriberKey{}, parentSubscriber)
		}
		defer cancel()

		// Use tracked delegation for file change tracking + agent_ref
		result, err := r.RunDelegateTracked(bgCtx, task, instructions, agentName, toolNames, maxRounds, temperature, modelID, "", delegatesTo)

		// Async delegates don't need to stay alive for continuation.
		// Clean up the live agent but keep the completedSnapshot for revert.
		if agentRef, ok := result["agent_ref"].(string); ok && agentRef != "" {
			r.mu.Lock()
			if la, isLive := r.liveAgents[agentRef]; isLive {
				if closer, ok := la.Provider.(io.Closer); ok {
					closer.Close()
				}
				delete(r.liveAgents, agentRef)
			}
			r.mu.Unlock()
		}

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
	r.cfg.Logger("COLLECT_ASYNC: waiting for %d pending tasks", r.pendingCount())

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

// subAgentResultProcessor returns a ToolResultProcessor for sub-agents.
// Large tool results are spilled to disk (inside the sandbox workspace) instead
// of being truncated. The agent gets a preview + file path reference and can
// read the full output with file_read if needed — same pattern as claude-code.
func subAgentResultProcessor(projectDir string) func(ctx context.Context, toolName, toolCallID, result string, toolArgs map[string]any) string {
	const spillThreshold = 4000  // bytes — spill anything larger
	const previewLines = 30      // lines of preview kept in-context

	spillDir := filepath.Join(projectDir, ".pux", "spill")
	// Sandbox path for the reference — sub-agent reads via file_read
	sandboxSpillDir := "/sandbox/workspace/.pux/spill"

	return func(ctx context.Context, toolName, toolCallID, result string, toolArgs map[string]any) string {
		if len(result) <= spillThreshold {
			return result
		}

		// Create spill directory on host (maps to sandbox via bind mount)
		if err := os.MkdirAll(spillDir, 0755); err != nil {
			// Can't spill — fall back to tail truncation
			tail := result
			if len(tail) > 3000 {
				tail = tail[len(tail)-3000:]
			}
			return "...[spill failed, showing tail]\n" + tail
		}

		// Generate deterministic file name from tool call
		ref := fmt.Sprintf("%s-%s", toolName, toolCallID)
		if len(ref) > 60 {
			ref = ref[:60]
		}
		hostPath := filepath.Join(spillDir, ref+".txt")

		// Write full output to disk
		if err := os.WriteFile(hostPath, []byte(result), 0644); err != nil {
			tail := result
			if len(tail) > 3000 {
				tail = tail[len(tail)-3000:]
			}
			return "...[spill write failed, showing tail]\n" + tail
		}

		// Build preview — first N lines
		lines := strings.Split(result, "\n")
		previewCount := previewLines
		if previewCount > len(lines) {
			previewCount = len(lines)
		}
		preview := strings.Join(lines[:previewCount], "\n")

		// Reference path inside sandbox
		sandboxPath := filepath.Join(sandboxSpillDir, ref+".txt")

		return fmt.Sprintf(
			"%s\n\n...[%d lines total. Full output written to %s — use file_read(\"%s\") to read it]",
			preview, len(lines), sandboxPath, sandboxPath,
		)
	}
}

// subSession is a minimal session that stores messages in memory.
// It wraps a parent session for context inheritance in sub-agents.
// When db is set, messages are also persisted to the database for transcript retrieval.
type subSession struct {
	core.Session // embed parent — delegates GetTree, Navigate, Branch, Fork, TruncateToolResults, ReplaceToolResults
	messages []core.Message
	msgCount int
	// Transcript persistence
	db          *storage.Database
	project     string // project name
	dbAgentID   string // composite agent_id for DB queries (e.g. "session-sub:marcus-1234")
}

func (s *subSession) ID() string { return s.Session.ID() + "-sub" }
func (s *subSession) Close() error { return nil }
func (s *subSession) AppendMessage(msg core.Message) error {
	s.messages = append(s.messages, msg)
	s.msgCount++

	// Persist to database if transcript storage is enabled
	if s.db != nil && s.project != "" && s.dbAgentID != "" {
		ctx := context.Background()
		switch msg.Role {
		case "user":
			_, _ = s.db.SaveUserMessage(ctx, s.project, s.dbAgentID, msg.Content)
		case "tool":
			toolName := msg.Name
			if toolName == "" {
				toolName = "unknown"
			}
			_, _ = s.db.SaveToolResult(ctx, s.project, s.dbAgentID, msg.ToolCallID, toolName, msg.Content)
		case "assistant":
			toolCallsJSON := "[]"
			if len(msg.ToolCalls) > 0 {
				if tcJSON, err := json.Marshal(msg.ToolCalls); err == nil {
					toolCallsJSON = string(tcJSON)
				}
			}
			_, _ = s.db.SaveAssistantMessage(ctx, s.project, s.dbAgentID, msg.Content, msg.ReasoningContent, toolCallsJSON)
		}
	}

	return nil
}
func (s *subSession) BuildContext(ctx context.Context) ([]core.Message, error) {
	return s.messages, nil
}
func (s *subSession) Compact(ctx context.Context, summary string) (string, error) {
	// Keep recent messages verbatim, truncate older ones.
	// Sub-agents should have lean context — compact aggressively.
	if len(s.messages) <= 6 {
		return "", nil
	}

	var compacted []core.Message
	kept := 0
	for i, msg := range s.messages {
		if i >= len(s.messages)-6 {
			// Keep the last 6 messages verbatim
			compacted = append(compacted, msg)
			kept++
		} else {
			// Older messages: truncate aggressively
			content := msg.Content
			switch msg.Role {
			case "tool":
				// Tool results: keep first 1000 chars
				if len(content) > 1000 {
					content = content[:1000] + "\n...[compacted]"
				}
			case "user", "assistant":
				// User/assistant: keep first 2000 chars
				if len(content) > 2000 {
					content = content[:2000] + "\n...[compacted]"
				}
			}
			if content != "" {
				compacted = append(compacted, core.Message{
					Role:       msg.Role,
					Content:    content,
					ToolCallID: msg.ToolCallID,
					Name:       msg.Name,
				})
				kept++
			}
		}
	}

	removed := len(s.messages) - kept
	s.messages = compacted
	s.msgCount = len(compacted)
	return fmt.Sprintf("compacted %d messages, kept %d", removed, kept), nil
}
func (s *subSession) GetCurrentNode() string { return s.Session.ID() + "-sub" }

// hasBrowserTools checks if the tool list contains browser/web automation tools.
// Used by the auto-director to decide whether to raise Chrome for VNC visibility.
func hasBrowserTools(toolNames []string) bool {
	browserTools := map[string]bool{
		"browse_to":         true,
		"sb_server":         true,
		"browser_navigate":  true,
		"browser_click":     true,
		"browser_type":      true,
		"browser_snapshot":  true,
		"browser_screenshot": true,
		"web_navigate":      true,
		"web_click":         true,
		"web_type":          true,
		"web_snapshot":      true,
		"web_screenshot":    true,
		"fill_form":         true,
		"find_element":      true,
		"click_element":     true,
		"navigate_url":      true,
		"get_page_text":     true,
		"execute_js":        true,
		"evaluate_js":       true,
		"read_page":         true,
		"download_file":     true,
	}
	for _, name := range toolNames {
		if browserTools[name] {
			return true
		}
		// Match patterns like "sb_*" for SeleniumBase tools
		if len(name) > 3 && name[:3] == "sb_" {
			return true
		}
	}
	return false
}

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

// maybeSummarize summarizes sub-agent results aggressively before returning to the CTO.
// Sub-agents should return artifacts, not their entire context.
// Falls back to tail-truncation if no summarizer is set or summarization fails.
func (r *ParallelRunner) maybeSummarize(ctx context.Context, text string) string {
	const summarizeThreshold = 500
	if len(text) <= summarizeThreshold {
		return text
	}

	// Use LLM summarizer if available — compress to concise artifact
	if r.cfg.Summarizer != nil {
		summarized, err := r.cfg.Summarizer(ctx, text, 400)
		if err == nil && len(summarized) >= 50 {
			r.cfg.Logger("SUMMARIZE_OK: %d -> %d chars", len(text), len(summarized))
			return summarized
		}
		r.cfg.Logger("SUMMARIZE_FAILED: len=%d err=%v, truncating", len(text), err)
	}

	// Fallback: keep only the tail (errors and final status are at the end)
	if len(text) > 600 {
		return "...[truncated]\n" + text[len(text)-500:]
	}
	return text
}

// extractLastAssistantFromSession returns the content of the last assistant message
// in the sub-session. This is the sub-agent's final summary — not its entire reasoning
// history. Used instead of accumulating all text_delta events across all turns.
func extractLastAssistantFromSession(sess *subSession) string {
	msgs := sess.messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			return msgs[i].Content
		}
	}
	return ""
}

// toolNamesFromSpecs extracts tool names from a list of tool specs (for logging).
func toolNamesFromSpecs(specs []core.OpenAITool) []string {
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Function.Name
	}
	return names
}

// discoverProjectContext reads project files (CLAUDE.md, build system files) and
// returns a brief summary for sub-agent context. Helps sub-agents know how to
// build, test, and lint the project without guessing.
func discoverProjectContext(projectDir string) string {
	var b strings.Builder
	const maxFileContent = 2000

	// Check for project instruction files — in priority order
	for _, name := range []string{
		"CLAUDE.md", "claude.md", ".claude/CLAUDE.md",
		"OpenCode.md", "opencode.md",
		".cursorrules",
	} {
		path := filepath.Join(projectDir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			content := string(data)
			if len(content) > maxFileContent {
				content = content[:maxFileContent] + "\n...(truncated)"
			}
			b.WriteString("Project instructions (" + name + "):\n")
			b.WriteString(content)
			b.WriteString("\n\n")
			break
		}
	}

	// Extract build/test sections from README if no instruction file found
	if b.Len() == 0 {
		for _, name := range []string{"README.md", "readme.md", "README.MD"} {
			path := filepath.Join(projectDir, name)
			data, err := os.ReadFile(path)
			if err == nil {
				content := extractRelevantReadme(string(data), maxFileContent)
				if content != "" {
					b.WriteString("README (build/test sections):\n")
					b.WriteString(content)
					b.WriteString("\n\n")
				}
				break
			}
		}
	}

	// Detect build system and suggest commands
	type buildSystem struct {
		file    string
		name    string
		build   string
		test    string
		lint    string
	}
	systems := []buildSystem{
		{"go.mod", "Go", "go build ./...", "go test ./...", "go vet ./..."},
		{"package.json", "Node.js", "npm run build", "npm test", "npm run lint"},
		{"Makefile", "Make", "make", "make test", "make lint"},
		{"Cargo.toml", "Rust", "cargo build", "cargo test", "cargo clippy"},
		{"pyproject.toml", "Python", "", "pytest", "ruff check ."},
		{"requirements.txt", "Python", "", "pytest", "ruff check ."},
	}

	for _, sys := range systems {
		if _, err := os.Stat(filepath.Join(projectDir, sys.file)); err == nil {
			b.WriteString("Build system: " + sys.name + " (" + sys.file + ")\n")
			if sys.build != "" {
				b.WriteString("Build: " + sys.build + "\n")
			}
			if sys.test != "" {
				b.WriteString("Test: " + sys.test + "\n")
			}
			if sys.lint != "" {
				b.WriteString("Lint: " + sys.lint + "\n")
			}
			break
		}
	}

	// Inject git status so sub-agents know what's changed
	if gitDir := filepath.Join(projectDir, ".git"); dirExists(gitDir) {
		if status, err := exec.Command("git", "-C", projectDir, "status", "--short").Output(); err == nil && len(status) > 0 {
			statusStr := string(status)
			if len(statusStr) > 1000 {
				statusStr = statusStr[:1000] + "\n...(truncated)"
			}
			b.WriteString("Git status:\n" + statusStr + "\n")
		}
		if diff, err := exec.Command("git", "-C", projectDir, "diff", "--stat", "HEAD").Output(); err == nil && len(diff) > 0 {
			diffStr := string(diff)
			if len(diffStr) > 1000 {
				diffStr = diffStr[:1000] + "\n...(truncated)"
			}
			b.WriteString("Git diff --stat:\n" + diffStr + "\n")
		}
	}

	// Top-level directory listing so agents know the file structure
	if entries, err := os.ReadDir(projectDir); err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue // skip hidden files
			}
			if e.IsDir() {
				names = append(names, e.Name()+"/")
			} else {
				names = append(names, e.Name())
			}
			if len(names) >= 30 {
				names = append(names, "...")
				break
			}
		}
		if len(names) > 0 {
			b.WriteString("Files: " + strings.Join(names, ", ") + "\n")
		}
	}

	return b.String()
}

// dirExists reports whether the path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// extractRelevantReadme pulls build, test, install, and usage sections
// from a README so sub-agents know how to work with the project.
func extractRelevantReadme(content string, maxLen int) string {
	// Target section headers that contain useful build/test info
	keywords := []string{"build", "install", "test", "usage", "run", "getting started", "quick start", "setup", "develop"}
	lines := strings.Split(content, "\n")
	var relevant []string
	inRelevant := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Detect section headers (## or ###)
		if strings.HasPrefix(trimmed, "##") {
			headerLower := strings.ToLower(trimmed)
			inRelevant = false
			for _, kw := range keywords {
				if strings.Contains(headerLower, kw) {
					inRelevant = true
					break
				}
			}
		}
		if inRelevant {
			relevant = append(relevant, line)
			totalLen := len(strings.Join(relevant, "\n"))
			if totalLen > maxLen {
				break
			}
		}
	}

	if len(relevant) == 0 {
		return ""
	}
	result := strings.Join(relevant, "\n")
	if len(result) > maxLen {
		result = result[:maxLen] + "\n...(truncated)"
	}
	return result
}

// scopedDelegationExecutor wraps a parent executor and adds scoped delegate_to/delegate_async
// tools so sub-agents with delegates_to can execute nested delegations.
type scopedDelegationExecutor struct {
	parent   core.ToolExecutor
	delegate core.Tool
	async    core.Tool
	collect  core.Tool
}

func (e *scopedDelegationExecutor) Execute(ctx context.Context, toolName string, args map[string]any) (any, error) {
	switch toolName {
	case "delegate_to":
		return e.delegate.Execute(ctx, args)
	case "delegate_async":
		return e.async.Execute(ctx, args)
	case "collect_results":
		return e.collect.Execute(ctx, args)
	default:
		return e.parent.Execute(ctx, toolName, args)
	}
}
