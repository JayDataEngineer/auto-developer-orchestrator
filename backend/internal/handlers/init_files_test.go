package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveInitFileLocalPath_LocalBranch verifies the legacy path: an
// init_files entry without the "@shared/" prefix resolves against the project
// directory.
func TestResolveInitFileLocalPath_LocalBranch(t *testing.T) {
	projectDir := t.TempDir()
	rel := "sandbox/foo.py"
	got, err := resolveInitFileLocalPath(rel, projectDir)
	if err != nil {
		t.Fatalf("local branch returned error: %v", err)
	}
	want := filepath.Join(projectDir, rel)
	if got != want {
		t.Errorf("local branch: got %q, want %q", got, want)
	}
}

// TestResolveInitFileLocalPath_SharedBranch verifies the @shared/ prefix
// resolves against orgs/_shared/clients/ (when it can be found).
//
// If _shared/clients/ cannot be located in this checkout the test is skipped
// — the finder logic is independently covered in the common package.
func TestResolveInitFileLocalPath_SharedBranch(t *testing.T) {
	projectDir := t.TempDir()
	// Touch a fake project file so the project dir is non-empty.
	if err := os.MkdirAll(filepath.Join(projectDir, "sandbox"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveInitFileLocalPath("@shared/surreal_client.py", projectDir)
	if err != nil {
		t.Fatalf("@shared branch returned error: %v", err)
	}

	// The resolved path must NOT live inside the project dir.
	if strings.HasPrefix(got, projectDir) {
		t.Errorf("@shared entry resolved into project dir: %q starts with %q", got, projectDir)
	}

	// It must end with surreal_client.py.
	if filepath.Base(got) != "surreal_client.py" {
		t.Errorf("@shared entry resolved to unexpected basename: %q", filepath.Base(got))
	}

	// The file must actually exist on disk.
	if _, err := os.Stat(got); err != nil {
		t.Errorf("@shared entry resolved to a path that does not exist: %v", err)
	}
}

// TestResolveInitFileLocalPath_UnknownSharedWhenUnset verifies the error
// branch: when PROJECT_ROOT is unset and the test is running from somewhere
// without orgs/_shared/ nearby, an @shared entry returns an error rather
// than silently resolving to a wrong path.
//
// This test forces the failure by clearing PROJECT_ROOT and chdir-ing to a
// temp dir with no orgs/_shared/ ancestor. If a shared clients dir happens to
// be findable from a tmp dir anyway (because the finder walks up to /), this
// test is skipped — non-fatal.
func TestResolveInitFileLocalPath_UnknownSharedWhenUnset(t *testing.T) {
	prev := os.Getenv("PROJECT_ROOT")
	t.Cleanup(func() { os.Setenv("PROJECT_ROOT", prev) })
	os.Unsetenv("PROJECT_ROOT")

	// Run in a temp dir that has no orgs/_shared ancestor (hopefully).
	tmp := t.TempDir()
	prevWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Skipf("cannot chdir to temp: %v", err)
	}
	t.Cleanup(func() { os.Chdir(prevWd) })

	_, err := resolveInitFileLocalPath("@shared/missing.py", tmp)
	if err == nil {
		// The finder walked up and found a real orgs/_shared. That's fine —
		// it means the shared dir is reachable from this environment, so the
		// error branch can't be exercised here.
		t.Skip("shared clients dir was locatable from tmp — error branch not reachable")
	}
	if !strings.Contains(err.Error(), "orgs/_shared/clients/") {
		t.Errorf("error should mention orgs/_shared/clients/, got: %v", err)
	}
}
