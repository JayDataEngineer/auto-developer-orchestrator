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

func TestLoadInvestDivisionHeads(t *testing.T) {
	investPath := "/home/ubuntu/Documents/programs/dev/invest"
	if _, err := os.Stat(investPath); os.IsNotExist(err) {
		t.Skip("invest project not found at", investPath)
	}

	org := LoadOrgManifest(investPath)
	if org == nil {
		t.Fatal("expected org to be loaded from invest project")
	}

	if org.RolesDir() == "" {
		t.Error("expected RolesDir to be set")
	}

	roles := LoadAgentRolesFrom(org.RolesDir())
	if len(roles) < 3 {
		t.Fatalf("expected at least 3 division head roles, got %d", len(roles))
	}

	// Check division heads
	rd := roles["research-director"]
	if rd == nil {
		t.Fatal("research-director role not found")
	}
	if rd.Division != "./divisions/research" {
		t.Errorf("research-director: expected division './divisions/research', got %q", rd.Division)
	}

	ro := roles["risk-officer"]
	if ro == nil {
		t.Fatal("risk-officer role not found")
	}
	if ro.Division != "./divisions/risk" {
		t.Errorf("risk-officer: expected division './divisions/risk', got %q", ro.Division)
	}

	em := roles["execution-manager"]
	if em == nil {
		t.Fatal("execution-manager role not found")
	}
	if em.Division != "./divisions/execution" {
		t.Errorf("execution-manager: expected division './divisions/execution', got %q", em.Division)
	}
}

func TestLoadDREOrg(t *testing.T) {
	drePath := "/home/ubuntu/Documents/programs/deep-research-engine"
	if _, err := os.Stat(drePath); os.IsNotExist(err) {
		t.Skip("deep-research-engine not found at", drePath)
	}

	org := LoadOrgManifest(drePath)
	if org == nil {
		t.Fatal("expected org from deep-research-engine")
	}

	roles := LoadAgentRolesFrom(org.RolesDir())
	if len(roles) < 3 {
		t.Fatalf("expected at least 3 division head roles, got %d", len(roles))
	}

	// Check division heads
	for _, name := range []string{"research-director", "ingestion-director", "artifact-director"} {
		role := roles[name]
		if role == nil {
			t.Errorf("missing division head: %s", name)
			continue
		}
		if role.Division == "" {
			t.Errorf("%s: expected division field, got empty", name)
		}
	}

	// Load research division
	rd := roles["research-director"]
	researchOrg := LoadOrgManifest(filepath.Join(drePath, rd.Division))
	if researchOrg == nil {
		t.Fatal("research division pux.yaml not found")
	}
	researchRoles := LoadAgentRolesFrom(researchOrg.RolesDir())
	if len(researchRoles) != 3 {
		t.Errorf("expected 3 research workers, got %d", len(researchRoles))
	}

	// Load ingestion division
	id := roles["ingestion-director"]
	ingestOrg := LoadOrgManifest(filepath.Join(drePath, id.Division))
	if ingestOrg == nil {
		t.Fatal("ingestion division pux.yaml not found")
	}
	ingestRoles := LoadAgentRolesFrom(ingestOrg.RolesDir())
	if len(ingestRoles) != 4 {
		t.Errorf("expected 4 ingestion workers, got %d", len(ingestRoles))
	}

	// Load generation division
	ad := roles["artifact-director"]
	genOrg := LoadOrgManifest(filepath.Join(drePath, ad.Division))
	if genOrg == nil {
		t.Fatal("generation division pux.yaml not found")
	}
	genRoles := LoadAgentRolesFrom(genOrg.RolesDir())
	if len(genRoles) != 5 {
		t.Errorf("expected 5 generation workers, got %d", len(genRoles))
	}
}

func TestLoadInvestSubDivisionRoles(t *testing.T) {
	investPath := "/home/ubuntu/Documents/programs/dev/invest"
	if _, err := os.Stat(investPath); os.IsNotExist(err) {
		t.Skip("invest project not found at", investPath)
	}

	// Load research division
	researchOrg := LoadOrgManifest(filepath.Join(investPath, "divisions", "research"))
	if researchOrg == nil {
		t.Fatal("expected org from research division")
	}
	roles := LoadAgentRolesFrom(researchOrg.RolesDir())
	if len(roles) != 3 {
		t.Fatalf("expected 3 research roles, got %d", len(roles))
	}
	for _, name := range []string{"signal-analyst", "regime-analyst", "researcher"} {
		if roles[name] == nil {
			t.Errorf("missing research role: %s", name)
		}
	}

	// Load risk division
	riskOrg := LoadOrgManifest(filepath.Join(investPath, "divisions", "risk"))
	if riskOrg == nil {
		t.Fatal("expected org from risk division")
	}
	riskRoles := LoadAgentRolesFrom(riskOrg.RolesDir())
	if len(riskRoles) != 2 {
		t.Fatalf("expected 2 risk roles, got %d", len(riskRoles))
	}

	// Load execution division
	execOrg := LoadOrgManifest(filepath.Join(investPath, "divisions", "execution"))
	if execOrg == nil {
		t.Fatal("expected org from execution division")
	}
	execRoles := LoadAgentRolesFrom(execOrg.RolesDir())
	if len(execRoles) != 2 {
		t.Fatalf("expected 2 execution roles, got %d", len(execRoles))
	}
}
