package common

import (
	"os"
	"testing"
)

func TestLoadAgentRoles(t *testing.T) {
	root := os.Getenv("PROJECT_ROOT")
	if root == "" {
		root = "../.."
	}
	os.Setenv("PROJECT_ROOT", root)

	roles := LoadAgentRoles()
	if len(roles) == 0 {
		t.Fatal("no agent roles loaded")
	}

	// Verify each expected role exists
	expected := []string{"researcher", "coder", "browser"}
	for _, name := range expected {
		role := roles[name]
		if role == nil {
			t.Errorf("missing role: %s", name)
			continue
		}
		if role.Description == "" {
			t.Errorf("%s: description is empty", name)
		}
		if len(role.Tools) == 0 {
			t.Errorf("%s: no tools configured", name)
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

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
