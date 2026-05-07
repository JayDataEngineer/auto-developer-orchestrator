package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrgManifest(t *testing.T) {
	// Use twitter-agent as integration test if available
	twitterPath := "/home/ubuntu/Documents/programs/dev/twitter-agent"
	if _, err := os.Stat(twitterPath); os.IsNotExist(err) {
		t.Skip("twitter-agent not found at", twitterPath)
	}

	org := LoadOrgManifest(twitterPath)
	if org == nil {
		t.Fatal("expected org to be loaded from twitter-agent")
	}

	if org.Name != "Twitter Content Division" {
		t.Errorf("expected name 'Twitter Content Division', got %q", org.Name)
	}

	if org.RolesDir() == "" {
		t.Error("expected RolesDir to be set")
	}

	if org.ManifestoContent() == "" {
		t.Error("expected manifesto content")
	}

	if len(org.Schedules) != 4 {
		t.Errorf("expected 4 schedules, got %d", len(org.Schedules))
	}

	// Check first schedule has role
	if org.Schedules[0].Role != "content-writer" {
		t.Errorf("expected role 'content-writer', got %q", org.Schedules[0].Role)
	}
	if !org.Schedules[0].Enabled {
		t.Error("expected morning_post to be enabled")
	}
}

func TestLoadOrgRolesFromTwitterAgent(t *testing.T) {
	twitterPath := "/home/ubuntu/Documents/programs/dev/twitter-agent"
	if _, err := os.Stat(twitterPath); os.IsNotExist(err) {
		t.Skip("twitter-agent not found at", twitterPath)
	}

	org := LoadOrgManifest(twitterPath)
	if org == nil {
		t.Fatal("expected org")
	}

	roles := LoadAgentRolesFrom(org.RolesDir())
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(roles))
	}

	expected := []string{"content-writer", "researcher", "engagement-manager"}
	for _, name := range expected {
		if _, ok := roles[name]; !ok {
			t.Errorf("missing role: %s", name)
		}
	}

	// Check content-writer has browser import
	cw := roles["content-writer"]
	found := false
	for _, imp := range cw.Imports {
		if imp == "browser" {
			found = true
			break
		}
	}
	if !found {
		t.Error("content-writer should import 'browser'")
	}
	if cw.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("expected content-writer model 'deepseek/deepseek-v4-flash', got %q", cw.Model)
	}
}

func TestOrgManifestNoPuxYaml(t *testing.T) {
	dir := t.TempDir()
	org := LoadOrgManifest(dir)
	if org != nil {
		t.Error("expected nil for directory without pux.yaml")
	}
}

func TestOrgManifestEmptyName(t *testing.T) {
	dir := t.TempDir()
	yaml := "description: test\n"
	os.WriteFile(filepath.Join(dir, "pux.yaml"), []byte(yaml), 0644)
	org := LoadOrgManifest(dir)
	if org != nil {
		t.Error("expected nil for pux.yaml without name")
	}
}
