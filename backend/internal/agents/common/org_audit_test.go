package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrgsDirectoryAudit walks the consolidated orgs/ directory and asserts
// every org loads cleanly. Catches drift the moment someone adds a new org
// or breaks an existing one.
//
// Layout contract (canonical, post-2026-06-18 reorg):
//   orgs/<name>/
//     pux.yaml                 # required
//     MANIFESTO.md             # required if `manifesto:` references it
//     roles/<role>/config.yaml # required if `staff_root: roles`
//     roles/<role>/prompt.md   # required if `staff_root: roles`
//     tool_packages/           # required if `tool_packages_root:`
//     skills/                  # required if `skills_dir:`
//
// The kernel reads only: name, description, manifesto, staff_root,
// tool_packages_root, extensions_dir, skills_dir, schedules, databases.
// Other fields (tools, requires, env, sandbox, prompts, commands) are
// vestigial — keep them out of new orgs.
func TestOrgsDirectoryAudit(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found relative to test file")
	}

	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", orgsDir, err)
	}

	if len(entries) < 7 {
		t.Errorf("expected at least 7 orgs (deep-research-engine, dev-bot, general, invest, tech-noir, telegram-agent, twitter-agent, video-production), got %d", len(entries))
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			auditOrg(t, filepath.Join(orgsDir, name), name)
		})
	}
}

func auditOrg(t *testing.T, orgPath, name string) {
	t.Helper()

	org := LoadOrgManifest(orgPath)
	if org == nil {
		t.Fatalf("%s: pux.yaml missing or invalid (LoadOrgManifest returned nil)", name)
	}

	// Name should match the directory (cosmetic but reduces confusion).
	// Allow `general` to have any name since it's a sandbox org.
	if name != "general" && org.Name != name {
		t.Errorf("%s: pux.yaml name=%q does not match dir name %q", name, org.Name, name)
	}

	if org.Description == "" {
		t.Errorf("%s: description is empty", name)
	}

	// Manifesto
	if org.Manifesto == "" && name != "general" {
		t.Errorf("%s: no manifesto field set (general is exempt)", name)
	}
	if org.Manifesto != "" {
		if org.ManifestoContent() == "" {
			t.Errorf("%s: manifesto %q referenced but unreadable or empty", name, org.Manifesto)
		}
	}

	// Roles
	if org.StaffRoot != "" {
		rolesDir := org.RolesDir()
		if _, err := os.Stat(rolesDir); err != nil {
			t.Fatalf("%s: staff_root=%q but roles dir %s does not exist", name, org.StaffRoot, rolesDir)
		}
		roles := LoadAgentRolesFrom(rolesDir)
		if len(roles) == 0 {
			t.Fatalf("%s: staff_root=%q but LoadAgentRolesFrom returned 0 roles", name, org.StaffRoot)
		}
		for roleName, role := range roles {
			if role.Description == "" {
				t.Errorf("%s/roles/%s: description is empty", name, roleName)
			}
			if role.Prompt == "" {
				t.Errorf("%s/roles/%s: prompt.md is empty or missing", name, roleName)
			}
			if role.MaxRounds == 0 {
				t.Errorf("%s/roles/%s: max_rounds is 0 (will default to 15 but should be explicit)", name, roleName)
			}
		}
		t.Logf("%s: %d role(s) loaded: %s", name, len(roles), sortedKeys(roles))
	} else if name != "general" {
		t.Errorf("%s: no staff_root set (general is exempt)", name)
	}

	// Tool packages
	if org.ToolPkgsRoot != "" {
		pkgsDir := org.ToolPkgsDir()
		if _, err := os.Stat(pkgsDir); err != nil {
			t.Errorf("%s: tool_packages_root=%q but dir %s does not exist", name, org.ToolPkgsRoot, pkgsDir)
		}
	}

	// Skills
	if org.SkillsDir != "" {
		skillsDir := org.SkillsDirPath()
		if _, err := os.Stat(skillsDir); err != nil {
			t.Errorf("%s: skills_dir=%q but dir %s does not exist", name, org.SkillsDir, skillsDir)
		}
	}

	// Schedules — if any are enabled, role must exist in staff_root
	if len(org.Schedules) > 0 && org.StaffRoot != "" {
		roles := LoadAgentRolesFrom(org.RolesDir())
		for _, sched := range org.Schedules {
			if sched.Role == "" {
				t.Errorf("%s/schedules/%s: no role assigned", name, sched.Name)
				continue
			}
			if _, ok := roles[sched.Role]; !ok {
				t.Errorf("%s/schedules/%s: role %q not in roles/ (%v)", name, sched.Name, sched.Role, sortedKeys(roles))
			}
			if sched.Cron == "" {
				t.Errorf("%s/schedules/%s: cron is empty", name, sched.Name)
			}
		}
	}
}

// findOrgsDir walks up from the test file to locate orgs/.
// Layout: backend/internal/agents/common/org_audit_test.go
//
//	../../../..  → repo root
//	+ orgs/
func findOrgsDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := cwd
	for range 10 {
		candidate := filepath.Join(dir, "orgs")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			// Confirm by looking for pux.yaml files inside
			entries, _ := os.ReadDir(candidate)
			for _, e := range entries {
				if e.IsDir() {
					if _, err := os.Stat(filepath.Join(candidate, e.Name(), "pux.yaml")); err == nil {
						return candidate
					}
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func sortedKeys(m map[string]*AgentRole) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple sort
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return strings.Join(keys, ", ")
}
