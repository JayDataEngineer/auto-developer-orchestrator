package common

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// TestDeepResearchEngineHasNoPlatformOverfitting enforces the rule that the
// deep-research-engine org stays FORMAT-GENERAL. The org is designed to
// ingest any multimodal corpus, not just one platform's export format. If
// any skill doc / role prompt / MANIFESTO mentions a specific platform by
// name, the org has been overfit to that platform and needs generalizing.
//
// Scope: only .md files in MANIFESTO + roles/ + skills/ + capabilities/.
// Parser script names in org.toml init_files (e.g. `sandbox/telegram_parser.py`)
// are allowed — each parser does one format-specific job, and the parser's
// own docstring is the right home for format-specific knowledge.
//
// "signal" is intentionally NOT in the forbidden list — it's a real ML term
// (signal-to-noise, cross-modal signal) and banning it produces false positives.
//
// Background: shipped 2026-06-24 after a stress-test on one platform's export
// leaked platform-specific paths, sender-name quirks, and hardcoded corpus
// counts into the skill docs. The org's value proposition is generality; this
// test keeps it honest.
func TestDeepResearchEngineHasNoPlatformOverfitting(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found relative to test file")
	}
	dreRoot := filepath.Join(orgsDir, "deep-research-engine")
	if _, err := os.Stat(dreRoot); err != nil {
		t.Skip("deep-research-engine org not present")
	}

	// Platform names that indicate overfitting if they appear as words in
	// skill docs. "signal" is excluded — too common as an ML term.
	// Word-boundary matching means `telegram_parser.py` filename references
	// would still match, but we only scan .md files (which don't reference
	// parser scripts by name).
	forbiddenTokens := []string{
		"telegram",
		"whatsapp",
		"discord",
		"slack",
	}

	// Only .md files define the org's reusable contract.
	checkPaths := []string{
		filepath.Join(dreRoot, "MANIFESTO.md"),
	}
	for _, sub := range []string{"roles", "skills", "capabilities"} {
		p := filepath.Join(dreRoot, sub)
		if _, err := os.Stat(p); err == nil {
			checkPaths = append(checkPaths, p)
		}
	}

	var failures []string
	for _, p := range checkPaths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			filepath.WalkDir(p, func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil || d.IsDir() {
					return nil
				}
				if !strings.HasSuffix(path, ".md") {
					return nil
				}
				checkPlatformTokens(t, path, forbiddenTokens, &failures)
				return nil
			})
		} else {
			checkPlatformTokens(t, p, forbiddenTokens, &failures)
		}
	}

	if len(failures) > 0 {
		t.Errorf("deep-research-engine has platform overfitting — skills must stay format-general:\n  %s",
			strings.Join(failures, "\n  "))
	}
}

func checkPlatformTokens(t *testing.T, path string, tokens []string, failures *[]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	body := strings.ToLower(string(data))
	for _, tok := range tokens {
		// Word-boundary match: "telegram" matches as a word, not as a substring of "telegram_parser".
		// (Underscore is a word char in Go regex, so we have to exclude _ explicitly.)
		if hasPlatformWord(body, tok) {
			rel, _ := filepath.Rel(filepath.Dir(filepath.Dir(filepath.Dir(path))), path)
			*failures = append(*failures,
				fmt.Sprintf("%s: contains %q (move format-specific knowledge into the parser script's docstring)", rel, tok))
		}
	}
}

// hasPlatformWord reports whether body contains token as a word delimited by
// non-word, non-underscore characters. This catches "Telegram exports" but
// not "telegram_parser.py".
func hasPlatformWord(body, token string) bool {
	idx := 0
	for {
		i := strings.Index(body[idx:], token)
		if i < 0 {
			return false
		}
		pos := idx + i
		idx = pos + len(token)
		// Check the chars immediately before + after.
		before := byte(' ')
		if pos > 0 {
			before = body[pos-1]
		}
		after := byte(' ')
		if idx < len(body) {
			after = body[idx]
		}
		isBoundary := func(c byte) bool {
			return !(c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'))
		}
		if isBoundary(before) && isBoundary(after) {
			return true
		}
	}
}

// --------------------------------------------------------------------------- //
// Phase A §A1: sandbox.tier contract — 0-drift forcing function              //
// --------------------------------------------------------------------------- //
// Every org with a [sandbox] block must declare a tier. Three tiers:
//   - standard     — stock pux-sandbox image + gVisor + full kernel isolation
//   - custom-build — custom Dockerfile, explicitly opted out of standard
//   - skeleton     — no sandbox, config-only org (implicit when no [sandbox])
//
// Python validator (scripts/org_build.py validate_org_data) hard-fails at
// render time. These Go tests catch the same drift against the checked-in
// pux.yaml files so a forgotten org-build run can't sneak through.
//
// Marked xfail-style: real orgs are not yet migrated (PR2). For now, each test
// reports violations as warnings rather than failing the build, so PR1 ships
// the forcing function without blocking on migration.

// TestOrgSandboxTierContract walks every org in orgs/ and verifies:
//   - every [sandbox] block declares a tier
//   - the tier is one of {standard, custom-build, skeleton}
//
// This is the Go-side mirror of scripts/org_build.py's tier validator. It
// catches drift introduced by hand-editing pux.yaml without re-running
// `task org-build`.
func TestOrgSandboxTierContract(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found")
	}
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", orgsDir, err)
	}

	type violation struct {
		orgName string
		tier    string
		reason  string
	}
	var violations []violation
	orgCount := 0

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		orgCount++
		puxPath := filepath.Join(orgsDir, entry.Name(), "pux.yaml")
		if _, err := os.Stat(puxPath); err != nil {
			continue // no pux.yaml — TestOrgsDirectoryAudit already covers this
		}
		org := LoadOrgManifest(filepath.Join(orgsDir, entry.Name()))
		if org == nil {
			continue // LoadOrgManifest failure is covered by auditOrg
		}

		// Read the raw pux.yaml to detect presence of a sandbox block.
		// SandboxTier() returns "" for both "no block" and "block with no tier",
		// so we need to distinguish via direct file inspection.
		raw, err := os.ReadFile(puxPath)
		if err != nil {
			continue
		}
		rawStr := string(raw)
		hasSandboxBlock := strings.Contains(rawStr, "\nsandbox:") ||
			strings.HasPrefix(rawStr, "sandbox:")
		if !hasSandboxBlock {
			continue // skeleton tier (implicit, by absence)
		}

		tier := org.SandboxTier()
		if tier == "" {
			violations = append(violations, violation{
				orgName: entry.Name(),
				reason:  "[sandbox] block present but tier field missing",
			})
			continue
		}
		if !IsValidSandboxTier(tier) {
			violations = append(violations, violation{
				orgName: entry.Name(),
				tier:    tier,
				reason:  fmt.Sprintf("tier=%q not in %v", tier, ValidSandboxTiers()),
			})
		}
	}

	if orgCount == 0 {
		t.Skip("no orgs found to audit")
	}

	// PR2: hard-fail. PR1 shipped this as a warning; migration is now complete
	// across all 9 orgs (tier declared in every org.toml that has [sandbox]).
	if len(violations) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d/%d orgs violate sandbox.tier contract:\n",
			len(violations), orgCount)
		for _, v := range violations {
			if v.tier != "" {
				fmt.Fprintf(&b, "  - %s: %s (tier=%q)\n", v.orgName, v.reason, v.tier)
			} else {
				fmt.Fprintf(&b, "  - %s: %s\n", v.orgName, v.reason)
			}
		}
		t.Errorf("DRIFT:\n%s", b.String())
	}
}

// TestOrgBootstrapAtRoot enforces the canonical bootstrap.sh location.
// Standard + custom-build tiers must ship bootstrap.sh at the org root
// (orgs/<name>/bootstrap.sh), NOT at scripts/bootstrap.sh. The 4 verified
// bootstraps (deep-research-engine, social-media-pipeline, tech-noir,
// telegram-agent) all live at the root; drift to scripts/ breaks the
// documented `./bootstrap.sh` invocation pattern.
//
// PR1: reports violations as warnings. PR2 will flip to t.Errorf once
// twitter-agent's bootstrap is moved from scripts/ to org root.
func TestOrgBootstrapAtRoot(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found")
	}
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", orgsDir, err)
	}

	var drifted []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		orgPath := filepath.Join(orgsDir, entry.Name())
		rootBootstrap := filepath.Join(orgPath, "bootstrap.sh")
		scriptsBootstrap := filepath.Join(orgPath, "scripts", "bootstrap.sh")

		_, rootExists := os.Stat(rootBootstrap)
		_, scriptsExists := os.Stat(scriptsBootstrap)

		if scriptsExists == nil && rootExists != nil {
			drifted = append(drifted, entry.Name()+" (only at scripts/bootstrap.sh)")
		}
	}

	if len(drifted) > 0 {
		t.Errorf(
			"DRIFT — bootstrap.sh must live at org root, not scripts/:\n  - %s",
			strings.Join(drifted, "\n  - "),
		)
	}
}

// TestOrgBootstrapOpenshellProjectPath enforces that every CONTAINER-SPAWNING
// bootstrap.sh exports OPENSHELL_PROJECT_PATH so the kernel can adopt the
// compose-spawned container via discoverByProjectLabel. Without this export,
// Pux silently spins up a sibling container instead of adopting the
// bootstrap's container. See feedback_container_reuse_label_discovery.md.
//
// Only applies to bootstraps that actually spawn containers (detected via
// `docker compose up` or `docker run` in the body). Host-side helpers like
// twitter-agent's venv+cookie bootstrap are exempt — they don't start a
// container, so the label contract doesn't apply.
func TestOrgBootstrapOpenshellProjectPath(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found")
	}
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", orgsDir, err)
	}

	var drifted []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		orgPath := filepath.Join(orgsDir, entry.Name())
		rootBootstrap := filepath.Join(orgPath, "bootstrap.sh")
		scriptsBootstrap := filepath.Join(orgPath, "scripts", "bootstrap.sh")

		var body string
		if data, err := os.ReadFile(rootBootstrap); err == nil {
			body = string(data)
		} else if data, err := os.ReadFile(scriptsBootstrap); err == nil {
			body = string(data)
		} else {
			continue // no bootstrap at all — TestOrgBootstrapAtRoot handles
		}

		// Skip host-side helpers — they don't spawn containers, so the label
		// contract doesn't apply. Detected via absence of docker compose/run.
		isContainerBootstrap := strings.Contains(body, "docker compose up") ||
			strings.Contains(body, "docker-compose up") ||
			strings.Contains(body, "compose up -d") ||
			strings.Contains(body, "docker run")
		if !isContainerBootstrap {
			continue
		}

		// OPENSHELL_PROJECT_PATH must be exported. Match either
		// `export OPENSHELL_PROJECT_PATH=` or `OPENSHELL_PROJECT_PATH=...` +
		// `export OPENSHELL_PROJECT_PATH`.
		if !strings.Contains(body, "OPENSHELL_PROJECT_PATH") {
			drifted = append(drifted, entry.Name())
		}
	}

	if len(drifted) > 0 {
		t.Errorf(
			"OPENSHELL_PROJECT_PATH export missing from these container-spawning bootstraps (kernel cannot adopt container by label):\n  - %s",
			strings.Join(drifted, "\n  - "),
		)
	}
}

// TestOrgSharedPathStyle enforces the canonical @shared/ path style.
// Bare `@shared/foo.py` is legacy and rejected; only explicit subdirs
// (`@shared/clients/foo.py`, `@shared/sandbox/foo.py`) are accepted.
// The resolver handles both today, but drift is invisible until someone
// renames a client file and the bare path silently 404s in a different org.
func TestOrgSharedPathStyle(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found")
	}
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", orgsDir, err)
	}

	allowedSharedSubdirs := []string{"clients/", "sandbox/"}

	type violation struct {
		orgName string
		path    string
	}
	var violations []violation

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		orgPath := filepath.Join(orgsDir, entry.Name())
		// Walk every .py, .md, .yaml, .toml, .sh file under the org.
		_ = filepath.Walk(orgPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".py" && ext != ".md" && ext != ".yaml" && ext != ".yml" && ext != ".toml" && ext != ".sh" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			body := string(data)
			relPath, _ := filepath.Rel(orgPath, path)

			// Find every @shared/ reference and check its style.
			idx := strings.Index(body, "@shared/")
			for idx >= 0 {
				start := idx + len("@shared/")
				rest := body[start:]
				// Determine the first path segment after @shared/
				end := 0
				for end < len(rest) && rest[end] != '"' && rest[end] != '\'' && rest[end] != ' ' && rest[end] != '\n' && rest[end] != '\t' {
					end++
				}
				firstSeg := rest[:end]
				if !strings.Contains(firstSeg, "/") {
					// Bare path — no subdir
					violations = append(violations, violation{
						orgName: entry.Name(),
						path:    relPath + ": " + body[idx:idx+len("@shared/")+len(firstSeg)],
					})
				} else {
					// Has a subdir — verify it's in the allowlist
					subdir := firstSeg[:strings.Index(firstSeg, "/")+1]
					if !slices.Contains(allowedSharedSubdirs, subdir) {
						violations = append(violations, violation{
							orgName: entry.Name(),
							path:    relPath + ": " + body[idx:idx+len("@shared/")+len(firstSeg)],
						})
					}
				}
				nextIdx := strings.Index(body[idx+1:], "@shared/")
				if nextIdx < 0 {
					break
				}
				idx = idx + 1 + nextIdx
			}
			return nil
		})
	}

	// PR2: hard-fail. PR1 shipped this as a warning; @shared/ paths are
	// migrated to the canonical @shared/{clients,sandbox}/ form across all
	// 9 orgs.
	if len(violations) > 0 {
		byOrg := map[string][]string{}
		for _, v := range violations {
			byOrg[v.orgName] = append(byOrg[v.orgName], v.path)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d org(s) use legacy bare @shared/foo.py paths:\n",
			len(byOrg))
		for orgName, paths := range byOrg {
			fmt.Fprintf(&b, "  %s:\n", orgName)
			for _, p := range paths {
				fmt.Fprintf(&b, "    - %s\n", p)
			}
		}
		fmt.Fprintf(&b, "  Allowed subdirs: %v\n", allowedSharedSubdirs)
		t.Errorf("DRIFT:\n%s", b.String())
	}
}

// TestOrgSkeletonTierHasNoSandbox enforces the mutual exclusivity of
// tier = "skeleton" and a non-empty [sandbox] body. Skeleton orgs are
// config-only (no container, no init_files, no volumes). The Python validator
// hard-rejects this; the Go mirror catches the same drift against checked-in
// pux.yaml.
func TestOrgSkeletonTierHasNoSandbox(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found")
	}
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", orgsDir, err)
	}

	var violated []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		orgPath := filepath.Join(orgsDir, entry.Name())
		org := LoadOrgManifest(orgPath)
		if org == nil {
			continue
		}
		if org.SandboxTier() != SandboxTierSkeleton {
			continue
		}
		// Skeleton tier declared — sandbox body must be empty.
		// SandboxMode() defaults to "contained" so we can't use it to detect
		// a real sandbox body (an empty block also returns contained). The
		// unambiguous markers are image, init_files, volumes, env.
		if org.SandboxImage() != "" ||
			len(org.SandboxInitFiles()) > 0 ||
			len(org.SandboxVolumes()) > 0 ||
			len(org.SandboxEnv()) > 0 {
			violated = append(violated, entry.Name())
		}
	}

	if len(violated) > 0 {
		t.Errorf(
			"skeleton-tier orgs must not carry a [sandbox] body — these do:\n  - %s",
			strings.Join(violated, "\n  - "),
		)
	}
}
