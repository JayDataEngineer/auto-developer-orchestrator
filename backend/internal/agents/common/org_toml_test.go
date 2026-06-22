package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrgStructuralContracts enforces the Stage-3 templating contract:
//
//  1. Every org ships an org.toml (the renderer source-of-truth).
//  2. Every generated file (pux.yaml, roles/*/config.yaml, tool_packages/*.yaml)
//     carries the `# AUTO-GENERATED` header so the renderer can safely
//     overwrite it.
//
// Why: without this, an engineer could hand-edit pux.yaml and have it
// silently overwritten (or never overwritten) by the renderer. The header
// is the contract marker.
//
// TestOrgsDirectoryAudit covers the *content* side — that pux.yaml parses
// as a valid OrgManifest and roles load. This test covers the *source*
// side — that org.toml exists and nothing bypassed the renderer.
func TestOrgStructuralContracts(t *testing.T) {
	orgsDir := findOrgsDir(t)
	if orgsDir == "" {
		t.Skip("orgs/ directory not found relative to test file")
	}

	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", orgsDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		name := entry.Name()
		orgPath := filepath.Join(orgsDir, name)

		t.Run(name, func(t *testing.T) {
			// 1. Every org MUST ship an org.toml.
			orgToml := filepath.Join(orgPath, "org.toml")
			if _, err := os.Stat(orgToml); err != nil {
				t.Fatalf("%s: missing org.toml — Stage 3 contract requires it", name)
			}

			// 2. pux.yaml must exist AND carry the AUTO-GENERATED header.
			assertGeneratedHeader(t, filepath.Join(orgPath, "pux.yaml"))

			// 3. Every roles/*/config.yaml must carry the header.
			rolesDir := filepath.Join(orgPath, "roles")
			if roleEntries, err := os.ReadDir(rolesDir); err == nil {
				for _, re := range roleEntries {
					if !re.IsDir() {
						continue
					}
					cfg := filepath.Join(rolesDir, re.Name(), "config.yaml")
					if _, err := os.Stat(cfg); err == nil {
						assertGeneratedHeader(t, cfg)
					}
				}
			}

			// 4. Every tool_packages/*.yaml must carry the header.
			pkgsDir := filepath.Join(orgPath, "tool_packages")
			if pkgEntries, err := os.ReadDir(pkgsDir); err == nil {
				for _, pe := range pkgEntries {
					if pe.IsDir() || !strings.HasSuffix(pe.Name(), ".yaml") {
						continue
					}
					assertGeneratedHeader(t, filepath.Join(pkgsDir, pe.Name()))
				}
			}
		})
	}
}

func assertGeneratedHeader(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("%s: cannot read: %v", path, err)
		return
	}
	text := strings.TrimSpace(string(data))
	if !strings.HasPrefix(text, "# AUTO-GENERATED") {
		t.Errorf("%s: missing '# AUTO-GENERATED' header — file was hand-edited "+			"(renderer will refuse to overwrite, then drift). Re-render via "+
			"`task org-build`.", path)
	}
}
