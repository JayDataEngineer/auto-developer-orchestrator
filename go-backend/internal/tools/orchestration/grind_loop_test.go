package orchestration

import (
	"context"
	"fmt"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// mockRunner tracks delegate calls for grind_loop tests.
type mockRunner struct {
	results []map[string]any    // results to return for each RunDelegate call
	errs    []error             // errors to return for each RunDelegate call
	calls   []delegateCall      // recorded calls
	idx     int
}

type delegateCall struct {
	task         string
	instructions string
	tools        []string
	maxRounds    int
	temperature  float32
	modelID      string
}

func (m *mockRunner) RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32, modelID string) (map[string]any, error) {
	m.calls = append(m.calls, delegateCall{task, instructions, toolNames, maxRounds, temperature, modelID})
	i := m.idx
	m.idx++
	if i < len(m.errs) && m.errs[i] != nil {
		result := map[string]any{}
		if i < len(m.results) {
			result = m.results[i]
		}
		return result, m.errs[i]
	}
	if i < len(m.results) {
		return m.results[i], nil
	}
	// Past configured entries: return last configured result
	last := len(m.results) - 1
	if last >= 0 {
		return m.results[last], nil
	}
	return map[string]any{"result": "mock result", "status": "completed"}, nil
}

func (m *mockRunner) RunDelegateAsync(ctx context.Context, taskID, task, instructions string, toolNames []string) (map[string]any, error) {
	return nil, nil
}

func (m *mockRunner) CollectAsyncResults(ctx context.Context) (map[string]any, error) {
	return nil, nil
}

func (m *mockRunner) RunDivisionDelegate(ctx context.Context, task, divisionPath, modelID string) (map[string]any, error) {
	return nil, nil
}

// mockBashExec tracks bash exec calls.
type mockBashExec struct {
	outputs []string // outputs for each call
	errors  []error  // errors for each call (non-nil = exit non-0)
	idx     int
	calls   []string
}

func (m *mockBashExec) Exec(ctx context.Context, command string) (string, error) {
	m.calls = append(m.calls, command)
	i := m.idx
	m.idx++
	if i < len(m.errors) {
		out := ""
		if i < len(m.outputs) {
			out = m.outputs[i]
		}
		return out, m.errors[i]
	}
	// Past configured entries: return last configured entry
	last := len(m.errors) - 1
	if last < 0 {
		return "", nil
	}
	out := ""
	if last < len(m.outputs) {
		out = m.outputs[last]
	}
	return out, m.errors[last]
}

func TestGrindLoop_SuccessOnFirstAttempt(t *testing.T) {
	runner := &mockRunner{
		results: []map[string]any{
			{"result": "file written", "status": "completed"},
		},
	}
	bash := &mockBashExec{
		outputs: []string{"ok"},
		errors:  []error{nil},
	}

	roleMap := makeRole("marcus", "Senior Developer", "You are marcus.", "", []string{"bash", "write_file"}, 15, "", 0)

	tool := NewGrindLoopTool(runner, bash, nil, roleMap, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"task":           "write a hello world program",
		"role":           "marcus",
		"verify_command": "go build ./...",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	grindResult := result.(GrindResult)
	if grindResult.Status != "success" {
		t.Errorf("expected status 'success', got %q", grindResult.Status)
	}
	if grindResult.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", grindResult.Attempts)
	}
	if !grindResult.VerifyPass {
		t.Error("expected verify pass")
	}
	if len(grindResult.History) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(grindResult.History))
	}
	if len(bash.calls) != 1 || bash.calls[0] != "go build ./..." {
		t.Errorf("expected verify_command 'go build ./...', got %v", bash.calls)
	}
}

func TestGrindLoop_RetryOnVerifyFailure(t *testing.T) {
	runner := &mockRunner{
		results: []map[string]any{
			{"result": "attempt 1", "status": "completed"},
			{"result": "attempt 2", "status": "completed"},
			{"result": "attempt 3 - fixed", "status": "completed"},
		},
	}
	bash := &mockBashExec{
		outputs: []string{"build error: undefined variable", "test failed: missing import", "PASS"},
		errors:  []error{fmt.Errorf("exec exited with code 1: build error"), fmt.Errorf("exec exited with code 1: test failed"), nil},
	}

	roleMap := makeRole("marcus", "Senior Developer", "You are marcus.", "", []string{"bash"}, 15, "", 0)

	tool := NewGrindLoopTool(runner, bash, nil, roleMap, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"task":           "fix the build",
		"role":           "marcus",
		"verify_command": "go test ./...",
		"max_attempts":   float64(5),
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	grindResult := result.(GrindResult)
	if grindResult.Status != "success" {
		t.Errorf("expected status 'success', got %q", grindResult.Status)
	}
	if grindResult.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", grindResult.Attempts)
	}
	if !grindResult.VerifyPass {
		t.Error("expected verify pass on third attempt")
	}
	if len(runner.calls) != 3 {
		t.Errorf("expected 3 delegate calls, got %d", len(runner.calls))
	}

	// Second call should have error feedback
	if len(runner.calls) >= 2 {
		if !containsString(runner.calls[1].task, "Previous attempt") {
			t.Errorf("expected retry task to include error feedback, got %q", runner.calls[1].task)
		}
	}
}

func TestGrindLoop_MaxAttemptsExhausted(t *testing.T) {
	runner := &mockRunner{
		results: []map[string]any{
			{"result": "fail 1", "status": "completed"},
			{"result": "fail 2", "status": "completed"},
			{"result": "fail 3", "status": "completed"},
		},
	}
	buildErr := fmt.Errorf("exec exited with code 1: still broken")
	bash := &mockBashExec{
		outputs: []string{"err1", "err2", "err3"},
		errors:  []error{buildErr, buildErr, buildErr},
	}

	roleMap := makeRole("alex", "IT Ops", "You are alex.", "", []string{"bash"}, 15, "", 0)

	tool := NewGrindLoopTool(runner, bash, nil, roleMap, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"task":           "fix the server",
		"role":           "alex",
		"verify_command": "systemctl status myapp",
		"max_attempts":   float64(3),
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	grindResult := result.(GrindResult)
	if grindResult.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", grindResult.Status)
	}
	if grindResult.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", grindResult.Attempts)
	}
	if grindResult.VerifyPass {
		t.Error("expected verify to not pass")
	}
	if len(grindResult.History) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(grindResult.History))
	}
	if grindResult.VerifierReview != "" {
		t.Error("expected no verifier review without verifier_model")
	}
}

func TestGrindLoop_VerifierModelEscalation(t *testing.T) {
	runner := &mockRunner{
		results: []map[string]any{
			{"result": "fail", "status": "completed"},
			{"result": "fail", "status": "completed"},
		},
	}
	buildErr := fmt.Errorf("exec exited with code 1: broken")
	bash := &mockBashExec{
		outputs: []string{"err1", "err2"},
		errors:  []error{buildErr, buildErr},
	}

	roleMap := makeRole("marcus", "Developer", "You are marcus.", "", []string{"bash"}, 15, "", 0)

	// Mock model resolver — returns nil so verifier review returns empty
	modelResolver := func(modelID string) core.LLMProvider { return nil }

	tool := NewGrindLoopTool(runner, bash, nil, roleMap, modelResolver)
	result, err := tool.Execute(context.Background(), map[string]any{
		"task":            "fix tests",
		"role":            "marcus",
		"verify_command":  "go test ./...",
		"max_attempts":    float64(2),
		"verifier_model":  "claude-sonnet-4-20250514",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	grindResult := result.(GrindResult)
	if grindResult.Status != "failed" {
		t.Errorf("expected status 'failed' (model resolver returns nil), got %q", grindResult.Status)
	}
	if grindResult.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", grindResult.Attempts)
	}
}

func TestGrindLoop_RoleResolution(t *testing.T) {
	runner := &mockRunner{
		results: []map[string]any{
			{"result": "done", "status": "completed"},
		},
	}
	bash := &mockBashExec{
		outputs: []string{"ok"},
		errors:  []error{nil},
	}

	roleMap := makeRole("marcus", "Senior Developer", "You are marcus, a senior developer.", "deepseek-v4", []string{"bash", "read_file", "write_file", "edit_file"}, 20, "", 0.3)

	tool := NewGrindLoopTool(runner, bash, nil, roleMap, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"task":           "refactor the auth module",
		"role":           "marcus",
		"verify_command": "go build ./...",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 delegate call, got %d", len(runner.calls))
	}

	call := runner.calls[0]
	if call.instructions != "You are marcus, a senior developer." {
		t.Errorf("expected role prompt, got %q", call.instructions)
	}
	if len(call.tools) != 4 {
		t.Errorf("expected 4 tools, got %d: %v", len(call.tools), call.tools)
	}
	if call.maxRounds != 20 {
		t.Errorf("expected maxRounds 20, got %d", call.maxRounds)
	}
	if call.modelID != "deepseek-v4" {
		t.Errorf("expected modelID 'deepseek-v4', got %q", call.modelID)
	}
}

func TestGrindLoop_MissingParameters(t *testing.T) {
	tool := NewGrindLoopTool(&mockRunner{}, &mockBashExec{}, nil, nil, nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing task", map[string]any{"role": "marcus", "verify_command": "go test"}},
		{"missing role", map[string]any{"task": "do thing", "verify_command": "go test"}},
		{"missing verify_command", map[string]any{"task": "do thing", "role": "marcus"}},
		{"all missing", map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tt.args)
			if err == nil {
				t.Error("expected error for missing parameters")
			}
		})
	}
}

func TestGrindLoop_ErrorFeedbackFormat(t *testing.T) {
	runner := &mockRunner{
		results: []map[string]any{
			{"result": "attempt 1 output", "status": "completed"},
			{"result": "attempt 2 with fix", "status": "completed"},
		},
	}
	bash := &mockBashExec{
		outputs: []string{"FAIL: test_bar: expected 5 got 3", "ok"},
		errors:  []error{fmt.Errorf("exec exited with code 1: FAIL: test_bar"), nil},
	}

	roleMap := makeRole("marcus", "Developer", "You are marcus.", "", []string{"bash"}, 15, "", 0)

	tool := NewGrindLoopTool(runner, bash, nil, roleMap, nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"task":           "fix test_bar",
		"role":           "marcus",
		"verify_command": "go test ./...",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	grindResult := result.(GrindResult)
	if grindResult.Status != "success" {
		t.Errorf("expected success on retry, got %q", grindResult.Status)
	}

	// Verify the retry prompt contains error context
	if len(runner.calls) < 2 {
		t.Fatal("expected at least 2 delegate calls")
	}
	retryTask := runner.calls[1].task
	if !containsString(retryTask, "Previous attempt") {
		t.Errorf("retry task should mention previous attempt, got %q", retryTask)
	}
	if !containsString(retryTask, "FAIL: test_bar") {
		t.Errorf("retry task should include verify output, got %q", retryTask)
	}
}

func TestGrindLoop_EmitsGrindEvents(t *testing.T) {
	runner := &mockRunner{
		results: []map[string]any{
			{"result": "done", "status": "completed"},
		},
	}
	bash := &mockBashExec{
		outputs: []string{"ok"},
		errors:  []error{nil},
	}

	events := make(chan core.AgentEvent, 32)
	roleMap := makeRole("marcus", "Developer", "You are marcus.", "", []string{"bash"}, 15, "", 0)

	tool := NewGrindLoopTool(runner, bash, nil, roleMap, nil)
	tool.SetSubscriber(events)

	_, err := tool.Execute(context.Background(), map[string]any{
		"task":           "write hello.go",
		"role":           "marcus",
		"verify_command": "go build",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get grind_attempt, grind_verify, grind_end
	expected := []core.AgentEventType{core.EventTypeGrindAttempt, core.EventTypeGrindVerify, core.EventTypeGrindEnd}
	for _, expectedType := range expected {
		select {
		case evt := <-events:
			if evt.Type != expectedType {
				t.Errorf("expected %q, got %q", expectedType, evt.Type)
			}
		default:
			t.Errorf("missing event of type %q", expectedType)
		}
	}
}

// helper
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
