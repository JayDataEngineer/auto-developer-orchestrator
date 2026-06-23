package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/skills"
)

// TestComputeSkillScope_ExplicitAllowlist proves RoleConfig.Skills is the
// authoritative scope when set. Capability-attached skills may add to it but
// never subtract.
func TestComputeSkillScope_ExplicitAllowlist(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "alpha", "SKILL.md"), "alpha", "Alpha skill", nil)
	writeSkill(t, filepath.Join(dir, "beta", "SKILL.md"), "beta", "Beta skill", nil)
	writeSkill(t, filepath.Join(dir, "gamma", "SKILL.md"), "gamma", "Gamma skill", nil)

	store := skills.NewStore()
	store.LoadFromDirs([]string{dir})
	if got := store.Count(); got != 3 {
		t.Fatalf("store loaded %d, want 3", got)
	}

	roleProvider := func() map[string]*common.AgentRole {
		return map[string]*common.AgentRole{
			"agent": {
				Name:   "agent",
				Skills: []string{"alpha", "beta"},
			},
		}
	}

	got := computeSkillScope(roleProvider, store, "agent")
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Errorf("scope = %v, want [alpha beta]", got)
	}
}

// TestComputeSkillScope_CapabilityAttached proves a skill with frontmatter
// `capabilities: [research]` is auto-attached to any role importing `research`.
func TestComputeSkillScope_CapabilityAttached(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "literature-review", "SKILL.md"), "literature-review",
		"Literature review skill", []string{"research"})
	writeSkill(t, filepath.Join(dir, "unrelated", "SKILL.md"), "unrelated",
		"Unrelated skill", []string{"browser"})

	store := skills.NewStore()
	store.LoadFromDirs([]string{dir})

	roleProvider := func() map[string]*common.AgentRole {
		return map[string]*common.AgentRole{
			"researcher": {
				Name:    "researcher",
				Imports: []string{"research"},
			},
		}
	}

	got := computeSkillScope(roleProvider, store, "researcher")
	if len(got) != 1 || got[0] != "literature-review" {
		t.Errorf("scope = %v, want [literature-review]", got)
	}
}

// TestComputeSkillScope_NoSkillsReturnsNil proves a role with neither explicit
// Skills nor matching capability-attached skills gets nil — caller treats nil
// as "no read_skill for this sub-agent."
func TestComputeSkillScope_NoSkillsReturnsNil(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "x", "SKILL.md"), "x", "X", nil)
	store := skills.NewStore()
	store.LoadFromDirs([]string{dir})

	roleProvider := func() map[string]*common.AgentRole {
		return map[string]*common.AgentRole{
			"plain": {Name: "plain"},
		}
	}
	if got := computeSkillScope(roleProvider, store, "plain"); got != nil {
		t.Errorf("scope = %v, want nil", got)
	}
}

// TestComputeSkillScope_UnknownExplicitSkillSilentlyDropped proves a typo in
// RoleConfig.Skills doesn't grant phantom access — the missing name is filtered
// out by the store.Get(name) check.
func TestComputeSkillScope_UnknownExplicitSkillSilentlyDropped(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "real", "SKILL.md"), "real", "Real", nil)
	store := skills.NewStore()
	store.LoadFromDirs([]string{dir})

	roleProvider := func() map[string]*common.AgentRole {
		return map[string]*common.AgentRole{
			"agent": {Name: "agent", Skills: []string{"real", "typo"}},
		}
	}
	got := computeSkillScope(roleProvider, store, "agent")
	if len(got) != 1 || got[0] != "real" {
		t.Errorf("scope = %v, want [real]", got)
	}
}

// TestSkillsExecutor_ReadSkillHitsScopedStore proves the wrapper dispatches
// read_skill to the scoped store, not the parent executor.
func TestSkillsExecutor_ReadSkillHitsScopedStore(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "alpha", "SKILL.md"), "alpha", "Alpha skill body content here.", nil)
	store := skills.NewStore()
	store.LoadFromDirs([]string{dir})
	scoped := store.Visible([]string{"alpha"})

	parent := &stubExecutor{}
	exec := &skillsExecutor{parent: parent, store: scoped}

	out, err := exec.Execute(context.Background(), "read_skill", map[string]any{"name": "alpha"})
	if err != nil {
		t.Fatalf("read_skill: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("read_skill returned %T, want map[string]any", out)
	}
	body, _ := m["instructions"].(string)
	if body == "" {
		t.Errorf("read_skill returned empty instructions; want alpha skill body")
	}
	for _, n := range parent.seen {
		if n == "read_skill" {
			t.Error("read_skill leaked to parent executor")
		}
	}
}

// TestSkillsExecutor_NonSkillToolsFallThrough proves other tools pass through
// unchanged.
func TestSkillsExecutor_NonSkillToolsFallThrough(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "alpha", "SKILL.md"), "alpha", "Alpha", nil)
	store := skills.NewStore()
	store.LoadFromDirs([]string{dir})

	parent := &stubExecutor{}
	exec := &skillsExecutor{parent: parent, store: store}

	if _, err := exec.Execute(context.Background(), "bash", map[string]any{"cmd": "ls"}); err != nil {
		t.Fatalf("bash: %v", err)
	}
	if len(parent.seen) != 1 || parent.seen[0] != "bash" {
		t.Errorf("parent saw %v, want [bash]", parent.seen)
	}
}

// writeSkill creates a canonical skill at <dir>/<name>/SKILL.md with optional
// capabilities frontmatter.
func writeSkill(t *testing.T, path, name, body string, capabilities []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	var sb strings.Builder
	sb.WriteString("---\nname: " + name + "\ndescription: " + body + "\n")
	if len(capabilities) > 0 {
		sb.WriteString("capabilities:\n")
		for _, c := range capabilities {
			sb.WriteString("  - " + c + "\n")
		}
	}
	sb.WriteString("---\n\n# " + name + "\n\n" + body + "\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
