package browser

import (
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestAllStealthScripts_NotEmpty(t *testing.T) {
	scripts := AllStealthScripts()
	if len(scripts) == 0 {
		t.Fatal("stealth scripts must not be empty")
	}
}

func TestAllStealthScripts_ContainsEvasion(t *testing.T) {
	scripts := AllStealthScripts()

	expectedEvasionKeywords := []string{
		"navigator.webdriver",
		"chrome.runtime",
		"navigator.plugins",
		"navigator.languages",
		"navigator.permissions",
		"WebGLRenderingContext",
		"hardwareConcurrency",
		"deviceMemory",
	}

	for _, keyword := range expectedEvasionKeywords {
		if !strings.Contains(scripts, keyword) {
			t.Errorf("stealth scripts should contain evasion for %q", keyword)
		}
	}
}

func TestAllStealthScripts_ValidSyntax(t *testing.T) {
	// Basic syntax check: count balanced braces and parentheses
	scripts := AllStealthScripts()
	braces := 0
	parens := 0
	for _, ch := range scripts {
		switch ch {
		case '{':
			braces++
		case '}':
			braces--
		case '(':
			parens++
		case ')':
			parens--
		}
	}
	if braces != 0 {
		t.Errorf("unbalanced braces: %d", braces)
	}
	if parens != 0 {
		t.Errorf("unbalanced parentheses: %d", parens)
	}
}

func TestApplyStealthPatches_NotConnected(t *testing.T) {
	// Should not panic when called without an active context
	_ = &SandboxBrowserClient{
		logger: zap.NewNop(),
	}
	// No allocator — ApplyStealthPatches will fail but should not panic
	// We just verify creating the client with a logger works
}
