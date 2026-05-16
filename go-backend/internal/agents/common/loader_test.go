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
	os.Setenv("PROJECT_ROOT", repoRoot)

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

	// Test researcher model override
	researcher := GetAgentRole("researcher")
	if researcher == nil {
		t.Fatal("researcher worker not found")
	}
	if researcher.Model != "qwen3.6-27b-q5_k_s" {
		t.Errorf("researcher: expected model 'qwen3.6-27b-q5_k_s', got %q", researcher.Model)
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
	os.Setenv("PROJECT_ROOT", repoRoot)

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

	// Verify new capabilities have SKILL.md content
	for _, name := range expected {
		pkg := pkgs[name]
		if pkg.Skill == "" {
			t.Errorf("capability %q has no SKILL.md content", name)
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
