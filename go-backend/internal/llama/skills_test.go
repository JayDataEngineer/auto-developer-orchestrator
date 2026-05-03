package llama

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── SKILL.md parsing ──────────────────────────────────────────────────────────

func TestParseSkillFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: docker-expert
description: Docker containerization expert
disable-model-invocation: false
---
# Docker Expert

Instructions here.`
	os.WriteFile(path, []byte(content), 0644)

	skill, err := parseSkillFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "docker-expert" {
		t.Errorf("expected name 'docker-expert', got '%s'", skill.Name)
	}
	if skill.Description != "Docker containerization expert" {
		t.Errorf("expected description, got '%s'", skill.Description)
	}
	if skill.Location != path {
		t.Errorf("expected location '%s', got '%s'", path, skill.Location)
	}
	if skill.DisableInvocation {
		t.Error("expected DisableInvocation=false")
	}
}

func TestParseSkillFile_MissingDescription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte(`---
name: test-skill
---
Instructions.`), 0644)

	skill, err := parseSkillFile(path)
	if err == nil {
		t.Error("expected error for missing description")
	}
	if skill != nil {
		t.Error("expected nil skill")
	}
}

func TestParseSkillFile_NameFromDir(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	os.MkdirAll(skillDir, 0755)
	path := filepath.Join(skillDir, "SKILL.md")
	os.WriteFile(path, []byte(`---
description: A skill without an explicit name
---
Instructions here.`), 0644)

	skill, err := parseSkillFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "my-skill" {
		t.Errorf("expected name 'my-skill' from dir, got '%s'", skill.Name)
	}
}

func TestParseSkillFile_DisableInvocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte(`---
name: hidden-skill
description: Should not appear in system prompt
disable-model-invocation: true
---
Secret instructions.`), 0644)

	skill, err := parseSkillFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skill.DisableInvocation {
		t.Error("expected DisableInvocation=true")
	}
}

func TestParseSkillFile_InvalidName(t *testing.T) {
	dir := t.TempDir()
	// Place SKILL.md in a subdirectory with an invalid name
	sub := filepath.Join(dir, "BAD NAME") // contains spaces and uppercase
	os.MkdirAll(sub, 0755)
	path := filepath.Join(sub, "SKILL.md")
	os.WriteFile(path, []byte(`---
name: "Bad Name With Spaces"
description: Invalid name
---
Content.`), 0644)

	// Should fail: frontmatter name is invalid, dir name "BAD NAME" is also invalid
	skill, err := parseSkillFile(path)
	if err == nil {
		t.Error("expected error for invalid name")
	}
	if skill != nil {
		t.Error("expected nil skill")
	}
}

func TestParseSkillFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "no-fm")
	os.MkdirAll(sub, 0755)
	path := filepath.Join(sub, "SKILL.md")
	os.WriteFile(path, []byte("Just plain instructions, no frontmatter."), 0644)

	// Should fail — no description
	skill, err := parseSkillFile(path)
	if err == nil {
		t.Error("expected error — no description in frontmatter")
	}
	if skill != nil {
		t.Error("expected nil skill")
	}
}

// ── Skill name validation ─────────────────────────────────────────────────────

func TestValidateSkillName(t *testing.T) {
	valid := []string{"docker", "docker-expert", "my-skill", "a", "a-b-c", "terraform-engineer"}
	for _, n := range valid {
		if validateSkillName(n, 64) != n {
			t.Errorf("expected '%s' to be valid", n)
		}
	}

	invalid := []string{"Bad Name", "UPPERCASE", "-leading", "trailing-", "double--dash", "", "  "}
	for _, n := range invalid {
		if validateSkillName(n, 64) != "" {
			t.Errorf("expected '%s' to be invalid", n)
		}
	}

	// Max length
	long := strings.Repeat("a", 65)
	if validateSkillName(long, 64) != "" {
		t.Error("expected too-long name to be invalid")
	}
}

// ── SkillStore discovery ──────────────────────────────────────────────────────

func TestSkillStore_LoadFromDirs(t *testing.T) {
	dir := t.TempDir()

	// Create a skill in .pux/skills/
	skillDir := filepath.Join(dir, ".pux", "skills", "docker-expert")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: docker-expert
description: Docker containerization expert
---
Docker instructions here.`), 0644)

	// Create another skill
	skillDir2 := filepath.Join(dir, ".agents", "skills", "golang-pro")
	os.MkdirAll(skillDir2, 0755)
	os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte(`---
name: golang-pro
description: Go programming expert
---
Go instructions here.`), 0644)

	store := NewSkillStore()
	dirs := []string{
		filepath.Join(dir, ".pux", "skills"),
		filepath.Join(dir, ".agents", "skills"),
	}
	count := store.LoadFromDirs(dirs)
	if count != 2 {
		t.Errorf("expected 2 skills, got %d", count)
	}

	// Verify both skills
	docker := store.Get("docker-expert")
	if docker == nil {
		t.Fatal("expected docker-expert skill")
	}
	if docker.Description != "Docker containerization expert" {
		t.Errorf("wrong description: %s", docker.Description)
	}

	golang := store.Get("golang-pro")
	if golang == nil {
		t.Fatal("expected golang-pro skill")
	}
}

func TestSkillStore_DeduplicateByName(t *testing.T) {
	dir := t.TempDir()

	// Two different SKILL.md files with same name — first wins
	dir1 := filepath.Join(dir, "skills1", "same-name")
	os.MkdirAll(dir1, 0755)
	os.WriteFile(filepath.Join(dir1, "SKILL.md"), []byte(`---
name: my-skill
description: First version
---
First.`), 0644)

	dir2 := filepath.Join(dir, "skills2", "same-name")
	os.MkdirAll(dir2, 0755)
	os.WriteFile(filepath.Join(dir2, "SKILL.md"), []byte(`---
name: my-skill
description: Second version (should be ignored)
---
Second.`), 0644)

	store := NewSkillStore()
	store.LoadFromDirs([]string{dir1, dir2})

	skill := store.Get("my-skill")
	if skill == nil {
		t.Fatal("expected my-skill")
	}
	if skill.Description != "First version" {
		t.Errorf("expected first-wins, got '%s'", skill.Description)
	}
}

func TestSkillStore_StopRecursionOnSKILLMD(t *testing.T) {
	dir := t.TempDir()

	// Create: skills/docker/SKILL.md and skills/docker/nested/SKILL.md
	// The nested one should NOT be loaded because discovery stops at docker/SKILL.md
	outerDir := filepath.Join(dir, "skills", "docker")
	os.MkdirAll(outerDir, 0755)
	os.WriteFile(filepath.Join(outerDir, "SKILL.md"), []byte(`---
name: docker
description: Outer docker skill
---
Outer.`), 0644)

	innerDir := filepath.Join(outerDir, "nested")
	os.MkdirAll(innerDir, 0755)
	os.WriteFile(filepath.Join(innerDir, "SKILL.md"), []byte(`---
name: docker-nested
description: Should not be found
---
Nested.`), 0644)

	store := NewSkillStore()
	store.LoadFromDirs([]string{dir})

	// docker should be found
	if store.Get("docker") == nil {
		t.Error("expected outer docker skill")
	}
	// docker-nested should NOT be found (recursion stopped at docker/SKILL.md)
	if store.Get("docker-nested") != nil {
		t.Error("nested skill should not be discovered")
	}
	if store.Count() != 1 {
		t.Errorf("expected 1 skill, got %d", store.Count())
	}
}

// ── Skill reading ─────────────────────────────────────────────────────────────

func TestSkillStore_ReadSkill(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	os.WriteFile(skillPath, []byte(`---
name: test-read
description: Testing read
---
These are the instructions for the test skill.
Line two.`), 0644)

	store := NewSkillStore()
	store.LoadFromDirs([]string{dir})

	instructions := store.ReadSkill("test-read")
	if !strings.Contains(instructions, "These are the instructions") {
		t.Errorf("unexpected instructions: %s", instructions)
	}
	if strings.Contains(instructions, "---") {
		t.Error("instructions should not contain frontmatter")
	}
}

func TestSkillStore_ReadSkill_NotFound(t *testing.T) {
	store := NewSkillStore()
	if store.ReadSkill("nonexistent") != "" {
		t.Error("expected empty string for nonexistent skill")
	}
}

// ── System prompt formatting ──────────────────────────────────────────────────

func TestFormatAvailableSkills(t *testing.T) {
	dir := t.TempDir()

	skillDir := filepath.Join(dir, "docker-expert")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: docker-expert
description: Docker containerization expert
---
Docker instructions.`), 0644)

	store := NewSkillStore()
	store.LoadFromDirs([]string{dir})

	prompt := store.FormatAvailableSkills()
	if !strings.Contains(prompt, "<available_skills>") {
		t.Error("expected <available_skills> block")
	}
	if !strings.Contains(prompt, "<name>docker-expert</name>") {
		t.Error("expected skill name in XML")
	}
	if !strings.Contains(prompt, "<description>Docker containerization expert</description>") {
		t.Error("expected description in XML")
	}
	if !strings.Contains(prompt, "<location>") {
		t.Error("expected location in XML")
	}
	// Should NOT contain full instructions (pi-mono: model loads via read_skill)
	if strings.Contains(prompt, "Docker instructions") {
		t.Error("available skills block should NOT contain full instructions")
	}
}

func TestFormatAvailableSkills_ExcludesDisabled(t *testing.T) {
	dir := t.TempDir()

	// Visible skill
	d1 := filepath.Join(dir, "visible-skill")
	os.MkdirAll(d1, 0755)
	os.WriteFile(filepath.Join(d1, "SKILL.md"), []byte(`---
name: visible-skill
description: Should appear
---
Content.`), 0644)

	// Hidden skill
	d2 := filepath.Join(dir, "hidden-skill")
	os.MkdirAll(d2, 0755)
	os.WriteFile(filepath.Join(d2, "SKILL.md"), []byte(`---
name: hidden-skill
description: Should NOT appear
disable-model-invocation: true
---
Secret.`), 0644)

	store := NewSkillStore()
	store.LoadFromDirs([]string{dir})

	prompt := store.FormatAvailableSkills()
	if !strings.Contains(prompt, "visible-skill") {
		t.Error("expected visible skill")
	}
	if strings.Contains(prompt, "hidden-skill") {
		t.Error("disabled skill should not appear in prompt")
	}
}

// ── StandardSkillDirs ─────────────────────────────────────────────────────────

func TestStandardSkillDirs_ReturnsPaths(t *testing.T) {
	dirs := StandardSkillDirs("/home/user/project", "/home/user")
	if len(dirs) == 0 {
		t.Error("expected non-empty dir list")
	}

	// Should include .pux/skills
	foundPux := false
	foundAgents := false
	foundUser := false
	for _, d := range dirs {
		if strings.Contains(d, ".pux/skills") {
			foundPux = true
		}
		if strings.Contains(d, ".agents/skills") {
			foundAgents = true
		}
		if strings.Contains(d, "/.agents/skills") {
			foundUser = true
		}
	}
	if !foundPux {
		t.Error("expected .pux/skills in paths")
	}
	if !foundAgents {
		t.Error("expected .agents/skills in paths")
	}
	if !foundUser {
		t.Error("expected user .agents/skills in paths")
	}
}
