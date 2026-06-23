package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/skills"
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
		// Skip underscore-prefixed dirs — they're shared resource pools
		// (orgs/_shared/clients/, etc.), not orgs.
		if strings.HasPrefix(name, "_") {
			continue
		}
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
		} else {
			// P0 fix #4: load the skills dir through the real loader and
			// assert that every .md candidate actually registers. Before this
			// audit, 6 of 7 orgs had skills/ dirs full of UPPER_CASE.md files
			// that silently dropped at parse time. Now any drop fails the test.
			auditOrgSkills(t, name, skillsDir)
		}
	}

	// Cross-field invariants on the manifest.
	for _, msg := range org.Validate() {
		t.Errorf("%s: invalid manifest: %s", name, msg)
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

// TestFindSharedClientsDir verifies the @shared/ resolver can locate
// orgs/_shared/clients/ from this repo. If this fails, every org that
// references "@shared/foo.py" in init_files will fail to upload that file.
func TestFindSharedClientsDir(t *testing.T) {
	dir := FindSharedClientsDir()
	if dir == "" {
		t.Fatal("FindSharedClientsDir returned empty — orgs/_shared/clients/ not found. PROJECT_ROOT must point at repo root.")
	}

	// Canonical files must exist in the resolved dir.
	mustExist := []string{
		"surreal_client.py",
		"forge_client.py",
	}
	for _, name := range mustExist {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist in _shared/clients/: %v", name, err)
		}
	}
}

// TestSharedClientCanonical verifies the canonical surreal_client.py in
// _shared/ carries the hyphen-in-NS/DB fix. Catches drift if someone copies
// an older broken version over the shared one.
func TestSharedClientCanonical(t *testing.T) {
	dir := FindSharedClientsDir()
	if dir == "" {
		t.Skip("shared clients dir not found")
	}
	data, err := os.ReadFile(filepath.Join(dir, "surreal_client.py"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)

	// The hyphen-bug fix uses backtick-escaped identifiers.
	if !strings.Contains(body, "DEFINE NAMESPACE IF NOT EXISTS `{client.ns}`") {
		t.Error("canonical surreal_client.py lost the backtick-escape fix for hyphenated NS/DB names")
	}
	// The IndexError guard for parents[2] when running from /sandbox/.
	if !strings.Contains(body, "except IndexError") {
		t.Error("canonical surreal_client.py lost the IndexError guard for _load_schema_sql()")
	}
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

// auditOrgSkills loads the org's skills dir through the production skills
// loader and fails the test if any candidate file is dropped. Catches the
// silent-failure class of bugs: malformed frontmatter, missing description,
// duplicate names, etc. Before this audit, 6/7 orgs loaded zero skills and
// nothing complained.
//
// Reports with zero walked files are allowed — an empty skills/ dir is
// legitimate (some orgs declare skills_dir for future use). The failure
// signal is "we walked files but loaded fewer than we walked."
func auditOrgSkills(t *testing.T, orgName, skillsDir string) {
	t.Helper()
	store := skills.NewStore()
	store.LoadFromDirs([]string{skillsDir})
	for _, r := range store.Reports() {
		if r.Walked == 0 {
			continue
		}
		if r.Loaded == r.Walked {
			t.Logf("%s/skills: %d skill(s) loaded cleanly", orgName, r.Loaded)
			continue
		}
		t.Errorf("%s/skills: %s\n", orgName, r.Summary())
		for _, reason := range r.Skipped {
			t.Errorf("  %s", reason)
		}
	}
}
