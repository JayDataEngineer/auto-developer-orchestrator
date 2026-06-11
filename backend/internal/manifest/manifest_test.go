package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestNotFound(t *testing.T) {
	dir := t.TempDir()
	mf, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if mf != nil {
		t.Fatal("expected nil manifest for missing file")
	}
}

func TestLoadManifestValid(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: test-app
version: "1.0.0"
description: "A test application"
requires:
  - bash
  - mcp_search
prompts:
  greet:
    text: |
      Hello {name}!
schedule:
  daily:
    cron: "0 9 * * *"
    prompt: greet
    description: "Daily greeting"
commands:
  run:
    description: "Run the app"
    exec: "python main.py"
`
	if err := os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mf, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if mf.Name != "test-app" {
		t.Errorf("expected name 'test-app', got %q", mf.Name)
	}
	if mf.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", mf.Version)
	}
	if len(mf.Requires) != 2 {
		t.Errorf("expected 2 requires, got %d", len(mf.Requires))
	}
	if len(mf.Prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(mf.Prompts))
	}
	if len(mf.Schedule) != 1 {
		t.Errorf("expected 1 schedule, got %d", len(mf.Schedule))
	}
	if len(mf.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(mf.Commands))
	}
}

func TestLoadManifestMissingName(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
version: "1.0.0"
description: "No name"
`
	if err := os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestResolvePromptInline(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: test
prompts:
  hello:
    text: "Hello World"
`
	os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yamlContent), 0644)

	mf, _ := LoadManifest(dir)
	text, err := mf.ResolvePrompt(dir, "hello")
	if err != nil {
		t.Fatalf("ResolvePrompt failed: %v", err)
	}
	if text != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", text)
	}
}

func TestResolvePromptFile(t *testing.T) {
	dir := t.TempDir()

	// Create prompt file
	os.MkdirAll(filepath.Join(dir, "prompts"), 0755)
	os.WriteFile(filepath.Join(dir, "prompts", "scan.md"), []byte("# Scan Prompt\nDo the scan."), 0644)

	yamlContent := `
name: test
prompts:
  scan:
    file: prompts/scan.md
`
	os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yamlContent), 0644)

	mf, _ := LoadManifest(dir)
	text, err := mf.ResolvePrompt(dir, "scan")
	if err != nil {
		t.Fatalf("ResolvePrompt failed: %v", err)
	}
	if text != "# Scan Prompt\nDo the scan." {
		t.Errorf("unexpected prompt text: %q", text)
	}
}

func TestResolvePromptNotFound(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: test
prompts:
  hello:
    text: "Hi"
`
	os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yamlContent), 0644)

	mf, _ := LoadManifest(dir)
	_, err := mf.ResolvePrompt(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent prompt")
	}
}

func TestBrief(t *testing.T) {
	yamlContent := `
name: my-app
version: "2.0.0"
description: "Test app"
requires:
  - bash
schedule:
  daily:
    cron: "0 9 * * *"
    prompt: greet
commands:
  run:
    description: "Run"
    exec: "python main.py"
`
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yamlContent), 0644)

	mf, _ := LoadManifest(dir)
	brief := mf.Brief()
	if brief == "" {
		t.Fatal("expected non-empty brief")
	}
	// Should contain name and counts
	if !contains(brief, "my-app") {
		t.Errorf("brief should contain app name: %q", brief)
	}
	if !contains(brief, "1 schedule") {
		t.Errorf("brief should mention schedules: %q", brief)
	}
	if !contains(brief, "1 command") {
		t.Errorf("brief should mention commands: %q", brief)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
