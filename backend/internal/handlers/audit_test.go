package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveProjectRootEnvVar — PROJECT_ROOT always wins when set.
// This is the load-bearing case for the audit handler: when the server is
// started via `task dev` from repo root, PROJECT_ROOT is set and points at
// the right place regardless of where the binary's cwd ends up.
func TestResolveProjectRootEnvVar(t *testing.T) {
	t.Setenv("PROJECT_ROOT", "/tmp/some-project-root")
	got := resolveProjectRoot()
	if got != "/tmp/some-project-root" {
		t.Fatalf("expected /tmp/some-project-root, got %s", got)
	}
}

// TestResolveProjectRootWalkUp — when PROJECT_ROOT is unset, walk up to find
// a dir containing scripts/audit_transcript.py. From the test file's location
// (backend/internal/handlers/), that's the repo root, 4 levels up.
func TestResolveProjectRootWalkUp(t *testing.T) {
	os.Unsetenv("PROJECT_ROOT")

	// Determine the repo root by walking up from this test file.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := wd
	for {
		if _, err := os.Stat(filepath.Join(want, "scripts", "audit_transcript.py")); err == nil {
			break
		}
		parent := filepath.Dir(want)
		if parent == want {
			t.Skip("could not locate scripts/audit_transcript.py — running outside repo?")
		}
		want = parent
	}

	got := resolveProjectRoot()
	if got != want {
		t.Fatalf("resolveProjectRoot: want %s, got %s", want, got)
	}
}

// TestResolveProjectRootFallback — if PROJECT_ROOT unset and no scripts/ dir
// in any ancestor (8 levels), fall back to cwd.
func TestResolveProjectRootFallback(t *testing.T) {
	t.Setenv("PROJECT_ROOT", "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Move to /tmp where no scripts/ dir exists.
	tmp := t.TempDir()
	t.Chdir(tmp)
	got := resolveProjectRoot()
	// t.Chdir makes tmp the new cwd, so cwd-based fallback == tmp.
	absTmp, _ := filepath.Abs(tmp)
	if got != absTmp && !strings.HasPrefix(got, tmp) {
		t.Fatalf("expected fallback to cwd %s, got %s", absTmp, got)
	}
	_ = wd
}
