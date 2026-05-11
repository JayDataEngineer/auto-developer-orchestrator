package common

import (
	"os"
	"path/filepath"
	"runtime"
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
	if len(ingestRoles) != 5 {
		t.Errorf("expected 5 ingestion workers (incl. face-recognition-specialist), got %d", len(ingestRoles))
	}
	for _, name := range []string{"audio-processor", "image-analyst", "text-extractor", "content-clusterer", "face-recognition-specialist"} {
		if ingestRoles[name] == nil {
			t.Errorf("missing ingestion role: %s", name)
		}
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

	// Verify databases section is parsed
	if len(org.Databases) != 3 {
		t.Fatalf("expected 3 database configs, got %d", len(org.Databases))
	}

	// Neo4j config
	neo4j, ok := org.Databases["neo4j"]
	if !ok {
		t.Fatal("missing neo4j database config")
	}
	if neo4j.URI != "bolt://172.17.0.9:7687" {
		t.Errorf("neo4j uri: expected bolt://172.17.0.9:7687, got %q", neo4j.URI)
	}
	if neo4j.Username != "neo4j" {
		t.Errorf("neo4j username: expected neo4j, got %q", neo4j.Username)
	}
	if neo4j.PasswordEnv != "NEO4J_PASSWORD" {
		t.Errorf("neo4j password_env: expected NEO4J_PASSWORD, got %q", neo4j.PasswordEnv)
	}

	// Postgres config
	pg, ok := org.Databases["postgres"]
	if !ok {
		t.Fatal("missing postgres database config")
	}
	if pg.URL != "postgresql://localhost:25432/shared_db" {
		t.Errorf("postgres url: expected postgresql://localhost:25432/shared_db, got %q", pg.URL)
	}

	// CompreFace config
	cf, ok := org.Databases["compreface"]
	if !ok {
		t.Fatal("missing compreface database config")
	}
	if cf.BaseURL != "http://172.17.0.14:8080" {
		t.Errorf("compreface base_url: expected http://172.17.0.14:8080, got %q", cf.BaseURL)
	}
	if cf.APIKeyEnv != "COMPREFACE_API_KEY" {
		t.Errorf("compreface api_key_env: expected COMPREFACE_API_KEY, got %q", cf.APIKeyEnv)
	}
}

func TestTechNoirOrg(t *testing.T) {
	techNoirPath := "/home/ubuntu/Documents/programs/creative/tech-noir"
	if _, err := os.Stat(techNoirPath); os.IsNotExist(err) {
		t.Skip("tech-noir not found at", techNoirPath)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	os.Setenv("PROJECT_ROOT", repoRoot)

	org := LoadOrgManifest(techNoirPath)
	if org == nil {
		t.Fatal("expected org to be loaded from tech-noir")
	}

	if org.Name != "tech-noir" {
		t.Errorf("expected name 'tech-noir', got %q", org.Name)
	}

	if org.RolesDir() == "" {
		t.Error("expected RolesDir to be set")
	}

	if org.ToolPkgsDir() == "" {
		t.Error("expected ToolPkgsDir to be set")
	}

	if manifesto := org.ManifestoContent(); manifesto == "" {
		t.Error("expected manifesto content")
	} else if !contains(manifesto, "Tech Noir Studio") {
		t.Errorf("manifesto missing 'Tech Noir Studio', got:\n%s", manifesto[:100])
	}

	// Load all 5 roles
	roles := LoadAgentRolesFrom(org.RolesDir())
	if len(roles) != 5 {
		t.Fatalf("expected 5 roles, got %d", len(roles))
	}

	expectedRoles := []string{
		"technical_artist",
		"narrative_designer",
		"gameplay_programmer",
		"qa_tester",
		"design_researcher",
	}
	for _, name := range expectedRoles {
		role := roles[name]
		if role == nil {
			t.Errorf("missing role: %s", name)
			continue
		}
		if role.Description == "" {
			t.Errorf("%s: description is empty", name)
		}
		if len(role.Imports) == 0 {
			t.Errorf("%s: no imports configured", name)
		}
		if role.Prompt == "" {
			t.Errorf("%s: prompt is empty", name)
		}
		if role.MaxRounds == 0 {
			t.Errorf("%s: max_rounds is zero", name)
		}
	}

	// Verify specific role properties
	ta := roles["technical_artist"]
	if ta.SandboxTier != "native" {
		t.Errorf("technical_artist: expected sandbox 'native', got %q", ta.SandboxTier)
	}
	if ta.Temperature != 0.2 {
		t.Errorf("technical_artist: expected temperature 0.2, got %f", ta.Temperature)
	}
	hasArtImport := false
	for _, imp := range ta.Imports {
		if imp == "tech_noir_art" {
			hasArtImport = true
		}
	}
	if !hasArtImport {
		t.Error("technical_artist should import 'tech_noir_art'")
	}

	nd := roles["narrative_designer"]
	if nd.SandboxTier != "isolated" {
		t.Errorf("narrative_designer: expected sandbox 'isolated', got %q", nd.SandboxTier)
	}
	if nd.Temperature != 0.7 {
		t.Errorf("narrative_designer: expected temperature 0.7, got %f", nd.Temperature)
	}

	gp := roles["gameplay_programmer"]
	if gp.SandboxTier != "native" {
		t.Errorf("gameplay_programmer: expected sandbox 'native', got %q", gp.SandboxTier)
	}
	hasGodotImport := false
	hasCodeImport := false
	for _, imp := range gp.Imports {
		if imp == "godot" {
			hasGodotImport = true
		}
		if imp == "code" {
			hasCodeImport = true
		}
	}
	if !hasGodotImport {
		t.Error("gameplay_programmer should import 'godot'")
	}
	if !hasCodeImport {
		t.Error("gameplay_programmer should import 'code'")
	}

	qt := roles["qa_tester"]
	if qt.SandboxTier != "native" {
		t.Errorf("qa_tester: expected sandbox 'native', got %q", qt.SandboxTier)
	}
	if qt.MaxRounds != 10 {
		t.Errorf("qa_tester: expected max_rounds 10, got %d", qt.MaxRounds)
	}

	dr := roles["design_researcher"]
	if dr.SandboxTier != "isolated" {
		t.Errorf("design_researcher: expected sandbox 'isolated', got %q", dr.SandboxTier)
	}
	if dr.Temperature != 0.3 {
		t.Errorf("design_researcher: expected temperature 0.3, got %f", dr.Temperature)
	}

	// Verify org tool packages are resolvable via imports
	// Simulate pux_prompt.go flow: warm kernel cache → merge org packages → load roles
	_ = LoadToolPackages() // ensure kernel packages are cached first
	dir := org.ToolPkgsDir()
	if dir == "" {
		t.Fatal("ToolPkgsDir is empty")
	}
	MergeToolPackages(dir)

	// Reload roles so they pick up the merged packages
	roles = LoadAgentRolesFrom(org.RolesDir())

	// technical_artist imports tech_noir_art + comfyui + studio_vision + code
	// Should have MCP servers: tech_noir, comfyui, qwen-vision
	ta = roles["technical_artist"]
	if ta == nil {
		t.Fatal("technical_artist role not found after reload")
	}
	hasTechNoirMCP := false
	hasComfyuiMCP := false
	hasQwenVisionMCP := false
	for _, s := range ta.MCPServers {
		switch s {
		case "tech_noir":
			hasTechNoirMCP = true
		case "comfyui":
			hasComfyuiMCP = true
		case "qwen-vision":
			hasQwenVisionMCP = true
		}
	}
	if !hasTechNoirMCP {
		t.Error("technical_artist missing 'tech_noir' MCP server from tech_noir_art package")
	}
	if !hasComfyuiMCP {
		t.Error("technical_artist missing 'comfyui' MCP server from comfyui package")
	}
	if !hasQwenVisionMCP {
		t.Error("technical_artist missing 'qwen-vision' MCP server from studio_vision package")
	}

	// gameplay_programmer imports godot + code
	// Should have MCP server: godot
	gp = roles["gameplay_programmer"]
	if gp == nil {
		t.Fatal("gameplay_programmer role not found after reload")
	}
	hasGodotMCP := false
	for _, s := range gp.MCPServers {
		if s == "godot" {
			hasGodotMCP = true
		}
	}
	if !hasGodotMCP {
		t.Error("gameplay_programmer missing 'godot' MCP server from godot package")
	}

	// Verify non-imported MCP servers are NOT present
	for _, s := range gp.MCPServers {
		if s == "comfyui" || s == "tech_noir" || s == "qwen-vision" {
			t.Errorf("gameplay_programmer should NOT have %q MCP server", s)
		}
	}

	// Verify kernel packages still resolve alongside org packages
	hasBash := false
	for _, t := range gp.Tools {
		if t == "bash" {
			hasBash = true
		}
	}
	if !hasBash {
		t.Error("gameplay_programmer missing 'bash' tool from kernel code package")
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
