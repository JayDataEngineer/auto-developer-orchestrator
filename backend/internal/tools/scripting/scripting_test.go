package scripting

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempScriptsDir points scripts.py at a temp dir for the duration of a
// test. Returns the temp dir. Cleanup is registered with t.Cleanup.
//
// Also sets PROJECT_ROOT to the repo root (resolved via test working dir) so
// scriptsPyPath() can locate sandbox/scripts/scripts.py.
func withTempScriptsDir(t *testing.T) string {
	t.Helper()

	tmp, err := os.MkdirTemp("", "pux-scripts-test-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })

	prevDir := os.Getenv("PUX_SCRIPTS_DIR")
	prevRoot := os.Getenv("PROJECT_ROOT")
	t.Cleanup(func() {
		os.Setenv("PUX_SCRIPTS_DIR", prevDir)
		os.Setenv("PROJECT_ROOT", prevRoot)
	})
	os.Setenv("PUX_SCRIPTS_DIR", tmp)

	// PROJECT_ROOT must point at the repo root so scriptsPyPath() finds
	// sandbox/scripts/scripts.py. The test runs from
	// backend/internal/tools/scripting/ — walk up until we see a `backend`
	// dir with a sibling `sandbox/`.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := wd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "sandbox", "scripts", "scripts.py")); err == nil {
			os.Setenv("PROJECT_ROOT", d)
			break
		}
	}
	if os.Getenv("PROJECT_ROOT") == "" {
		t.Skip("PROJECT_ROOT could not be resolved — running outside repo?")
	}
	return tmp
}

// scriptName calls list_scripts and returns the matching record, or nil.
func scriptByName(t *testing.T, name string) map[string]any {
	t.Helper()
	res, _ := ListScriptsTool{}.Execute(context.Background(), map[string]any{})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("list_scripts returned non-map: %T", res)
	}
	scripts, _ := m["scripts"].([]any)
	for _, s := range scripts {
		if rec, ok := s.(map[string]any); ok {
			if rec["name"] == name {
				return rec
			}
		}
	}
	return nil
}

// TestMakeScriptAcceptsHints proves that the hints param flows end-to-end:
// MakeScriptTool.Execute -> scripts.py --make --hints -> file written with
// hints section in docstring -> list_scripts returns hints in record.
func TestMakeScriptAcceptsHints(t *testing.T) {
	withTempScriptsDir(t)

	hints := "Use when greeting.\nReturns greeting string.\nPitfall: name required."
	args := map[string]any{
		"name":        "greet",
		"description": "Say hello.",
		"code":        "import sys\nprint(f\"hello {sys.argv[1:]}\")",
		"hints":       hints,
	}

	res, _ := MakeScriptTool{}.Execute(context.Background(), args)
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("make_script returned non-map: %T", res)
	}
	if _, exists := m["created"]; !exists {
		t.Fatalf("make_script did not return 'created': %v", m)
	}

	rec := scriptByName(t, "greet")
	if rec == nil {
		t.Fatal("greet not in list_scripts after make_script")
	}
	listHints, _ := rec["hints"].(string)
	if !strings.Contains(listHints, "Use when greeting") {
		t.Errorf("list_scripts hints missing first line; got: %q", listHints)
	}
	if !strings.Contains(listHints, "Pitfall") {
		t.Errorf("list_scripts hints missing pitfall; got: %q", listHints)
	}
}

// TestMakeScriptWorksWithoutHints verifies backward compat — callers that
// don't pass hints still get a working script.
func TestMakeScriptWorksWithoutHints(t *testing.T) {
	withTempScriptsDir(t)

	args := map[string]any{
		"name":        "plain",
		"description": "Plain script.",
		"code":        "print('plain')",
	}
	res, _ := MakeScriptTool{}.Execute(context.Background(), args)
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("make_script returned non-map: %T", res)
	}
	if _, exists := m["created"]; !exists {
		t.Fatalf("make_script did not return 'created': %v", m)
	}
	rec := scriptByName(t, "plain")
	if rec == nil {
		t.Fatal("plain not in list_scripts")
	}
	if rec["hints"] != "" {
		t.Errorf("plain script should have empty hints; got: %v", rec["hints"])
	}
}

// TestReadScriptReturnsHintsField proves the renamed tool surfaces hints in
// its output. Mirrors read_skill's behavior of returning the full body.
func TestReadScriptReturnsHintsField(t *testing.T) {
	withTempScriptsDir(t)

	// Create a script with hints
	MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "peekme",
		"description": "For peeking.",
		"code":        "print('x')",
		"hints":       "First hint.\nSecond hint.",
	})

	res, _ := ReadScriptTool{}.Execute(context.Background(), map[string]any{
		"name": "peekme",
	})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("read_script returned non-map: %T", res)
	}
	if m["name"] != "peekme" {
		t.Errorf("read_script name mismatch: %v", m["name"])
	}
	hints, _ := m["hints"].(string)
	if !strings.Contains(hints, "First hint") || !strings.Contains(hints, "Second hint") {
		t.Errorf("read_script hints missing expected lines; got: %q", hints)
	}
	if _, exists := m["code"]; !exists {
		t.Errorf("read_script should still return code; got keys: %v", mapKeys(m))
	}
}

// TestShowScriptDeprecatedAliasStillWorks proves the old name still resolves.
// The plan calls for keeping it as a one-release alias.
func TestShowScriptDeprecatedAliasStillWorks(t *testing.T) {
	withTempScriptsDir(t)

	MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "legacy",
		"description": "Legacy peek.",
		"code":        "print('legacy')",
		"hints":       "Legacy hint.",
	})

	res, _ := ShowScriptTool{}.Execute(context.Background(), map[string]any{
		"name": "legacy",
	})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("show_script returned non-map: %T", res)
	}
	if m["name"] != "legacy" {
		t.Errorf("show_script name mismatch: %v", m["name"])
	}
	if _, exists := m["code"]; !exists {
		t.Errorf("show_script should still return code; got keys: %v", mapKeys(m))
	}
}

// TestRemoveScript proves the new tool deletes a script.
func TestRemoveScript(t *testing.T) {
	withTempScriptsDir(t)

	MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "todelete",
		"description": "Will be removed.",
		"code":        "print('bye')",
	})
	if scriptByName(t, "todelete") == nil {
		t.Fatal("setup failed: todelete not in list_scripts")
	}

	res, _ := RemoveScriptTool{}.Execute(context.Background(), map[string]any{
		"name": "todelete",
	})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("remove_script returned non-map: %T", res)
	}
	if _, exists := m["removed"]; !exists {
		t.Errorf("remove_script did not return 'removed': %v", m)
	}
	if scriptByName(t, "todelete") != nil {
		t.Error("todelete still in list_scripts after remove_script")
	}
}

// TestEditScriptPreservesHintsWhenOmitted proves that omitting the hints
// param keeps existing hints intact (None=preserve semantics from scripts.py).
func TestEditScriptPreservesHintsWhenOmitted(t *testing.T) {
	withTempScriptsDir(t)

	MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "keep",
		"description": "Original.",
		"code":        "print('v1')",
		"hints":       "Original hint.",
	})

	// Edit without hints field — should preserve
	EditScriptTool{}.Execute(context.Background(), map[string]any{
		"name": "keep",
		"code": "print('v2')",
	})

	rec := scriptByName(t, "keep")
	if rec == nil {
		t.Fatal("keep missing after edit")
	}
	if h, _ := rec["hints"].(string); !strings.Contains(h, "Original hint") {
		t.Errorf("hints should be preserved; got: %q", h)
	}
}

// TestEditScriptClearsHintsWhenEmptyString proves that hints="" clears the
// section. This is distinct from "omitted" which preserves.
func TestEditScriptClearsHintsWhenEmptyString(t *testing.T) {
	withTempScriptsDir(t)

	MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "clearme",
		"description": "Has hints.",
		"code":        "print('v1')",
		"hints":       "Will be cleared.",
	})

	// Edit WITH explicit empty-string hints — should clear
	EditScriptTool{}.Execute(context.Background(), map[string]any{
		"name":  "clearme",
		"code":  "print('v2')",
		"hints": "",
	})

	rec := scriptByName(t, "clearme")
	if rec == nil {
		t.Fatal("clearme missing after edit")
	}
	if h, _ := rec["hints"].(string); h != "" {
		t.Errorf("hints should be empty after clear; got: %q", h)
	}
}

// TestAllToolsIncludesReadAndRemove verifies the registration list has the
// new tools alongside the existing ones. Catches a registration regression.
func TestAllToolsIncludesReadAndRemove(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range AllTools() {
		seen[tool.Name()] = true
	}
	expected := []string{
		"make_script", "run_script", "list_scripts",
		"edit_script", "read_script", "show_script", "remove_script",
	}
	for _, name := range expected {
		if !seen[name] {
			t.Errorf("AllTools() missing %q (have: %v)", name, seen)
		}
	}
}

// TestSchemaAdvertisesHintsField proves the make_script schema carries the
// hints property — the model needs to see it to use it. Parses the JSON
// schema rather than string-matching.
func TestSchemaAdvertisesHintsField(t *testing.T) {
	schema := MakeScriptTool{}.Schema()
	var parsed struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("make_script schema not valid JSON: %v", err)
	}
	field, ok := parsed.Properties["hints"]
	if !ok {
		t.Fatal("make_script schema missing 'hints' property — model will never discover it")
	}
	if field.Type != "string" {
		t.Errorf("hints field should be string; got %q", field.Type)
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestAvailableScriptsBlockEmpty proves the prompt block is a silent no-op
// when no scripts exist — never injects an empty <available_scripts> tag.
func TestAvailableScriptsBlockEmpty(t *testing.T) {
	withTempScriptsDir(t)
	if got := AvailableScriptsBlock(); got != "" {
		t.Errorf("empty scripts dir should produce empty block; got: %q", got)
	}
}

// TestAvailableScriptsBlockWithScripts proves the block is formatted with
// name, description, and first hint — capped at one line per script.
func TestAvailableScriptsBlockWithScripts(t *testing.T) {
	withTempScriptsDir(t)

	MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "greet",
		"description": "Say hello.",
		"code":        "print('hi')",
		"hints":       "Use for greetings. | Takes one arg. | No side effects.",
	})
	MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "fetch_price",
		"description": "Get stock price.",
		"code":        "print('100.0')",
		"hints":       "Ticker required. | Returns float.",
	})

	got := AvailableScriptsBlock()
	if !strings.Contains(got, "<available_scripts>") {
		t.Errorf("missing tag: %q", got)
	}
	if !strings.Contains(got, "greet:") {
		t.Errorf("missing greet entry: %q", got)
	}
	if !strings.Contains(got, "Say hello.") {
		t.Errorf("missing description: %q", got)
	}
	if !strings.Contains(got, "Use for greetings.") {
		t.Errorf("missing first hint; only the first hint should appear: %q", got)
	}
	// The SECOND hint line ("Takes one arg.") should NOT be in the prompt block —
	// we cap each script at one line, joining only the first hint.
	if strings.Contains(got, "Takes one arg.") {
		t.Errorf("second hint leaked into prompt block: %q", got)
	}
}

// TestAvailableScriptsBlockCapsAtTwenty proves the block stops at 20 entries
// and prints the overflow message rather than spamming the prompt.
func TestAvailableScriptsBlockCapsAtTwenty(t *testing.T) {
	withTempScriptsDir(t)

	// Create 25 scripts
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("script_%02d", i)
		MakeScriptTool{}.Execute(context.Background(), map[string]any{
			"name":        name,
			"description": fmt.Sprintf("Helper #%d", i),
			"code":        "print('x')",
		})
	}

	got := AvailableScriptsBlock()
	// Should mention the overflow
	if !strings.Contains(got, "use list_scripts to see all") {
		t.Errorf("overflow message missing: %q", got)
	}
	// Count bullet lines — should be 20 + 1 overflow line
	bulletCount := strings.Count(got, "\n  - ")
	if bulletCount != 20 {
		t.Errorf("expected 20 bullet entries, got %d", bulletCount)
	}
}
