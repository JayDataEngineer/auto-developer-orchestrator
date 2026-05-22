package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

	// File change tracking
	snapshotter Snapshotter // git-based snapshot/diff/revert

	// Per-tier executor override: when set, uses this instead of r.executor
	executorFactory func(tier string) core.ToolExecutor

	// Visual context for vision caching: when set, sub-agent executors are wrapped
	// with VisionAwareExecutor to skip vision calls when the page hasn't changed.
	visualContext   vision.VisualContext
	visionChain     *vision.FallbackChain
	nativeVision    bool // true if sub-agent models support native image_url
	// NOTE: nativeVision is inherited from the CTO's engine. Workers with different
	// models (e.g. code_ops uses DeepSeek text-only) are safe because DetectImage()
	// only triggers for browser/desktop tools — workers without those never hit the
	// vision path. If a future worker mixes browser capability with a text-only model,
	// this needs per-worker HasVision() resolution.
	visionLogger    func(format string, args ...interface{})

	// Auto-director: raise browser window for VNC visibility when browser agent starts
	raiseBrowserFunc func(ctx context.Context) // injected by orchestrator if sandbox available

	// Scoped delegation: when set, sub-agents with delegates_to get scoped delegation tools
	roleProvider RoleProvider
	mcpResolver  MCPResolver

	// Sub-agent result summarization: when set, long sub-agent results are summarized
	// before returning to the CTO instead of just being truncated.
	summarizer SummarizerFunc

	// Background task manager: when set, synchronous delegations can be
	// backgrounded by the user (Ctrl+B). The delegation registers as a foreground
	// task; Ctrl+B signals BackgroundReq; the subagent keeps running.
	taskMgr *core.TaskManager

	// Shared scratch store for sub-agent scratch pad hooks (same instance as CTO).
	scratchStore *ctxpkg.ScratchStore

	// Sub-agent transcript persistence: when set, sub-agent messages are stored
	// in the database so their full chat log can be retrieved later.
	db             *storage.Database
	project        string // project name for DB storage

	mu                sync.Mutex
	tasks             map[string]*asyncTask
	wg                sync.WaitGroup
	liveAgents        map[string]*liveAgent // kept-alive sub-agents for continuation
	completedSnapshots map[string]string    // agentRef → snapshotID for auto-accepted delegates (enables revert)
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
func NewParallelRunner(executor core.ToolExecutor, toolSpecs []core.OpenAITool, baseSession core.Session, ctxSize int, modelResolver ModelResolver) *ParallelRunner {
	// Enforce minimum context size for sub-agents
	if ctxSize < 50000 {
		ctxSize = 50000
	}
	return &ParallelRunner{
		executor:      executor,
		toolSpecs:     toolSpecs,
		baseSession:   baseSession,
		ctxSize:       ctxSize,
		modelResolver: modelResolver,
		logger:        func(format string, args ...interface{}) {},
		tasks:             make(map[string]*asyncTask),
		liveAgents:        make(map[string]*liveAgent),
		completedSnapshots: make(map[string]string),
	}
}

// SetProviderFactory sets the factory for creating isolated providers per sub-agent.
func (r *ParallelRunner) SetProviderFactory(factory ProviderFactory) {
	r.providerFactory = factory
}

// SetSnapshotter sets the git-based snapshotter for change tracking.
func (r *ParallelRunner) SetSnapshotter(s Snapshotter) {
	r.snapshotter = s
}

// SetExecutorFactory sets a per-tier executor override.
// When set, the factory is called with the role's sandbox tier to produce
// an appropriate executor (e.g., HostExecutor for native tier).
func (r *ParallelRunner) SetExecutorFactory(f func(tier string) core.ToolExecutor) {
	r.executorFactory = f
}

// SetRaiseBrowserFunc sets a callback that raises Chrome to the foreground
// in the sandbox. Called automatically when browser tools are detected
// in a delegation, so the VNC stream shows the browser.
func (r *ParallelRunner) SetRaiseBrowserFunc(f func(ctx context.Context)) {
	r.raiseBrowserFunc = f
}

// SetTaskManager enables Ctrl+B backgrounding for synchronous delegations.
// When set, delegate_to registers as a foreground task; the user can press
// Ctrl+B to send it to the background while the subagent keeps running.
func (r *ParallelRunner) SetTaskManager(m *core.TaskManager) {
	r.taskMgr = m
}

// SetScratchStore sets the shared scratch store for sub-agent scratch pad hooks.
// When set, sub-agents get scratch pad re-injection just like the CTO.
func (r *ParallelRunner) SetScratchStore(s *ctxpkg.ScratchStore) {
	r.scratchStore = s
}

// SetVisualContext enables frame-based vision caching for sub-agent executors.
// When set, sub-agents that produce screenshots will have their vision calls
// skipped if the page hasn't changed since the last description.
func (r *ParallelRunner) SetVisualContext(vc vision.VisualContext, chain *vision.FallbackChain, nativeVision bool, logger func(format string, args ...interface{})) {
	r.visualContext = vc
	r.visionChain = chain
	r.nativeVision = nativeVision
	r.visionLogger = logger
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

	// Append workspace path awareness so sub-agents always write to the bind-mounted directory
	if r.projectDir != "" {
		b.WriteString("\n\n<workspace_note>\n")
		b.WriteString("All file operations must target /sandbox/workspace/ (the project root mount). ")
		b.WriteString("Do NOT write to /home/ubuntu/ or other paths — they are not persisted to the host. ")
		b.WriteString("The project files appear at /sandbox/workspace/ and that is the only persistent location.\n")
		b.WriteString("</workspace_note>")

		// Inject project context — CLAUDE.md, build system hints
		projectCtx := discoverProjectContext(r.projectDir)
		if projectCtx != "" {
			b.WriteString("\n\n<project_context>\n")
			b.WriteString(projectCtx)
			b.WriteString("\n</project_context>")
		}
	}

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

// SetRoleProviders configures role resolution for scoped delegation.
// When set, sub-agents whose roles have DelegatesTo will get scoped delegation tools.
func (r *ParallelRunner) SetRoleProviders(roleProvider RoleProvider, mcpResolver MCPResolver) {
	r.roleProvider = roleProvider
	r.mcpResolver = mcpResolver
}

func (r *ParallelRunner) SetSummarizer(fn SummarizerFunc) {
	r.summarizer = fn
}

// SetTranscriptDB enables sub-agent transcript persistence to the database.
// When set, sub-agent messages (user, assistant, tool) are stored in conversation_messages
// with a composite agent_id so they can be retrieved later via the history API.
func (r *ParallelRunner) SetTranscriptDB(db *storage.Database, project string) {
	r.db = db
	r.project = project
}

// makeTranscriptID builds a deterministic composite ID for subagent transcript persistence.
// Returns empty string if baseSession is nil (e.g. in tests).
func (r *ParallelRunner) makeTranscriptID(agentName string) string {
	if r.baseSession == nil {
		return ""
	}
	return r.baseSession.ID() + ":sub:" + agentName + "-" + fmt.Sprintf("%d", time.Now().UnixMilli())
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
func (r *ParallelRunner) RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32, modelID string, sandboxTier string) (map[string]any, error) {
	agentName := extractAgentName(instructions)
	subscriber := subscriberFromCtx(ctx)
	r.logger("SYNC_DELEGATE: task=%q agent=%s tools=%v model=%q", task, agentName, toolNames, modelID)

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
	for _, t := range r.toolSpecs {
		if toolSet[t.Function.Name] {
			selectedTools = append(selectedTools, t)
		}
	}
	if len(selectedTools) == 0 {
		r.logger("DELEGATE_FAIL: agent=%s no tools found — requested=%v available=%v", agentName, toolNames, toolNamesFromSpecs(r.toolSpecs))
		core.SendEvent(subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: fmt.Sprintf("none of the requested tools were found: %v", toolNames), TranscriptID: transcriptID},
		})
		return nil, core.NewToolError("delegate_to", fmt.Sprintf("none of the requested tools were found: %v", toolNames))
	}

	// Create isolated provider for this sub-agent via factory.
	// Each sub-agent gets its own Adapter → own session → own llama-server slot.
	// If modelID is specified, use the model resolver to pick the right engine instead.
	provider := r.providerFactory()
	if provider == nil {
		r.logger("DELEGATE_FAIL: agent=%s providerFactory returned nil", agentName)
		core.SendEvent(subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: "provider factory returned nil", TranscriptID: transcriptID},
		})
		return nil, core.NewToolError("delegate_to", "provider factory returned nil for sub-agent "+agentName)
	}
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

	// Auto-director: raise Chrome for VNC visibility when browser tools are detected.
	if r.raiseBrowserFunc != nil && hasBrowserTools(toolNames) {
		r.raiseBrowserFunc(ctx)
	}

	sess := &subSession{
		parent:    r.baseSession,
		msgCount:  0,
		db:        r.db,
		project:   r.project,
		dbAgentID: transcriptID,
	}
	executor := r.executor
	if r.executorFactory != nil && sandboxTier != "" {
		executor = r.executorFactory(sandboxTier)
	}
	// Wrap sub-agent executor with vision processing (always)
	// Native vision: extracts image_url, strips base64 from text
	// Fallback: describes via chain when model can't see
	{
		vExec := vision.NewVisionAwareExecutor(executor, r.visionChain, log.New(io.Discard, "", 0))
		vExec.SetNativeVision(r.nativeVision)
		if r.visualContext != nil {
			vExec.SetVisualContext(r.visualContext)
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
		ContextSize:     r.ctxSize,
		ProjectDir:      r.projectDir,
		GenerateOptions: core.GenerateOptions{MaxTokens: 8192, Temperature: temperature, TopP: 0.95, TopK: 20},
		ScratchStore:    r.scratchStore,
		ToolResultProcessor: subAgentResultProcessor(),
	})
	loop := subAgent.Loop()

	r.logger("DELEGATE_START: agent=%s tools=%v maxRounds=%d model=%q sandboxTier=%q", agentName, toolNamesFromSpecs(selectedTools), maxRounds, modelID, sandboxTier)

	// Register as foreground task for Ctrl+B backgrounding
	var bgTask *core.BackgroundTask
	if r.taskMgr != nil {
		bgTask, _ = r.taskMgr.StartTracked(
			fmt.Sprintf("delegate %s", agentName),
			truncateTask(task, 120),
		)
	}

	// Collect events into result
	events := make(chan core.AgentEvent, 128)
	done := make(chan struct{})
	var runErr error

	go func() {
		defer close(done)
		defer close(events)
		runErr = loop.Run(ctx, enrichedTask, events)
		if runErr != nil {
			r.logger("DELEGATE_LOOP_ERR: agent=%s err=%v", agentName, runErr)
		}
	}()

	// Drain events: accumulate text result + forward tool events to parent subscriber
	var finalText string
	var firstError string
	var backgrounded bool
	evtDone := false
	for !evtDone {
		select {
		case <-done:
			// Drain remaining events
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
				r.logger("DELEGATE_LOOP_ERROR: agent=%s error=%s", agentName, firstError)
			}
			// Forward tool + text/thinking events to parent subscriber with agent context
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

	// Handle backgrounded delegation — subagent keeps running in goroutine
	if backgrounded && bgTask != nil {
		go func() {
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
			if runErr != nil {
				errStr = runErr.Error()
			}
			r.taskMgr.CompleteTracked(bgTask.ID, finalText+bgText, 0, errStr)
			endStatus := "completed"
			if runErr != nil {
				endStatus = "error"
			}
			core.SendEvent(subscriber, core.AgentEvent{
				Type: core.EventTypeSubAgentEnd,
				Data: core.AgentEventData{AgentName: agentName, Status: endStatus, Task: truncateTask(task, 120), TranscriptID: transcriptID},
			})
		}()
		return map[string]any{
			"status":  "backgrounded",
			"task_id": bgTask.ID,
			"message": fmt.Sprintf("Delegation to %s sent to background. Use task_output with task_id '%s' to check results.", agentName, bgTask.ID),
		}, nil
	}

	// Emit subagent_end
	endStatus := "completed"
	if runErr != nil {
		endStatus = "error"
	}
	if runErr != nil {
		r.logger("DELEGATE_DONE: agent=%s status=error err=%v firstLoopError=%s", agentName, runErr, firstError)
	} else {
		r.logger("DELEGATE_DONE: agent=%s status=ok textLen=%d", agentName, len(finalText))
	}
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName:    agentName,
			Status:       endStatus,
			Task:         truncateTask(task, 120),
			TranscriptID: transcriptID,
			Error:        firstError,
		},
	})

	if runErr != nil {
		return map[string]any{"error": runErr.Error(), "partial_result": finalText}, runErr
	}

	// Return only the final assistant message, not the entire accumulated context
	artifact := extractLastAssistantFromSession(sess)
	if artifact == "" {
		artifact = finalText
	}
	artifact = r.maybeSummarize(ctx, artifact)

	return map[string]any{"result": artifact, "status": "completed"}, nil
}

// RunDivisionDelegate creates a full sub-orchestrator for a division head.
// The division path points to a sub-directory with its own pux.yaml, roles, etc.
func (r *ParallelRunner) RunDivisionDelegate(ctx context.Context, task, divisionPath, modelID string) (map[string]any, error) {
	subscriber := subscriberFromCtx(ctx)
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

	subOrch, err := r.orchestratorFactory(ctx, SubOrchestratorConfig{
		DivisionPath: absPath,
		ProjectDir:   r.projectDir,
		SandboxID:    r.baseSession.ID(),
		Depth:        r.depth + 1,
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
	if r.taskMgr != nil {
		bgTask, _ = r.taskMgr.StartTracked(
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

	var finalText string
	var backgrounded bool
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
			// Forward tool + text/thinking events to parent subscriber with agent context
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

	// Handle backgrounded division delegation
	if backgrounded && bgTask != nil {
		go func() {
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
			if runErr != nil {
				errStr = runErr.Error()
			}
			r.taskMgr.CompleteTracked(bgTask.ID, finalText+bgText, 0, errStr)
			endStatus := "completed"
			if runErr != nil {
				endStatus = "error"
			}
			core.SendEvent(subscriber, core.AgentEvent{
				Type: core.EventTypeSubAgentEnd,
				Data: core.AgentEventData{AgentName: agentName, Status: endStatus, Task: truncateTask(task, 120), TranscriptID: transcriptID},
			})
		}()
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
		},
	})

	if runErr != nil {
		return map[string]any{"error": runErr.Error(), "partial_result": finalText}, runErr
	}

	// Summarize long sub-agent results before returning to CTO
	finalText = r.maybeSummarize(ctx, finalText)

	return map[string]any{"result": finalText, "status": "completed", "division": divisionPath}, nil
}

// RunDelegateAsync launches a sub-agent in a goroutine. Returns immediately.

// RunDelegateTracked runs a sub-agent with file change tracking.
// Returns the result, an agent reference for continuation, and file changes.
func (r *ParallelRunner) RunDelegateTracked(ctx context.Context, task, instructions, agentName string, toolNames []string, maxRounds int, temperature float32, modelID string, sandboxTier string, delegatesTo []string) (map[string]any, error) {
	subscriber := subscriberFromCtx(ctx)
	// Take pre-snapshot
	var snapshotID string
	if r.snapshotter != nil && r.projectDir != "" {
		var err error
		snapshotID, err = r.snapshotter.Snapshot(ctx, r.projectDir)
		if err != nil {
			r.logger("SNAPSHOT_WARN: failed to snapshot: %v", err)
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
	for _, t := range r.toolSpecs {
		if toolSet[t.Function.Name] {
			selectedTools = append(selectedTools, t)
		}
	}
	if len(selectedTools) == 0 {
		r.logger("DELEGATE_FAIL: agent=%s no tools found — requested=%v available=%v", agentName, toolNames, toolNamesFromSpecs(r.toolSpecs))
		core.SendEvent(subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: fmt.Sprintf("none of the requested tools were found: %v", toolNames), TranscriptID: transcriptID},
		})
		return nil, core.NewToolError("delegate_to", fmt.Sprintf("none of the requested tools were found: %v", toolNames))
	}

	// Inject scoped delegation tools if the sub-agent's role has delegates_to
	if len(delegatesTo) > 0 && r.depth < r.maxDepth && r.roleProvider != nil {
		scopedDelegate := NewDelegateToToolDynamic(r, r.mcpResolver, r.roleProvider, func() []string { return delegatesTo })
		scopedAsync := NewDelegateAsyncToolDynamic(r, r.mcpResolver, r.roleProvider, func() []string { return delegatesTo })
		selectedTools = append(selectedTools,
			jsonToToolSpec(scopedDelegate),
			jsonToToolSpec(scopedAsync),
		)
		r.logger("SCOPED_DELEGATION: sub-agent %q can delegate to %v", agentName, delegatesTo)
	}

	// Create isolated provider — kept alive for delegate_continue
	provider := r.providerFactory()
	if provider == nil {
		r.logger("DELEGATE_FAIL: agent=%s providerFactory returned nil", agentName)
		core.SendEvent(subscriber, core.AgentEvent{
			Type: core.EventTypeSubAgentEnd,
			Data: core.AgentEventData{AgentName: agentName, Status: "error", Error: "provider factory returned nil", TranscriptID: transcriptID},
		})
		return nil, core.NewToolError("delegate_to", "provider factory returned nil for sub-agent "+agentName)
	}
	if modelID != "" && r.modelResolver != nil {
		if resolved := r.modelResolver(modelID); resolved != nil {
			if closer, ok := provider.(io.Closer); ok {
				closer.Close()
			}
			provider = resolved
			r.logger("MODEL_OVERRIDE: using %s for sub-agent", modelID)
		}
	}

	// Enrich task with parent context
	enrichedTask := r.enrichTask(ctx, task)

	// Auto-director: raise Chrome for VNC visibility when browser tools are detected.
	if r.raiseBrowserFunc != nil && hasBrowserTools(toolNames) {
		r.raiseBrowserFunc(ctx)
	}

	// Build sub-agent config
	cfg := core.AgentLoopConfig{
		SystemPrompt:   instructions,
		MaxToolRounds:  maxRounds,
		MaxTokens:      8192,
		ContextSize:    r.ctxSize,
		Tools:          selectedTools,
		ToolResultProcessor: subAgentResultProcessor(),
		Opts: core.GenerateOptions{
			MaxTokens:   8192,
			Temperature: temperature,
			TopP:        0.95,
			TopK:        20,
		},
	}

	sess := &subSession{
		parent:    r.baseSession,
		msgCount:  0,
		db:        r.db,
		project:   r.project,
		dbAgentID: transcriptID,
	}
	executor := r.executor
	if r.executorFactory != nil && sandboxTier != "" {
		executor = r.executorFactory(sandboxTier)
	}
	// Wrap sub-agent executor with vision processing (always)
	{
		vExec := vision.NewVisionAwareExecutor(executor, r.visionChain, log.New(io.Discard, "", 0))
		vExec.SetNativeVision(r.nativeVision)
		if r.visualContext != nil {
			vExec.SetVisualContext(r.visualContext)
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
		ContextSize:     r.ctxSize,
		ProjectDir:      r.projectDir,
		GenerateOptions: core.GenerateOptions{MaxTokens: 8192, Temperature: temperature, TopP: 0.95, TopK: 20},
		ScratchStore:    r.scratchStore,
		ToolResultProcessor: subAgentResultProcessor(),
	})
	loop := subAgent.Loop()

	r.logger("DELEGATE_START: agent=%s tools=%v maxRounds=%d model=%q sandboxTier=%q", agentName, toolNamesFromSpecs(selectedTools), maxRounds, modelID, sandboxTier)

	// Register as a foreground task so Ctrl+B can background the delegation
	var bgTask *core.BackgroundTask
	if r.taskMgr != nil {
		bgTask, _ = r.taskMgr.StartTracked(
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
			r.logger("DELEGATE_LOOP_ERR: agent=%s err=%v", agentName, runErr)
		}
	}()

	// Drain events: accumulate text result + forward tool events to parent subscriber
	// Also watch for background signal (Ctrl+B) if TaskManager is wired in.
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
				r.logger("DELEGATE_LOOP_ERROR: agent=%s error=%s", agentName, firstError)
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
			return nil // nil channel never fires
		}():
			// User pressed Ctrl+B — detach from the blocking loop.
			// The subagent goroutine keeps running; we'll collect its result
			// when it finishes and store it in the task.
			backgrounded = true
			evtDone = true
		}
	}

	// If backgrounded, spin up a goroutine to collect the final result and
	// store it in the TaskManager so the user can check on it later.
	if backgrounded && bgTask != nil {
		go func() {
			// Drain remaining events from the still-running subagent
			var bgText string
			for evt := range events {
				if evt.Type == core.EventTypeTextDelta || evt.Type == core.EventTypeAgentEnd {
					bgText += evt.Data.Text
				}
				// Keep forwarding events to SSE so TUI shows live progress
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
			<-done // wait for loop.Run to return

			var errStr string
			if runErr != nil {
				errStr = runErr.Error()
			}
			r.taskMgr.CompleteTracked(bgTask.ID, finalText+bgText, 0, errStr)

			// Emit subagent_end for the backgrounded delegation
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
				},
			})
		}()

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
		r.logger("DELEGATE_DONE: agent=%s status=error err=%v firstLoopError=%s", agentName, runErr, firstError)
	} else {
		r.logger("DELEGATE_DONE: agent=%s status=ok textLen=%d", agentName, len(finalText))
	}
	core.SendEvent(subscriber, core.AgentEvent{
		Type: core.EventTypeSubAgentEnd,
		Data: core.AgentEventData{
			AgentName:    agentName,
			Status:       endStatus,
			Task:         truncateTask(task, 120),
			TranscriptID: transcriptID,
			Error:        firstError,
		},
	})

	// Generate agent_ref
	agentRef := fmt.Sprintf("%s-%d", agentName, time.Now().UnixMilli())

	// Compute diff
	var changes *ChangeSet
	if r.snapshotter != nil && snapshotID != "" && r.projectDir != "" {
		var diffErr error
		changes, diffErr = r.snapshotter.Diff(ctx, r.projectDir, snapshotID)
		if diffErr != nil {
			r.logger("DIFF_WARN: failed to diff: %v", diffErr)
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
		result := map[string]any{"error": runErr.Error(), "partial_result": finalText, "agent_ref": agentRef}
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
		artifact = finalText // fallback if session has no assistant messages
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

	r.logger("CONTINUE: agent=%s feedback_len=%d", la.Role, len(feedback))

	// Compact the session to make room for the feedback
	la.Session.Compact(ctx, "Context compacted for continuation")

	// Create a new agent loop with the SAME session (has full history)
	continueExecutor := core.ToolExecutor(r.executor)
	{
		vExec := vision.NewVisionAwareExecutor(continueExecutor, r.visionChain, log.New(io.Discard, "", 0))
		vExec.SetNativeVision(r.nativeVision)
		if r.visualContext != nil {
			vExec.SetVisualContext(r.visualContext)
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
		ProjectDir:      r.projectDir,
		GenerateOptions: la.Config.Opts,
		ScratchStore:    r.scratchStore,
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
			if subscriber != nil {
				switch evt.Type {
				case core.EventTypeToolStart, core.EventTypeToolEnd, core.EventTypeToolUpdate,
					core.EventTypeTextDelta, core.EventTypeThinkingDelta:
					forwarded := evt
					forwarded.Data.AgentName = la.Role
					core.SendEvent(subscriber, forwarded)
				}
			}
		}
	}

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
		},
	})

	// Compute updated diff
	var changes *ChangeSet
	if r.snapshotter != nil && la.Snapshot != "" && r.projectDir != "" {
		var diffErr error
		changes, diffErr = r.snapshotter.Diff(ctx, r.projectDir, la.Snapshot)
		if diffErr != nil {
			r.logger("DIFF_WARN: failed to diff after continue: %v", diffErr)
		}
	}

	enriched := map[string]any{"agent_ref": agentRef}
	if runErr != nil {
		enriched["error"] = runErr.Error()
		enriched["partial_result"] = finalText
		return enriched, runErr
	}

	enriched["result"] = r.maybeSummarize(ctx, finalText)
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
	if r.snapshotter != nil && snapshotID != "" && r.projectDir != "" {
		if err := r.snapshotter.Revert(ctx, r.projectDir, snapshotID); err != nil {
			r.logger("REVERT_WARN: failed to revert: %v", err)
			return map[string]any{
				"status":    "revert_failed",
				"agent_ref": agentRef,
				"error":     err.Error(),
			}, err
		}
		// Get the list of files that were reverted
		changes, _ := r.snapshotter.Diff(ctx, r.projectDir, snapshotID)
		if changes != nil {
			revertedFiles = changes.Files
		}
	}

	r.logger("REVERT: agent=%s ref=%s files=%v", agentRole, agentRef, revertedFiles)

	return map[string]any{
		"status":         "reverted",
		"agent_ref":      agentRef,
		"files_restored": revertedFiles,
	}, nil
}

// RunDelegateAsync launches a sub-agent in a goroutine. Returns immediately.
func (r *ParallelRunner) RunDelegateAsync(ctx context.Context, taskID, task, instructions, agentName string, toolNames []string, maxRounds int, temperature float32, modelID string) (map[string]any, error) {
	// Capture subscriber from parent ctx before spawning goroutine.
	// The goroutine uses context.Background() to survive parent cancellation,
	// so we re-inject the subscriber into the background context.
	parentSubscriber := subscriberFromCtx(ctx)

	if r.depth >= r.maxDepth {
		return nil, fmt.Errorf("max delegation depth (%d) reached", r.maxDepth)
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

	r.logger("ASYNC_DELEGATE: task_id=%s task=%q tools=%v", taskID, task, toolNames)

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
		result, err := r.RunDelegateTracked(bgCtx, task, instructions, agentName, toolNames, maxRounds, temperature, modelID, "", nil)

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

// subAgentResultProcessor returns a ToolResultProcessor for sub-agents.
// Aggressively truncates tool results to prevent context bloat.
// The sub-agent's full output is persisted to the transcript DB — the in-context
// result only needs enough for the model to understand the outcome.
func subAgentResultProcessor() func(ctx context.Context, toolName, toolCallID, result string) string {
	return func(ctx context.Context, toolName, toolCallID, result string) string {
		if len(result) > 6000 {
			// Keep the end — errors and final results are most important
			tail := result[len(result)-5000:]
			// Find first newline to avoid mid-line split
			if idx := strings.Index(tail, "\n"); idx >= 0 && idx < 200 {
				tail = tail[idx+1:]
			}
			return "...[output truncated, showing tail]\n" + tail
		}
		return result
	}
}

// subSession is a minimal session that stores messages in memory.
// It wraps a parent session for context inheritance in sub-agents.
// When db is set, messages are also persisted to the database for transcript retrieval.
type subSession struct {
	parent   core.Session
	messages []core.Message
	msgCount int
	// Transcript persistence
	db          *storage.Database
	project     string // project name
	dbAgentID   string // composite agent_id for DB queries (e.g. "session-sub:marcus-1234")
}

func (s *subSession) ID() string { return s.parent.ID() + "-sub" }
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
func (s *subSession) GetTree() *core.TreeNode    { return nil }
func (s *subSession) Navigate(nodeID string) error { return fmt.Errorf("sub-sessions do not support navigation") }
func (s *subSession) Branch(label string) (string, error) { return "", fmt.Errorf("sub-sessions do not support branching") }
func (s *subSession) Fork(nodeID string) (core.Session, error) { return nil, fmt.Errorf("sub-sessions do not support forking") }
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
func (s *subSession) TruncateToolResults(keep int) (int, error) { return 0, nil }
func (s *subSession) ReplaceToolResults(replace func(i int, name, content string) string, keep int) (int, error) {
	return 0, nil
}
func (s *subSession) GetCurrentNode() string { return s.parent.ID() + "-sub" }

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
	if r.summarizer != nil {
		summarized, err := r.summarizer(ctx, text, 400)
		if err == nil && len(summarized) >= 50 {
			r.logger("SUMMARIZE_OK: %d -> %d chars", len(text), len(summarized))
			return summarized
		}
		r.logger("SUMMARIZE_FAILED: len=%d err=%v, truncating", len(text), err)
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

	// Check for CLAUDE.md — project-specific instructions
	for _, name := range []string{"CLAUDE.md", "claude.md", ".claude/CLAUDE.md"} {
		path := filepath.Join(projectDir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			content := string(data)
			if len(content) > 2000 {
				content = content[:2000] + "\n...(truncated)"
			}
			b.WriteString("CLAUDE.md (" + name + "):\n")
			b.WriteString(content)
			b.WriteString("\n\n")
			break
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
		{"go.mod", "Go", "go build ./...", "go test ./...", ""},
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

	return b.String()
}
