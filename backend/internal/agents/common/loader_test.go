package common

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadAgentRoles(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	t.Setenv("PROJECT_ROOT", repoRoot)

	ReloadPromptTemplate()

	roles := LoadAgentRoles()
	if len(roles) == 0 {
		t.Fatal("no roles loaded")
	}

	// Verify new worker names exist (from config/workers/)
	newWorkers := []string{"browser_ops", "code_ops", "desktop_ops", "researcher", "vision_ops", "shell_ops", "general"}
	for _, name := range newWorkers {
		role := roles[name]
		if role == nil {
			t.Errorf("missing worker: %s", name)
			continue
		}
		if role.Prompt == "" {
			t.Errorf("%s: prompt is empty", name)
		}
		if len(role.Tools) == 0 && len(role.MCPServers) == 0 && len(role.Capabilities) == 0 {
			t.Errorf("%s: no tools, mcp_servers, or capabilities configured", name)
		}
		if role.MaxRounds == 0 {
			t.Errorf("%s: max_rounds is zero", name)
		}
	}

	// Test new workers have capability skills stitched in
	desktopOps := GetAgentRole("desktop_ops")
	if desktopOps == nil {
		t.Fatal("desktop_ops worker not found")
	}
	if !contains(desktopOps.Prompt, "desktop_observe") {
		t.Errorf("desktop_ops prompt missing capability skill — expected 'desktop_observe' from SKILL.md, got:\n%s", desktopOps.Prompt[:200])
	}
	if desktopOps.SandboxTier != "bridged" {
		t.Errorf("desktop_ops: expected sandbox 'bridged', got %q", desktopOps.SandboxTier)
	}

	// Researcher intentionally has no hardcoded model — it inherits the
	// worker default (set by the user via /api/pux/defaults). Hardcoding a
	// model in the role would override the user's choice. See fix for the
	// stuck-researcher bug where qwen3.6-27b-q5_k_s was hardcoded.
	researcher := GetAgentRole("researcher")
	if researcher == nil {
		t.Fatal("researcher worker not found")
	}
	if researcher.Model != "" {
		t.Errorf("researcher: expected empty model (inherits worker default), got %q", researcher.Model)
	}

	// Test FormatAgentList includes all workers
	list := FormatAgentList()
	if list == "" {
		t.Error("FormatAgentList returned empty string")
	}
	for _, name := range newWorkers {
		if !contains(list, name) {
			t.Errorf("FormatAgentList missing worker: %s", name)
		}
	}
}

func TestToolPackages(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	t.Setenv("PROJECT_ROOT", repoRoot)

	ReloadPromptTemplate()

	pkgs := LoadToolPackages()
	if len(pkgs) == 0 {
		t.Fatal("no tool packages loaded")
	}

	// Verify both legacy and new capabilities are loaded
	expected := []string{"browser", "research", "vision", "shell", "desktop", "code"}
	for _, name := range expected {
		if pkgs[name] == nil {
			t.Errorf("missing tool package/capability: %s", name)
		}
	}

	// Verify non-polymorphic capabilities still ship SKILL.md content.
	// Polymorphic capabilities (those with implementations[]) route their
	// prompt through ActiveImpl.Prompt instead, so Skill is legitimately empty.
	polymorphic := map[string]bool{"research": true}
	for _, name := range expected {
		if polymorphic[name] {
			continue
		}
		pkg := pkgs[name]
		if pkg.Skill == "" {
			t.Errorf("capability %q has no SKILL.md content", name)
		}
	}

	// Polymorphic capabilities MUST have implementations[] populated.
	// Regression guard for Stage 2: research migrated to 2-tier form.
	if pkg := pkgs["research"]; pkg != nil {
		if len(pkg.Implementations) < 2 {
			t.Errorf("research capability should have 2+ implementations (cloud, bash-ddg), got %d", len(pkg.Implementations))
		}
		// PromptFile must be resolved into Prompt by LoadCapabilitiesFrom.
		for i := range pkg.Implementations {
			impl := pkg.Implementations[i]
			if impl.PromptFile != "" && impl.Prompt == "" {
				t.Errorf("research impl %q: PromptFile %q not resolved into Prompt", impl.Name, impl.PromptFile)
			}
		}
	}

	// Test ResolveImports
	tools, mcpServers := ResolveImports([]string{"shell", "vision"})
	if len(tools) == 0 {
		t.Error("ResolveImports returned no tools for shell+vision")
	}
	if len(mcpServers) == 0 {
		t.Error("ResolveImports returned no mcp_servers for shell+vision")
	}
	hasMedia := false
	for _, s := range mcpServers {
		if s == "media" {
			hasMedia = true
		}
	}
	if !hasMedia {
		t.Error("ResolveImports missing 'media' mcp_server from vision package")
	}
	hasBash := false
	for _, t := range tools {
		if t == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Error("ResolveImports missing 'bash' tool from shell package")
	}

	// Test new workers resolve capabilities into tools
	browserOps := GetAgentRole("browser_ops")
	if browserOps == nil {
		t.Fatal("browser_ops worker not found")
	}
	// browser_ops imports browser → should have bash tool
	hasBash = false
	for _, t := range browserOps.Tools {
		if t == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Error("browser_ops missing 'bash' tool from browser capability")
	}
}

func TestDivisionFieldLoading(t *testing.T) {
	// Create a temp role directory with division field
	dir := t.TempDir()
	roleDir := filepath.Join(dir, "research-director")
	if err := os.MkdirAll(roleDir, 0755); err != nil {
		t.Fatal(err)
	}

	configYAML := `description: "Research Division Head"
division: "./divisions/research"
max_rounds: 25
model: "deepseek/deepseek-v4-flash"
imports:
  - research
`
	if err := os.WriteFile(filepath.Join(roleDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roleDir, "prompt.md"), []byte("You manage the research division."), 0644); err != nil {
		t.Fatal(err)
	}

	roles := LoadAgentRolesFrom(dir)
	role := roles["research-director"]
	if role == nil {
		t.Fatal("research-director role not loaded")
	}
	if role.Division != "./divisions/research" {
		t.Errorf("expected division './divisions/research', got %q", role.Division)
	}
	if role.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("expected model 'deepseek/deepseek-v4-flash', got %q", role.Model)
	}
	if role.MaxRounds != 25 {
		t.Errorf("expected max_rounds 25, got %d", role.MaxRounds)
	}
}

func TestDivisionInFormatList(t *testing.T) {
	roles := map[string]*AgentRole{
		"research-director": {
			Name:        "research-director",
			Description: "Research Division Head",
			Division:    "./divisions/research",
		},
		"analyst": {
			Name:        "analyst",
			Description: "Data Analyst",
			Tools:       []string{"bash", "read_file"},
		},
	}

	list := formatRolesList(roles)
	if !contains(list, "division: ./divisions/research") {
		t.Error("formatRolesList missing division info")
		t.Logf("output:\n%s", list)
	}
	if !contains(list, "research-director") {
		t.Error("formatRolesList missing division head name")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestBackwardCompatNoImplementations proves capabilities without
// implementations[] continue to flow through the legacy path (top-level
// tools/mcp_servers + SKILL.md) unchanged. The resolver MUST NOT break them.
func TestBackwardCompatNoImplementations(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	t.Setenv("PROJECT_ROOT", repoRoot)

	// Force a fresh global resolver so this test is hermetic against whatever
	// a prior test left in the package cache.
	SetGlobalResolver(nil)
	ReloadPromptTemplate()

	pkgs := LoadToolPackages()
	for _, name := range []string{"vision", "shell", "code"} {
		pkg := pkgs[name]
		if pkg == nil {
			t.Fatalf("missing capability: %s", name)
		}
		if len(pkg.Implementations) != 0 {
			t.Errorf("%s: expected zero implementations (legacy), got %d", name, len(pkg.Implementations))
		}
		if pkg.ActiveImpl != nil {
			t.Errorf("%s: expected nil ActiveImpl (legacy), got %v", name, pkg.ActiveImpl)
		}
	}

	// BuildWorkerPrompt against a legacy capability must stitch SKILL.md
	// content in. Without a resolver set, capabilityPrompt falls through
	// to GetCapabilitySkill.
	prompt := BuildWorkerPrompt("test-persona", []string{"vision"})
	if !contains(prompt, "vision") {
		t.Errorf("BuildWorkerPrompt missing vision capability, got:\n%s", prompt)
	}
	if !contains(prompt, "test-persona") {
		t.Errorf("BuildWorkerPrompt missing persona prefix, got:\n%s", prompt)
	}
}

// TestBuildWorkerPromptUsesImplPromptWhenResolved proves the morphing-prompt
// hook works: when the global resolver returns an Implementation for a
// capability, its Prompt is preferred over the legacy SKILL.md.
func TestBuildWorkerPromptUsesImplPromptWhenResolved(t *testing.T) {
	prev := GetGlobalResolver()
	t.Cleanup(func() { SetGlobalResolver(prev) })

	r := NewResolver(nil)
	// Inject a resolved impl directly into the cache to avoid relying on
	// disk state for the test. The once has already fired; this is the
	// shape ResolveAll would have produced.
	r.once.Do(func() {
		r.resolved = map[string]*Implementation{
			"research": {
				Name:   "bash-ddg",
				Prompt: "MORPHED-BASH-DDG-PROMPT-MARKER",
			},
		}
	})
	SetGlobalResolver(r)

	prompt := BuildWorkerPrompt("persona-x", []string{"research"})
	if !contains(prompt, "MORPHED-BASH-DDG-PROMPT-MARKER") {
		t.Errorf("expected morphed prompt marker, got:\n%s", prompt)
	}
}

// TestResolveImportsUsesActiveImplTools proves ResolveImports routes through
// ActiveImpl.Tools/MCPServers when set. This is the load-bearing fix for
// Stage 2: if ActiveImpl is set but the legacy Tools field is read, workers
// would get the dead MCP server despite the morphed prompt saying bash.
func TestResolveImportsUsesActiveImplTools(t *testing.T) {
	prev := GetGlobalResolver()
	t.Cleanup(func() { SetGlobalResolver(prev) })

	// Inject a polymorphic capability into the package cache with ActiveImpl
	// set to the bash tier. ResolveImports should return bash tools, NOT
	// the legacy web MCP server.
	toolPkgMu.Lock()
	toolPackages = map[string]*ToolPackage{
		"research": {
			Name:       "research",
			Tools:      []string{}, // legacy empty
			MCPServers: []string{"web"}, // legacy MCP
			ActiveImpl: &Implementation{
				Name:       "bash-ddg",
				Tools:      []string{"bash"},
				MCPServers: []string{}, // bash tier has no MCP
			},
		},
	}
	toolPkgMu.Unlock()
	t.Cleanup(func() {
		toolPkgMu.Lock()
		toolPackages = nil
		toolPkgMu.Unlock()
	})

	tools, mcpServers := ResolveImports([]string{"research"})
	hasBash := false
	hasWeb := false
	for _, t := range tools {
		if t == "bash" {
			hasBash = true
		}
	}
	for _, s := range mcpServers {
		if s == "web" {
			hasWeb = true
		}
	}
	if !hasBash {
		t.Errorf("expected bash tool from ActiveImpl, got tools=%v", tools)
	}
	if hasWeb {
		t.Errorf("expected web MCP to be dropped (ActiveImpl overrides), got mcpServers=%v", mcpServers)
	}
}
