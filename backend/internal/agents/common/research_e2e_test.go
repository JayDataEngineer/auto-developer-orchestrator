package common

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResearchTierSwapE2E proves the Stage 2 RFC's primary success criterion:
// when the cloud web-research MCP is unavailable, the research capability
// resolves to bash-ddg, the worker prompt morphs to describe ddg.py, and
// the MCP server list drops the dead "web" entry. Cloud-down → re-planning
// agent, not failing agent.
//
// Stage 2 RFC:
//   ~/.claude/projects/-home-ubuntu-Documents-programs-dev-auto-developer-orchestrator/
//     memory/pux-declarative-stack.md
func TestResearchTierSwapE2E(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	t.Setenv("PROJECT_ROOT", repoRoot)

	// Hermetic global resolver. nil MultiClient → mcp-available always returns
	// false → cloud tier probe fails → bash-ddg wins.
	prev := GetGlobalResolver()
	t.Cleanup(func() {
		SetGlobalResolver(prev)
		// Also reset the package cache so subsequent tests rebuild.
		toolPkgMu.Lock()
		toolPackages = nil
		toolPkgMu.Unlock()
	})

	ReloadPromptTemplate()
	r := NewResolver(nil) // nil MultiClient = cloud tier unreachable
	SetGlobalResolver(r)

	// Step 1: ResolveAll runs health probes, assigns ActiveImpl per package.
	resolved := r.ResolveAll()
	rr, ok := resolved["research"]
	if !ok {
		t.Fatal("research capability not resolved — expected it to appear in ResolveAll output")
	}
	if rr.Name != "bash-ddg" {
		t.Fatalf("expected research to resolve to bash-ddg tier, got %q", rr.Name)
	}

	// Step 2: the package cache now has ActiveImpl set on research.
	// LoadToolPackages returns the same cache.
	pkgs := LoadToolPackages()
	pkg := pkgs["research"]
	if pkg == nil {
		t.Fatal("research capability missing from package cache post-resolve")
	}
	if pkg.ActiveImpl == nil || pkg.ActiveImpl.Name != "bash-ddg" {
		t.Errorf("expected pkg.ActiveImpl.Name=bash-ddg, got %+v", pkg.ActiveImpl)
	}

	// Step 3: ResolveImports returns bash-tier tools, NOT cloud-tier web MCP.
	tools, mcpServers := ResolveImports([]string{"research"})
	hasWeb := false
	for _, s := range mcpServers {
		if s == "web" {
			hasWeb = true
		}
	}
	if hasWeb {
		t.Errorf("ResolveImports should drop 'web' MCP when bash tier active, got mcpServers=%v", mcpServers)
	}
	hasBash := false
	for _, tn := range tools {
		if tn == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Errorf("ResolveImports should expose 'bash' tool when bash tier active, got tools=%v", tools)
	}

	// Step 4: BuildWorkerPrompt injects the bash-ddg prompt, NOT cloud.md.
	// The morphed prompt is what teaches the worker how to call ddg.py.
	prompt := BuildWorkerPrompt("research-persona", []string{"research"})
	if !strings.Contains(prompt, "ddg.py") {
		t.Errorf("morphed prompt missing ddg.py reference — got:\n%s", prompt)
	}
	if strings.Contains(prompt, "mcp__web__") {
		t.Errorf("morphed prompt should NOT contain mcp__web__ references — got:\n%s", prompt)
	}
}
