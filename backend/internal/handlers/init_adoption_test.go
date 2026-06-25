package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/manifest"
	"go.uber.org/zap"
)

// fakeSandboxInitializer records every InitFromManifest call so tests can
// assert that the adoption path actually triggered init. Returns whatever
// result + err the test injected.
type fakeSandboxInitializer struct {
	calls          []initCall
	result         *SandboxInitResult
	err            error
	skipRecording  bool
}

type initCall struct {
	SandboxID   string
	ProjectName string
	ProjectDir  string
	InitFiles   []string
}

func (f *fakeSandboxInitializer) InitFromManifest(ctx context.Context, projectName string, sandboxCfg *manifest.SandboxConfig, projectDir string) *SandboxInitResult {
	if !f.skipRecording {
		files := []string{}
		if sandboxCfg != nil {
			files = sandboxCfg.InitFiles
		}
		f.calls = append(f.calls, initCall{
			SandboxID:   projectName, // prompt path passes sandboxID here
			ProjectName: projectName,
			ProjectDir:  projectDir,
			InitFiles:   files,
		})
	}
	if f.result == nil {
		return &SandboxInitResult{FilesUploaded: len(f.calls)}
	}
	return f.result
}

func (f *fakeSandboxInitializer) InitIfSandboxExists(ctx context.Context, projectName string, sandboxCfg *manifest.SandboxConfig, projectDir string) *SandboxInitResult {
	return f.InitFromManifest(ctx, projectName, sandboxCfg, projectDir)
}

// writePuxYaml creates a temp dir with a pux.yaml declaring the given
// init_files. Used to set up the manifest load that ensureOrgSandboxInit does.
func writePuxYaml(t *testing.T, initFiles []string) string {
	t.Helper()
	dir := t.TempDir()
	var filesYaml strings.Builder
	for _, f := range initFiles {
		filesYaml.WriteString("    - \"" + f + "\"\n")
	}
	body := `
name: test-org
sandbox:
  tier: standard
  image: pux-sandbox:latest
  init_files:
` + filesYaml.String() + `
`
	if err := os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(body), 0644); err != nil {
		t.Fatalf("write pux.yaml: %v", err)
	}
	return dir
}

// TestEnsureOrgSandboxInitRunsForAdoptedContainer proves the helper invokes
// InitFromManifest when a manifest is present. This is the regression test
// for the silent adoption gap: a container started by `docker compose up`
// via org bootstrap.sh has zero init_files at /sandbox/*.py until this
// helper runs. Without this test, the wiring can break without any signal.
func TestEnsureOrgSandboxInitRunsForAdoptedContainer(t *testing.T) {
	dir := writePuxYaml(t, []string{
		"@shared/clients/surreal_client.py",
		"sandbox/context_engine.py",
	})
	fake := &fakeSandboxInitializer{}

	res := ensureOrgSandboxInit(context.Background(), fake, zap.NewNop(), "sb-adopt-1", dir, dir)

	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 InitFromManifest call, got %d", len(fake.calls))
	}
	call := fake.calls[0]
	if call.SandboxID != "sb-adopt-1" {
		t.Errorf("expected sandboxID sb-adopt-1, got %q", call.SandboxID)
	}
	if call.ProjectDir != dir {
		t.Errorf("expected projectDir=%s, got %q", dir, call.ProjectDir)
	}
	if len(call.InitFiles) != 2 {
		t.Errorf("expected 2 init_files, got %d", len(call.InitFiles))
	}
	if res == nil {
		t.Error("expected non-nil result when manifest is present")
	}
}

// TestEnsureOrgSandboxInitNoManifest verifies the helper is a no-op when
// the org has no pux.yaml — preserves backward compat for non-org projects.
func TestEnsureOrgSandboxInitNoManifest(t *testing.T) {
	dir := t.TempDir() // no pux.yaml
	fake := &fakeSandboxInitializer{}

	res := ensureOrgSandboxInit(context.Background(), fake, zap.NewNop(), "sb-x", dir, dir)

	if len(fake.calls) != 0 {
		t.Errorf("expected 0 calls when no manifest, got %d", len(fake.calls))
	}
	if res != nil {
		t.Errorf("expected nil result when no manifest, got %+v", res)
	}
}

// TestEnsureOrgSandboxInitNilInitializer verifies nil SandboxInitializer
// doesn't panic — defensive for paths where wiring is optional (e.g. tests
// that don't construct a full app).
func TestEnsureOrgSandboxInitNilInitializer(t *testing.T) {
	dir := writePuxYaml(t, []string{"sandbox/foo.py"})

	// Should not panic.
	res := ensureOrgSandboxInit(context.Background(), nil, zap.NewNop(), "sb-x", dir, dir)
	if res != nil {
		t.Errorf("expected nil result when initializer is nil, got %+v", res)
	}
}

// TestEnsureOrgSandboxInitEmptyOrgPath verifies empty org path is a no-op
// — caller error, not a crash.
func TestEnsureOrgSandboxInitEmptyOrgPath(t *testing.T) {
	fake := &fakeSandboxInitializer{}

	res := ensureOrgSandboxInit(context.Background(), fake, zap.NewNop(), "sb-x", "", "")

	if len(fake.calls) != 0 {
		t.Errorf("expected 0 calls for empty org path, got %d", len(fake.calls))
	}
	if res != nil {
		t.Errorf("expected nil result for empty org path, got %+v", res)
	}
}

// TestEnsureOrgSandboxInitManifestWithoutSandbox verifies a manifest that
// has no [sandbox] block is a no-op. Non-org pux.yaml (legacy) or orgs
// without sandbox tiers.
func TestEnsureOrgSandboxInitManifestWithoutSandbox(t *testing.T) {
	dir := t.TempDir()
	body := `
name: test-org
description: has pux.yaml but no sandbox block
`
	if err := os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(body), 0644); err != nil {
		t.Fatalf("write pux.yaml: %v", err)
	}
	fake := &fakeSandboxInitializer{}

	res := ensureOrgSandboxInit(context.Background(), fake, zap.NewNop(), "sb-x", dir, dir)

	if len(fake.calls) != 0 {
		t.Errorf("expected 0 calls when manifest has no sandbox block, got %d", len(fake.calls))
	}
	if res != nil {
		t.Errorf("expected nil result, got %+v", res)
	}
}

// TestEnsureOrgSandboxInitLogsErrors verifies that when InitFromManifest
// returns errors, they're logged — preserves the audit trail. We can't
// easily assert log content without a zap observer, so we just verify
// the function doesn't swallow the result + still returns it.
func TestEnsureOrgSandboxInitReturnsErrors(t *testing.T) {
	dir := writePuxYaml(t, []string{"sandbox/foo.py"})
	fake := &fakeSandboxInitializer{
		result: &SandboxInitResult{
			Errors: []string{"docker copy failed: permission denied"},
		},
	}

	res := ensureOrgSandboxInit(context.Background(), fake, zap.NewNop(), "sb-x", dir, dir)

	if res == nil {
		t.Fatal("expected non-nil result with errors")
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(res.Errors))
	}
	if !strings.Contains(res.Errors[0], "permission denied") {
		t.Errorf("expected 'permission denied' in error, got %q", res.Errors[0])
	}
}

// TestPuxHandlerSandboxInitializerWired proves the field exists on PuxHandler
// and is settable. This catches the case where someone refactors the struct
// and forgets to keep the field — without it, ensureOrgSandboxInit gets nil
// and the adoption gap silently returns.
func TestPuxHandlerSandboxInitializerWired(t *testing.T) {
	// Static check: PuxHandler has a SetSandboxInitializer method that
	// stores the initializer. We construct a fake handler without going
	// through NewPuxHandler (which needs DB, gitOps, githubHandler).
	h := &PuxHandler{log: zap.NewNop()}
	fake := &fakeSandboxInitializer{}

	h.SetSandboxInitializer(fake)

	if h.sandboxIn == nil {
		t.Fatal("SetSandboxInitializer did not wire sandboxIn field")
	}
	// Verify it's the same pointer we passed.
	got, ok := h.sandboxIn.(*fakeSandboxInitializer)
	if !ok {
		t.Fatalf("expected *fakeSandboxInitializer, got %T", h.sandboxIn)
	}
	if got != fake {
		t.Error("stored initializer is not the one we passed")
	}
}

// TestSandboxHandlerSandboxInitializerWired proves the same field + setter
// exist on SandboxHandler (the standalone /api/sandbox POST path).
func TestSandboxHandlerSandboxInitializerWired(t *testing.T) {
	h := &SandboxHandler{logger: zap.NewNop()}
	fake := &fakeSandboxInitializer{}

	h.SetSandboxInitializer(fake)

	if h.sandboxIn == nil {
		t.Fatal("SetSandboxInitializer did not wire sandboxIn field")
	}
	got, ok := h.sandboxIn.(*fakeSandboxInitializer)
	if !ok || got != fake {
		t.Errorf("stored initializer mismatch: %T", h.sandboxIn)
	}
}

// TestEnsureOrgSandboxInitSurvivesLoadManifestErr verifies that if the
// manifest is malformed (LoadManifest returns err), the helper doesn't
// crash — it treats the err as "no manifest" and skips. This is the
// correct behavior: a malformed manifest shouldn't break the prompt path.
func TestEnsureOrgSandboxInitSurvivesLoadManifestErr(t *testing.T) {
	dir := t.TempDir()
	// Malformed YAML — unclosed quote.
	if err := os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte("sandbox: {image: 'broken"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fake := &fakeSandboxInitializer{}

	// Should not panic, should not call InitFromManifest.
	res := ensureOrgSandboxInit(context.Background(), fake, zap.NewNop(), "sb-x", dir, dir)

	if len(fake.calls) != 0 {
		t.Errorf("expected 0 calls for malformed manifest, got %d", len(fake.calls))
	}
	if res != nil {
		t.Errorf("expected nil result for malformed manifest, got %+v", res)
	}
}
