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
