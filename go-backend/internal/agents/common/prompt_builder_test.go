package common

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	os.Setenv("PROJECT_ROOT", root)
	return root
}

func TestPromptBuilder_SectionFiles(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)
	ctx := &PromptContext{
		KernelRoles: LoadAgentRoles(),
	}

	// Verify all stable section files load
	for _, s := range builder.sections {
		if s.Level == Stable && s.File != "" {
			content := builder.getStable(s)
			if content == "" {
				t.Errorf("stable section %q (%s) returned empty", s.Name, s.File)
			}
		}
	}

	// Verify the full prompt builds
	prompt := builder.Build(ctx)
	if prompt == "" {
		t.Fatal("Build returned empty prompt")
	}
}

func TestPromptBuilder_BoundaryMarker(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)
	ctx := &PromptContext{
		KernelRoles: LoadAgentRoles(),
	}

	prompt := builder.Build(ctx)

	count := strings.Count(prompt, DynamicBoundary)
	if count != 1 {
		t.Errorf("expected exactly 1 boundary marker, got %d", count)
	}

	// Boundary should appear after identity section
	idx := strings.Index(prompt, DynamicBoundary)
	if !strings.Contains(prompt[:idx], "CTO") {
		t.Error("expected CTO identity before boundary marker")
	}
}

func TestPromptBuilder_StableCaching(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)

	// First call — loads from file
	content1 := builder.getStable(builder.sections[0]) // identity
	if content1 == "" {
		t.Fatal("first load returned empty")
	}

	// Second call — should hit cache
	content2 := builder.getStable(builder.sections[0])
	if content2 != content1 {
		t.Error("cached content differs from initial load")
	}

	// Verify it's in the cache map
	builder.mu.RLock()
	cached, ok := builder.cache["identity"]
	builder.mu.RUnlock()
	if !ok || cached != content1 {
		t.Error("identity not found in cache")
	}
}

func TestPromptBuilder_VolatileNotCached(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)

	ctx1 := &PromptContext{SandboxID: "sandbox-abc"}
	ctx2 := &PromptContext{SandboxID: "sandbox-xyz"}

	content1 := builder.renderSandboxID(ctx1)
	content2 := builder.renderSandboxID(ctx2)

	if content1 == content2 {
		t.Error("volatile section should differ between contexts")
	}
	if !strings.Contains(content1, "sandbox-abc") {
		t.Error("volatile content should contain sandbox-abc")
	}
}

func TestFormatRolesList_HintField(t *testing.T) {
	roles := map[string]*AgentRole{
		"test_worker": {
			Name:        "test_worker",
			Hint:        "Short hint for CTO",
			Description: "Full persona with detailed instructions for the subagent",
		},
		"no_hint_worker": {
			Name:        "no_hint_worker",
			Description: "Fallback persona when hint is empty",
		},
	}

	result := formatRolesList(roles)

	// test_worker should use hint
	if !strings.Contains(result, "Short hint for CTO") {
		t.Error("expected hint text for test_worker")
	}
	if strings.Contains(result, "Full persona with detailed instructions") {
		t.Error("persona should NOT appear for worker with hint — it's subagent-only")
	}

	// no_hint_worker should fall back to Description
	if !strings.Contains(result, "Fallback persona when hint is empty") {
		t.Error("expected Description fallback for no_hint_worker")
	}
}

func TestBuildOrchestratorPromptV2_Fallback(t *testing.T) {
	root := repoRoot(t)
	os.Setenv("PROJECT_ROOT", root)

	// With prompt_sections/ existing, V2 should use section pipeline
	prompt := BuildOrchestratorPromptV2(nil, "sandbox-123", "", "", nil, nil)
	if prompt == "" {
		t.Fatal("V2 returned empty prompt")
	}
	if !strings.Contains(prompt, DynamicBoundary) {
		t.Error("V2 with prompt_sections should contain boundary marker")
	}
}

func TestBuildOrchestratorPromptV2_LegacyFallback(t *testing.T) {
	// Temporarily point to a non-existent config dir to trigger fallback
	origConfigDir := FindKernelConfigDir()

	// Create a temp dir without prompt_sections
	tmpDir := t.TempDir()
	os.Setenv("PROJECT_ROOT", tmpDir)

	// Reset singleton so it picks up the new config dir
	ResetGlobalBuilder()

	prompt := BuildOrchestratorPromptV2(nil, "sandbox-legacy", "", "", nil, nil)
	if prompt == "" {
		t.Fatal("legacy fallback returned empty prompt")
	}
	// Legacy path should NOT have boundary marker
	if strings.Contains(prompt, DynamicBoundary) {
		t.Error("legacy fallback should not contain boundary marker")
	}

	// Restore
	if origConfigDir != "" {
		os.Setenv("PROJECT_ROOT", origConfigDir)
	} else {
		os.Unsetenv("PROJECT_ROOT")
	}
	ResetGlobalBuilder()
}

func TestBuildOrchestratorPromptV2_ToolsSection(t *testing.T) {
	root := repoRoot(t)
	os.Setenv("PROJECT_ROOT", root)

	// Create a mock tool
	mockTool := &mockTool{name: "test_tool", desc: "A test tool"}
	prompt := BuildOrchestratorPromptV2([]core.Tool{mockTool}, "", "", "", nil, nil)

	if !strings.Contains(prompt, "test_tool") {
		t.Error("prompt should contain tool name")
	}
	if !strings.Contains(prompt, "A test tool") {
		t.Error("prompt should contain tool description")
	}
}

// mockTool implements core.Tool for testing
type mockTool struct {
	name string
	desc string
}

func (m *mockTool) Name() string                    { return m.name }
func (m *mockTool) Description() string             { return m.desc }
func (m *mockTool) Schema() json.RawMessage         { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return nil, nil
}

func TestPromptBuilder_OrgAfterBoundary(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)
	ctx := &PromptContext{
		KernelRoles: LoadAgentRoles(),
		Org: &OrgManifest{
			Name:        "test-org",
			Description: "Test organization",
		},
	}

	prompt := builder.Build(ctx)
	idx := strings.Index(prompt, DynamicBoundary)

	if idx == -1 {
		t.Fatal("expected boundary marker in prompt")
	}

	// Org header should appear AFTER the boundary, not before
	beforeBoundary := prompt[:idx]
	afterBoundary := prompt[idx+len(DynamicBoundary):]

	if strings.Contains(beforeBoundary, "Organization: test-org") {
		t.Error("org header should appear AFTER boundary, not before stable sections")
	}
	if !strings.Contains(afterBoundary, "Organization: test-org") {
		t.Error("org header should appear after boundary marker")
	}

	// Stable content (CTO identity) should still be before boundary
	if !strings.Contains(beforeBoundary, "CTO") {
		t.Error("expected CTO identity before boundary marker")
	}
}

func TestPromptBuilder_SingletonPersists(t *testing.T) {
	root := repoRoot(t)
	os.Setenv("PROJECT_ROOT", root)
	ResetGlobalBuilder()

	// First call creates the singleton
	prompt1 := BuildOrchestratorPromptV2(nil, "sandbox-1", "", "", nil, nil)
	if prompt1 == "" {
		t.Fatal("first call returned empty prompt")
	}

	// Verify singleton exists
	if globalBuilder == nil {
		t.Fatal("globalBuilder should be initialized after first call")
	}

	// Second call should reuse same builder
	builderBefore := globalBuilder
	prompt2 := BuildOrchestratorPromptV2(nil, "sandbox-2", "", "", nil, nil)
	if globalBuilder != builderBefore {
		t.Error("singleton builder should persist across calls")
	}

	// Stable content should be identical
	idx1 := strings.Index(prompt1, DynamicBoundary)
	idx2 := strings.Index(prompt2, DynamicBoundary)
	if idx1 == -1 || idx2 == -1 {
		t.Fatal("expected boundary marker in prompts")
	}
	if prompt1[:idx1] != prompt2[:idx2] {
		t.Error("stable content should be identical across calls (from cache)")
	}

	ResetGlobalBuilder()
}

func TestPromptBuilder_MCPInstructions(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)
	ctx := &PromptContext{
		KernelRoles: LoadAgentRoles(),
		MCPInstructions: map[string]string{
			"web":   "Use the web research tools for searching and scraping.",
			"media": "Use media tools for image analysis.",
		},
	}

	prompt := builder.Build(ctx)

	if !strings.Contains(prompt, "MCP Server Instructions") {
		t.Error("expected MCP instructions section header")
	}
	if !strings.Contains(prompt, "web research tools") {
		t.Error("expected web MCP instructions")
	}
	if !strings.Contains(prompt, "media tools") {
		t.Error("expected media MCP instructions")
	}
}

func TestPromptBuilder_MCPInstructionsEmpty(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)
	ctx := &PromptContext{
		KernelRoles: LoadAgentRoles(),
	}

	prompt := builder.Build(ctx)

	if strings.Contains(prompt, "MCP Server Instructions") {
		t.Error("should not render MCP section when no instructions provided")
	}
}

func TestRegisterMCPInstructions(t *testing.T) {
	RegisterMCPInstructions("test_server", "Use test tools wisely.")

	m := MCPInstructionsMap()
	if m["test_server"] != "Use test tools wisely." {
		t.Error("expected registered instruction to be available")
	}

	// Clean up
	mcpInstructionStore.mu.Lock()
	delete(mcpInstructionStore.instructions, "test_server")
	mcpInstructionStore.mu.Unlock()
}

func TestPromptBuilder_InheritedCacheKeyChangesWithTools(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)

	// Context with one tool
	ctx1 := &PromptContext{
		Tools:       []core.Tool{&mockTool{name: "tool_a", desc: "A"}},
		KernelRoles: LoadAgentRoles(),
	}

	// Context with different tool
	ctx2 := &PromptContext{
		Tools:       []core.Tool{&mockTool{name: "tool_b", desc: "B"}},
		KernelRoles: LoadAgentRoles(),
	}

	key1 := builder.inheritedCacheKey(builder.sections[9], ctx1) // tools section
	key2 := builder.inheritedCacheKey(builder.sections[9], ctx2) // tools section

	if key1 == key2 {
		t.Error("cache key should differ when tool names change")
	}
}

func TestPromptBuilder_InheritedCacheKeyChangesWithMCP(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	builder := NewPromptBuilder(configDir)

	ctx1 := &PromptContext{
		KernelRoles:     LoadAgentRoles(),
		MCPInstructions: map[string]string{"web": "use web tools"},
	}
	ctx2 := &PromptContext{
		KernelRoles:     LoadAgentRoles(),
		MCPInstructions: map[string]string{"media": "use media tools"},
	}

	// Use the mcp section (index 10)
	key1 := builder.inheritedCacheKey(builder.sections[10], ctx1)
	key2 := builder.inheritedCacheKey(builder.sections[10], ctx2)

	if key1 == key2 {
		t.Error("cache key should differ when MCP instructions change")
	}
}
