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

	// Verify expected roles exist (employee names)
	expected := []string{"jake", "sarah", "alex", "marcus", "elena", "ryan"}
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
	r := GetAgentRole("sarah")
	if r == nil {
		t.Fatal("GetAgentRole(\"sarah\") returned nil")
	}
	if r.Name != "sarah" {
		t.Errorf("expected name 'sarah', got '%s'", r.Name)
	}
	if r.Model != "gemini-3-flash-preview" {
		t.Errorf("expected sarah model 'gemini-3-flash-preview', got '%s'", r.Model)
	}

	// Test roles without model (inherit CTO's)
	jake := GetAgentRole("jake")
	if jake == nil {
		t.Fatal("jake role not found")
	}
	if jake.Model != "" {
		t.Errorf("expected jake model to be empty (inherit), got '%s'", jake.Model)
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
	jake := GetAgentRole("jake")
	if jake == nil {
		t.Fatal("jake role not found")
	}
	if len(jake.Imports) == 0 {
		t.Error("jake has no imports")
	}
	// Jake imports browser — should have bash tool resolved
	hasBash = false
	for _, t := range jake.Tools {
		if t == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Error("jake missing 'bash' tool from browser import")
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
