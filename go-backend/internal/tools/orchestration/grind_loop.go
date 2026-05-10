package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// GrindLoopTool implements core.Tool for iterative delegation with verification.
// It creates fresh sub-agents for each attempt, verifies work via a bash command,
// and optionally escalates to a larger model for review when all attempts fail.
type GrindLoopTool struct {
	runner      DelegateRunner
	bashExec    bashExecutor
	mcpResolver MCPResolver
	roleMap     map[string]*common.AgentRole
	modelResolver ModelResolver
	subscriber  chan<- core.AgentEvent
}

// bashExecutor is a local interface matching bash.Executor to avoid import cycle.
type bashExecutor interface {
	Exec(ctx context.Context, command string) (string, error)
}

// GrindResult is the JSON result returned by grind_loop.
type GrindResult struct {
	Status         string          `json:"status"`
	Attempts       int             `json:"attempts"`
	FinalResult    string          `json:"final_result,omitempty"`
	VerifyPass     bool            `json:"verify_pass"`
	History        []AttemptRecord `json:"history"`
	VerifierReview string          `json:"verifier_review,omitempty"`
}

// AttemptRecord captures a single grind attempt.
type AttemptRecord struct {
	Attempt       int    `json:"attempt"`
	ResultSnippet string `json:"result_snippet"`
	VerifyOutput  string `json:"verify_output"`
	Pass          bool   `json:"pass"`
}

// NewGrindLoopTool creates a new grind_loop tool.
func NewGrindLoopTool(r DelegateRunner, bashExec bashExecutor, mcpResolver MCPResolver, roleMap map[string]*common.AgentRole, modelResolver ModelResolver) *GrindLoopTool {
	return &GrindLoopTool{
		runner:        r,
		bashExec:      bashExec,
		mcpResolver:   mcpResolver,
		roleMap:       roleMap,
		modelResolver: modelResolver,
	}
}

// SetSubscriber sets the parent SSE subscriber channel for forwarding grind events.
func (t *GrindLoopTool) SetSubscriber(ch chan<- core.AgentEvent) {
	t.subscriber = ch
}

func (t *GrindLoopTool) Name() string { return "grind_loop" }
func (t *GrindLoopTool) Description() string {
	return "Iteratively delegate a task to an employee and verify with a command. Retries with error feedback until verification passes or max attempts reached. Optionally escalates to a reviewer model if all attempts fail."
}

func (t *GrindLoopTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {"type": "string", "description": "The task to iteratively execute and verify"},
			"role": {"type": "string", "description": "Employee role name (marcus, alex, etc.) or custom instructions"},
			"verify_command": {"type": "string", "description": "Bash command to verify the work (e.g. 'go test ./...', 'npm run build')"},
			"max_attempts": {"type": "integer", "description": "Maximum grind attempts (default: 5)"},
			"verifier_model": {"type": "string", "description": "Model ID for final review if all attempts fail (optional)"}
		},
		"required": ["task", "role", "verify_command"]
	}`)
}

func (t *GrindLoopTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	task, _ := args["task"].(string)
	role, _ := args["role"].(string)
	verifyCommand, _ := args["verify_command"].(string)

	if task == "" {
		return nil, core.NewToolError("grind_loop", "missing required parameter 'task'")
	}
	if role == "" {
		return nil, core.NewToolError("grind_loop", "missing required parameter 'role'")
	}
	if verifyCommand == "" {
		return nil, core.NewToolError("grind_loop", "missing required parameter 'verify_command'")
	}

	maxAttempts := 5
	if v, ok := args["max_attempts"].(float64); ok && v > 0 {
		maxAttempts = int(v)
	}
	verifierModel, _ := args["verifier_model"].(string)

	// Resolve role
	resolvedInstructions, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel, _ := resolveRole(role, nil, 15, 0.4, t.mcpResolver, t.roleMap)

	if len(resolvedTools) == 0 {
		return nil, core.NewToolError("grind_loop", "role '"+role+"' has no tools and no explicit tools provided")
	}

	var history []AttemptRecord
	var lastResult string
	var verifyOutput string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Build task prompt — raw on first attempt, augmented with error context on retries
		taskPrompt := task
		if attempt > 1 && verifyOutput != "" {
			taskPrompt = fmt.Sprintf("%s\n\n---\nPrevious attempt (%d/%d) failed verification.\nVerification output:\n%s\n\nFix the issues and try again.", task, attempt-1, maxAttempts, truncateVerifyOutput(verifyOutput, 4000))
		}

		// Emit grind_attempt event
		core.SendEvent(t.subscriber, core.AgentEvent{
			Type: core.EventTypeGrindAttempt,
			Data: core.AgentEventData{
				AgentName: extractAgentName(role),
				Task:      truncateTask(fmt.Sprintf("Attempt %d/%d", attempt, maxAttempts), 80),
				Status:    "running",
			},
		})

		// GRIND: Create fresh sub-agent
		result, err := t.runner.RunDelegate(ctx, taskPrompt, resolvedInstructions, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel)

		if err != nil {
			// Sub-agent error — treat as failed attempt
			lastResult = fmt.Sprintf("Error: %v", err)
			if resultMap, ok := result["partial_result"].(string); ok && resultMap != "" {
				lastResult = resultMap
			}
		} else {
			if r, ok := result["result"].(string); ok {
				lastResult = r
			}
		}

		// VERIFY: Run verify command
		var verifyErr error
		verifyOutput, verifyErr = t.bashExec.Exec(ctx, verifyCommand)
		verifyPass := verifyErr == nil

		// Extract exit code from error message if present
		exitCode := 0
		exitOutput := verifyOutput
		if verifyErr != nil {
			exitCode = extractExitCode(verifyErr.Error())
			exitOutput = verifyOutput
			if exitOutput == "" {
				exitOutput = verifyErr.Error()
			}
		}

		record := AttemptRecord{
			Attempt:       attempt,
			ResultSnippet: truncateStr(lastResult, 500),
			VerifyOutput:  truncateStr(exitOutput, 500),
			Pass:          verifyPass,
		}
		history = append(history, record)

		// Emit grind_verify event
		core.SendEvent(t.subscriber, core.AgentEvent{
			Type: core.EventTypeGrindVerify,
			Data: core.AgentEventData{
				AgentName: extractAgentName(role),
				Task:      fmt.Sprintf("Attempt %d: exit=%d", attempt, exitCode),
				Status:    boolToStatus(verifyPass),
			},
		})

		if verifyPass {
			// Success — emit end and return
			result := GrindResult{
				Status:      "success",
				Attempts:    attempt,
				FinalResult: lastResult,
				VerifyPass:  true,
				History:     history,
			}
			t.emitGrindEnd("success", attempt)
			return result, nil
		}
	}

	// All attempts exhausted
	status := "failed"

	// Optional: escalate to verifier model
	var verifierReview string
	if verifierModel != "" && t.modelResolver != nil {
		verifierReview = t.runVerifierReview(ctx, verifierModel, task, history)
		if verifierReview != "" {
			status = "failed_with_review"
		}
	}

	t.emitGrindEnd(status, maxAttempts)

	return GrindResult{
		Status:         status,
		Attempts:       maxAttempts,
		FinalResult:    lastResult,
		VerifyPass:     false,
		History:        history,
		VerifierReview: verifierReview,
	}, nil
}

// runVerifierReview creates a single-shot LLM call with full attempt history.
func (t *GrindLoopTool) runVerifierReview(ctx context.Context, modelID, task string, history []AttemptRecord) string {
	provider := t.modelResolver(modelID)
	if provider == nil {
		return ""
	}

	// Build review prompt from attempt history
	var sb strings.Builder
	sb.WriteString("The following task was attempted multiple times but verification kept failing.\n\n")
	fmt.Fprintf(&sb, "Task: %s\n\n", task)
	sb.WriteString("Attempt history:\n")
	for _, h := range history {
		fmt.Fprintf(&sb, "\n--- Attempt %d ---\n", h.Attempt)
		fmt.Fprintf(&sb, "Result: %s\n", h.ResultSnippet)
		fmt.Fprintf(&sb, "Verify output: %s\n", h.VerifyOutput)
	}
	sb.WriteString("\nPlease review the attempts above and provide a corrected solution or explain what went wrong.")

	reviewCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	events := make(chan core.AgentEvent, 64)
	cfg := core.AgentLoopConfig{
		SystemPrompt:  "You are a senior engineer reviewing failed attempts. Provide a corrected solution.",
		MaxToolRounds: 1, // single-shot, no tools
		MaxTokens:     4096,
		ContextSize:   8192,
		Tools:         nil, // no tools
		Opts: core.GenerateOptions{
			MaxTokens: 4096,
			Temperature: 0.3,
		},
	}

	sess := &subSession{parent: &noopSession{}, msgCount: 0}
	loop := core.NewAgentLoop(provider, nil, sess, cfg)

	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		defer close(events)
		runErr = loop.Run(reviewCtx, sb.String(), events)
	}()

	var reviewText string
	for {
		select {
		case <-done:
			for evt := range events {
				if evt.Type == core.EventTypeTextDelta || evt.Type == core.EventTypeAgentEnd {
					reviewText += evt.Data.Text
				}
			}
			if runErr != nil {
				return ""
			}
			return reviewText
		case evt, ok := <-events:
			if !ok {
				return reviewText
			}
			if evt.Type == core.EventTypeTextDelta || evt.Type == core.EventTypeAgentEnd {
				reviewText += evt.Data.Text
			}
		}
	}
}

func (t *GrindLoopTool) emitGrindEnd(status string, attempts int) {
	core.SendEvent(t.subscriber, core.AgentEvent{
		Type: core.EventTypeGrindEnd,
		Data: core.AgentEventData{
			Status: status,
			Task:   fmt.Sprintf("%d attempts", attempts),
		},
	})
}

// extractExitCode parses "exec exited with code N" from error messages.
func extractExitCode(errMsg string) int {
	// Look for pattern: "exec exited with code N"
	prefix := "exec exited with code "
	if _, after, found := strings.Cut(errMsg, prefix); found {
		var code int
		if _, err := fmt.Sscanf(after, "%d", &code); err == nil {
			return code
		}
	}
	return 1 // default non-zero exit code
}

func truncateVerifyOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}

func boolToStatus(pass bool) string {
	if pass {
		return "pass"
	}
	return "fail"
}

// noopSession is a minimal session for verifier review loops.
type noopSession struct{}

func (s *noopSession) ID() string                                         { return "verifier" }
func (s *noopSession) Close() error                                        { return nil }
func (s *noopSession) AppendMessage(msg core.Message) error                { return nil }
func (s *noopSession) BuildContext(ctx context.Context) ([]core.Message, error) { return nil, nil }
func (s *noopSession) GetTree() *core.TreeNode                             { return nil }
func (s *noopSession) Navigate(nodeID string) error                        { return nil }
func (s *noopSession) Branch(label string) (string, error)                 { return "", nil }
func (s *noopSession) Fork(nodeID string) (core.Session, error)            { return nil, nil }
func (s *noopSession) Compact(ctx context.Context, llmProvider any) (string, error) { return "", nil }
func (s *noopSession) TruncateToolResults(keep int) (int, error)           { return 0, nil }
func (s *noopSession) GetCurrentNode() string                              { return "verifier" }
