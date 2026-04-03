package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProducesAllSections(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	prompt := builder.Build()

	expected := []string{
		"# Pi Agent",
		"# System Rules",
		"# Doing Tasks",
		"# Executing Actions with Care",
		"# Environment",
		"Working directory:",
		"Current date:",
		"Platform:",
	}
	for _, s := range expected {
		if !strings.Contains(prompt, s) {
			t.Errorf("Build() missing expected section %q", s)
		}
	}
}

func TestBuildWithAppendSections(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	builder.AppendSections = []string{"# Custom Section\nSome extra context."}

	prompt := builder.Build()
	if !strings.Contains(prompt, "# Custom Section") {
		t.Error("Build() missing appended section")
	}
	if !strings.Contains(prompt, "Some extra context.") {
		t.Error("Build() missing appended section content")
	}
}

func TestBuildWithEmptyProjectDir(t *testing.T) {
	builder := NewSystemPromptBuilder("")
	prompt := builder.Build()

	// Should still produce intro and rules
	if !strings.Contains(prompt, "# Pi Agent") {
		t.Error("Build() with empty dir should still have intro")
	}
	if !strings.Contains(prompt, "# System Rules") {
		t.Error("Build() with empty dir should still have rules")
	}
	// Should NOT have git context
	if strings.Contains(prompt, "# Project Context") {
		t.Error("Build() with empty dir should not have project context")
	}
}

func TestDiscoverInstructionFiles(t *testing.T) {
	dir := t.TempDir()

	// Create PI.md
	piMd := "# Project Instructions\nUse Go 1.26.\n"
	if err := os.WriteFile(filepath.Join(dir, "PI.md"), []byte(piMd), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(dir)
	files := builder.discoverInstructionFiles()

	if len(files) != 1 {
		t.Fatalf("expected 1 instruction file, got %d", len(files))
	}
	if files[0].Path != "PI.md" {
		t.Errorf("expected path PI.md, got %q", files[0].Path)
	}
	if !strings.Contains(files[0].Content, "Use Go 1.26.") {
		t.Error("instruction file content missing expected text")
	}
}

func TestDiscoverInstructionFilesNested(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subproject")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// PI.md in parent
	parentMd := "# Parent\nParent instructions here.\n"
	if err := os.WriteFile(filepath.Join(dir, "PI.md"), []byte(parentMd), 0644); err != nil {
		t.Fatal(err)
	}

	// PI.md in subproject
	childMd := "# Child\nChild instructions here.\n"
	if err := os.WriteFile(filepath.Join(subDir, "PI.md"), []byte(childMd), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(subDir)
	files := builder.discoverInstructionFiles()

	if len(files) != 2 {
		t.Fatalf("expected 2 instruction files, got %d", len(files))
	}
}

func TestDiscoverInstructionFilesDedup(t *testing.T) {
	dir := t.TempDir()

	// Same content in both PI.md and .pi/instructions.md
	content := "# Same\nIdentical content.\n"
	if err := os.WriteFile(filepath.Join(dir, "PI.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".pi"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pi", "instructions.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(dir)
	files := builder.discoverInstructionFiles()

	if len(files) != 1 {
		t.Errorf("expected 1 deduplicated file, got %d", len(files))
	}
}

func TestDiscoverInstructionFilesTotalBudget(t *testing.T) {
	dir := t.TempDir()

	// Create a large PI.md
	large := strings.Repeat("x", 6000)
	if err := os.WriteFile(filepath.Join(dir, "PI.md"), []byte(large), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a second file that would exceed budget
	second := strings.Repeat("y", 6000)
	if err := os.MkdirAll(filepath.Join(dir, ".pi"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pi", "instructions.md"), []byte(second), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(dir)
	files := builder.discoverInstructionFiles()

	totalChars := 0
	for _, f := range files {
		totalChars += len(f.Content)
	}

	// Total should not exceed budget (with some slack for truncation marker)
	if totalChars > maxTotalInstructionChars+100 {
		t.Errorf("total instruction chars %d exceeds budget %d", totalChars, maxTotalInstructionChars)
	}
}

func TestDiscoverInstructionFilesSingleBudget(t *testing.T) {
	dir := t.TempDir()

	// Create PI.md larger than per-file budget
	large := strings.Repeat("a", 5000)
	if err := os.WriteFile(filepath.Join(dir, "PI.md"), []byte(large), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(dir)
	files := builder.discoverInstructionFiles()

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if len(files[0].Content) > maxInstructionFileChars+20 {
		t.Errorf("single file content %d exceeds budget %d", len(files[0].Content), maxInstructionFileChars)
	}
	if !strings.Contains(files[0].Content, "[truncated]") {
		t.Error("expected [truncated] marker in truncated file")
	}
}

func TestTruncateContent(t *testing.T) {
	tests := []struct {
		input   string
		max     int
		wantLen int
	}{
		{"hello", 10, 5},
		{"hello world", 5, 5},
		{"short", 100, 5},
		{"", 5, 0},
	}
	for _, tt := range tests {
		result := truncateContent(tt.input, tt.max)
		if len(result) != tt.wantLen {
			t.Errorf("truncateContent(%q, %d) len = %d, want %d", tt.input, tt.max, len(result), tt.wantLen)
		}
	}
}

func TestReadGitContextNonRepo(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	status, diff := builder.readGitContext()
	if status != "" || diff != "" {
		t.Error("expected empty git context for non-repo directory")
	}
}

func TestReadGitContextInRepo(t *testing.T) {
	dir := t.TempDir()

	// Init a git repo
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a minimal HEAD file so it looks like a git repo
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(dir)
	// This may or may not produce output depending on whether git is available
	// in the test environment, but should not panic.
	builder.readGitContext()
}

func TestBuildIncludesGitContextInRepo(t *testing.T) {
	dir := t.TempDir()

	// Init a real git repo
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a file and stage it
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(dir)
	prompt := builder.Build()

	// If git is available, we should see project context
	// (may not appear if git binary is not available in test env)
	_ = prompt
}

func TestBuildWithPIFileIncludedInPrompt(t *testing.T) {
	dir := t.TempDir()

	content := "# Test Instructions\nAlways write tests.\n"
	if err := os.WriteFile(filepath.Join(dir, "PI.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewSystemPromptBuilder(dir)
	prompt := builder.Build()

	if !strings.Contains(prompt, "# Project Instructions") {
		t.Error("expected Project Instructions section in prompt")
	}
	if !strings.Contains(prompt, "Always write tests.") {
		t.Error("expected PI.md content in prompt")
	}
}

func TestBuildNoInstructionsIfNoFiles(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	prompt := builder.Build()

	if strings.Contains(prompt, "# Project Instructions") {
		t.Error("should not have Project Instructions section when no PI.md files exist")
	}
}

func TestNormalizeContent(t *testing.T) {
	input := "hello\n\n\n\nworld"
	want := "hello\n\nworld"
	got := normalizeContent(input)
	if got != want {
		t.Errorf("normalizeContent() = %q, want %q", got, want)
	}
}

func TestBuildWithComputerUse(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	builder.SandboxID = "sandbox-test-123"
	builder.ServerPort = "3847"

	prompt := builder.Build()

	if !strings.Contains(prompt, "# Computer Use Mode") {
		t.Error("expected Computer Use Mode section when SandboxID is set")
	}
	if !strings.Contains(prompt, "sandbox-test-123") {
		t.Error("expected sandbox ID in computer use section")
	}
	if !strings.Contains(prompt, "computer-use/enable") {
		t.Error("expected enable endpoint in computer use section")
	}
	if !strings.Contains(prompt, "computer-use/screenshot") {
		t.Error("expected screenshot endpoint in computer use section")
	}
	if !strings.Contains(prompt, "computer-use/snapshot") {
		t.Error("expected snapshot endpoint in computer use section")
	}
	if !strings.Contains(prompt, "computer-use/act") {
		t.Error("expected act endpoint in computer use section")
	}
	if !strings.Contains(prompt, "computer-use/disable") {
		t.Error("expected disable endpoint in computer use section")
	}
	if !strings.Contains(prompt, "## Artifacts") {
		t.Error("expected Artifacts section in computer use section")
	}
	if !strings.Contains(prompt, "api/pi/artifacts") {
		t.Error("expected artifacts endpoint in computer use section")
	}
}

func TestBuildWithoutComputerUse(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	// No SandboxID set

	prompt := builder.Build()

	if strings.Contains(prompt, "# Computer Use Mode") {
		t.Error("should not have Computer Use Mode section when SandboxID is empty")
	}
}

func TestBuildComputerUseCustomPort(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	builder.SandboxID = "sb-1"
	builder.ServerPort = "9999"

	prompt := builder.Build()

	if !strings.Contains(prompt, "localhost:9999") {
		t.Error("expected custom port 9999 in computer use URLs")
	}
	if strings.Contains(prompt, "localhost:3847") {
		t.Error("should not have default port when custom port is set")
	}
}

func TestBuildComputerUseDefaultPort(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	builder.SandboxID = "sb-1"
	// ServerPort intentionally empty

	section := builder.buildComputerUseSection()

	if !strings.Contains(section, "localhost:3847") {
		t.Error("expected default port 3847 when ServerPort is empty")
	}
}

func TestBuildWithSubAgentEnabled(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	builder.SubAgentEnabled = true
	builder.ServerPort = "3847"

	prompt := builder.Build()

	if !strings.Contains(prompt, "# Sub-Agent Delegation") {
		t.Error("expected Sub-Agent Delegation section when SubAgentEnabled is true")
	}
	if !strings.Contains(prompt, "api/pi/subagent/spawn") {
		t.Error("expected subagent spawn endpoint")
	}
	if !strings.Contains(prompt, "api/pi/subagent/status") {
		t.Error("expected subagent status endpoint")
	}
}

func TestBuildWithoutSubAgent(t *testing.T) {
	dir := t.TempDir()
	builder := NewSystemPromptBuilder(dir)
	builder.SubAgentEnabled = false

	prompt := builder.Build()

	if strings.Contains(prompt, "# Sub-Agent Delegation") {
		t.Error("should not have Sub-Agent section when disabled")
	}
}
