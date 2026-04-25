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
	persona := NewOrchestratorPersona(personaCfg)

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

// SetApprovalManager configures the approval manager for plan approval.
func (o *OrchestratorLoop) SetApprovalManager(mgr ApprovalManager) {
	if o.executor != nil {
		o.executor.approvalMgr = mgr
	}
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
	approvalMgr  ApprovalManager // optional: for plan approval when PlanApprovalEnabled=true

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
	taskSlug   string
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
		return e.createPlan(ctx, args)
	case "update_plan":
		return e.updatePlan(ctx, args)
	case "clarify":
		return e.clarify(ctx, args)
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

// delegate runs a sub-agent synchronously with dynamic instructions and tools:
// 1. Parse task, instructions, tools from the orchestrator's call
// 2. Build a custom system prompt from the instructions + tool reference
// 3. Create a new AgentLoop with the selected tools (minimal KV cache)
// 4. Run it with the task prompt
// 5. Collect the final output as an Artifact
// 6. Close the sub-agent session (free VRAM immediately)
func (e *OrchestratorExecutor) delegate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	task, _ := args["task"].(string)
	instructions, _ := args["instructions"].(string)

	// Also accept "step" or "step_description" as aliases for "task"
	if task == "" {
		task, _ = args["step"].(string)
	}
	if task == "" {
		task, _ = args["step_description"].(string)
	}
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

	// Parse tool names
	var toolNames []string
	if toolsRaw, ok := args["tools"].([]interface{}); ok {
		for _, t := range toolsRaw {
			if name, ok := t.(string); ok {
				toolNames = append(toolNames, name)
			}
		}
	}

	// Validate required fields
	if task == "" {
		return nil, fmt.Errorf("<tool_use_error>Missing 'task'. Example: delegate_to{\"task\":\"Search prices\",\"instructions\":\"You are a researcher...\",\"tools\":[\"mcp_call\",\"scrape\"]}</tool_use_error>")
	}
	if instructions == "" {
		return nil, fmt.Errorf("<tool_use_error>Missing 'instructions'. Write specific instructions telling the sub-agent how to approach the task. Example: delegate_to{\"task\":\"Search prices\",\"instructions\":\"You are a researcher. Search 3 stores and compare.\",\"tools\":[\"mcp_call\"]}</tool_use_error>")
	}
	if len(toolNames) == 0 {
		return nil, fmt.Errorf("<tool_use_error>Missing 'tools'. Select tools for the sub-agent. Example: delegate_to{\"task\":\"...\",\"instructions\":\"...\",\"tools\":[\"mcp_call\",\"scrape\",\"bash\"]}</tool_use_error>")
	}

	// Parse optional overrides
	maxRounds := 15
	if v, ok := args["max_rounds"].(float64); ok && v > 0 {
		maxRounds = int(v)
	}
	thinkingBudget := 2048
	if v, ok := args["thinking_budget"].(float64); ok && v > 0 {
		thinkingBudget = int(v)
	}
	temperature := float32(0.4)
	if v, ok := args["temperature"].(float64); ok {
		temperature = float32(v)
	}

	// Build tool specs (validates names, always includes yield_artifact)
	specs := SubAgentToolSpecs(toolNames)
	if len(specs) <= 1 { // only yield_artifact
		return nil, fmt.Errorf("<tool_use_error>No valid tools found in %v. Valid tool names: bash, file_read, file_write, file_edit, file_grep, file_glob, code_search, search_web, browse_to, click_element, type_text, read_page, observe, scroll_page, scrape, mcp_call, desktop_screenshot, desktop_click, desktop_type, desktop_key, image_read, http_request, wait</tool_use_error>", toolNames)
	}

	// Build sub-agent system prompt
	toolsBlock := FormatToolList(specs)
	systemPrompt := buildSubAgentPrompt(instructions, toolsBlock, e.personaCfg)

	// Generate descriptive sub-agent ID
	taskSlug := slugifyTask(task, 20)
	subAgentID := fmt.Sprintf("sub-%s-%d", taskSlug, time.Now().UnixMilli())

	e.logger.Info("ORCHESTRATOR: delegating to sub-agent",
		zap.String("taskSlug", taskSlug),
		zap.String("task", truncate(task, 80)),
		zap.Int("toolCount", len(specs)),
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
					"taskSlug":   taskSlug,
					"task":       task,
					"tools":      toolNames,
				},
			},
		})
	}

	// Create ToolWhitelistExecutor wrapping the base SandboxToolExecutor
	whitelist := make([]string, len(specs))
	for i, s := range specs {
		whitelist[i] = s.Name
	}
	subExecutor := &ToolWhitelistExecutor{
		toolWhitelist: whitelist,
		baseExecutor:  e.baseExecutor,
		logger:        e.logger,
	}

	// Create sub-agent AgentLoop (minimal KV cache — ephemeral, not persistent)
	maxTokens := 4096
	loopCfg := AgentLoopConfig{
		SystemPrompt:   systemPrompt,
		MaxToolRounds:  maxRounds,
		MaxTokens:      maxTokens,
		ContextSize:    cfg.SubAgentContextSize,
		ThinkingBudget: thinkingBudget,
		Tools:          ToOpenAITools(specs),
		Compaction:     SubAgentCompactionConfig(),
		Opts: GenerateOptions{
			MaxTokens:   maxTokens,
			Temperature: temperature,
			TopP:        cfg.TopP,
			TopK:        cfg.TopK,
		},
	}
	subLoop, err := NewAgentLoop(e.engine, subExecutor, loopCfg, e.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create sub-agent: %w", err)
	}
	defer subLoop.Close() // Free VRAM when done

	// Run sub-agent synchronously, collecting events
	subEvents := make(chan AgentEvent, 256)
	var subOutput strings.Builder

	// Forward sub-agent events to the main subscriber (for SSE)
	done := make(chan error, 1)
	go func() {
		for evt := range subEvents {
			if e.subscriber != nil {
				sendEvent(e.subscriber, evt)
			}
			if evt.Type == EventTypeTextDelta {
				subOutput.WriteString(evt.Data.Text)
			}
		}
		close(done)
	}()

	// Run sub-agent — no artificial timeout here. The agent_loop handles
	// per-tool timeouts (300s regular, 30min for nested delegates).
	// MaxToolRounds (default 15) is the real limiter.
	err = subLoop.Run(ctx, task, subEvents)
	close(subEvents)
	<-done

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
					"taskSlug":   taskSlug,
					"status":     "complete",
					"outputLen":  len(output),
				},
			},
		})
	}

	if err != nil {
		e.logger.Error("ORCHESTRATOR: sub-agent failed",
			zap.String("taskSlug", taskSlug),
			zap.Error(err),
		)
		return map[string]interface{}{
			"subAgentId": subAgentID,
			"taskSlug":   taskSlug,
			"status":     "failed",
			"error":      err.Error(),
			"suggestion": "Consider simplifying the task, writing more specific instructions, or selecting different tools.",
		}, nil
	}

	// Create artifact from sub-agent output
	artifact := &Artifact{
		SourceID: subAgentID,
		Source:   taskSlug,
		Type:     ArtifactSummary,
		Title:    fmt.Sprintf("%s: %s", taskSlug, truncate(task, 60)),
		Content:  output,
		Metadata: map[string]string{"task": task},
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
		zap.String("taskSlug", taskSlug),
		zap.String("artifactId", artID),
		zap.Int("outputLen", len(output)),
	)

	return map[string]interface{}{
		"artifactId": artID,
		"subAgentId": subAgentID,
		"taskSlug":   taskSlug,
		"status":     "complete",
		"output":     truncate(output, cfg.SynthesisMaxChars),
	}, nil
}

// slugifyTask creates a short URL-safe slug from a task description.
func slugifyTask(task string, maxLen int) string {
	s := strings.ToLower(task)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, s)
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
		// Trim to last dash to avoid cutting mid-word
		if idx := strings.LastIndex(s, "-"); idx > maxLen/2 {
			s = s[:idx]
		}
	}
	if s == "" {
		s = "task"
	}
	return s
}

// createPlan creates a new execution plan.
// When PlanApprovalEnabled is true, it pauses and waits for user approval before returning.
func (e *OrchestratorExecutor) createPlan(ctx context.Context, args map[string]interface{}) (interface{}, error) {
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
		Source:  "orchestrator",
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

	// Plan approval gate — when enabled, pause and wait for user approval
	if cfg.PlanApprovalEnabled && e.approvalMgr != nil {
		requestID := fmt.Sprintf("plan-%d", time.Now().UnixMilli())
		subscriber := SubscriberFromContext(ctx)

		if subscriber != nil {
			sendEvent(subscriber, AgentEvent{
				Type: EventTypeApprovalRequest,
				Data: AgentEventData{
					ToolName: "create_plan",
					ToolID:   requestID,
					ToolArgs: args,
					Result: map[string]interface{}{
						"requestId": requestID,
						"type":      "plan",
						"steps":     steps,
						"message":   "Plan created. Awaiting approval to proceed.",
					},
				},
			})
		}

		respCh := e.approvalMgr.Register(requestID)
		defer e.approvalMgr.Cleanup(requestID)

		select {
		case resp := <-respCh:
			if resp.Action == "approve" {
				return map[string]interface{}{
					"stepCount": len(steps),
					"approved":  true,
					"next":      "Plan approved. Execute steps directly using your tools (mcp_call, bash, browse_to). Only delegate if a step needs isolated execution.",
				}, nil
			}
			return nil, fmt.Errorf("<tool_use_error>Plan was denied. User says: %s. Revise the plan and call create_plan again with updated steps.</tool_use_error>", resp.Message)
		case <-ctx.Done():
			return nil, fmt.Errorf("plan approval timed out: context cancelled")
		}
	}

	return map[string]interface{}{
		"stepCount": len(steps),
		"next":      "Plan created. Execute steps directly using your tools (mcp_call, bash, browse_to). Only delegate if a step needs isolated execution.",
	}, nil
}

// updatePlan updates a step's status in the current plan.
// When discovered:true and PlanApprovalEnabled, pauses for user approval.
func (e *OrchestratorExecutor) updatePlan(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	stepIdx, _ := args["step_index"].(float64)
	status, _ := args["status"].(string)
	note, _ := args["note"].(string)
	artifactID, _ := args["artifactId"].(string)
	discovered, _ := args["discovered"].(bool)

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
					"discovered": discovered,
				},
			},
		})
	}

	// Discovered-task approval gate — when enabled and discovered:true, pause for user approval
	if discovered && cfg.PlanApprovalEnabled && e.approvalMgr != nil {
		requestID := fmt.Sprintf("plan-update-%d", time.Now().UnixMilli())
		subscriber := SubscriberFromContext(ctx)

		if subscriber != nil {
			sendEvent(subscriber, AgentEvent{
				Type: EventTypeApprovalRequest,
				Data: AgentEventData{
					ToolName: "update_plan",
					ToolID:   requestID,
					ToolArgs: args,
					Result: map[string]interface{}{
						"requestId":  requestID,
						"type":       "plan_update",
						"stepIndex":  idx,
						"note":       note,
						"message":    fmt.Sprintf("Discovered new work at step %d: %s", idx, note),
					},
				},
			})
		}

		respCh := e.approvalMgr.Register(requestID)
		defer e.approvalMgr.Cleanup(requestID)

		select {
		case resp := <-respCh:
			if resp.Action == "approve" {
				// Continue with the discovered step
			} else {
				return nil, fmt.Errorf("<tool_use_error>Discovered task at step %d was denied. User says: %s. Skip this step and continue.</tool_use_error>", idx, resp.Message)
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("discovered task approval timed out: context cancelled")
		}
	}

	result := map[string]interface{}{"updated": true, "step": idx}
	if status == "failed" {
		result["hint"] = "Step failed. Consider delegating to a different persona, simplifying the task, or splitting into smaller steps."
	}
	return result, nil
}

// clarify asks the user up to 5 clarifying questions before planning.
// Only active when PlanApprovalEnabled is true.
func (e *OrchestratorExecutor) clarify(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if !cfg.PlanApprovalEnabled {
		return nil, fmt.Errorf("<tool_use_error>clarify is only available when plan-approval mode is enabled. Enable it in agent settings, or proceed with create_plan directly.</tool_use_error>")
	}
	if e.approvalMgr == nil {
		return nil, fmt.Errorf("<tool_use_error>clarify is not available in non-interactive sessions</tool_use_error>")
	}

	questionsRaw, ok := args["questions"].([]interface{})
	if !ok || len(questionsRaw) == 0 {
		return nil, fmt.Errorf("<tool_use_error>Missing 'questions' argument. Example: clarify{\"questions\":[\"Which framework?\",\"Database?\"]}</tool_use_error>")
	}
	if len(questionsRaw) > 5 {
		return nil, fmt.Errorf("<tool_use_error>Too many questions (%d). Maximum is 5.</tool_use_error>", len(questionsRaw))
	}

	var questions []string
	for _, q := range questionsRaw {
		s, ok := q.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("<tool_use_error>All questions must be non-empty strings.</tool_use_error>")
		}
		questions = append(questions, s)
	}

	// Format as numbered list for display
	var questionText string
	for i, q := range questions {
		questionText += fmt.Sprintf("%d. %s\n", i+1, q)
	}

	requestID := fmt.Sprintf("clarify-%d", time.Now().UnixMilli())
	subscriber := SubscriberFromContext(ctx)

	if subscriber != nil {
		sendEvent(subscriber, AgentEvent{
			Type: EventTypeApprovalRequest,
			Data: AgentEventData{
				ToolName: "clarify",
				ToolID:   requestID,
				ToolArgs: args,
				Result: map[string]interface{}{
					"requestId": requestID,
					"type":      "clarify",
					"questions": questions,
					"message":   questionText,
				},
			},
		})
	}

	respCh := e.approvalMgr.Register(requestID)
	defer e.approvalMgr.Cleanup(requestID)

	select {
	case resp := <-respCh:
		if resp.Action == "answer" && resp.Message != "" {
			return map[string]interface{}{
				"answered": true,
				"answers":  resp.Message,
			}, nil
		}
		if resp.Action == "approve" {
			return map[string]interface{}{
				"answered": true,
				"answers":  resp.Message,
			}, nil
		}
		return nil, fmt.Errorf("<tool_use_error>User declined to answer clarification questions. Proceed with best assumptions.</tool_use_error>")
	case <-ctx.Done():
		return nil, fmt.Errorf("clarify timed out: context cancelled")
	}
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

// delegateAsync launches a sub-agent in the background and returns immediately.
// The caller should use collect_results to wait for completion.
func (e *OrchestratorExecutor) delegateAsync(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("<tool_use_error>Missing required parameter 'task_id'. Example: delegate_async{\"task\":\"search prices\",\"instructions\":\"You are a researcher...\",\"tools\":[\"mcp_call\"],\"task_id\":\"price-search\"}</tool_use_error>")
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

	// Launch delegate in background goroutine with detached context
	// so it survives after the parent request's context is canceled.
	asyncCtx, asyncCancel := context.WithCancel(context.Background())
	go func() {
		defer asyncCancel()
		result, err := e.delegate(asyncCtx, args)

		e.asyncMu.Lock()
		output := ""
		var artifactID, taskSlug string
		if err == nil && result != nil {
			if m, ok := result.(map[string]interface{}); ok {
				if o, ok := m["output"].(string); ok {
					output = o
				}
				if a, ok := m["artifactId"].(string); ok {
					artifactID = a
				}
				if s, ok := m["taskSlug"].(string); ok {
					taskSlug = s
				}
			}
		}
		e.asyncTasks[taskID] = &asyncDelegateResult{
			taskID:     taskID,
			taskSlug:   taskSlug,
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
					"taskId":   r.taskID,
					"taskSlug": r.taskSlug,
					"status":   "complete",
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

// ── ToolWhitelistExecutor ────────────────────────────────────────────

// ToolWhitelistExecutor wraps a ToolExecutor and enforces a dynamic tool whitelist.
// Used by sub-agents created via delegate_to — the orchestrator picks the tool set.
type ToolWhitelistExecutor struct {
	toolWhitelist []string
	baseExecutor  ToolExecutor
	logger        *zap.Logger
}

// Execute checks the tool whitelist before delegating to the base executor.
func (e *ToolWhitelistExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
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
	for _, allowed := range e.toolWhitelist {
		if normalized == allowed {
			return e.baseExecutor.Execute(ctx, normalized, args)
		}
	}

	e.logger.Error("Tool whitelist rejection",
		zap.String("original", toolName),
		zap.String("normalized", normalized),
		zap.Int("whitelistLen", len(e.toolWhitelist)),
	)
	return nil, fmt.Errorf(
		"[SYSTEM: Tool %q is not available. Available tools: %v. Use only the tools listed above.]",
		toolName, e.toolWhitelist,
	)
}

