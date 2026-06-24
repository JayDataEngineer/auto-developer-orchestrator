package sandboxtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeShutdown is the ContainerShutdown test stand-in. Records every call
// so tests can assert on the project path passed + inject errors.
type fakeShutdown struct {
	calls     []string
	removed   []string
	err       error
}

func (f *fakeShutdown) ShutdownByProjectLabel(ctx context.Context, projectPath string) ([]string, error) {
	f.calls = append(f.calls, projectPath)
	if f.err != nil {
		return nil, f.err
	}
	return f.removed, nil
}

// TestShutdownContainerToolReturnsJSON proves the tool's result shape
// matches what the agent (and frontend) expect: shutdown flag, container
// list, count, project path, reason, timestamp, next_prompt.
func TestShutdownContainerToolReturnsJSON(t *testing.T) {
	fake := &fakeShutdown{removed: []string{"abc123", "def456"}}
	tool := NewShutdownContainerTool(fake, "/proj/test")

	res, err := tool.Execute(context.Background(), map[string]any{
		"reason": "task complete",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}

	if m["shutdown"] != true {
		t.Errorf("expected shutdown=true, got %v", m["shutdown"])
	}
	if m["count"].(int) != 2 {
		t.Errorf("expected count=2, got %v", m["count"])
	}
	if m["project_path"].(string) != "/proj/test" {
		t.Errorf("expected project_path=/proj/test, got %v", m["project_path"])
	}
	if m["reason"].(string) != "task complete" {
		t.Errorf("expected reason='task complete', got %v", m["reason"])
	}
	if m["next_prompt"].(string) != "will re-create sandbox from scratch" {
		t.Errorf("expected next_prompt marker, got %v", m["next_prompt"])
	}
	containers, _ := m["containers"].([]string)
	if len(containers) != 2 || containers[0] != "abc123" {
		t.Errorf("expected containers=[abc123, def456], got %v", m["containers"])
	}

	// shutdown_at must parse as RFC3339.
	if ts, _ := m["shutdown_at"].(string); ts != "" {
		if !strings.Contains(ts, "T") {
			t.Errorf("shutdown_at should be RFC3339, got %q", ts)
		}
	} else {
		t.Errorf("shutdown_at missing or empty")
	}

	if len(fake.calls) != 1 || fake.calls[0] != "/proj/test" {
		t.Errorf("expected one call to /proj/test, got %v", fake.calls)
	}
}

// TestShutdownContainerToolDefaultReason proves the tool fills in a
// default reason when the agent doesn't supply one — preserves the audit
// trail's coherence (every shutdown has SOME reason attached).
func TestShutdownContainerToolDefaultReason(t *testing.T) {
	fake := &fakeShutdown{removed: []string{"x"}}
	tool := NewShutdownContainerTool(fake, "/proj/x")

	res, _ := tool.Execute(context.Background(), map[string]any{})
	m := res.(map[string]any)

	if m["reason"].(string) == "" {
		t.Error("expected default reason to be non-empty")
	}
	if !strings.Contains(m["reason"].(string), "shutdown_container") {
		t.Errorf("expected default reason to mention shutdown_container, got %v", m["reason"])
	}
}

// TestShutdownContainerToolEmptyProjectPath proves the tool returns an
// error result (not a crash) when project path wasn't wired. Defensive —
// the orchestrator should always set this, but a wiring regression
// shouldn't take down the agent loop.
func TestShutdownContainerToolEmptyProjectPath(t *testing.T) {
	fake := &fakeShutdown{}
	tool := NewShutdownContainerTool(fake, "") // empty

	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("tool should never return Go error — it returns JSON error. Got: %v", err)
	}
	m := res.(map[string]any)
	if _, hasErr := m["error"]; !hasErr {
		t.Errorf("expected error key in result, got %v", m)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 calls when project path empty, got %d", len(fake.calls))
	}
}

// TestShutdownContainerToolNilManager proves nil manager reference is
// handled defensively — a misconfigured CTO shouldn't crash.
func TestShutdownContainerToolNilManager(t *testing.T) {
	tool := NewShutdownContainerTool(nil, "/proj/x")
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("expected nil Go error, got %v", err)
	}
	m := res.(map[string]any)
	if _, hasErr := m["error"]; !hasErr {
		t.Errorf("expected error key in result, got %v", m)
	}
}

// TestShutdownContainerToolPropagatesManagerError proves errors from the
// manager surface as JSON errors, not Go panics.
func TestShutdownContainerToolPropagatesManagerError(t *testing.T) {
	fake := &fakeShutdown{err: context.DeadlineExceeded}
	tool := NewShutdownContainerTool(fake, "/proj/x")

	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("expected nil Go error even when manager fails, got %v", err)
	}
	m := res.(map[string]any)
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "shutdown_container failed") {
		t.Errorf("expected error to mention 'shutdown_container failed', got %q", errMsg)
	}
}

// TestShutdownContainerToolSchemaIsValidJSON proves the schema parses as
// JSON — frontends that introspect tool schemas rely on this.
func TestShutdownContainerToolSchemaIsValidJSON(t *testing.T) {
	tool := NewShutdownContainerTool(&fakeShutdown{}, "/proj/x")
	var v map[string]any
	if err := json.Unmarshal(tool.Schema(), &v); err != nil {
		t.Errorf("schema is not valid JSON: %v", err)
	}
}

// TestShutdownContainerToolNameAndDescription proves Name + Description
// are non-empty — the CTO prompt builder depends on both.
func TestShutdownContainerToolNameAndDescription(t *testing.T) {
	tool := NewShutdownContainerTool(&fakeShutdown{}, "/proj/x")
	if tool.Name() != "shutdown_container" {
		t.Errorf("expected Name=shutdown_container, got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description must be non-empty")
	}
	if !strings.Contains(tool.Description(), "AFTER yielding your final response") {
		t.Errorf("description should warn that it's terminal, got %q", tool.Description())
	}
}

// TestAllToolsReturnsAtLeastShutdownContainer proves AllTools() bulk
// registration includes our tool. This is the contract enforced by
// tool_audit_test.go — every tool package exposes AllTools().
func TestAllToolsReturnsAtLeastShutdownContainer(t *testing.T) {
	tools := AllTools(&fakeShutdown{}, "/proj/x")
	if len(tools) == 0 {
		t.Fatal("expected AllTools to return at least one tool, got empty slice")
	}
	found := false
	for _, tool := range tools {
		if tool.Name() == "shutdown_container" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected AllTools to include shutdown_container")
	}
}
