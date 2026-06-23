package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillFile writes a canonical skill at <dir>/<name>/SKILL.md.
func writeSkillFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + body + "\n---\n\n# " + name + "\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestSkillsList_LoadsFromKernelConfigDir proves orch skills list surfaces
// skills from the kernel config dir. PROJECT_ROOT points at a tmpdir whose
// config/skills/ is scanned.
func TestSkillsList_LoadsFromKernelConfigDir(t *testing.T) {
	root := t.TempDir()
	// Loader looks for config/capabilities/<name>/SKILL.md to recognize the
	// kernel config dir (via FindKernelConfigDir). Skills live next to it.
	capDir := filepath.Join(root, "config", "capabilities", "stub")
	if err := os.MkdirAll(capDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(capDir, "SKILL.md"), []byte("# stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// drop a prompt.md so FindKernelConfigDir recognizes the dir
	if err := os.WriteFile(filepath.Join(root, "config", "prompt.md"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	skillsDir := filepath.Join(root, "config", "skills")
	writeSkillFile(t, skillsDir, "alpha", "Alpha skill")
	writeSkillFile(t, skillsDir, "beta", "Beta skill")

	t.Setenv("PROJECT_ROOT", root)
	stdout, _, err := runCommand(t, "http://unused:9999", "skills", "list")
	if err != nil {
		t.Fatalf("skills list failed: %v", err)
	}
	if !strings.Contains(stdout, "alpha") {
		t.Errorf("alpha missing from list:\n%s", stdout)
	}
	if !strings.Contains(stdout, "beta") {
		t.Errorf("beta missing from list:\n%s", stdout)
	}
}

// TestSkillsShow_PrintsBody proves orch skills show <name> returns the full
// skill markdown.
func TestSkillsShow_PrintsBody(t *testing.T) {
	root := t.TempDir()
	capDir := filepath.Join(root, "config", "capabilities", "stub")
	os.MkdirAll(capDir, 0o755)
	os.WriteFile(filepath.Join(capDir, "SKILL.md"), []byte("# stub\n"), 0o644)
	os.WriteFile(filepath.Join(root, "config", "prompt.md"), []byte("test"), 0o644)

	writeSkillFile(t, filepath.Join(root, "config", "skills"), "alpha", "Unique body content alpha.")

	t.Setenv("PROJECT_ROOT", root)
	stdout, _, err := runCommand(t, "http://unused:9999", "skills", "show", "alpha")
	if err != nil {
		t.Fatalf("skills show failed: %v", err)
	}
	if !strings.Contains(stdout, "Unique body content alpha") {
		t.Errorf("skill body missing from show output:\n%s", stdout)
	}
}

// TestSkillsJSON_ReturnsStructuredOutput proves orch skills json produces
// valid JSON with name + description fields per skill.
func TestSkillsJSON_ReturnsStructuredOutput(t *testing.T) {
	root := t.TempDir()
	capDir := filepath.Join(root, "config", "capabilities", "stub")
	os.MkdirAll(capDir, 0o755)
	os.WriteFile(filepath.Join(capDir, "SKILL.md"), []byte("# stub\n"), 0o644)
	os.WriteFile(filepath.Join(root, "config", "prompt.md"), []byte("test"), 0o644)

	writeSkillFile(t, filepath.Join(root, "config", "skills"), "alpha", "Alpha")

	t.Setenv("PROJECT_ROOT", root)
	stdout, _, err := runCommand(t, "http://unused:9999", "skills", "json")
	if err != nil {
		t.Fatalf("skills json failed: %v", err)
	}
	if !strings.Contains(stdout, "\"name\": \"alpha\"") {
		t.Errorf("alpha missing from json:\n%s", stdout)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "[") {
		t.Errorf("expected JSON array; got: %s", stdout[:min(80, len(stdout))])
	}
}
