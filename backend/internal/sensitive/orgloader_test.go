package sensitive

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrgCredentialsAcceptsPathOrName is a regression test for the bug
// where the orch CLI sets req.Org to the resolved org PATH (not the bare org
// name), so LoadOrgCredentials was looking for
// ~/.pux/credentials//home/.../deep-research-engine.json and always missing.
func TestLoadOrgCredentialsAcceptsPathOrName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PUX_CREDENTIALS_PATH", "") // bypass env override

	const body = `{
	  "version": 1,
	  "secrets": {
	    "openrouter": {"api_key": "sk-test-value"},
	    "media":      {"pyannote_token": "hf-test"}
	  }
	}`

	credDir := filepath.Join(tmp, ".pux", "credentials")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "deep-research-engine.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		input string
	}{
		{"bare name", "deep-research-engine"},
		{"absolute path", filepath.Join(tmp, ".pux", "orgs", "deep-research-engine")},
		{"trailing slash", filepath.Join(tmp, ".pux", "orgs", "deep-research-engine") + "/"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := NewStore()
			n, err := LoadOrgCredentials(store, c.input)
			if err != nil {
				t.Fatalf("LoadOrgCredentials(%q): %v", c.input, err)
			}
			if n != 2 {
				t.Fatalf("LoadOrgCredentials(%q): expected 2 secrets, got %d", c.input, n)
			}
			got := store.Resolve("<secret>openrouter.api_key</secret>")
			if got != "sk-test-value" {
				t.Fatalf("placeholder not resolved for %q: got %q", c.input, got)
			}
		})
	}
}

// TestLoadOrgCredentialsMissingFileOK confirms a missing credentials file
// returns (0, nil) — never an error. This is what lets orgs run without creds.
func TestLoadOrgCredentialsMissingFileOK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PUX_CREDENTIALS_PATH", "")

	store := NewStore()
	n, err := LoadOrgCredentials(store, "nonexistent-org")
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if n != 0 {
		t.Fatalf("missing file should load 0 secrets, got %d", n)
	}
}

// TestLoadOrgCredentialsEnvOverride confirms $PUX_CREDENTIALS_PATH takes
// precedence over the per-org file lookup. Useful for tests + bootstrap scripts.
func TestLoadOrgCredentialsEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	overridePath := filepath.Join(tmp, "custom-creds.json")
	body := `{"version":1,"secrets":{"custom":{"key":"val1"}}}`
	if err := os.WriteFile(overridePath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PUX_CREDENTIALS_PATH", overridePath)
	t.Setenv("HOME", tmp)

	store := NewStore()
	n, err := LoadOrgCredentials(store, "anything-here")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 secret via override, got %d", n)
	}
	if store.Resolve("<secret>custom.key</secret>") != "val1" {
		t.Fatal("override secret not resolved")
	}
}
