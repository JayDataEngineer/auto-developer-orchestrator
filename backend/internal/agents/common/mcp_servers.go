package common

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MCPServerDecl is one row in config/mcp_servers.yaml.
type MCPServerDecl struct {
	Prefix string `yaml:"prefix"`
	URL    string `yaml:"url"`
}

type mcpServersFile struct {
	Servers []MCPServerDecl `yaml:"servers"`
}

// LoadMCPServers reads config/mcp_servers.yaml from the kernel config dir.
// Returns nil if the file is missing — the caller treats that as "no cloud
// MCPs declared" and falls through to extension discovery + user settings.
// Parse errors are logged-and-skipped per file so one bad row doesn't poison
// the whole boot; matching the LoadWorkersFrom convention.
func LoadMCPServers(configDir string) []MCPServerDecl {
	if configDir == "" {
		return nil
	}
	path := filepath.Join(configDir, "mcp_servers.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f mcpServersFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil
	}
	// Drop entries missing required fields — silent skip, same as worker YAML.
	out := make([]MCPServerDecl, 0, len(f.Servers))
	for _, s := range f.Servers {
		if s.Prefix == "" || s.URL == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// MCPServerURLOverride returns the env-var override for a prefix, if set.
// Empty string means "no override — use the declared URL." Mirrors the
// MCP_<PREFIX>_URL pattern that lived in app.go before this loader existed.
func MCPServerURLOverride(prefix string) string {
	return strings.TrimSpace(os.Getenv("MCP_" + strings.ToUpper(prefix) + "_URL"))
}
