package common

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadAgentRoles(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	os.Setenv("PROJECT_ROOT", repoRoot)

	ReloadPromptTemplate()

	roles := LoadAgentRoles()
	if len(roles) == 0 {
		t.Fatal("no roles loaded")
	}

	// Verify expected roles exist
	expected := []string{"web_expert", "researcher", "it_worker", "developer", "designer", "desktop_operator"}
	for _, name := range expected {
		role := roles[name]
		if role == nil {
			t.Errorf("missing role: %s", name)
			continue
		}
		if role.Description == "" {
			t.Errorf("%s: description is empty", name)
		}
		if len(role.Tools) == 0 && len(role.MCPServers) == 0 && len(role.Imports) == 0 {
			t.Errorf("%s: no tools, mcp_servers, or imports configured", name)
		}
		if role.Prompt == "" {
			t.Errorf("%s: prompt is empty", name)
		}
		if role.MaxRounds == 0 {
			t.Errorf("%s: max_rounds is zero", name)
		}
	}

	// Test GetAgentRole resolves
	r := GetAgentRole("researcher")
	if r == nil {
		t.Fatal("GetAgentRole(\"researcher\") returned nil")
	}
	if r.Name != "researcher" {
		t.Errorf("expected name 'researcher', got '%s'", r.Name)
	}

	// Test FormatAgentList
	list := FormatAgentList()
	if list == "" {
		t.Error("FormatAgentList returned empty string")
	}
	for _, name := range expected {
		if !contains(list, name) {
			t.Errorf("FormatAgentList missing role: %s", name)
		}
	}
}

func TestToolPackages(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	os.Setenv("PROJECT_ROOT", repoRoot)

	ReloadPromptTemplate()

	pkgs := LoadToolPackages()
	if len(pkgs) == 0 {
		t.Fatal("no tool packages loaded")
	}

	expected := []string{"browser", "research", "vision", "shell", "desktop", "code"}
	for _, name := range expected {
		if pkgs[name] == nil {
			t.Errorf("missing tool package: %s", name)
		}
	}

	// Test ResolveImports
	tools, mcpServers := ResolveImports([]string{"shell", "vision"})
	if len(tools) == 0 {
		t.Error("ResolveImports returned no tools for shell+vision")
	}
	if len(mcpServers) == 0 {
		t.Error("ResolveImports returned no mcp_servers for shell+vision")
	}
	hasMedia := false
	for _, s := range mcpServers {
		if s == "media" {
			hasMedia = true
		}
	}
	if !hasMedia {
		t.Error("ResolveImports missing 'media' mcp_server from vision package")
	}
	hasBash := false
	for _, t := range tools {
		if t == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Error("ResolveImports missing 'bash' tool from shell package")
	}

	// Test imports are resolved in roles
	webExpert := GetAgentRole("web_expert")
	if webExpert == nil {
		t.Fatal("web_expert role not found")
	}
	if len(webExpert.Imports) == 0 {
		t.Error("web_expert has no imports")
	}
	// Should have tools from resolved imports
	hasBash = false
	for _, t := range webExpert.Tools {
		if t == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Error("web_expert missing 'bash' from import resolution")
	}
	hasWebMCP := false
	for _, s := range webExpert.MCPServers {
		if s == "web" {
			hasWebMCP = true
		}
	}
	if !hasWebMCP {
		t.Error("web_expert missing 'web' mcp_server from research import")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
