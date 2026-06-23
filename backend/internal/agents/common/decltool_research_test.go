package common

import (
	"strings"
	"testing"
)

// TestResearchDeclToolsLoaded proves the research capability's bash-ddg
// tier carries the ddg_search declarative tool definition. Regression guard
// for Phase 4: if anyone removes the decl_tools stanza or breaks the YAML
// loader, this test fails.
func TestResearchDeclToolsLoaded(t *testing.T) {
	pkgs := LoadToolPackages()
	pkg, ok := pkgs["research"]
	if !ok {
		t.Fatal("research capability missing from LoadToolPackages")
	}

	var bashDDG *Implementation
	for i := range pkg.Implementations {
		if pkg.Implementations[i].Name == "bash-ddg" {
			bashDDG = &pkg.Implementations[i]
			break
		}
	}
	if bashDDG == nil {
		t.Fatal("bash-ddg impl missing from research capability")
	}

	if len(bashDDG.DeclTools) == 0 {
		t.Fatalf("bash-ddg impl has no decl_tools — Phase 4 wiring broken")
	}

	var ddg *DeclarativeTool
	for i := range bashDDG.DeclTools {
		if bashDDG.DeclTools[i].Name == "ddg_search" {
			ddg = &bashDDG.DeclTools[i]
			break
		}
	}
	if ddg == nil {
		t.Fatalf("ddg_search decltool missing; got: %+v", bashDDG.DeclTools)
	}

	if !strings.Contains(ddg.Command, "/sandbox/ddg.py") {
		t.Errorf("ddg_search command should reference ddg.py, got %q", ddg.Command)
	}
	if !strings.Contains(ddg.Command, "{{query}}") {
		t.Errorf("ddg_search command should have {{query}} placeholder, got %q", ddg.Command)
	}
	if !strings.Contains(ddg.Command, "{{max}}") {
		t.Errorf("ddg_search command should have {{max}} placeholder, got %q", ddg.Command)
	}

	// Required param present
	var hasQueryParam, hasMaxParam bool
	for _, p := range ddg.Parameters {
		if p.Name == "query" && p.Required {
			hasQueryParam = true
		}
		if p.Name == "max" {
			hasMaxParam = true
		}
	}
	if !hasQueryParam {
		t.Errorf("ddg_search missing required 'query' param; got %+v", ddg.Parameters)
	}
	if !hasMaxParam {
		t.Errorf("ddg_search missing 'max' param; got %+v", ddg.Parameters)
	}
}
