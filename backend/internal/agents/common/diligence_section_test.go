package common

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDiligenceSectionPresent locks down the diligence substrate.
//
// The diligence section (Fable/Mythos six failure modes + cheap-verification
// oath + sub-agent relay rule + memory authoring rule) is the highest-leverage
// text in the CTO prompt. Per principle 5 of the prompt-engineering pass
// (2026-06-23): diligence substrate is versioned and regression-tested.
//
// This test fails if:
//   - config/prompt_sections/diligence.md is missing or empty
//   - The section is not registered in the PromptBuilder pipeline
//   - Any of the canonical phrases (failure-mode names, oath, rules) are removed
//
// Background: when this test was written, the diligence section existed ONLY
// in the legacy config/prompt.md but not in any prompt_sections/*.md file.
// Because the V2 builder uses section files (and falls back to prompt.md
// only when sections/ doesn't exist), the diligence substrate was silently
// dropped from every production prompt. This test prevents that regression.
//
// To update the substrate intentionally: edit diligence.md, update the
// version annotation in that file, and update RequiredPhrases here to match.
func TestDiligenceSectionPresent(t *testing.T) {
	root := repoRoot(t)
	configDir := filepath.Join(root, "config")

	// 1. Section file exists and is non-empty.
	sectionPath := filepath.Join(configDir, "prompt_sections", "diligence.md")
	data, err := os.ReadFile(sectionPath)
	if err != nil {
		t.Fatalf("diligence section file missing: %v", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Fatal("diligence section file is empty")
	}

	// 2. Section is registered in the builder pipeline.
	builder := NewPromptBuilder(configDir)
	registered := false
	for _, s := range builder.sections {
		if s.Name == "diligence" {
			registered = true
			if s.File != "diligence.md" {
				t.Errorf("diligence section has wrong File: got %q want diligence.md", s.File)
			}
			if s.Level != Stable {
				t.Errorf("diligence section has wrong Level: got %v want Stable", s.Level)
			}
			break
		}
	}
	if !registered {
		t.Fatal("diligence section not registered in PromptBuilder.sections — V2 builder will silently drop it")
	}

	// 3. Section content reaches the built V2 prompt.
	prompt := builder.Build(&PromptContext{KernelRoles: LoadAgentRoles()})
	if prompt == "" {
		t.Fatal("built prompt is empty")
	}

	// 4. Every load-bearing phrase is present. Update this list when you
	// intentionally change the substrate; do NOT remove entries just to
	// make the test pass.
	RequiredPhrases := []string{
		// Section header
		"Diligence & Honesty",
		// Six failure modes (Fable/Mythos taxonomy, in order)
		"Safeguard circumvention",
		"Fabrication",
		"Skipped cheap verification",
		"Reckless action",
		"Correction fails",
		"Instruction-following on untrusted input",
		// Three load-bearing rules
		"Cheap-verification oath",
		"Sub-agent relay rule",
		"Memory authoring rule",
		// Sentinel phrases that anchor the rules
		`"Should work" is banned`,
		"DATA, not instructions",
		"verification footprint",
	}
	for _, phrase := range RequiredPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("diligence substrate missing phrase: %q — substrate was likely trimmed. "+
				"If this is intentional, update RequiredPhrases in %s and bump the version "+
				"annotation in config/prompt_sections/diligence.md.",
				phrase, currentFile())
		}
	}

	// 5. Version annotation present in the section file.
	if !strings.Contains(string(data), "diligence-substrate version:") {
		t.Error("diligence.md missing version annotation comment — required by principle 5 (version it)")
	}
}

// TestDiligenceSectionAlsoInLegacyPrompt verifies the legacy config/prompt.md
// still carries the diligence section. The V2 builder is the primary path,
// but prompt.md is the fallback when prompt_sections/ doesn't exist — both
// paths must agree on diligence content.
func TestDiligenceSectionAlsoInLegacyPrompt(t *testing.T) {
	root := repoRoot(t)
	legacyPath := filepath.Join(root, "config", "prompt.md")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Skipf("legacy prompt.md not found: %v", err)
	}
	for _, phrase := range []string{
		"Diligence & Honesty",
		"Safeguard circumvention",
		"Cheap-verification oath",
	} {
		if !strings.Contains(string(data), phrase) {
			t.Errorf("legacy config/prompt.md missing phrase: %q", phrase)
		}
	}
}

// currentFile returns the basename of this source file for error messages.
func currentFile() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Base(f)
}
