package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMCPServersParsesRows(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "mcp_servers.yaml"), []byte(`
servers:
  - prefix: web
    url: http://example.com/research/mcp
  - prefix: media
    url: http://example.com/media/mcp
`), 0o644)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	servers := LoadMCPServers(dir)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d: %+v", len(servers), servers)
	}
	if servers[0].Prefix != "web" || servers[0].URL != "http://example.com/research/mcp" {
		t.Errorf("row 0 mismatch: %+v", servers[0])
	}
	if servers[1].Prefix != "media" || servers[1].URL != "http://example.com/media/mcp" {
		t.Errorf("row 1 mismatch: %+v", servers[1])
	}
}

func TestLoadMCPServersMissingFileReturnsNil(t *testing.T) {
	dir := t.TempDir()
	if got := LoadMCPServers(dir); got != nil {
		t.Errorf("missing file should return nil, got %v", got)
	}
}

func TestLoadMCPServersEmptyDirReturnsNil(t *testing.T) {
	if got := LoadMCPServers(""); got != nil {
		t.Errorf("empty configDir should return nil, got %v", got)
	}
}

func TestLoadMCPServersSkipsRowsMissingRequiredFields(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "mcp_servers.yaml"), []byte(`
servers:
  - prefix: web
    url: http://example.com/mcp
  - prefix: ""            # missing prefix → drop
    url: http://example.com/mcp
  - prefix: media
    url: ""               # missing URL → drop
  - prefix: meta
    url: http://example.com/meta
`), 0o644)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	servers := LoadMCPServers(dir)
	if len(servers) != 2 {
		t.Fatalf("expected 2 valid servers (web, meta), got %d: %+v", len(servers), servers)
	}
	if servers[0].Prefix != "web" {
		t.Errorf("row 0 should be web, got %s", servers[0].Prefix)
	}
	if servers[1].Prefix != "meta" {
		t.Errorf("row 1 should be meta, got %s", servers[1].Prefix)
	}
}

func TestLoadMCPServersMalformedYAMLReturnsNil(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "mcp_servers.yaml"), []byte(`: ! ! !`), 0o644)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if got := LoadMCPServers(dir); got != nil {
		t.Errorf("malformed YAML should return nil silently, got %v", got)
	}
}

func TestMCPServerURLOverrideReadsEnvVar(t *testing.T) {
	t.Setenv("MCP_WEB_URL", "http://localhost:9000/mcp")
	if got := MCPServerURLOverride("web"); got != "http://localhost:9000/mcp" {
		t.Errorf("env override mismatch: got %q", got)
	}
	if got := MCPServerURLOverride("media"); got != "" {
		t.Errorf("unset env var should return empty, got %q", got)
	}
}

// TestMCPServerFallbackURLOverrideReadsEnvVar verifies the fallback URL env-var
// override mirrors the primary URL override. Names an explicit env-var convention
// (MCP_<PREFIX>_FALLBACK_URL) so future contributors don't reinvent it.
func TestMCPServerFallbackURLOverrideReadsEnvVar(t *testing.T) {
	t.Setenv("MCP_MEDIA_FALLBACK_URL", "http://fallback.example.com/mcp")
	if got := MCPServerFallbackURLOverride("media"); got != "http://fallback.example.com/mcp" {
		t.Errorf("fallback env override mismatch: got %q", got)
	}
	if got := MCPServerFallbackURLOverride("web"); got != "" {
		t.Errorf("unset fallback env var should return empty, got %q", got)
	}
}

// TestLoadMCPServersParsesFallbackURL verifies the YAML schema accepts the
// new fallback_url field without disturbing rows that don't set it.
func TestLoadMCPServersParsesFallbackURL(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "mcp_servers.yaml"), []byte(`
servers:
  - prefix: web
    url: http://primary.example.com/mcp
    fallback_url: http://fallback.example.com/mcp
  - prefix: media
    url: http://media.example.com/mcp
`), 0o644)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	servers := LoadMCPServers(dir)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	if servers[0].FallbackURL != "http://fallback.example.com/mcp" {
		t.Errorf("row 0 fallback_url mismatch: got %q", servers[0].FallbackURL)
	}
	if servers[1].FallbackURL != "" {
		t.Errorf("row 1 fallback_url should default to empty, got %q", servers[1].FallbackURL)
	}
}

func TestLoadMCPServersRealConfigParses(t *testing.T) {
	// The checked-in config/mcp_servers.yaml must always parse and yield at
	// least the four legacy cloud MCPs. Catches a broken file before CI does.
	cfgDir := FindKernelConfigDir()
	if cfgDir == "" {
		t.Skip("no kernel config dir (PROJECT_ROOT unset?)")
	}
	servers := LoadMCPServers(cfgDir)
	if len(servers) < 4 {
		t.Fatalf("config/mcp_servers.yaml should declare ≥4 servers (web/media/meta/equibles), got %d: %+v",
			len(servers), servers)
	}
	seen := map[string]bool{}
	for _, s := range servers {
		seen[s.Prefix] = true
	}
	for _, want := range []string{"web", "media", "meta", "equibles"} {
		if !seen[want] {
			t.Errorf("config/mcp_servers.yaml missing prefix %q", want)
		}
	}
}
