package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
	"gopkg.in/yaml.v3"
)

func TestValidateSkillName(t *testing.T) {
	tests := []struct {
		in     string
		wantOK bool
	}{
		{"my-skill", true},
		{"abc123", true},
		{"a", true},
		{"multi-word-name", true},
		{"", false},
		{"My-Skill", false},
		{"my_skill", false},
		{"-leading", false},
		{"trailing-", false},
		{"has space", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := validateSkillName(tc.in, 64)
			if (got != "") != tc.wantOK {
				t.Errorf("validateSkillName(%q) = %q, want ok=%v", tc.in, got, tc.wantOK)
			}
		})
	}
}

func TestValidateSkillName_TooLong(t *testing.T) {
	long := strings.Repeat("a", 65)
	if got := validateSkillName(long, 64); got != "" {
		t.Errorf("expected empty for 65-char name, got %q", got)
	}
	atLimit := strings.Repeat("a", 64)
	if got := validateSkillName(atLimit, 64); got == "" {
		t.Errorf("expected non-empty for 64-char name (at limit)")
	}
}

// TestStemToKebab covers the filename-stem normalization that powers flat-layout
// auto-discovery. CONTEXT_ENGINE_QUERY.md → context-engine-query is the
// canonical regression case (it was the silent-drop victim).
func TestStemToKebab(t *testing.T) {
	cases := map[string]string{
		"CONTEXT_ENGINE_QUERY": "context-engine-query",
		"SEC_FILINGS":           "sec-filings",
		"social-captcha":        "social-captcha",
		"multi__underscore":     "multi-underscore",
		"trailing_":             "trailing",
		"Mixed_Case.Name":       "mixed-case-name",
		"already-kebab":         "already-kebab",
	}
	for in, want := range cases {
		if got := stemToKebab(in); got != want {
			t.Errorf("stemToKebab(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeSkillFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestParseSkillFile_WithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	writeSkillFile(t, path, "---\nname: my-skill\ndescription: A test skill\nversion: 1.2.0\ncapabilities: [research]\n---\n# Instructions\nDo the thing.")

	skill, skipReason, err := parseSkillFile(path, true, false)
	testutil.AssertNoError(t, err)
	if skipReason != "" {
		t.Fatalf("unexpected skip: %s", skipReason)
	}
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	testutil.AssertEqual(t, skill.Name, "my-skill")
	testutil.AssertEqual(t, skill.Description, "A test skill")
	testutil.AssertEqual(t, skill.Location, path)
	testutil.AssertEqual(t, skill.Dir, dir)
	testutil.AssertEqual(t, skill.Version, "1.2.0")
	testutil.AssertEqual(t, len(skill.Capabilities), 1)
	testutil.AssertEqual(t, skill.Capabilities[0], "research")
}

// TestParseSkillFile_NameFallsBackToDir proves canonical layout still works:
// SKILL.md without frontmatter name inherits the parent dir name.
func TestParseSkillFile_NameFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	namedDir := filepath.Join(dir, "auto-named")
	path := filepath.Join(namedDir, "SKILL.md")
	writeSkillFile(t, path, "---\ndescription: skill with no name\n---\nbody")

	skill, _, err := parseSkillFile(path, true, false)
	testutil.AssertNoError(t, err)
	testutil.AssertEqual(t, skill.Name, "auto-named")
}

// TestParseSkillFile_FlatStemFallback is the P0 fix: CONTEXT_ENGINE_QUERY.md
// at top level now derives name "context-engine-query" instead of "skills".
// Before the fix, every flat file fell back to parent dir name ("skills")
// and the duplicate check silently dropped everything but the first.
func TestParseSkillFile_FlatStemFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CONTEXT_ENGINE_QUERY.md")
	writeSkillFile(t, path, "# CONTEXT_ENGINE_QUERY\n\nRead this before research.")

	skill, _, err := parseSkillFile(path, false, true)
	testutil.AssertNoError(t, err)
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	testutil.AssertEqual(t, skill.Name, "context-engine-query")
}

// TestParseSkillFile_DescriptionFromFirstParagraph proves auto-stub derives
// a description when frontmatter is absent. The first paragraph after the
// H1 is used so titles like "# CONTEXT_ENGINE_QUERY" don't get treated as
// the description.
func TestParseSkillFile_DescriptionFromFirstParagraph(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AUTO_STUB.md")
	writeSkillFile(t, path, "# AUTO_STUB\n\n**Read this.** The world is not ephemeral.\n\n## Section\nMore stuff.")

	skill, _, err := parseSkillFile(path, false, true)
	testutil.AssertNoError(t, err)
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	testutil.AssertEqual(t, skill.Name, "auto-stub")
	// Bold markers stripped; whole first paragraph joined.
	if !strings.Contains(skill.Description, "Read this.") {
		t.Errorf("description missing first paragraph: %q", skill.Description)
	}
	if !strings.Contains(skill.Description, "not ephemeral") {
		t.Errorf("description missing continuation: %q", skill.Description)
	}
	if strings.Contains(skill.Description, "**") {
		t.Errorf("description should have markdown stripped: %q", skill.Description)
	}
	if strings.Contains(skill.Description, "Section") {
		t.Errorf("description should not bleed into H2: %q", skill.Description)
	}
}

// TestParseSkillFile_YAMLv3NestedFields proves the parser migrated from the
// line-by-line hand-roller to gopkg.in/yaml.v3. Lists, nested maps, and
// quoted strings all parse without bespoke logic.
func TestParseSkillFile_YAMLv3NestedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	// yaml.v3 handles quoted values and list syntax that broke the old parser.
	writeSkillFile(t, path, "---\nname: nested\ndescription: \"Has: colons, and 'quotes'\"\ncapabilities:\n  - research\n  - vision\nversion: \"2.0.0\"\n---\nbody")

	skill, _, err := parseSkillFile(path, true, false)
	testutil.AssertNoError(t, err)
	testutil.AssertEqual(t, skill.Name, "nested")
	if !strings.Contains(skill.Description, "colons") {
		t.Errorf("description with colon should survive yaml.v3: %q", skill.Description)
	}
	testutil.AssertEqual(t, len(skill.Capabilities), 2)
	testutil.AssertEqual(t, skill.Version, "2.0.0")
}

func TestParseSkillFile_MissingDescriptionErrors(t *testing.T) {
	dir := t.TempDir()
	// No H1, no paragraph — nothing to derive description from. The parser
	// returns the skill as nil with a skipReason rather than a hard error
	// so the discoverer can log it as a "skipped" instead of a parse failure.
	path := filepath.Join(dir, "SKILL.md")
	writeSkillFile(t, path, "---\nname: no-desc\n---\n\n\n")

	skill, skipReason, err := parseSkillFile(path, true, false)
	testutil.AssertNoError(t, err)
	if skill != nil {
		t.Fatal("expected nil skill when description cannot be derived")
	}
	if skipReason == "" {
		t.Fatal("expected non-empty skipReason when description cannot be derived")
	}
	if !strings.Contains(skipReason, "description") {
		t.Errorf("skipReason should mention description: %q", skipReason)
	}
}

func TestParseSkillFile_InvalidNameErrors(t *testing.T) {
	dir := t.TempDir()
	// Dir name has uppercase so canonical fallback fails too.
	path := filepath.Join(dir, "BadName", "SKILL.md")
	writeSkillFile(t, path, "---\ndescription: desc\n---\nbody")

	_, _, err := parseSkillFile(path, true, false)
	if err == nil {
		t.Fatal("expected error when name is invalid and dir fallback fails")
	}
}

func TestParseSkillFile_DisableInvocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	writeSkillFile(t, path, "---\nname: hidden\ndescription: x\ndisable-model-invocation: true\n---\nbody")

	skill, _, err := parseSkillFile(path, true, false)
	testutil.AssertNoError(t, err)
	if !skill.DisableInvocation {
		t.Error("expected DisableInvocation=true")
	}
}

func TestParseSkillFile_LongDescriptionTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	long := strings.Repeat("a", 2000)
	writeSkillFile(t, path, "---\nname: big\ndescription: "+long+"\n---\nbody")

	skill, _, err := parseSkillFile(path, true, false)
	testutil.AssertNoError(t, err)
	if len(skill.Description) > 1024 {
		t.Errorf("description len = %d, want <= 1024", len(skill.Description))
	}
	if !strings.HasSuffix(skill.Description, "...") {
		t.Errorf("expected truncated description to end with ...")
	}
}

func TestStore_LoadFromDirs(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: first\n---\nA")
	writeSkillFile(t, filepath.Join(root, "nested", "beta", "SKILL.md"), "---\nname: beta\ndescription: second\n---\nB")

	s := NewStore()
	count := s.LoadFromDirs([]string{root})
	if count != 2 {
		t.Fatalf("LoadFromDirs count = %d, want 2", count)
	}
	if s.Count() != 2 {
		t.Errorf("Count = %d, want 2", s.Count())
	}
	if s.Get("alpha") == nil {
		t.Error("expected to find alpha")
	}
	if s.Get("missing") != nil {
		t.Error("expected nil for missing skill")
	}
	all := s.All()
	if len(all) != 2 {
		t.Errorf("All() len = %d, want 2", len(all))
	}
}

// TestStore_LoadFromDirs_FlatLayout loads the actual invest-like layout
// (13 UPPER_CASE.md files at top level) and proves they ALL load. This is
// the regression test for the "6/7 orgs load zero skills" bug.
func TestStore_LoadFromDirs_FlatLayout(t *testing.T) {
	root := t.TempDir()
	names := []string{
		"CONTEXT_ENGINE_QUERY", "CRYPTO_ONCHAIN", "FUNDAMENTAL_ANALYSIS",
		"JOURNAL_PREDICTIONS", "MACRO_ANALYSIS", "MARKET_REGIME",
		"MULTI_ASSET_FUSION", "NEWS_SENTIMENT", "OPTIONS_FLOW",
		"RISK_MANAGEMENT", "SEC_FILINGS", "SOCIAL_SENTIMENT", "TECHNICAL_ANALYSIS",
	}
	for _, n := range names {
		writeSkillFile(t, filepath.Join(root, n+".md"), "# "+n+"\n\nDoes a thing.\n")
	}
	s := NewStore()
	count := s.LoadFromDirs([]string{root})
	if count != len(names) {
		t.Fatalf("expected %d flat skills loaded, got %d (reports: %+v)", len(names), count, s.Reports())
	}
	// Each should be kebab-cased and registered under byName.
	for _, n := range names {
		kebab := stemToKebab(n)
		if s.Get(kebab) == nil {
			t.Errorf("expected %q in store", kebab)
		}
	}
	// No silent skips — every file should have loaded.
	if skipped := s.SkippedCount(); skipped != 0 {
		t.Errorf("expected 0 skips, got %d: %+v", skipped, s.Reports())
	}
}

// TestStore_LoadFromDirs_ReportsSkips is the P0 fix #2+#3: every dropped
// file is surfaced with a reason, and the report summary reflects the gap.
func TestStore_LoadFromDirs_ReportsSkips(t *testing.T) {
	root := t.TempDir()
	// Good skill.
	writeSkillFile(t, filepath.Join(root, "good", "SKILL.md"), "---\nname: good\ndescription: ok\n---\nx")
	// Skill with no derivable description (empty body, no frontmatter).
	writeSkillFile(t, filepath.Join(root, "empty", "SKILL.md"), "")
	// Nested .md that isn't SKILL.md (should be skipped).
	writeSkillFile(t, filepath.Join(root, "doc", "README.md"), "# Read me")
	// Duplicate name.
	writeSkillFile(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: good\ndescription: dup\n---\nx")

	s := NewStore()
	s.LoadFromDirs([]string{root})
	reports := s.Reports()
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	r := reports[0]
	if r.Loaded != 1 {
		t.Errorf("Loaded = %d, want 1", r.Loaded)
	}
	if r.Walked <= r.Loaded {
		t.Errorf("Walked (%d) should exceed Loaded (%d)", r.Walked, r.Loaded)
	}
	if r.Summary() == "" {
		t.Error("Summary should be non-empty when files were skipped")
	}
	// Every skip reason should mention the file path.
	for _, reason := range r.Skipped {
		if !strings.Contains(reason, root) {
			t.Errorf("skip reason should reference file path: %q", reason)
		}
	}
}

func TestStore_LoadFromDirs_SkipsIgnoredDirs(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "good", "SKILL.md"), "---\nname: good\ndescription: ok\n---\nx")
	writeSkillFile(t, filepath.Join(root, "node_modules", "pkg", "SKILL.md"), "---\nname: pkg\ndescription: ignored\n---\nx")
	writeSkillFile(t, filepath.Join(root, ".hidden", "SKILL.md"), "---\nname: hid\ndescription: ignored\n---\nx")
	writeSkillFile(t, filepath.Join(root, "vendor", "lib", "SKILL.md"), "---\nname: lib\ndescription: ignored\n---\nx")

	s := NewStore()
	count := s.LoadFromDirs([]string{root})
	if count != 1 {
		t.Fatalf("LoadFromDirs count = %d, want 1 (only 'good')", count)
	}
	if s.Get("good") == nil {
		t.Error("expected 'good' loaded")
	}
	for _, skipped := range []string{"pkg", "hid", "lib"} {
		if s.Get(skipped) != nil {
			t.Errorf("expected %q to be skipped", skipped)
		}
	}
}

func TestStore_LoadFromDirs_DedupByName(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeSkillFile(t, filepath.Join(a, "SKILL.md"), "---\nname: dup\ndescription: first\n---\nx")
	writeSkillFile(t, filepath.Join(b, "SKILL.md"), "---\nname: dup\ndescription: second\n---\nx")

	s := NewStore()
	count := s.LoadFromDirs([]string{a, b})
	if count != 1 {
		t.Errorf("expected 1 after dedup, got %d", count)
	}
	// The dup must appear in the second report's Skipped list with a reason.
	reports := s.Reports()
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	foundReason := false
	for _, r := range reports {
		for _, reason := range r.Skipped {
			if strings.Contains(reason, "duplicate skill name") {
				foundReason = true
			}
		}
	}
	if !foundReason {
		t.Error("expected duplicate skip reason in reports")
	}
}

func TestStore_LoadFromDirs_NonexistentDir(t *testing.T) {
	s := NewStore()
	if count := s.LoadFromDirs([]string{"/does/not/exist"}); count != 0 {
		t.Errorf("expected 0 for nonexistent dir, got %d", count)
	}
	// Nonexistent dir is normal — should NOT appear as a skip.
	if s.SkippedCount() != 0 {
		t.Errorf("expected 0 skips for nonexistent dir, got %d", s.SkippedCount())
	}
}

func TestStore_FormatAvailableSkills_ExcludesDisabled(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "shown", "SKILL.md"), "---\nname: shown\ndescription: visible\n---\nx")
	writeSkillFile(t, filepath.Join(root, "hidden", "SKILL.md"), "---\nname: hidden\ndescription: invisible\ndisable-model-invocation: true\n---\nx")

	s := NewStore()
	s.LoadFromDirs([]string{root})

	out := s.FormatAvailableSkills()
	if !strings.Contains(out, "shown") {
		t.Errorf("expected 'shown' in output:\n%s", out)
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("did not expect disabled 'hidden' in output:\n%s", out)
	}
}

func TestStore_FormatAvailableSkills_EmptyReturnsEmpty(t *testing.T) {
	s := NewStore()
	if out := s.FormatAvailableSkills(); out != "" {
		t.Errorf("expected empty string for empty store, got %q", out)
	}
}

// TestStore_FormatAvailableSkills_EmitsVersion proves version: surfaces in
// the prompt block when frontmatter declares it.
func TestStore_FormatAvailableSkills_EmitsVersion(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "v", "SKILL.md"), "---\nname: v\ndescription: x\nversion: 0.2.0\n---\nx")
	s := NewStore()
	s.LoadFromDirs([]string{root})
	out := s.FormatAvailableSkills()
	if !strings.Contains(out, "0.2.0") {
		t.Errorf("expected version in output: %s", out)
	}
}

func TestStore_ReadSkill_StripsFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "alpha", "SKILL.md"),
		"---\nname: alpha\ndescription: x\n---\n# Real Body\nLine two")

	s := NewStore()
	s.LoadFromDirs([]string{root})

	got := s.ReadSkill("alpha")
	if !strings.Contains(got, "Real Body") {
		t.Errorf("expected body in ReadSkill output: %q", got)
	}
	if strings.Contains(got, "description") {
		t.Errorf("frontmatter should be stripped, got: %q", got)
	}
	if strings.Contains(got, "---") {
		t.Errorf("frontmatter markers should be stripped, got: %q", got)
	}
}

func TestStore_ReadSkill_UnknownReturnsEmpty(t *testing.T) {
	s := NewStore()
	if got := s.ReadSkill("nope"); got != "" {
		t.Errorf("expected empty for unknown skill, got %q", got)
	}
}

// TestStore_Visible proves the scope view (P2): only allowed skill names
// surface via Get/All/FormatAvailableSkills. Unknown allowed names are
// silently dropped (no phantom skills in the scoped view).
func TestStore_Visible(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: a\n---\nA")
	writeSkillFile(t, filepath.Join(root, "beta", "SKILL.md"), "---\nname: beta\ndescription: b\n---\nB")
	writeSkillFile(t, filepath.Join(root, "gamma", "SKILL.md"), "---\nname: gamma\ndescription: g\n---\nG")

	base := NewStore()
	base.LoadFromDirs([]string{root})
	if base.Count() != 3 {
		t.Fatalf("base count = %d, want 3", base.Count())
	}

	scoped := base.Visible([]string{"alpha", "gamma", "ghost"})
	if scoped.Count() != 2 {
		t.Errorf("scoped count = %d, want 2", scoped.Count())
	}
	if scoped.Get("alpha") == nil {
		t.Error("alpha should be visible")
	}
	if scoped.Get("beta") != nil {
		t.Error("beta should be filtered out")
	}
	if scoped.Get("ghost") != nil {
		t.Error("unknown allowed name should not appear")
	}
	out := scoped.FormatAvailableSkills()
	if strings.Contains(out, "beta") {
		t.Errorf("scoped format should exclude beta: %s", out)
	}
}

// TestStore_Visible_NilAllowlistReturnsBase proves empty allowlist is the
// unscoped identity view.
func TestStore_Visible_NilAllowlistReturnsBase(t *testing.T) {
	s := NewStore()
	if got := s.Visible(nil); got != s {
		t.Error("Visible(nil) must return the receiver unchanged")
	}
	if got := s.Visible([]string{}); got != s {
		t.Error("Visible([]) must return the receiver unchanged")
	}
}

// TestStore_ForCapability proves P2 capability-scoped attachment: a skill
// declaring `capabilities: [research]` is auto-granted to any worker whose
// imports include `research`.
func TestStore_ForCapability(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "skill-r", "SKILL.md"), "---\nname: skill-r\ndescription: r\ncapabilities: [research]\n---\nbody")
	writeSkillFile(t, filepath.Join(root, "skill-v", "SKILL.md"), "---\nname: skill-v\ndescription: v\ncapabilities:\n  - vision\n  - research\n---\nbody")
	writeSkillFile(t, filepath.Join(root, "skill-x", "SKILL.md"), "---\nname: skill-x\ndescription: x\n---\nbody")

	s := NewStore()
	s.LoadFromDirs([]string{root})

	r := s.ForCapability("research")
	if len(r) != 2 {
		t.Errorf("expected 2 skills for research capability, got %v", r)
	}
	v := s.ForCapability("vision")
	if len(v) != 1 || v[0] != "skill-v" {
		t.Errorf("expected only skill-v for vision, got %v", v)
	}
	x := s.ForCapability("nonexistent")
	if len(x) != 0 {
		t.Errorf("expected 0 skills for unknown capability, got %v", x)
	}
}

func TestStandardSkillDirs_StopsAtGitRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "a", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := StandardSkillDirs(deep, "")
	found := false
	for _, d := range dirs {
		if strings.Contains(d, deep) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dirs derived from %q, got %v", deep, dirs)
	}
}

// TestStore_HotReload verifies polling-based hot-reload picks up new files
// without restarting the process.
func TestStore_HotReload(t *testing.T) {
	root := t.TempDir()
	s := NewStore()
	s.LoadFromDirs([]string{root})
	if s.Count() != 0 {
		t.Fatalf("expected 0 skills initially, got %d", s.Count())
	}

	done := make(chan struct{})
	reloaded := make(chan struct{}, 4)
	s.WatchAndReload([]string{root}, 25*time.Millisecond, done, func(format string, args ...any) {
		if strings.Contains(format, "hot-reloaded") {
			select {
			case reloaded <- struct{}{}:
			default:
			}
		}
	})
	defer close(done)

	// Add a new skill file.
	writeSkillFile(t, filepath.Join(root, "fresh", "SKILL.md"), "---\nname: fresh\ndescription: dynamic\n---\nbody")

	// Wait for at least one reload tick to detect the change.
	select {
	case <-reloaded:
	case <-time.After(2 * time.Second):
		t.Fatal("hot-reload did not fire after adding a skill file")
	}
	if got := s.Get("fresh"); got == nil {
		t.Error("expected 'fresh' to load via hot-reload")
	}
}

func TestReadSkillTool_Schema(t *testing.T) {
	store := NewStore()
	tool := NewReadSkillTool(store)
	testutil.AssertValidSchema(t, tool)
	testutil.AssertEqual(t, tool.Name(), "read_skill")
}

func TestReadSkillTool_Execute_MissingName(t *testing.T) {
	tool := NewReadSkillTool(NewStore())
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestReadSkillTool_Execute_UnknownSkill(t *testing.T) {
	tool := NewReadSkillTool(NewStore())
	_, err := tool.Execute(context.Background(), map[string]any{"name": "ghost"})
	testutil.AssertErrorContains(t, err, "ghost")
}

func TestReadSkillTool_Execute_Success(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: d\n---\n# Body")
	store := NewStore()
	store.LoadFromDirs([]string{root})
	tool := NewReadSkillTool(store)

	result, err := tool.Execute(context.Background(), map[string]any{"name": "alpha"})
	testutil.AssertNoError(t, err)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	testutil.AssertEqual(t, m["skill"], "alpha")
	body, _ := m["instructions"].(string)
	if !strings.Contains(body, "Body") {
		t.Errorf("expected instructions body, got %q", body)
	}
}

// TestReadSkillTool_Execute_RespectsScope proves the scoped store gate works:
// a skill outside the worker's allowlist is "not found" even if it exists in
// the parent store.
func TestReadSkillTool_Execute_RespectsScope(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: a\n---\nA")
	writeSkillFile(t, filepath.Join(root, "beta", "SKILL.md"), "---\nname: beta\ndescription: b\n---\nB")
	base := NewStore()
	base.LoadFromDirs([]string{root})
	scoped := base.Visible([]string{"alpha"})
	tool := NewReadSkillTool(scoped)

	if _, err := tool.Execute(context.Background(), map[string]any{"name": "alpha"}); err != nil {
		t.Errorf("alpha should be readable in scope: %v", err)
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"name": "beta"}); err == nil {
		t.Error("beta should NOT be readable in alpha-only scope")
	}
}

// TestFrontmatterYAMLv3RoundTrip proves the migrated frontmatter parser
// behaves correctly with a struct unmarshal — no hand-rolled line parsing.
func TestFrontmatterYAMLv3RoundTrip(t *testing.T) {
	raw := "name: my-skill\ndescription: \"does a thing\"\ncapabilities:\n  - research\nversion: 1.0.0\ndisable-model-invocation: true"
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if fm.Name != "my-skill" {
		t.Errorf("name = %q", fm.Name)
	}
	if fm.Description != "does a thing" {
		t.Errorf("description = %q", fm.Description)
	}
	if !fm.DisableInvocation {
		t.Error("DisableInvocation should be true")
	}
	if fm.Version != "1.0.0" {
		t.Errorf("version = %q", fm.Version)
	}
	if len(fm.Capabilities) != 1 || fm.Capabilities[0] != "research" {
		t.Errorf("capabilities = %v", fm.Capabilities)
	}
}
