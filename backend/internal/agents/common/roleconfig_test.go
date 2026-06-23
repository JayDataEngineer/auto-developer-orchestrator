package common

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// TestRoleConfigEveryFieldRoundTrips is the structural guard against the
// dual-struct bug (bug_dual_config_struct_split). If anyone removes a field
// from RoleConfig, OR forgets to wire it in either loader, this test fails.
//
// The original bug: agentConfig (legacy roles) and workerConfig (kernel
// workers) were two separate structs. Adding a YAML tag to one did NOT add
// it to the other, so delegates_to: silently vanished for org roles. We
// unified them into one RoleConfig; this test ensures they stay unified.
func TestRoleConfigEveryFieldRoundTrips(t *testing.T) {
	// YAML exercising every field. Written in the legacy role shape; the
	// loader must still pick up every value.
	const yamlContent = `description: "Test role — every field populated"
hint: "cto-facing one-liner"
persona: "alt identity text"
imports:
  - shell
capabilities:
  - explore
tools:
  - bash
mcp_servers:
  - web
max_rounds: 42
temperature: 0.7
model: "test-model"
division: "research"
sandbox: "bridged"
delegates_to:
  - risk-analyst
  - position-sizer
hooks:
  - file_checkpoint
`

	t.Run("legacy loader (loadRoleFromFolder)", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yamlContent), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}

		role := loadRoleFromFolder(dir)
		if role == nil {
			t.Fatal("loadRoleFromFolder returned nil")
		}

		// Description wins over persona when both are set (legacy format)
		if role.Description != "Test role — every field populated" {
			t.Errorf("Description: got %q", role.Description)
		}
		if role.MaxRounds != 42 {
			t.Errorf("MaxRounds: got %d", role.MaxRounds)
		}
		if role.Temperature != 0.7 {
			t.Errorf("Temperature: got %f", role.Temperature)
		}
		if role.Model != "test-model" {
			t.Errorf("Model: got %q", role.Model)
		}
		if role.Division != "research" {
			t.Errorf("Division: got %q — THIS IS THE BUGdual-struct-split SYMPTOM if it fails", role.Division)
		}
		if role.SandboxTier != "bridged" {
			t.Errorf("SandboxTier: got %q", role.SandboxTier)
		}
		if !reflect.DeepEqual(role.DelegatesTo, []string{"risk-analyst", "position-sizer"}) {
			t.Errorf("DelegatesTo: got %v — THIS IS THE BUGdual-struct-split SYMPTOM if it fails", role.DelegatesTo)
		}
		if !reflect.DeepEqual(role.Hooks, []string{"file_checkpoint"}) {
			t.Errorf("Hooks: got %v", role.Hooks)
		}
		// imports + capabilities are merged
		if len(role.Imports) != 2 {
			t.Errorf("Imports (merged imports+capabilities): got %v", role.Imports)
		}
		// MCP from direct mcp_servers plus resolved packages (shell/explore don't add MCP)
		if !containsString(role.MCPServers, "web") {
			t.Errorf("MCPServers missing 'web': got %v", role.MCPServers)
		}
	})

	t.Run("worker loader (LoadWorkersFrom)", func(t *testing.T) {
		dir := t.TempDir()
		// Same YAML content, but in workers/<name>.yaml layout
		if err := os.WriteFile(filepath.Join(dir, "full_worker.yaml"), []byte(yamlContent), 0644); err != nil {
			t.Fatal(err)
		}

		roles := LoadWorkersFrom(dir)
		role, ok := roles["full_worker"]
		if !ok {
			t.Fatalf("LoadWorkersFrom did not return 'full_worker'; got roles: %v", roles)
		}

		// Persona wins over description for workers
		if role.Description != "alt identity text" {
			t.Errorf("Description (should fall back to persona): got %q", role.Description)
		}
		if role.Hint != "cto-facing one-liner" {
			t.Errorf("Hint: got %q", role.Hint)
		}
		if role.MaxRounds != 42 {
			t.Errorf("MaxRounds: got %d", role.MaxRounds)
		}
		if role.Temperature != 0.7 {
			t.Errorf("Temperature: got %f", role.Temperature)
		}
		if role.Model != "test-model" {
			t.Errorf("Model: got %q", role.Model)
		}
		if role.Division != "research" {
			t.Errorf("Division: got %q — THIS IS THE BUGdual-struct-split SYMPTOM if it fails", role.Division)
		}
		if role.SandboxTier != "bridged" {
			t.Errorf("SandboxTier: got %q", role.SandboxTier)
		}
		if !reflect.DeepEqual(role.DelegatesTo, []string{"risk-analyst", "position-sizer"}) {
			t.Errorf("DelegatesTo: got %v — THIS IS THE BUGdual-struct-split SYMPTOM if it fails", role.DelegatesTo)
		}
		if !reflect.DeepEqual(role.Hooks, []string{"file_checkpoint"}) {
			t.Errorf("Hooks: got %v", role.Hooks)
		}
		if !containsString(role.MCPServers, "web") {
			t.Errorf("MCPServers missing 'web': got %v", role.MCPServers)
		}
	})
}

func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

// TestRoleConfigThinkingRoundTrip is the guard for the Anthropic-Fable
// thinking-mode plumbing (PR1). If anyone removes Thinking from RoleConfig,
// OR forgets to wire it in either loader, this test fails — the parallel of
// TestRoleConfigEveryFieldRoundTrips for the single new field added in PR1
// alongside the diligence prompt work.
func TestRoleConfigThinkingRoundTrip(t *testing.T) {
	const yamlContent = `description: "thinking-enabled role"
hint: "uses extended CoT"
persona: "body"
capabilities:
  - shell
thinking: true
`

	t.Run("legacy loader (loadRoleFromFolder)", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yamlContent), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}
		role := loadRoleFromFolder(dir)
		if role == nil {
			t.Fatal("loadRoleFromFolder returned nil")
		}
		if !role.Thinking {
			t.Errorf("Thinking: got false, want true — check that the field is wired in loadRoleFromFolder")
		}
	})

	t.Run("worker loader (LoadWorkersFrom)", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "thinker.yaml"), []byte(yamlContent), 0644); err != nil {
			t.Fatal(err)
		}
		roles := LoadWorkersFrom(dir)
		role, ok := roles["thinker"]
		if !ok {
			t.Fatalf("LoadWorkersFrom did not return 'thinker'; got roles: %v", roles)
		}
		if !role.Thinking {
			t.Errorf("Thinking: got false, want true — check that the field is wired in LoadWorkersFrom")
		}
	})

	t.Run("default is false when omitted", func(t *testing.T) {
		dir := t.TempDir()
		const noThinkingYAML = `description: "no thinking field"
persona: "body"
capabilities:
  - shell
`
		if err := os.WriteFile(filepath.Join(dir, "quietist.yaml"), []byte(noThinkingYAML), 0644); err != nil {
			t.Fatal(err)
		}
		roles := LoadWorkersFrom(dir)
		role, ok := roles["quietist"]
		if !ok {
			t.Fatalf("LoadWorkersFrom did not return 'quietist'; got roles: %v", roles)
		}
		if role.Thinking {
			t.Errorf("Thinking: got true, want false (field should default to off)")
		}
	})
}

// TestRoleConfigImportsAndCapabilitiesAreAliases verifies the video-producer
// pattern: a role config that specifies BOTH imports: and capabilities: must
// have both expanded, not just one. This was a latent inconsistency in the
// original split-struct design — only the legacy loader looked at imports,
// only the worker loader looked at capabilities.
func TestRoleConfigImportsAndCapabilitiesAreAliases(t *testing.T) {
	dir := t.TempDir()
	const yaml = `description: "mixes imports + capabilities"
imports:
  - shell
capabilities:
  - explore
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}

	role := loadRoleFromFolder(dir)
	if role == nil {
		t.Fatal("loadRoleFromFolder returned nil")
	}
	// Both should be merged into Imports
	if len(role.Imports) != 2 {
		t.Errorf("expected imports+capabilities merged to 2 entries, got %v", role.Imports)
	}
}

// TestInvestDelegationChainLoads is the end-to-end regression for the original
// user-reported bug ("yeah make sure invest bot has research agent to help").
// Loads the ACTUAL invest org roles from disk and asserts the three division
// heads have their delegates_to populated. This catches regressions in
// RoleConfig YAML parsing against real production config files, not just
// synthetic test fixtures.
func TestInvestDelegationChainLoads(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	investRoles := filepath.Join(repoRoot, "orgs", "invest", "roles")

	if _, err := os.Stat(investRoles); err != nil {
		t.Skipf("invest org not present at %s", investRoles)
	}

	roles := LoadAgentRolesFrom(investRoles)

	cases := []struct {
		head     string
		expected []string
	}{
		{"research-director", []string{"signal-analyst", "regime-analyst", "news-analyst", "filings-analyst", "crypto-analyst", "researcher"}},
		{"risk-officer", []string{"risk-analyst", "position-sizer"}},
		{"execution-manager", []string{"trader", "reporter"}},
	}

	for _, c := range cases {
		role, ok := roles[c.head]
		if !ok {
			t.Errorf("%s: role not loaded", c.head)
			continue
		}
		if !reflect.DeepEqual(role.DelegatesTo, c.expected) {
			t.Errorf("%s: DelegatesTo mismatch\n  want: %v\n  got:  %v\n"+
				"This is the dual-struct-split symptom — if this fails, check that the field is wired in RoleConfig AND the loader.",
				c.head, c.expected, role.DelegatesTo)
		}
	}
}
