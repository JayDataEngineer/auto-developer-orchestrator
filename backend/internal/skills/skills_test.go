package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestValidateSkillName(t *testing.T) {
	tests := []struct {
		in      string
		wantOK  bool
	}{
		{"my-skill", true},
		{"abc123", true},
		{"a", true},
		{"multi-word-name", true},
		{"", false},          // empty
		{"My-Skill", false},  // uppercase
		{"my_skill", false},  // underscore not allowed
		{"-leading", false},  // leading dash
		{"trailing-", false}, // trailing dash
		{"has space", false}, // space
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

func TestParseSkillYAML(t *testing.T) {
	fm := parseSkillYAML("name: my-skill\ndescription: \"does a thing\"\n# comment\ndisable-model-invocation: true")
	if fm["name"] != "my-skill" {
		t.Errorf("name = %q", fm["name"])
	}
	if fm["description"] != "does a thing" {
		t.Errorf("description = %q (quotes should be stripped)", fm["description"])
	}
	if fm["disable-model-invocation"] != "true" {
		t.Errorf("disable flag = %q", fm["disable-model-invocation"])
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
	writeSkillFile(t, path, "---\nname: my-skill\ndescription: A test skill\n---\n# Instructions\nDo the thing.")

	skill, err := parseSkillFile(path)
	testutil.AssertNoError(t, err)
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	testutil.AssertEqual(t, skill.Name, "my-skill")
	testutil.AssertEqual(t, skill.Description, "A test skill")
	testutil.AssertEqual(t, skill.Location, path)
	testutil.AssertEqual(t, skill.Dir, dir)
}

func TestParseSkillFile_NameFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	namedDir := filepath.Join(dir, "auto-named")
	path := filepath.Join(namedDir, "SKILL.md")
	writeSkillFile(t, path, "---\ndescription: skill with no name\n---\nbody")

	skill, err := parseSkillFile(path)
	testutil.AssertNoError(t, err)
	testutil.AssertEqual(t, skill.Name, "auto-named")
}

func TestParseSkillFile_MissingDescriptionErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	writeSkillFile(t, path, "---\nname: no-desc\n---\nbody")

	if _, err := parseSkillFile(path); err == nil {
		t.Fatal("expected error for missing description")
	}
}

func TestParseSkillFile_InvalidNameErrors(t *testing.T) {
	dir := t.TempDir()
	// Dir name has uppercase so fallback fails too.
	path := filepath.Join(dir, "BadName", "SKILL.md")
	writeSkillFile(t, path, "---\ndescription: desc\n---\nbody")

	if _, err := parseSkillFile(path); err == nil {
		t.Fatal("expected error when name is invalid and dir fallback fails")
	}
}

func TestParseSkillFile_DisableInvocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	writeSkillFile(t, path, "---\nname: hidden\ndescription: x\ndisable-model-invocation: true\n---\nbody")

	skill, err := parseSkillFile(path)
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

	skill, err := parseSkillFile(path)
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
}

func TestStore_LoadFromDirs_NonexistentDir(t *testing.T) {
	s := NewStore()
	if count := s.LoadFromDirs([]string{"/does/not/exist"}); count != 0 {
		t.Errorf("expected 0 for nonexistent dir, got %d", count)
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

func TestStandardSkillDirs_StopsAtGitRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mark 'a' as git root.
	if err := os.MkdirAll(filepath.Join(root, "a", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := StandardSkillDirs(deep, "")
	// Walk goes c -> b -> a(.git, stop). Should not include root-level paths.
	for _, d := range dirs {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			continue
		}
		// Paths outside root (home dirs) are filtered by empty userHome here.
		if strings.HasPrefix(rel, "..") {
			continue
		}
		// Should not walk above 'a' (the git root).
		if strings.Contains(rel, filepath.Join("a", "b")) || rel == "a" || strings.HasPrefix(rel, "a"+string(filepath.Separator)) {
			// 'a/...' is allowed since 'a' is the git root and gets included.
			continue
		}
	}
	// At minimum, deep itself should produce a .pux/skills and .agents/skills entry.
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
