package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrgManifestValidate_SurrealDBMissingNS proves the validator catches
// the canonical "SURREALDB_URL set but namespace missing" typo. This is the
// exact failure mode that tech-noir hit on first run (SurrealDB parsed
// "tech-noir" as subtraction because the backtick escape was missing — but
// the underlying config was fine; this test guards the next layer down).
func TestOrgManifestValidate_SurrealDBMissingNS(t *testing.T) {
	org := &OrgManifest{
		Name: "test-org",
		Databases: map[string]DatabaseConfig{
			"surrealdb": {
				URL:      "http://localhost:8000",
				Database: "main",
				// Namespace intentionally empty
			},
		},
	}
	errs := org.Validate()
	if len(errs) == 0 {
		t.Fatal("Validate returned no errors for surrealdb without namespace")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "namespace is required") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected namespace-required error, got: %v", errs)
	}
}

// TestOrgManifestValidate_SurrealDBComplete confirms a fully-specified
// surrealdb entry produces no errors.
func TestOrgManifestValidate_SurrealDBComplete(t *testing.T) {
	org := &OrgManifest{
		Name: "test-org",
		Databases: map[string]DatabaseConfig{
			"surrealdb": {
				URL:       "http://localhost:8000",
				Namespace: "research",
				Database:  "main",
			},
		},
	}
	if errs := org.Validate(); len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// TestOrgManifestValidate_PostgresMissingURL verifies the postgres kind
// requires a url field.
func TestOrgManifestValidate_PostgresMissingURL(t *testing.T) {
	org := &OrgManifest{
		Name: "test-org",
		Databases: map[string]DatabaseConfig{
			"postgres": {Username: "root"},
		},
	}
	errs := org.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e, "postgres: url is required") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected postgres url-required error, got: %v", errs)
	}
}

// TestOrgManifestValidate_SharedInitFileMissing proves that an @shared/
// init_files entry that doesn't exist in orgs/_shared/clients/ is caught.
//
// We don't try to construct a fake shared dir — we point at a name that
// definitely doesn't exist in the canonical shared dir. If the shared dir
// can't be located at all, the test is skipped (that's a different failure
// mode covered by TestFindSharedClientsDir).
func TestOrgManifestValidate_SharedInitFileMissing(t *testing.T) {
	if FindSharedClientsDir() == "" {
		t.Skip("shared clients dir not locatable — skip @shared/ file-existence check")
	}

	tmp := t.TempDir()
	// Minimal pux.yaml on disk so SandboxInitFiles() can re-read it.
	puxYaml := `name: test-org
sandbox:
  init_files:
    - "@shared/this_file_does_not_exist.py"
`
	if err := os.WriteFile(filepath.Join(tmp, "pux.yaml"), []byte(puxYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	org := LoadOrgManifest(tmp)
	if org == nil {
		t.Fatal("LoadOrgManifest returned nil")
	}
	errs := org.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e, "this_file_does_not_exist.py") && strings.Contains(e, "_shared") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected @shared/ missing-file error, got: %v", errs)
	}
}

// TestOrgManifestValidate_SharedInitFilePresent confirms @shared/ entries
// that DO exist don't produce errors.
func TestOrgManifestValidate_SharedInitFilePresent(t *testing.T) {
	if FindSharedClientsDir() == "" {
		t.Skip("shared clients dir not locatable")
	}
	tmp := t.TempDir()
	puxYaml := `name: test-org
sandbox:
  init_files:
    - "@shared/surreal_client.py"
`
	if err := os.WriteFile(filepath.Join(tmp, "pux.yaml"), []byte(puxYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	org := LoadOrgManifest(tmp)
	if org == nil {
		t.Fatal("LoadOrgManifest returned nil")
	}
	// Filter out any errors about missing surreal_client.py specifically.
	for _, e := range org.Validate() {
		if strings.Contains(e, "surreal_client.py") {
			t.Errorf("expected surreal_client.py to exist in shared dir, got error: %s", e)
		}
	}
}

// TestOrgManifestValidate_SharedSandboxSubdir proves the @shared/ resolver
// honors explicit subdirs — @shared/sandbox/X.py resolves to
// orgs/_shared/sandbox/X.py, NOT orgs/_shared/clients/sandbox/X.py. This
// locks down the extension that unblocked twitter-agent/telegram-agent
// wiring (their session scripts live under _shared/sandbox/, not clients/).
func TestOrgManifestValidate_SharedSandboxSubdir(t *testing.T) {
	if root, err := FindSharedRoot(); err != nil {
		t.Skipf("shared root not locatable: %v", err)
	} else if _, err := os.Stat(filepath.Join(root, "sandbox", "twitter_session.py")); err != nil {
		t.Skipf("twitter_session.py not in _shared/sandbox/ — run org-build first: %v", err)
	}

	tmp := t.TempDir()
	puxYaml := `name: test-org
sandbox:
  init_files:
    - "@shared/sandbox/twitter_session.py"
    - "@shared/sandbox/telegram_session.py"
`
	if err := os.WriteFile(filepath.Join(tmp, "pux.yaml"), []byte(puxYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	org := LoadOrgManifest(tmp)
	if org == nil {
		t.Fatal("LoadOrgManifest returned nil")
	}
	for _, e := range org.Validate() {
		if strings.Contains(e, "twitter_session.py") || strings.Contains(e, "telegram_session.py") {
			t.Errorf("expected @shared/sandbox/ resolution to succeed, got error: %s", e)
		}
	}
}

// TestOrgManifestValidate_SharedSandboxSubdirMissing confirms the validator
// still catches a typo'd @shared/sandbox/ path — extension didn't weaken
// the existence check.
func TestOrgManifestValidate_SharedSandboxSubdirMissing(t *testing.T) {
	if _, err := FindSharedRoot(); err != nil {
		t.Skip("shared root not locatable")
	}
	tmp := t.TempDir()
	puxYaml := `name: test-org
sandbox:
  init_files:
    - "@shared/sandbox/this_does_not_exist.py"
`
	if err := os.WriteFile(filepath.Join(tmp, "pux.yaml"), []byte(puxYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	org := LoadOrgManifest(tmp)
	if org == nil {
		t.Fatal("LoadOrgManifest returned nil")
	}
	errs := org.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e, "this_does_not_exist.py") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected missing @shared/sandbox/ file to be flagged, got: %v", errs)
	}
}
