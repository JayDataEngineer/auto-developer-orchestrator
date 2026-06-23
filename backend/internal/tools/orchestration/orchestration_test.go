package orchestration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/meta"
)

// makeRole is a helper to create a single-entry role map.
func makeRole(name, description, prompt, model string, tools []string, rounds int, division string, temp float32) map[string]*common.AgentRole {
	role := &common.AgentRole{
		Name:        name,
		Description: description,
		Prompt:      prompt,
		Tools:       tools,
		MaxRounds:   rounds,
		Division:    division,
		Model:       model,
	}
	if temp != 0 {
		role.Temperature = temp
	}
	return map[string]*common.AgentRole{name: role}
}

// webMCPServer returns a resolver that expands "web" and "media" MCP prefixes.
func webMCPServer(prefix string) []string {
	switch prefix {
	case "web":
		return []string{"mcp__research__search", "mcp__research__scrape", "mcp__research__research"}
	case "media":
		return []string{"mcp__media__analyze_image"}
	}
	return nil
}

// setupProjectRoot sets PROJECT_ROOT for kernel role lookups.
func setupProjectRoot(t *testing.T) {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	t.Setenv("PROJECT_ROOT", repoRoot)
	common.ReloadPromptTemplate()
}

func TestResolveRole_KernelRole(t *testing.T) {
	setupProjectRoot(t)

	instructions, tools, _, _, model, division, _, _, _, _ := resolveRole("researcher", nil, 15, 0.4, webMCPServer, nil)

	if division != "" {
		t.Errorf("expected no division for kernel role, got %q", division)
	}
	if instructions == "" {
		t.Error("expected non-empty instructions from kernel role")
	}
	if len(tools) == 0 {
		t.Error("expected tools from kernel role after MCP expansion")
	}
	// Model is intentionally empty in researcher.yaml so the worker default
	// (set by the user via /api/pux/defaults) is respected. Hardcoding a model
	// in the role config would override the user's choice — see fix for the
	// stuck-researcher bug where qwen3.6-27b-q5_k_s was hardcoded.
	if model != "" {
		t.Errorf("expected empty model from kernel role 'researcher' (should inherit worker default), got %q", model)
	}
}

func TestResolveRole_OrgRoleOverridesKernel(t *testing.T) {
	roleMap := makeRole("sarah", "Org-specific Sarah", "You are org-sarah.", "custom-model",
		[]string{"bash", "read_file"}, 10, "", 0.8)

	instructions, tools, rounds, temp, model, division, _, _, _, _ := resolveRole("sarah", nil, 15, 0.4, nil, roleMap)

	if instructions != "You are org-sarah." {
		t.Errorf("expected org-specific prompt, got %q", instructions)
	}
	if len(tools) != 2 || tools[0] != "bash" || tools[1] != "read_file" {
		t.Errorf("expected [bash, read_file], got %v", tools)
	}
	if rounds != 10 {
		t.Errorf("expected rounds 10, got %d", rounds)
	}
	if temp != 0.8 {
		t.Errorf("expected temp 0.8, got %f", temp)
	}
	if model != "custom-model" {
		t.Errorf("expected model 'custom-model', got %q", model)
	}
	if division != "" {
		t.Errorf("expected no division, got %q", division)
	}
}

func TestResolveRole_DivisionHead(t *testing.T) {
	roleMap := makeRole("research-director", "Research Division Head", "You manage research analysts.",
		"deepseek/deepseek-v4-flash", nil, 25, "./divisions/research", 0)

	instructions, _, _, _, model, division, _, _, _, _ := resolveRole("research-director", nil, 15, 0.4, nil, roleMap)

	if division != "./divisions/research" {
		t.Errorf("expected division './divisions/research', got %q", division)
	}
	if model != "deepseek/deepseek-v4-flash" {
		t.Errorf("expected model override, got %q", model)
	}
	if instructions != "You manage research analysts." {
		t.Errorf("expected division head prompt, got %q", instructions)
	}
}

func TestResolveRole_CustomInstructions(t *testing.T) {
	customInstructions := "Analyze this dataset and find anomalies"
	instructions, tools, rounds, temp, model, division, _, _, _, _ := resolveRole(customInstructions, []string{"bash", "grep"}, 20, 0.6, nil, nil)

	if instructions != customInstructions {
		t.Errorf("expected custom instructions to pass through, got %q", instructions)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	if rounds != 20 {
		t.Errorf("expected rounds 20, got %d", rounds)
	}
	if temp != 0.6 {
		t.Errorf("expected temp 0.6, got %f", temp)
	}
	if model != "" {
		t.Errorf("expected no model for custom, got %q", model)
	}
	if division != "" {
		t.Errorf("expected no division for custom, got %q", division)
	}
}

func TestResolveRole_ExplicitToolsOverrideRole(t *testing.T) {
	roleMap := makeRole("alex", "IT Ops", "You are alex.", "", []string{"bash", "memory"}, 10, "", 0)

	_, tools, _, _, _, _, _, _, _, _ := resolveRole("alex", []string{"bash", "read_file", "write_file"}, 15, 0.4, nil, roleMap)

	if len(tools) != 3 {
		t.Errorf("expected 3 explicit tools, got %d: %v", len(tools), tools)
	}
}

func TestResolveRole_MCPExpansion(t *testing.T) {
	roleMap := makeRole("scout", "News Scout", "You research news.", "",
		[]string{"bash"}, 15, "", 0)
	roleMap["scout"].MCPServers = []string{"web", "media"}

	_, tools, _, _, _, _, _, _, _, _ := resolveRole("scout", nil, 15, 0.4, webMCPServer, roleMap)

	expectedCount := 5 // bash + 3 web tools + 1 media tool
	if len(tools) != expectedCount {
		t.Errorf("expected %d tools, got %d: %v", expectedCount, len(tools), tools)
	}

	hasSearch := false
	hasAnalyze := false
	for _, t := range tools {
		if t == "mcp__research__search" {
			hasSearch = true
		}
		if t == "mcp__media__analyze_image" {
			hasAnalyze = true
		}
	}
	if !hasSearch {
		t.Error("missing mcp__research__search from web server expansion")
	}
	if !hasAnalyze {
		t.Error("missing mcp__media__analyze_image from media server expansion")
	}
}

func TestResolveRole_DefaultOverrides(t *testing.T) {
	roleMap := makeRole("custom", "Custom agent", "Custom prompt.", "",
		[]string{"bash"}, 25, "", 0.9)

	// Default values (15, 0.4) should be overridden by role
	_, _, rounds, temp, _, _, _, _, _, _ := resolveRole("custom", nil, 15, 0.4, nil, roleMap)
	if rounds != 25 {
		t.Errorf("expected rounds 25 from role, got %d", rounds)
	}
	if temp != 0.9 {
		t.Errorf("expected temp 0.9 from role, got %f", temp)
	}

	// Non-default values should be preserved
	_, _, rounds2, temp2, _, _, _, _, _, _ := resolveRole("custom", nil, 30, 0.7, nil, roleMap)
	if rounds2 != 30 {
		t.Errorf("expected explicit rounds 30, got %d", rounds2)
	}
	if temp2 != 0.7 {
		t.Errorf("expected explicit temp 0.7, got %f", temp2)
	}
}

// ---- Tests for sub-agent event forwarding helpers ----

func TestExtractAgentName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sarah", "sarah"},
		{"jake", "jake"},
		{"marcus", "marcus"},
		{"", "agent"},
		{"You are a helpful assistant that researches topics", "You are a helpful as..."},
		{"short", "short"},
	}
	for _, tt := range tests {
		got := extractAgentName(tt.input)
		if got != tt.expected {
			t.Errorf("extractAgentName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestTruncateTask(t *testing.T) {
	short := "search the web for recent AI papers"
	if got := truncateTask(short, 120); got != short {
		t.Errorf("expected short task unchanged, got %q", got)
	}
	long := string(make([]byte, 200))
	if got := truncateTask(long, 120); len(got) != 123 { // 120 + "..."
		t.Errorf("expected 123 chars, got %d", len(got))
	}
}

func TestRunDelegate_EmitsSubAgentEvents(t *testing.T) {
	// Verify that RunDelegate emits subagent_start and subagent_end events
	// to the subscriber channel injected via context (Contract 3.4 compliance).
	events := make(chan core.AgentEvent, 32)

	runner := NewParallelRunner(RunnerConfig{
		ToolSpecs: []core.OpenAITool{},
	})

	// Inject subscriber via context (Contract 3.4 pattern)
	ctx := context.WithValue(context.Background(), core.SubscriberKey{}, events)

	// RunDelegate with no tools should still emit start+end events
	_, err := runner.RunDelegate(ctx, "test task", "sarah", []string{"nonexistent_tool"}, 5, 0.4, "", "", false)

	// Should get subagent_start
	evt := <-events
	if evt.Type != core.EventTypeSubAgentStart {
		t.Errorf("expected subagent_start, got %q", evt.Type)
	}
	startData, ok := evt.Data.(core.SubAgentStartData)
	if !ok {
		t.Fatalf("expected SubAgentStartData payload, got %T", evt.Data)
	}
	if startData.AgentName != "sarah" {
		t.Errorf("expected agentName 'sarah', got %q", startData.AgentName)
	}

	// Should get subagent_end
	evt = <-events
	if evt.Type != core.EventTypeSubAgentEnd {
		t.Errorf("expected subagent_end, got %q", evt.Type)
	}
	endData, ok := evt.Data.(core.SubAgentEndData)
	if !ok {
		t.Fatalf("expected SubAgentEndData payload, got %T", evt.Data)
	}
	if endData.Status != "error" {
		t.Errorf("expected status 'error' for no-tools case, got %q", endData.Status)
	}

	if err == nil {
		t.Error("expected error from RunDelegate with no matching tools")
	}
}

func TestSubscriberFromCtx(t *testing.T) {
	// Contract 3.4: subscriber is retrieved from context, not struct field

	// No subscriber in background context
	ch := subscriberFromCtx(context.Background())
	if ch != nil {
		t.Error("expected nil subscriber from empty context")
	}

	// Inject subscriber via context
	events := make(chan core.AgentEvent, 16)
	ctx := context.WithValue(context.Background(), core.SubscriberKey{}, events)
	ch = subscriberFromCtx(ctx)
	if ch == nil {
		t.Error("expected subscriber from context with SubscriberKey")
	}
}

// --- Error path tests ---

func TestDelegateTo_MissingTask(t *testing.T) {
	tool := NewDelegateToTool(nil, nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"role": "researcher",
	})
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	te, ok := err.(*core.ToolError)
	if !ok {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	if te.ToolName != "delegate_to" {
		t.Errorf("expected tool name 'delegate_to', got %q", te.ToolName)
	}
}

func TestDelegateTo_MissingRole(t *testing.T) {
	tool := NewDelegateToTool(nil, nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"task": "do something",
	})
	if err == nil {
		t.Fatal("expected error for missing role")
	}
	te, ok := err.(*core.ToolError)
	if !ok {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	if te.ToolName != "delegate_to" {
		t.Errorf("expected tool name 'delegate_to', got %q", te.ToolName)
	}
}

func TestDelegateTo_EmptyTaskAfterStepFallback(t *testing.T) {
	tool := NewDelegateToTool(nil, nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"role": "researcher",
		"step": "",
	})
	if err == nil {
		t.Fatal("expected error for empty task and step")
	}
}

func TestDelegateTo_BackwardsCompatInstructions(t *testing.T) {
	// 'instructions' field should work as fallback for 'role'
	tool := NewDelegateToTool(nil, nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"task":         "analyze the data",
		"instructions": "custom-instructions-field",
	})
	// Should not fail on missing 'role' because 'instructions' is used as fallback
	if err == nil {
		t.Fatal("expected error — no role provider means no tools resolved")
	}
	te, ok := err.(*core.ToolError)
	if !ok {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	// Should fail because role 'custom-instructions-field' has no tools
	if te.ToolName != "delegate_to" {
		t.Errorf("expected delegate_to error, got %q", te.ToolName)
	}
}

func TestResolveRole_NoToolsForCustom(t *testing.T) {
	// Custom instructions with no tools and no matching role = empty tool list
	_, tools, _, _, _, _, _, _, _, _ := resolveRole("totally-unknown-role-xyz", nil, 15, 0.4, nil, nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for unknown role with no explicit tools, got %d", len(tools))
	}
}

func TestResolveRole_StepFieldFallback(t *testing.T) {
	// When 'task' is empty but 'step' is provided, step should be used
	// (This is tested indirectly through DelegateTo, but let's test resolveRole defaults)
	instructions, tools, rounds, temp, model, division, _, _, _, _ := resolveRole("test-agent", []string{"bash"}, 5, 0.3, nil, nil)
	if instructions != "test-agent" {
		t.Errorf("expected custom instructions passthrough, got %q", instructions)
	}
	if len(tools) != 1 || tools[0] != "bash" {
		t.Errorf("expected [bash], got %v", tools)
	}
	if rounds != 5 {
		t.Errorf("expected rounds 5, got %d", rounds)
	}
	if temp != 0.3 {
		t.Errorf("expected temp 0.3, got %f", temp)
	}
	if model != "" {
		t.Errorf("expected no model for custom, got %q", model)
	}
	if division != "" {
		t.Errorf("expected no division for custom, got %q", division)
	}
}

func TestMemoArtifact_WritesToDisk(t *testing.T) {
	dir := t.TempDir()
	content := "## Codebase Brief\n\n### File Tree\n- foo.go\n- bar.go"
	filePath := filepath.Join(dir, ".pux", "memos", "explorer-20260601-120000.md")
	header := fmt.Sprintf("<!-- agent: %s | saved: %s -->\n\n", "explorer", time.Now().Format(time.RFC3339))

	path, err := meta.WriteArtifact(context.Background(), nil, filePath, "explorer-123", "memo", "explorer", header+content)
	if err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read memo: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, content) {
		t.Errorf("memo file doesn't contain original content.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "<!-- agent: explorer") {
		t.Errorf("memo missing agent frontmatter.\ngot:\n%s", got)
	}
}

func TestMemoArtifact_SlugifiedPath(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, ".pux", "memos", "code-orchestrator-20260601-120000.md")

	path, err := meta.WriteArtifact(context.Background(), nil, filePath, "co-123", "memo", "code_orchestrator", "test content")
	if err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}
	if !strings.Contains(path, "code-orchestrator-") {
		t.Errorf("expected slugified name in path, got %s", path)
	}
}

// TestDrainAndForward_MouseActionForwarded is a regression test for the
// mouse_action forwarding bug. drainAndForward previously filtered out
// EventTypeMouseAction, so the visual cursor overlay never received events
// for browser/desktop tool calls executed by sub-agents.
//
// This test directly drives drainAndForward by feeding it events through
// the events channel and confirms that mouse_action survives the filter
// and reaches the parent subscriber.
func TestDrainAndForward_MouseActionForwarded(t *testing.T) {
	r := &ParallelRunner{cfg: RunnerConfig{
		Logger: func(string, ...interface{}) {},
	}}

	events := make(chan core.AgentEvent, 8)
	done := make(chan struct{})
	subscriber := make(chan core.AgentEvent, 8)

	// Feed representative events: tool_start, mouse_action, text_delta.
	events <- core.AgentEvent{Type: core.EventTypeToolStart, Data: core.ToolStart{ToolName: "click_element"}}
	events <- core.AgentEvent{Type: core.EventTypeMouseAction, Data: core.MouseActionData{NormX: 0.5, NormY: 0.25, Action: "click"}}
	events <- core.AgentEvent{Type: core.EventTypeTextDelta, Data: core.TextDelta{Text: "hi"}}
	close(events)
	// NOTE: do NOT close `done` here — closing done makes drainAndForward
	// take the early-exit path that drains events without forwarding them.
	// Closing events alone is enough to terminate the function.

	res := r.drainAndForward(context.Background(), subscriber, "browser_ops", events, done, nil, "t1", "test")

	if res.FinalText != "hi" {
		t.Fatalf("expected final text 'hi', got %q", res.FinalText)
	}
	close(subscriber)

	var types []core.AgentEventType
	for evt := range subscriber {
		types = append(types, evt.Type)
	}

	// Expect all three types to be forwarded, including mouse_action.
	want := []core.AgentEventType{core.EventTypeToolStart, core.EventTypeMouseAction, core.EventTypeTextDelta}
	if len(types) != len(want) {
		t.Fatalf("expected %d forwarded events, got %d: %v", len(want), len(types), types)
	}
	for i, w := range want {
		if types[i] != w {
			t.Fatalf("event %d: want %s, got %s", i, w, types[i])
		}
	}
}
