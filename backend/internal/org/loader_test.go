package org

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testdataDir is the absolute path to this package's testdata/ fixture root.
// Computed from the source file location so the test runs regardless of the
// caller's working directory (go test runs from the package dir, but `go test
// -C` / IDE runners vary).
func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate testdata/")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

func TestLoadOne_ValidOrg(t *testing.T) {
	dir := filepath.Join(testdataDir(t), "valid")
	org, err := LoadOne(dir)
	if err != nil {
		t.Fatalf("LoadOne: unexpected error: %v", err)
	}
	if org.Name != "valid" {
		t.Errorf("Name = %q, want %q", org.Name, "valid")
	}
	if org.Description == "" {
		t.Error("Description is empty")
	}
	if org.SandboxImage != "pux-sandbox:latest" {
		t.Errorf("SandboxImage = %q", org.SandboxImage)
	}

	// CTO loads from cto.md with the configured tools + round budget.
	if org.CTO.Name != "cto" {
		t.Errorf("CTO.Name = %q, want %q", org.CTO.Name, "cto")
	}
	if !strings.Contains(org.CTO.Prompt, "Valid Org CTO") {
		t.Errorf("CTO.Prompt doesn't include cto.md body: %q", org.CTO.Prompt)
	}
	if org.CTO.MaxRounds != 5 {
		t.Errorf("CTO.MaxRounds = %d, want 5", org.CTO.MaxRounds)
	}
	if len(org.CTO.Tools) != 3 {
		t.Errorf("CTO.Tools len = %d, want 3", len(org.CTO.Tools))
	}

	// Researcher role loaded from roles/researcher.md.
	r, ok := org.Roles["researcher"]
	if !ok {
		t.Fatal("Roles missing 'researcher'")
	}
	if !strings.Contains(r.Prompt, "Researcher") {
		t.Errorf("researcher prompt body missing: %q", r.Prompt)
	}
	if r.MaxRounds != 3 {
		t.Errorf("researcher MaxRounds = %d, want 3", r.MaxRounds)
	}
}

func TestLoadOne_MalformedTOML(t *testing.T) {
	dir := filepath.Join(testdataDir(t), "malformed_org")
	_, err := LoadOne(dir)
	if err == nil {
		t.Fatal("LoadOne returned nil error for malformed TOML")
	}
	if !strings.Contains(err.Error(), "parse org.toml") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestLoadOne_MissingPromptFile(t *testing.T) {
	dir := filepath.Join(testdataDir(t), "missing_prompt_org")
	_, err := LoadOne(dir)
	if err == nil {
		t.Fatal("LoadOne returned nil error for missing prompt file")
	}
	if !strings.Contains(err.Error(), "load cto prompt") {
		t.Errorf("expected 'load cto prompt' wrap, got: %v", err)
	}
}

func TestLoadOne_DuplicateRoles(t *testing.T) {
	dir := filepath.Join(testdataDir(t), "dup_roles_org")
	_, err := LoadOne(dir)
	if err == nil {
		t.Fatal("LoadOne returned nil error for duplicate role names")
	}
	if !strings.Contains(err.Error(), "duplicate role") {
		t.Errorf("expected duplicate-role error, got: %v", err)
	}
}

func TestLoadOne_EmptyRoleName(t *testing.T) {
	dir := filepath.Join(testdataDir(t), "empty_role_name_org")
	_, err := LoadOne(dir)
	if err == nil {
		t.Fatal("LoadOne returned nil error for empty role name")
	}
	if !strings.Contains(err.Error(), "missing name") {
		t.Errorf("expected 'missing name' error, got: %v", err)
	}
}

func TestLoadOne_NoCTOBlock(t *testing.T) {
	dir := filepath.Join(testdataDir(t), "no_cto_org")
	_, err := LoadOne(dir)
	if err == nil {
		t.Fatal("LoadOne returned nil error for missing [cto] block")
	}
	// readPromptFile gets an empty path → surfaces as "load cto prompt: ... prompt path is empty"
	if !strings.Contains(err.Error(), "cto prompt") {
		t.Errorf("expected cto-prompt error, got: %v", err)
	}
}

func TestLoadOne_NoName(t *testing.T) {
	dir := filepath.Join(testdataDir(t), "no_name_org")
	_, err := LoadOne(dir)
	if err == nil {
		t.Fatal("LoadOne returned nil error for missing top-level name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected name-required error, got: %v", err)
	}
}

func TestLoader_LoadAll_SkipsBrokenOrgs(t *testing.T) {
	// Point Loader at testdata/, which has 1 valid + 5 broken fixtures.
	// LoadAll should return ONLY the valid one, not error out.
	l := &Loader{root: testdataDir(t)}
	orgs, err := l.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: unexpected error: %v", err)
	}
	if len(orgs) != 1 {
		names := make([]string, len(orgs))
		for i, o := range orgs {
			names[i] = o.Name
		}
		t.Fatalf("LoadAll returned %d orgs (%v), want 1 (only valid)", len(orgs), names)
	}
	if orgs[0].Name != "valid" {
		t.Errorf("LoadAll[0].Name = %q, want %q", orgs[0].Name, "valid")
	}
}

func TestLoader_LoadAll_SortedByName(t *testing.T) {
	// Build a tmp dir with three valid orgs whose names are out of order.
	dir := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "mango"} {
		orgDir := filepath.Join(dir, name)
		if err := writeFixtureFiles(orgDir, name); err != nil {
			t.Fatal(err)
		}
	}

	l := &Loader{root: dir}
	orgs, err := l.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(orgs) != 3 {
		t.Fatalf("LoadAll returned %d orgs, want 3", len(orgs))
	}
	got := []string{orgs[0].Name, orgs[1].Name, orgs[2].Name}
	want := []string{"alpha", "mango", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("orgs[%d].Name = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestLoader_LoadAll_EmptyDirIsNil(t *testing.T) {
	// Fresh TempDir → no orgs/ children → LoadAll returns nil, nil.
	l := &Loader{root: t.TempDir()}
	orgs, err := l.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll on empty dir: %v", err)
	}
	if orgs != nil {
		t.Fatalf("LoadAll returned %v, want nil", orgs)
	}
}

func TestLoader_LoadAll_MissingRootIsNil(t *testing.T) {
	// Non-existent root directory behaves like empty: nil, nil. So list_orgs
	// works on a fresh project with no orgs/ created yet.
	l := &Loader{root: filepath.Join(t.TempDir(), "does-not-exist")}
	orgs, err := l.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll on missing dir: %v", err)
	}
	if orgs != nil {
		t.Fatalf("LoadAll returned %v, want nil", orgs)
	}
}

func TestLoader_LoadByName(t *testing.T) {
	l := &Loader{root: testdataDir(t)}

	org, err := l.LoadByName("valid")
	if err != nil {
		t.Fatalf("LoadByName(valid): %v", err)
	}
	if org.Name != "valid" {
		t.Errorf("Name = %q", org.Name)
	}

	// Missing name surfaces an error (not a panic).
	if _, err := l.LoadByName("does-not-exist"); err == nil {
		t.Error("LoadByName(missing) returned nil error")
	}
	if _, err := l.LoadByName(""); err == nil {
		t.Error("LoadByName('') returned nil error")
	}
}

func TestLoader_HiddenDirsSkipped(t *testing.T) {
	// A .dot directory under root should NOT be treated as an org.
	dir := t.TempDir()
	if err := writeFixtureFiles(filepath.Join(dir, "real"), "real"); err != nil {
		t.Fatal(err)
	}
	// Drop a hidden directory that would crash LoadOne (no org.toml).
	if err := writeFixtureFiles(filepath.Join(dir, ".cache"), ".cache"); err != nil {
		// .cache is a valid fixture — but we only need the directory to be
		// skipped, so the test passes regardless of its contents. Write
		// succeeds; not a problem if it doesn't.
		_ = err
	}

	l := &Loader{root: dir}
	orgs, err := l.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(orgs) != 1 {
		names := make([]string, 0, len(orgs))
		for _, o := range orgs {
			names = append(names, o.Name)
		}
		t.Fatalf("expected only 'real' (hidden dir skipped); got %v", names)
	}
	if orgs[0].Name != "real" {
		t.Errorf("got %q, want 'real'", orgs[0].Name)
	}
}

// writeFixtureFiles creates a minimal valid org directory at orgDir whose
// name+description match the supplied `name` argument. Used by tests that
// need to populate a tmpdir with multiple orgs.
func writeFixtureFiles(orgDir, name string) error {
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		return err
	}
	tomlBody := "name = " + quote(name) + "\n" +
		"description = " + quote("generated by test") + "\n" +
		"\n" +
		"[cto]\n" +
		"prompt = " + quote("cto.md") + "\n" +
		"max_rounds = 5\n" +
		"tools = [" + quote("bash") + "]\n"
	files := map[string]string{
		"org.toml": tomlBody,
		"cto.md":   "# " + name + " CTO\n",
	}
	for path, body := range files {
		full := filepath.Join(orgDir, path)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// quote wraps s as a TOML basic string. Fixture-quality only.
func quote(s string) string {
	return "\"" + s + "\""
}
