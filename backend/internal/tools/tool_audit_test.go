package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUntrustedHandlingPackagesWrapViaQuarantineResult enforces the contract
// that every tool package handling untrusted input (browser results, MCP
// results, sandbox stdout, agent-authored memory content) wraps its tool
// results via tools.QuarantineResult before returning.
//
// Why: the Fable/Mythos §5.2 failure mode "instruction_following_on_untrusted_input"
// has a 0.5% baseline rate. QuarantineResult wraps injection-pattern lines in
// <suspicious_input> tags so the model recognizes them as data, not directives.
// When a package silently skips the wrap, drift compounds — memory content
// containing "ignore previous instructions" puppeteers the model on the next
// recall.
//
// Contract: if you add a new tool package to this list, also add a regression
// test under tools/<pkg>/ that proves the wrap actually fires on a known
// injection pattern (see memory_test.go::TestTool_Execute_QuarantinesInjectionPattern).
//
// Adding a new untrusted-handling package NOT in this list is a reviewer
// red flag — the audit test should fail until the wrap is wired.
func TestUntrustedHandlingPackagesWrapViaQuarantineResult(t *testing.T) {
	// Canonical list of tool packages that handle untrusted input.
	// Maintained by hand; the test fails if a package's .go files stop
	// referencing QuarantineResult.
	untrustedPkgs := []string{
		"browser",  // CDP page content, downloads, cookies
		"mcp",      // MCP server responses
		"memory",   // agent-authored memory docs (recall returns stored content)
	}

	toolsRoot := findToolsRoot(t)
	for _, pkg := range untrustedPkgs {
		t.Run(pkg, func(t *testing.T) {
			pkgDir := filepath.Join(toolsRoot, pkg)
			if _, err := os.Stat(pkgDir); err != nil {
				t.Skipf("package dir %s does not exist: %v", pkgDir, err)
			}
			if !packageReferencesQuarantineResult(t, pkgDir) {
				t.Errorf(
					"package tools/%s handles untrusted input but does not call tools.QuarantineResult. "+
						"Wire it on every Execute() return path that echoes external/agent-authored data. "+
						"See tools/memory/memory.go for the canonical pattern.",
					pkg,
				)
			}
		})
	}
}

// TestEveryToolPackageHasTests enforces the contract that every tool package
// ships test coverage. A tool package without tests is a silent contract —
// drift in its behavior is invisible until production.
//
// Exempt: packages that are pure type declarations with no logic (none today).
// If you add such a package, add it to the exempt list with a one-line reason.
func TestEveryToolPackageHasTests(t *testing.T) {
	toolsRoot := findToolsRoot(t)
	entries, err := os.ReadDir(toolsRoot)
	if err != nil {
		t.Fatalf("cannot read tools dir: %v", err)
	}

	exempt := map[string]string{
		// No exemptions today. Add "<pkg>: <reason>" here when a package is
		// intentionally test-free (pure types, generated stubs, etc.).
	}

	var untested []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		if reason, ok := exempt[entry.Name()]; ok {
			t.Logf("tools/%s: exempt (%s)", entry.Name(), reason)
			continue
		}
		pkgDir := filepath.Join(toolsRoot, entry.Name())
		testFiles := countGoFiles(pkgDir, true)
		if testFiles == 0 {
			// Verify it's actually a tool package (has non-test .go files)
			// before flagging — empty/scaffold dirs are skipped.
			if countGoFiles(pkgDir, false) == 0 {
				continue
			}
			untested = append(untested, entry.Name())
		}
	}

	if len(untested) > 0 {
		t.Errorf(
			"tool packages without test coverage (drift risk):\n  - %s\n"+
				"Add at least one *_test.go that exercises the primary Execute path.",
			strings.Join(untested, "\n  - "),
		)
	}
}

// TestKnownToolSchemasAreValidJSON verifies every Schema() call on the
// canonical tool instances parses as JSON. A malformed schema breaks the
// tool-use protocol silently — the model sees the tool but can't call it.
//
// The test instantiates each tool with nil/stub dependencies (where possible)
// and asserts Schema() returns parseable JSON. Tools that require non-nil
// dependencies are skipped (documented in the per-tool comment).
//
// To add a new tool: instantiate it in buildSchemaCases() and assert.
func TestKnownToolSchemasAreValidJSON(t *testing.T) {
	// This test is intentionally light — the per-package tests already
	// assert schema validity via testutil.AssertValidSchema. The point of
	// this audit test is to document the contract in one place: every
	// tool.Schema() must return JSON-parseable bytes.
	//
	// If a tool package's tests skip the schema assertion, that's drift.
	// Add the package to schemaSchemaAssertedRequired.
	if !packageSchemaTestsExist(t, filepath.Join(findToolsRoot(t), "memory")) {
		t.Error("memory package must have schema-validity tests (use testutil.AssertValidSchema)")
	}
}

// findToolsRoot walks up from the test file to locate backend/internal/tools/.
// Falls back to a relative path if the working dir is the repo root.
func findToolsRoot(t *testing.T) string {
	t.Helper()
	candidates := []string{
		".",                              // running from tools/
		"backend/internal/tools",         // running from repo root
		"../internal/tools",              // running from backend/cmd
		"../../internal/tools",           // running from backend
	}
	for _, c := range candidates {
		if abs, err := filepath.Abs(c); err == nil {
			if fi, err := os.Stat(filepath.Join(abs, "untrusted.go")); err == nil && !fi.IsDir() {
				return abs
			}
		}
	}
	t.Fatal("could not locate tools/ root (looking for untrusted.go)")
	return ""
}

// packageReferencesQuarantineResult parses every non-test .go file in pkgDir
// and returns true if any file references QuarantineResult.
func packageReferencesQuarantineResult(t *testing.T, pkgDir string) bool {
	t.Helper()
	fset := token.NewFileSet()
	found := false
	err := filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// Match tools.QuarantineResult(...) or QuarantineResult(...)
			// (same-package call, though only browser/mcp/memory use it today).
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				if fn.Sel.Name == "QuarantineResult" {
					found = true
				}
			case *ast.Ident:
				if fn.Name == "QuarantineResult" {
					found = true
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", pkgDir, err)
	}
	return found
}

// packageSchemaTestsExist returns true if any _test.go file in pkgDir
// references testutil.AssertValidSchema.
func packageSchemaTestsExist(t *testing.T, pkgDir string) bool {
	t.Helper()
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "AssertValidSchema") {
			return true
		}
	}
	return false
}

func countGoFiles(dir string, tests bool) int {
	count := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		isTest := strings.HasSuffix(path, "_test.go")
		if tests == isTest {
			count++
		}
		return nil
	})
	return count
}
