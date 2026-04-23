package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OrchestratorLoop manages the top-level planning and delegation loop.
// It owns the orchestrator's AgentLoop (KV session) and the ArtifactRegistry.
// Sub-agents are created on demand, run synchronously, and freed after yielding.
type OrchestratorLoop struct {
	engine    *HTTPEngine
	artifacts *ArtifactRegistry
	executor  *OrchestratorExecutor
	loop      *AgentLoop
	cfg       PersonaConfig
	logger    *zap.Logger
	saver     TranscriptSaver

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	plan    *Plan
}

// OrchestratorConfig holds configuration for the orchestrator.
type OrchestratorConfig struct {
	ProjectDir  string
	SandboxID   string
	ContextSize int // 0 = use ModelConfig default (32K)
}

// NewOrchestratorLoop creates a new orchestrator with a fresh KV session.
func NewOrchestratorLoop(
	engine *HTTPEngine,
	baseExecutor ToolExecutor, // SandboxToolExecutor for sub-agent tool dispatch
	ocfg OrchestratorConfig,
	logger *zap.Logger,
) (*OrchestratorLoop, error) {
	if !engine.IsLoaded() {
		return nil, fmt.Errorf("engine model not loaded")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	personaCfg := PersonaConfig{
		ProjectDir: ocfg.ProjectDir,
		SandboxID:  ocfg.SandboxID,
	}

	artifacts := NewArtifactRegistry()
	persona := NewPersona(PersonaOrchestrator, personaCfg)

	ctxSize := ocfg.ContextSize
	if ctxSize == 0 {
		ctxSize = cfg.DefaultContextSize
	}

	orchestratorExec := &OrchestratorExecutor{
		engine:       engine,
		artifacts:    artifacts,
		personaCfg:   personaCfg,
		baseExecutor: baseExecutor,
		logger:       logger,
	}

	loopCfg := AgentLoopConfig{
		SystemPrompt:  persona.SystemPrompt,
		MaxToolRounds: persona.MaxToolRounds,
		MaxTokens:     persona.MaxTokens,
		ContextSize:   ctxSize,
		Tools:         PersonaOpenAITools(PersonaOrchestrator),
		Compaction:    DefaultCompactionConfig(),
		Opts: GenerateOptions{
			MaxTokens:   persona.MaxTokens,
			Temperature: persona.Temperature,
			TopP:        cfg.TopP,
			TopK:        cfg.TopK,
		},
	}

	loop, err := NewAgentLoop(engine, orchestratorExec, loopCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator loop: %w", err)
	}

	orchestratorExec.subscriber = nil // set in Run/Continue

	return &OrchestratorLoop{
		engine:    engine,
		artifacts: artifacts,
		executor:  orchestratorExec,
		loop:      loop,
		cfg:       personaCfg,
		logger:    logger,
	}, nil
}

// SetTranscriptSaver configures the transcript saver for pre-compaction snapshots.
// Propagates to the orchestrator's own loop. Sub-agents get it via their own creation.
func (o *OrchestratorLoop) SetTranscriptSaver(saver TranscriptSaver) {
	o.saver = saver
	if o.loop != nil {
		o.loop.SetTranscriptSaver(saver)
	}
}

// SetMemory configures the project memory for persistent MEMORY.md storage.
func (o *OrchestratorLoop) SetMemory(memory *ProjectMemory) {
	if o.executor != nil {
		o.executor.memory = memory
	}
}

// Memory returns the project memory instance.
func (o *OrchestratorLoop) Memory() *ProjectMemory {
	if o.executor != nil {
		return o.executor.memory
	}
	return nil
}

// Run starts the orchestrator for a user prompt. Blocks until complete.
// Events (including sub-agent events) are emitted to the subscriber channel.
func (o *OrchestratorLoop) Run(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	o.running = true
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	o.executor.subscriber = subscriber
	return o.loop.Run(ctx, userMsg, subscriber)
}

// Continue sends a follow-up message to the orchestrator session.
func (o *OrchestratorLoop) Continue(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	o.running = true
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	o.executor.subscriber = subscriber
	return o.loop.Continue(ctx, userMsg, subscriber)
}

// Close releases the orchestrator's KV session.
func (o *OrchestratorLoop) Close() error {
	if o.cancel != nil {
		o.cancel()
	}
	return o.loop.Close()
}

// Artifacts returns the artifact registry for inspection.
func (o *OrchestratorLoop) Artifacts() *ArtifactRegistry {
	return o.artifacts
}

// Plan returns the current execution plan (nil if not yet created).
func (o *OrchestratorLoop) Plan() *Plan {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.plan
}

// IsRunning returns whether the orchestrator is currently active.
func (o *OrchestratorLoop) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}

// ── OrchestratorExecutor ─────────────────────────────────────────────

// OrchestratorExecutor implements ToolExecutor for the orchestrator persona.
// It handles delegate_to, delegate_async, collect_results, create_plan, update_plan, and synthesize.
type OrchestratorExecutor struct {
	engine       *HTTPEngine
	artifacts    *ArtifactRegistry
	personaCfg   PersonaConfig
	baseExecutor ToolExecutor // SandboxToolExecutor for sub-agent tool dispatch
	subscriber   chan<- AgentEvent
	logger       *zap.Logger
	memory       *ProjectMemory // optional: per-project persistent memory

	mu   sync.Mutex
	plan *Plan

	// Async delegate tracking
	asyncMu    sync.Mutex
	asyncTasks map[string]*asyncDelegateResult
	asyncWait  chan struct{}
}

// asyncDelegateResult holds the result of an async delegate_to call.
type asyncDelegateResult struct {
	taskID     string
	persona    PersonaType
	artifactID string
	output     string
	err        error
	done       bool
}

// Execute handles orchestrator-specific tools and falls through to the base
// executor (SandboxToolExecutor) for all other tools (bash, search_web, etc.).
func (e *OrchestratorExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	switch toolName {
	case "delegate_to":
		return e.delegate(ctx, args)
	case "delegate_async":
		return e.delegateAsync(ctx, args)
	case "collect_results":
		return e.collectResults(ctx)
	case "create_plan":
		return e.createPlan(args)
	case "update_plan":
		return e.updatePlan(args)
	case "synthesize":
		return e.synthesize(args)
	case "update_memory":
		return e.updateMemory(args)
	default:
		// Fall through to SandboxToolExecutor for bash, search_web, mcp_call, browser, etc.
		if e.baseExecutor != nil {
			return e.baseExecutor.Execute(ctx, toolName, args)
		}
		return nil, fmt.Errorf(
			"<tool_use_error>Unknown tool %q. Available tools: delegate_to, create_plan, update_plan, synthesize, bash, search_web, mcp_call, browse_to, etc.</tool_use_error>",
			toolName,
		)
	}
}

// delegate runs a sub-agent synchronously:
// 1. Create a new AgentLoop with the target persona's prompt and tool whitelist (minimal KV cache)
// 2. Run it with the task prompt
// 3. Collect the final output as an Artifact
// 4. Close the sub-agent session (free VRAM immediately)
// 5. Return the artifact summary to the orchestrator
func (e *OrchestratorExecutor) delegate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	personaName, _ := args["persona"].(string)
	// Accept "task" or common aliases "step", "step_description"
	task, _ := args["task"].(string)
	if task == "" {
		task, _ = args["step"].(string)
	}
	if task == "" {
		task, _ = args["step_description"].(string)
	}
	// If "step" was sent as a number, look up the plan step text at that index
	if task == "" {
		if stepNum, ok := args["step"].(float64); ok {
			e.mu.Lock()
			if e.plan != nil {
				idx := int(stepNum)
				if idx >= 0 && idx < len(e.plan.Steps) {
					task = e.plan.Steps[idx].Desc
				}
			}
			e.mu.Unlock()
		}
	}
	if personaName == "" {
		return nil, fmt.Errorf("<tool_use_error>Missing required parameter 'persona'. Valid options: web, code, desktop, mcp. Example: delegate_to{\"persona\":\"web\",\"task\":\"your task\"}</tool_use_error>")
	}
	if task == "" {
		return nil, fmt.Errorf("<tool_use_error>Missing required parameter 'task'. Example: delegate_to{\"persona\":\"web\",\"task\":\"Go to URL and fill form\"}</tool_use_error>")
	}

	personaType := PersonaType(personaName)
	persona := NewPersona(personaType, e.personaCfg)
	if persona == nil {
		return nil, fmt.Errorf("<tool_use_error>Unknown persona %q. Valid options: web, code, desktop, mcp. Example: delegate_to{\"persona\":\"web\",\"task\":\"your task\"}</tool_use_error>", personaName)
	}

	subAgentID := fmt.Sprintf("sub-%s-%d", personaType, time.Now().UnixMilli())

	e.logger.Info("ORCHESTRATOR: delegating to sub-agent",
		zap.String("persona", string(personaType)),
		zap.String("task", truncate(task, 80)),
		zap.String("subAgentId", subAgentID),
	)

	// Emit subagent_start event
	if e.subscriber != nil {
		sendEvent(e.subscriber, AgentEvent{
			Type: EventTypeSubAgentStart,
			Data: AgentEventData{
				ToolName: "delegate_to",
				ToolID:   subAgentID,
				ToolArgs: args,
				Result: map[string]interface{}{
					"subAgentId": subAgentID,
					"persona":    string(personaType),
					"task":       task,
				},
			},
		})
	}

	// Create PersonaAwareExecutor wrapping the base SandboxToolExecutor
	subExecutor := &PersonaAwareExecutor{
		persona:      persona,
		baseExecutor: e.baseExecutor,
		logger:       e.logger,
	}

	// Create sub-agent AgentLoop (minimal KV cache — ephemeral, not persistent)
	loopCfg := AgentLoopConfig{
		SystemPrompt:  persona.SystemPrompt,
		MaxToolRounds: persona.MaxToolRounds,
		MaxTokens:     persona.MaxTokens,
		ContextSize:   cfg.SubAgentContextSize, // 8K — much smaller than orchestrator's 32K
		Tools:         PersonaOpenAITools(personaType),
		Compaction:    SubAgentCompactionConfig(),
		Opts: GenerateOptions{
			MaxTokens:   persona.MaxTokens,
			Temperature: persona.Temperature,
			TopP:        cfg.TopP,
			TopK:        cfg.TopK,
		},
	}
	subLoop, err := NewAgentLoop(e.engine, subExecutor, loopCfg, e.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create sub-agent %s: %w", personaType, err)
	}
	defer subLoop.Close() // Free VRAM when done

	// Run sub-agent synchronously, collecting events
	subEvents := make(chan AgentEvent, 256)
	var subOutput strings.Builder

	// Forward sub-agent events to the main subscriber (for SSE)
	done := make(chan error, 1)
	go func() {
		for evt := range subEvents {
			// Forward to orchestrator's subscriber (which goes to SSE)
			if e.subscriber != nil {
				sendEvent(e.subscriber, evt)
			}
			// Accumulate text output
			if evt.Type == EventTypeTextDelta {
				subOutput.WriteString(evt.Data.Text)
			}
		}
		close(done)
	}()

	err = subLoop.Run(ctx, task, subEvents)
	close(subEvents) // Signal the forwarding goroutine to finish
	<-done // Wait for event forwarding to complete

	// Build result
	output := subOutput.String()

	// Emit subagent_end event
	if e.subscriber != nil {
		sendEvent(e.subscriber, AgentEvent{
			Type: EventTypeSubAgentEnd,
			Data: AgentEventData{
				ToolID: subAgentID,
				Result: map[string]interface{}{
					"subAgentId": subAgentID,
					"persona":    string(personaType),
					"status":     "complete",
					"outputLen":  len(output),
				},
			},
		})
	}

	if err != nil {
		e.logger.Error("ORCHESTRATOR: sub-agent failed",
			zap.String("persona", string(personaType)),
			zap.Error(err),
		)
		return map[string]interface{}{
			"subAgentId": subAgentID,
			"persona":    string(personaType),
			"status":     "failed",
			"error":      err.Error(),
			"suggestion": fmt.Sprintf(
				"Consider: trying %s instead, simplifying the task, or breaking it into smaller steps.",
				alternativePersona(personaType),
			),
		}, nil // Return as result, not error — orchestrator can retry or adapt
	}

	// Create artifact from sub-agent output
	artifact := &Artifact{
		SourceID:  subAgentID,
		Persona:   personaType,
		Type:      persona.InferArtifactType(),
		Title:     fmt.Sprintf("%s: %s", personaType, truncate(task, 60)),
		Content:   output,
		Metadata:  map[string]string{"task": task},
	}
	artID := e.artifacts.Create(artifact)

	// Emit artifact_created event
	if e.subscriber != nil {
		sendEvent(e.subscriber, AgentEvent{
			Type: EventTypeArtifactCreated,
			Data: AgentEventData{
				ToolName: "delegate_to",
				ToolID:   subAgentID,
				Result:   artifact,
			},
		})
	}

	e.logger.Info("ORCHESTRATOR: sub-agent completed",
		zap.String("persona", string(personaType)),
		zap.String("artifactId", artID),
		zap.Int("outputLen", len(output)),
	)

	return map[string]interface{}{
		"artifactId": artID,
		"subAgentId": subAgentID,
		"persona":    string(personaType),
		"status":     "complete",
		"output":     truncate(output, cfg.SynthesisMaxChars),
	}, nil
}

// createPlan creates a new execution plan.
func (e *OrchestratorExecutor) createPlan(args map[string]interface{}) (interface{}, error) {
	stepsRaw, ok := args["steps"].([]interface{})
	if !ok || len(stepsRaw) == 0 {
		// Try parsing from a string (model may send as JSON string)
		if raw, ok := args["raw"].(string); ok {
			var parsed []string
			if err := json.Unmarshal([]byte(raw), &parsed); err == nil && len(parsed) > 0 {
				stepsRaw = make([]interface{}, len(parsed))
				for i, s := range parsed {
					stepsRaw[i] = s
				}
			}
		}
	}
	if len(stepsRaw) == 0 {
		return nil, fmt.Errorf("missing 'steps' argument. Example: {\"steps\":[\"step 1\",\"step 2\"]}")
	}

	var steps []PlanStep
	for i, s := range stepsRaw {
		desc, _ := s.(string)
		steps = append(steps, PlanStep{
			Index:  i,
			Desc:   desc,
			Status: "pending",
		})
	}

	plan := &Plan{Steps: steps}

	e.mu.Lock()
	e.plan = plan
	e.mu.Unlock()

	// Store plan as artifact
	artifact := &Artifact{
		Persona: PersonaOrchestrator,
		Type:    ArtifactPlan,
		Title:   "Execution Plan",
		Content: plan.ToContent(),
	}
	artID := e.artifacts.Create(artifact)

	// Emit plan_created SSE event
	if e.subscriber != nil {
		sendEvent(e.subscriber, AgentEvent{
			Type: EventTypePlanCreated,
			Data: AgentEventData{
				Result: map[string]interface{}{
					"artifactId": artID,
					"steps":      steps,
				},
			},
		})
	}

	return map[string]interface{}{
		"stepCount": len(steps),
		"next":      "Now call delegate_to for step 1. Pick the right persona: web, code, or desktop.",
	}, nil
}

// updatePlan updates a step's status in the current plan.
func (e *OrchestratorExecutor) updatePlan(args map[string]interface{}) (interface{}, error) {
	stepIdx, _ := args["step_index"].(float64)
	status, _ := args["status"].(string)
	note, _ := args["note"].(string)
	artifactID, _ := args["artifactId"].(string)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.plan == nil {
		return nil, fmt.Errorf("no plan created yet. Call create_plan first.")
	}
	idx := int(stepIdx)
	if idx < 0 || idx >= len(e.plan.Steps) {
		return nil, fmt.Errorf("step_index %d out of range (0-%d)", idx, len(e.plan.Steps)-1)
	}

	e.plan.Steps[idx].Status = status
	e.plan.Steps[idx].Note = note
	if artifactID != "" {
		e.plan.Steps[idx].Artifact = artifactID
	}

	// Update plan artifact
	planArtifacts := e.artifacts.ListByType(ArtifactPlan)
	if len(planArtifacts) > 0 {
		planArtifacts[0].Content = e.plan.ToContent()
	}

	// Emit plan_updated SSE event
	if e.subscriber != nil {
		sendEvent(e.subscriber, AgentEvent{
			Type: EventTypePlanUpdated,
			Data: AgentEventData{
				Result: map[string]interface{}{
					"stepIndex": idx,
					"status":    status,
					"note":      note,
				},
			},
		})
	}

	result := map[string]interface{}{"updated": true, "step": idx}
	if status == "failed" {
		result["hint"] = "Step failed. Consider delegating to a different persona, simplifying the task, or splitting into smaller steps."
	}
	return result, nil
}

// synthesize returns the final summary. The orchestrator's text output
// after this tool call becomes the final response to the user.
func (e *OrchestratorExecutor) synthesize(args map[string]interface{}) (interface{}, error) {
	conclusion, _ := args["conclusion"].(string)
	return map[string]interface{}{
		"synthesized": true,
		"conclusion":  conclusion,
	}, nil
}

// updateMemory saves information to the project's persistent MEMORY.md file.
func (e *OrchestratorExecutor) updateMemory(args map[string]interface{}) (interface{}, error) {
	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("<tool_use_error>Missing 'content' parameter. Example: update_memory{\"content\":\"Key finding: ...\"}</tool_use_error>")
	}
	if e.memory == nil {
		return map[string]interface{}{
			"saved":  false,
			"reason": "No project directory configured for memory",
		}, nil
	}

	// Support section-based updates
	if section, ok := args["section"].(string); ok && section != "" {
		e.memory.UpdateSection(MemorySection("## "+section), content)
	} else {
		e.memory.Update(content)
	}

	if err := e.memory.Save(); err != nil {
		return map[string]interface{}{
			"saved": false,
			"error": err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"saved":     true,
		"lineCount": e.memory.LineCount(),
		"sizeBytes": len(e.memory.Content()),
	}, nil
}

// alternativePersona suggests a different persona when one fails.
func alternativePersona(failed PersonaType) string {
	alternatives := map[PersonaType]string{
		PersonaWeb:      "desktop (for browser automation), code (for API/scripting), or mcp (for research)",
		PersonaCode:     "desktop (for GUI-based tools), web (for web-based solutions), or mcp (for research)",
		PersonaDesktop:  "code (for CLI-based approach), web (for web-based approach), or mcp (for research)",
		PersonaMCP:      "web (for browser automation) or code (for scripting)",
		PersonaResearch: "mcp (for simpler research) or web (for browser-based research)",
	}
	if alt, ok := alternatives[failed]; ok {
		return alt
	}
	return "a different persona"
}

// delegateAsync launches a sub-agent in the background and returns immediately.
// The caller should use collect_results to wait for completion.
func (e *OrchestratorExecutor) delegateAsync(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("<tool_use_error>Missing required parameter 'task_id'. Example: delegate_async{\"persona\":\"web\",\"task\":\"search prices\",\"task_id\":\"price-search\"}</tool_use_error>")
	}

	// Initialize async tracking
	e.asyncMu.Lock()
	if e.asyncTasks == nil {
		e.asyncTasks = make(map[string]*asyncDelegateResult)
	}
	if e.asyncWait == nil {
		e.asyncWait = make(chan struct{}, 16)
	}
	e.asyncTasks[taskID] = &asyncDelegateResult{taskID: taskID}
	e.asyncMu.Unlock()

	// Launch delegate in background goroutine
	go func() {
		result, err := e.delegate(ctx, args)

		e.asyncMu.Lock()
		output := ""
		var artifactID string
		if err == nil && result != nil {
			if m, ok := result.(map[string]interface{}); ok {
				if o, ok := m["output"].(string); ok {
					output = o
				}
				if a, ok := m["artifactId"].(string); ok {
					artifactID = a
				}
			}
		}
		personaName, _ := args["persona"].(string)
		e.asyncTasks[taskID] = &asyncDelegateResult{
			taskID:     taskID,
			persona:    PersonaType(personaName),
			artifactID: artifactID,
			output:     output,
			err:        err,
			done:       true,
		}
		e.asyncMu.Unlock()

		// Signal completion
		select {
		case e.asyncWait <- struct{}{}:
		default:
		}
	}()

	return map[string]interface{}{
		"taskId":  taskID,
		"status":  "launched",
		"message": "Task running in background. Call collect_results{} when ready.",
	}, nil
}

// collectResults blocks until all pending async delegates complete, then returns their results.
func (e *OrchestratorExecutor) collectResults(ctx context.Context) (interface{}, error) {
	for {
		e.asyncMu.Lock()
		allDone := true
		pending := 0
		for _, r := range e.asyncTasks {
			if !r.done {
				allDone = false
				pending++
			}
		}
		if allDone && len(e.asyncTasks) > 0 {
			results := e.asyncTasks
			e.asyncTasks = make(map[string]*asyncDelegateResult)
			e.asyncMu.Unlock()

			out := make(map[string]interface{})
			for id, r := range results {
				entry := map[string]interface{}{
					"taskId":  r.taskID,
					"persona": string(r.persona),
					"status":  "complete",
				}
				if r.err != nil {
					entry["status"] = "failed"
					entry["error"] = r.err.Error()
				}
				if r.artifactID != "" {
					entry["artifactId"] = r.artifactID
				}
				if r.output != "" {
					entry["output"] = truncate(r.output, cfg.SynthesisMaxChars)
				}
				out[id] = entry
			}
			return out, nil
		}
		e.asyncMu.Unlock()

		if len(e.asyncTasks) == 0 {
			return map[string]interface{}{"message": "No pending async tasks. Use delegate_async first."}, nil
		}

		// Wait for a completion signal or context cancellation
		select {
		case <-e.asyncWait:
			// A task completed, loop to check
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
			e.logger.Warn("collect_results: still waiting for async tasks",
				zap.Int("pending", pending),
			)
		}
	}
}

// ── PersonaAwareExecutor ─────────────────────────────────────────────

// PersonaAwareExecutor wraps a ToolExecutor and enforces the persona's tool whitelist.
// If the model calls a tool not in its persona's list, an error is returned.
type PersonaAwareExecutor struct {
	persona      *Persona
	baseExecutor ToolExecutor
	logger       *zap.Logger
}

// Execute checks the persona's tool whitelist before delegating to the base executor.
func (e *PersonaAwareExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	// Normalize tool name (handle common aliases)
	normalized := normalizeToolName(toolName, args)

	// yield_artifact is handled by the agent loop (terminal signal)
	// but we need to accept it here so the whitelist check passes.
	if normalized == "yield_artifact" {
		output, _ := args["output"].(string)
		return map[string]interface{}{
			"yielded": true,
			"output":  output,
		}, nil
	}

	// Check whitelist
	if !e.persona.HasTool(normalized) {
		return nil, fmt.Errorf(
			"[SYSTEM: Tool %q is not available for %s persona. Available tools: %v. Use only the tools listed above.]",
			toolName, e.persona.Type, e.persona.Tools,
		)
	}

	return e.baseExecutor.Execute(ctx, normalized, args)
}

