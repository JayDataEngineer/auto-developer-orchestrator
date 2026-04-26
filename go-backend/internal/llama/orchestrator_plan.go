package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

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
					"stepIndex":  idx,
					"status":     status,
					"note":       note,
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
						"requestId": requestID,
						"type":      "plan_update",
						"stepIndex": idx,
						"note":      note,
						"message":   fmt.Sprintf("Discovered new work at step %d: %s", idx, note),
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
