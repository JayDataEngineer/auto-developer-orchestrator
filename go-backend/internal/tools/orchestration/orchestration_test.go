package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
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
	os.Setenv("PROJECT_ROOT", repoRoot)
	common.ReloadPromptTemplate()
}

func TestResolveRole_KernelRole(t *testing.T) {
	setupProjectRoot(t)

	instructions, tools, _, _, model, division, _ := resolveRole("sarah", nil, 15, 0.4, webMCPServer, nil)

	if division != "" {
		t.Errorf("expected no division for kernel role, got %q", division)
	}
	if instructions == "" {
		t.Error("expected non-empty instructions from kernel role")
	}
	if len(tools) == 0 {
		t.Error("expected tools from kernel role after MCP expansion")
	}
	if model == "" {
		t.Errorf("expected non-empty model from kernel role 'sarah', got empty")
	}
}

func TestResolveRole_OrgRoleOverridesKernel(t *testing.T) {
	roleMap := makeRole("sarah", "Org-specific Sarah", "You are org-sarah.", "custom-model",
		[]string{"bash", "read_file"}, 10, "", 0.8)

	instructions, tools, rounds, temp, model, division, _ := resolveRole("sarah", nil, 15, 0.4, nil, roleMap)

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

	instructions, _, _, _, model, division, _ := resolveRole("research-director", nil, 15, 0.4, nil, roleMap)

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
	instructions, tools, rounds, temp, model, division, _ := resolveRole(customInstructions, []string{"bash", "grep"}, 20, 0.6, nil, nil)

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

	_, tools, _, _, _, _, _ := resolveRole("alex", []string{"bash", "read_file", "write_file"}, 15, 0.4, nil, roleMap)

	if len(tools) != 3 {
		t.Errorf("expected 3 explicit tools, got %d: %v", len(tools), tools)
	}
}

func TestResolveRole_MCPExpansion(t *testing.T) {
	roleMap := makeRole("scout", "News Scout", "You research news.", "",
		[]string{"bash"}, 15, "", 0)
	roleMap["scout"].MCPServers = []string{"web", "media"}

	_, tools, _, _, _, _, _ := resolveRole("scout", nil, 15, 0.4, webMCPServer, roleMap)

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
	_, _, rounds, temp, _, _, _ := resolveRole("custom", nil, 15, 0.4, nil, roleMap)
	if rounds != 25 {
		t.Errorf("expected rounds 25 from role, got %d", rounds)
	}
	if temp != 0.9 {
		t.Errorf("expected temp 0.9 from role, got %f", temp)
	}

	// Non-default values should be preserved
	_, _, rounds2, temp2, _, _, _ := resolveRole("custom", nil, 30, 0.7, nil, roleMap)
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
	// to the subscriber channel when a subscriber is set.
	events := make(chan core.AgentEvent, 32)

	runner := &ParallelRunner{
		toolSpecs:  []core.OpenAITool{},
		tasks:      make(map[string]*asyncTask),
		subscriber: events,
	}
	runner.SetLogger(func(format string, args ...interface{}) {})

	// RunDelegate with no tools should still emit start+end events
	_, err := runner.RunDelegate(context.Background(), "test task", "sarah", []string{"nonexistent_tool"}, 5, 0.4, "", "")

	// Should get subagent_start
	evt := <-events
	if evt.Type != core.EventTypeSubAgentStart {
		t.Errorf("expected subagent_start, got %q", evt.Type)
	}
	if evt.Data.AgentName != "sarah" {
		t.Errorf("expected agentName 'sarah', got %q", evt.Data.AgentName)
	}

	// Should get subagent_end
	evt = <-events
	if evt.Type != core.EventTypeSubAgentEnd {
		t.Errorf("expected subagent_end, got %q", evt.Type)
	}
	if evt.Data.Status != "error" {
		t.Errorf("expected status 'error' for no-tools case, got %q", evt.Data.Status)
	}

	if err == nil {
		t.Error("expected error from RunDelegate with no matching tools")
	}
}

func TestSetSubscriber(t *testing.T) {
	runner := NewParallelRunner(nil, nil, nil, 0, nil)
	if runner.subscriber != nil {
		t.Error("expected nil subscriber initially")
	}
	ch := make(chan core.AgentEvent, 16)
	runner.SetSubscriber(ch)
	if runner.subscriber != ch {
		t.Error("expected subscriber to be set")
	}
}
