package orchestration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
)

func TestResolveRole_KernelRole(t *testing.T) {
	// When no roleMap is provided, falls back to kernel roles
	// Kernel roles require PROJECT_ROOT to be set
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	os.Setenv("PROJECT_ROOT", repoRoot)

	// Force reload kernel roles with correct PROJECT_ROOT
	common.ReloadPromptTemplate()

	// Sarah uses imports: [research] → mcp_servers: [web]
	// Without mcpResolver, tools are empty (MCP expansion requires resolver)
	mcpResolver := func(prefix string) []string {
		if prefix == "web" {
			return []string{"mcp__research__search", "mcp__research__scrape", "mcp__research__research"}
		}
		return nil
	}

	instructions, tools, _, _, model, division := resolveRole("sarah", nil, 15, 0.4, mcpResolver, nil)

	if division != "" {
		t.Errorf("expected no division for kernel role, got %q", division)
	}
	if instructions == "" {
		t.Error("expected non-empty instructions from kernel role")
	}
	if len(tools) == 0 {
		t.Error("expected tools from kernel role after MCP expansion")
	}
	// Sarah uses gemini-3-flash-preview
	if model != "gemini-3-flash-preview" {
		t.Errorf("expected model 'gemini-3-flash-preview', got %q", model)
	}
}

func TestResolveRole_OrgRoleOverridesKernel(t *testing.T) {
	// Org roleMap takes priority over kernel roles
	roleMap := map[string]*common.AgentRole{
		"sarah": {
			Name:        "sarah",
			Description: "Org-specific Sarah",
			Prompt:      "You are org-sarah.",
			Tools:       []string{"bash", "read_file"},
			MaxRounds:   10,
			Temperature: 0.8,
			Model:       "custom-model",
		},
	}

	instructions, tools, rounds, temp, model, division := resolveRole("sarah", nil, 15, 0.4, nil, roleMap)

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
	// A role with Division set returns the division path
	roleMap := map[string]*common.AgentRole{
		"research-director": {
			Name:        "research-director",
			Description: "Research Division Head",
			Prompt:      "You manage research analysts.",
			Division:    "./divisions/research",
			Model:       "deepseek/deepseek-v4-flash",
			MaxRounds:   25,
		},
	}

	instructions, _, _, _, model, division := resolveRole("research-director", nil, 15, 0.4, nil, roleMap)

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
	// Non-role instructions pass through as-is
	customInstructions := "Analyze this dataset and find anomalies"
	instructions, tools, rounds, temp, model, division := resolveRole(customInstructions, []string{"bash", "grep"}, 20, 0.6, nil, nil)

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
	// When tools are explicitly passed, they override role defaults
	roleMap := map[string]*common.AgentRole{
		"alex": {
			Name:        "alex",
			Description: "IT Ops",
			Prompt:      "You are alex.",
			Tools:       []string{"bash", "memory"},
			MaxRounds:   10,
		},
	}

	_, tools, _, _, _, _ := resolveRole("alex", []string{"bash", "read_file", "write_file"}, 15, 0.4, nil, roleMap)

	// Explicit tools should override role defaults
	if len(tools) != 3 {
		t.Errorf("expected 3 explicit tools, got %d: %v", len(tools), tools)
	}
}

func TestResolveRole_MCPExpansion(t *testing.T) {
	// MCP resolver expands mcp_servers into tool names
	roleMap := map[string]*common.AgentRole{
		"scout": {
			Name:        "scout",
			Description: "News Scout",
			Prompt:      "You research news.",
			Tools:       []string{"bash"},
			MCPServers:  []string{"web", "media"},
		},
	}

	mcpResolver := func(prefix string) []string {
		if prefix == "web" {
			return []string{"mcp__research__search", "mcp__research__scrape"}
		}
		if prefix == "media" {
			return []string{"mcp__media__analyze_image"}
		}
		return nil
	}

	_, tools, _, _, _, _ := resolveRole("scout", nil, 15, 0.4, mcpResolver, roleMap)

	expectedCount := 4 // bash + 2 web tools + 1 media tool
	if len(tools) != expectedCount {
		t.Errorf("expected %d tools, got %d: %v", expectedCount, len(tools), tools)
	}

	// Verify MCP tools are present
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
	// max_rounds and temperature are only overridden if NOT at default values
	roleMap := map[string]*common.AgentRole{
		"custom": {
			Name:        "custom",
			Description: "Custom agent",
			Prompt:      "Custom prompt.",
			Tools:       []string{"bash"},
			MaxRounds:   25,
			Temperature: 0.9,
		},
	}

	// Default values (15, 0.4) should be overridden by role
	_, _, rounds, temp, _, _ := resolveRole("custom", nil, 15, 0.4, nil, roleMap)
	if rounds != 25 {
		t.Errorf("expected rounds 25 from role, got %d", rounds)
	}
	if temp != 0.9 {
		t.Errorf("expected temp 0.9 from role, got %f", temp)
	}

	// Non-default values should be preserved
	_, _, rounds2, temp2, _, _ := resolveRole("custom", nil, 30, 0.7, nil, roleMap)
	if rounds2 != 30 {
		t.Errorf("expected explicit rounds 30, got %d", rounds2)
	}
	if temp2 != 0.7 {
		t.Errorf("expected explicit temp 0.7, got %f", temp2)
	}
}
