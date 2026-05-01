package llama

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ── OrchestratorExecutor ─────────────────────────────────────────────

// OrchestratorExecutor implements ToolExecutor for the orchestrator persona.
// It handles delegate_to, delegate_async, collect_results, create_plan, update_plan, and synthesize.
type OrchestratorExecutor struct {
	engine       ChatProvider
	artifacts    *ArtifactRegistry
	personaCfg   PersonaConfig
	memory       *ProjectMemory
	approvalMgr  ApprovalManager
	baseExecutor ToolExecutor
	subscriber   chan<- AgentEvent
	mu           sync.Mutex
	plan         *Plan
	logger       *zap.Logger

	// Async delegate tracking
	asyncMu    sync.Mutex
	asyncTasks map[string]*asyncDelegateResult
	asyncWait  chan struct{} // signaled on task completion
}

// asyncDelegateResult holds the result of a background sub-agent run.
type asyncDelegateResult struct {
	taskID     string
	taskSlug   string
	artifactID string
	output     string
	err        error
	done       bool
}

// Execute routes tool calls to the appropriate handler.
// Orchestrator-specific tools (delegate_to, create_plan, etc.) are handled here.
// All other tools (bash, file_read, file_write, browser, desktop, etc.) fall through
// to the baseExecutor (SandboxToolExecutor) so the orchestrator can use them directly.
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
	case "yield_artifact":
		output, _ := args["output"].(string)
		return map[string]interface{}{
			"yielded": true,
			"output":  output,
		}, nil
	default:
		// Fall through to base executor for sandbox tools (bash, file_*, browser, desktop, etc.)
		if e.baseExecutor != nil {
			return e.baseExecutor.Execute(ctx, toolName, args)
		}
		return nil, fmt.Errorf("orchestrator-executor: unknown tool %q and no base executor available", toolName)
	}
}

// ExecuteStreaming forwards streaming tool calls to the base executor.
// This lets the orchestrator use streaming tools (like bash) directly.
func (e *OrchestratorExecutor) ExecuteStreaming(ctx context.Context, toolName string, args map[string]interface{}, onUpdate func(string)) (interface{}, error) {
	// Orchestrator-specific tools don't stream — route normally
	switch toolName {
	case "delegate_to", "delegate_async", "collect_results", "create_plan",
		"update_plan", "clarify", "synthesize", "update_memory", "yield_artifact":
		return e.Execute(ctx, toolName, args)
	default:
		if streamer, ok := e.baseExecutor.(ToolExecutorStreaming); ok {
			return streamer.ExecuteStreaming(ctx, toolName, args, onUpdate)
		}
		if e.baseExecutor != nil {
			return e.baseExecutor.Execute(ctx, toolName, args)
		}
		return nil, fmt.Errorf("orchestrator-executor: no base executor for streaming tool %q", toolName)
	}
}

// delegate runs a sub-agent synchronously with a restricted tool set.
func (e *OrchestratorExecutor) delegate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Parse required parameters
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
