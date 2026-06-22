package common

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// resolveOrg returns the canonical path to a consolidated org under orgs/.
// Falls back to t.Skip if the org dir isn't found (e.g. running outside the repo).
func resolveOrg(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// backend/internal/agents/common/org_test.go → repo root = ../../../..
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	candidate := filepath.Join(repoRoot, "orgs", name)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		t.Skipf("org %s not found at %s", name, candidate)
	}
	return candidate
}

func TestLoadOrgManifest(t *testing.T) {
	org := LoadOrgManifest(resolveOrg(t, "twitter-agent"))
	if org == nil {
		t.Fatal("expected org to be loaded from twitter-agent")
	}

	if org.Name != "twitter-agent" {
		t.Errorf("expected name 'twitter-agent', got %q", org.Name)
	}

	if org.RolesDir() == "" {
		t.Error("expected RolesDir to be set")
	}

	if org.ManifestoContent() == "" {
		t.Error("expected manifesto content")
	}

	if len(org.Schedules) != 4 {
		t.Errorf("expected 4 schedules, got %d", len(org.Schedules))
	}

	// First schedule (morning_post) should be enabled and assigned to content-writer
	if org.Schedules[0].Role != "content-writer" {
		t.Errorf("expected role 'content-writer', got %q", org.Schedules[0].Role)
	}
	if !org.Schedules[0].Enabled {
		t.Error("expected morning_post to be enabled")
	}
}

func TestLoadOrgRolesFromTwitterAgent(t *testing.T) {
	org := LoadOrgManifest(resolveOrg(t, "twitter-agent"))
	if org == nil {
		t.Fatal("expected org")
	}

	roles := LoadAgentRolesFrom(org.RolesDir())
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(roles))
	}

	for _, name := range []string{"content-writer", "researcher", "engagement-manager"} {
		if _, ok := roles[name]; !ok {
			t.Errorf("missing role: %s", name)
		}
	}

	// content-writer should import browser
	cw := roles["content-writer"]
	if !slices.Contains(cw.Imports, "browser") {
		t.Error("content-writer should import 'browser'")
	}
	if cw.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("expected content-writer model 'deepseek/deepseek-v4-flash', got %q", cw.Model)
	}
}

func TestOrgManifestNoPuxYaml(t *testing.T) {
	dir := t.TempDir()
	org := LoadOrgManifest(dir)
	if org != nil {
		t.Error("expected nil for directory without pux.yaml")
	}
}

func TestOrgManifestEmptyName(t *testing.T) {
	dir := t.TempDir()
	yaml := "description: test\n"
	os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yaml), 0644)
	org := LoadOrgManifest(dir)
	if org != nil {
		t.Error("expected nil for pux.yaml without name")
	}
}

func TestTechNoirOrg(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	t.Setenv("PROJECT_ROOT", repoRoot)

	org := LoadOrgManifest(resolveOrg(t, "tech-noir"))
	if org == nil {
		t.Fatal("expected org to be loaded from tech-noir")
	}

	if org.Name != "tech-noir" {
		t.Errorf("expected name 'tech-noir', got %q", org.Name)
	}

	if org.RolesDir() == "" {
		t.Error("expected RolesDir to be set")
	}

	if org.ToolPkgsDir() == "" {
		t.Error("expected ToolPkgsDir to be set")
	}

	if manifesto := org.ManifestoContent(); manifesto == "" {
		t.Error("expected manifesto content")
	} else if !contains(manifesto, "Tech Noir Studio") {
		t.Errorf("manifesto missing 'Tech Noir Studio', got:\n%s", manifesto[:100])
	}

	// Load all 7 roles (5 original + studio-director + docs-writer from Phase 3)
	roles := LoadAgentRolesFrom(org.RolesDir())
	if len(roles) != 7 {
		t.Fatalf("expected 7 roles, got %d", len(roles))
	}

	expectedRoles := []string{
		"technical_artist",
		"narrative_designer",
		"gameplay_programmer",
		"qa_tester",
		"design_researcher",
		"studio-director",
		"docs-writer",
	}
	for _, name := range expectedRoles {
		role := roles[name]
		if role == nil {
			t.Errorf("missing role: %s", name)
			continue
		}
		if role.Description == "" {
			t.Errorf("%s: description is empty", name)
		}
		if len(role.Imports) == 0 {
			t.Errorf("%s: no imports configured", name)
		}
		if role.Prompt == "" {
			t.Errorf("%s: prompt is empty", name)
		}
		if role.MaxRounds == 0 {
			t.Errorf("%s: max_rounds is zero", name)
		}
	}

	// Verify specific role properties
	ta := roles["technical_artist"]
	if ta.SandboxTier != "native" {
		t.Errorf("technical_artist: expected sandbox 'native', got %q", ta.SandboxTier)
	}
	if ta.Temperature != 0.2 {
		t.Errorf("technical_artist: expected temperature 0.2, got %f", ta.Temperature)
	}
	hasArtImport := slices.Contains(ta.Imports, "tech_noir_art")
	if !hasArtImport {
		t.Error("technical_artist should import 'tech_noir_art'")
	}

	nd := roles["narrative_designer"]
	if nd.SandboxTier != "isolated" {
		t.Errorf("narrative_designer: expected sandbox 'isolated', got %q", nd.SandboxTier)
	}
	if nd.Temperature != 0.7 {
		t.Errorf("narrative_designer: expected temperature 0.7, got %f", nd.Temperature)
	}

	gp := roles["gameplay_programmer"]
	if gp.SandboxTier != "native" {
		t.Errorf("gameplay_programmer: expected sandbox 'native', got %q", gp.SandboxTier)
	}
	hasGodotImport := slices.Contains(gp.Imports, "godot")
	hasCodeImport := slices.Contains(gp.Imports, "code")
	if !hasGodotImport {
		t.Error("gameplay_programmer should import 'godot'")
	}
	if !hasCodeImport {
		t.Error("gameplay_programmer should import 'code'")
	}

	qt := roles["qa_tester"]
	if qt.SandboxTier != "native" {
		t.Errorf("qa_tester: expected sandbox 'native', got %q", qt.SandboxTier)
	}
	if qt.MaxRounds != 10 {
		t.Errorf("qa_tester: expected max_rounds 10, got %d", qt.MaxRounds)
	}

	dr := roles["design_researcher"]
	if dr.SandboxTier != "isolated" {
		t.Errorf("design_researcher: expected sandbox 'isolated', got %q", dr.SandboxTier)
	}
	if dr.Temperature != 0.3 {
		t.Errorf("design_researcher: expected temperature 0.3, got %f", dr.Temperature)
	}

	// Verify org tool packages are resolvable via imports
	_ = LoadToolPackages() // warm kernel cache
	dir := org.ToolPkgsDir()
	if dir == "" {
		t.Fatal("ToolPkgsDir is empty")
	}
	MergeToolPackages(dir)

	// Reload roles so they pick up the merged packages
	roles = LoadAgentRolesFrom(org.RolesDir())

	// technical_artist imports tech_noir_art + comfyui + studio_vision + code.
	// Phase 3 redesign: Ray/ComfyUI/Godot are now Python HTTP bridges driven
	// by bash, not MCP servers. The only MCP server technical_artist should
	// pick up is `media` (from tech_noir_art + studio_vision).
	ta = roles["technical_artist"]
	if ta == nil {
		t.Fatal("technical_artist role not found after reload")
	}
	hasMediaMCP := false
	for _, s := range ta.MCPServers {
		switch s {
		case "media":
			hasMediaMCP = true
		case "tech_noir", "comfyui", "qwen-vision":
			t.Errorf("technical_artist should NOT have legacy %q MCP server (Phase 3 switched to HTTP bridges)", s)
		}
	}
	if !hasMediaMCP {
		t.Error("technical_artist missing 'media' MCP server (needed for QA vibe reports)")
	}

	// gameplay_programmer imports godot + code. godot is now an HTTP bridge
	// via bash + godot_client.py — no MCP servers at all.
	gp = roles["gameplay_programmer"]
	if gp == nil {
		t.Fatal("gameplay_programmer role not found after reload")
	}
	for _, s := range gp.MCPServers {
		t.Errorf("gameplay_programmer should have NO MCP servers (godot is HTTP bridge), got %q", s)
	}

	// Verify kernel packages still resolve alongside org packages
	hasBash := false
	for _, t := range gp.Tools {
		if t == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Error("gameplay_programmer missing 'bash' tool from kernel code package")
	}
}

func TestOrgPromptMergesKernelRoles(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	t.Setenv("PROJECT_ROOT", repoRoot)

	// test-suite/ is at the repo root (not under orgs/) — it's a project-local
	// org used by the AI test runner. Resolve directly.
	testSuitePath := filepath.Join(repoRoot, "test-suite")
	if _, err := os.Stat(testSuitePath); os.IsNotExist(err) {
		t.Skipf("test-suite not found at %s", testSuitePath)
	}

	org := LoadOrgManifest(testSuitePath)
	if org == nil {
		t.Fatal("expected org to be loaded from test-suite")
	}

	orgRoles := LoadAgentRolesFrom(org.RolesDir())
	if len(orgRoles) == 0 {
		t.Fatal("expected org roles to be loaded")
	}

	prompt := BuildOrchestratorPromptWithOrg(nil, "", "", "", org, orgRoles)

	// Verify kernel workers appear in the merged prompt
	for _, name := range []string{"browser_ops", "desktop_ops", "researcher", "code_ops", "vision_ops", "shell_ops"} {
		if !contains(prompt, name) {
			t.Errorf("kernel role %q missing from merged prompt — org should add to kernel, not replace", name)
		}
	}

	// Verify org roles also appear
	for _, name := range []string{"api_auditor", "interaction_tester", "visual_auditor", "regression_hunter"} {
		if !contains(prompt, name) {
			t.Errorf("org role %q missing from merged prompt", name)
		}
	}

	// Verify manifesto is prepended
	if !contains(prompt, "AI QA Organization") {
		t.Error("manifesto content not found in prompt")
	}
}
