package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiscoverEmptyWhenNoSkillsDir proves the common case (project has no
// skills/ dir) is not an error — returns nil silently.
func TestDiscoverEmptyWhenNoSkillsDir(t *testing.T) {
	root := t.TempDir()
	ss, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(ss) != 0 {
		t.Errorf("expected 0 skills, got %d", len(ss))
	}
}

// TestDiscoverFindsSkills proves happy-path discovery: two skill dirs each
// with SKILL.md → both surface with name/description/path populated.
func TestDiscoverFindsSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "Alpha skill", "Body of alpha.")
	writeSkill(t, root, "beta", "Beta skill", "Body of beta.")

	ss, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(ss) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(ss), ss)
	}

	// Order matches ReadDir (lexical), so alpha should be first.
	if ss[0].Name != "alpha" || ss[1].Name != "beta" {
		t.Errorf("order: got %q, %q", ss[0].Name, ss[1].Name)
	}
	if ss[0].Description != "Alpha skill" {
		t.Errorf("description: got %q", ss[0].Description)
	}
	// Discover returns Content empty by design — Load fills it.
	if ss[0].Content != "" {
		t.Errorf("Discover should not fill Content, got %q", ss[0].Content)
	}
	// Path should be absolute.
	if !filepath.IsAbs(ss[0].Path) {
		t.Errorf("path should be absolute, got %q", ss[0].Path)
	}
}

// TestDiscoverSkipsDirsWithoutSkillFile proves a skills/ subdir that lacks
// SKILL.md doesn't break the scan — it's skipped silently.
func TestDiscoverSkipsDirsWithoutSkillFile(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "real", "Real skill", "body")
	// Scratch subdir without SKILL.md.
	if err := os.MkdirAll(filepath.Join(root, "skills", "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Loose file at top of skills/ — also ignored.
	if err := os.WriteFile(filepath.Join(root, "skills", "README.md"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	ss, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(ss) != 1 || ss[0].Name != "real" {
		t.Errorf("expected only 'real', got %+v", ss)
	}
}

// TestLoadReturnsBody proves Load reads the full markdown body and that
// it's distinct from what Discover returns (Content populated).
func TestLoadReturnsBody(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "loaded", "Loaded skill", "# Title\n\nDetailed body.")

	s, err := Load(root, "loaded")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Name != "loaded" {
		t.Errorf("name: got %q", s.Name)
	}
	if s.Description != "Loaded skill" {
		t.Errorf("description: got %q", s.Description)
	}
	if s.Content != "# Title\n\nDetailed body." {
		t.Errorf("content: got %q", s.Content)
	}
}

// TestLoadMissingSkillIsError proves the error path produces a clear
// "not found" message rather than a generic os error.
func TestLoadMissingSkillIsError(t *testing.T) {
	root := t.TempDir()
	_, err := Load(root, "ghost")
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// TestParseMalformedFrontmatter proves a missing closing `---` doesn't
// silently produce garbage — it returns a parse error.
func TestParseMalformedFrontmatter(t *testing.T) {
	raw := "---\nname: broken\ndescription: missing close\nbody never ends"
	if _, err := parse(raw); err == nil {
		t.Fatal("expected parse error for unclosed frontmatter")
	}
}

// TestParseRequiresName proves a SKILL.md with frontmatter but no `name`
// field is rejected — name is the load-bearing identifier.
func TestParseRequiresName(t *testing.T) {
	raw := "---\ndescription: has desc but no name\n---\nbody"
	if _, err := parse(raw); err == nil {
		t.Fatal("expected error when name missing")
	}
}

// TestParseHandlesCRLF proves Windows-style line endings don't break the
// frontmatter delimiter scan.
func TestParseHandlesCRLF(t *testing.T) {
	raw := "---\r\nname: crlf\r\ndescription: windows\r\n---\r\nbody line"
	s, err := parse(raw)
	if err != nil {
		t.Fatalf("parse CRLF: %v", err)
	}
	if s.Name != "crlf" || s.Description != "windows" {
		t.Errorf("got name=%q desc=%q", s.Name, s.Description)
	}
	if s.Content != "body line" {
		t.Errorf("content: got %q", s.Content)
	}
}

// TestParseStripsQuotes proves quoted YAML values get unquoted — common
// pattern when description contains commas or colons.
func TestParseStripsQuotes(t *testing.T) {
	raw := "---\nname: q\ndescription: \"Quoted, with: special chars\"\n---\nbody"
	s, err := parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := "Quoted, with: special chars"
	if s.Description != want {
		t.Errorf("description: got %q want %q", s.Description, want)
	}
}

// writeSkill creates <root>/skills/<name>/SKILL.md with the given fields.
// Helper for table-style setup.
func writeSkill(t *testing.T, root, name, desc, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, SkillNameFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
