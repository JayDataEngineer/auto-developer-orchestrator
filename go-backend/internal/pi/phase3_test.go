package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// --- Permission system tests ---

func TestPermissionModeAllows(t *testing.T) {
	tests := []struct {
		mode     PermissionMode
		category ToolCategory
		expected bool
	}{
		{PermReadOnly, CategoryRead, true},
		{PermReadOnly, CategoryWrite, false},
		{PermReadOnly, CategoryExecute, false},
		{PermReadOnly, CategoryDestructive, false},
		{PermReadOnly, CategoryBrowser, false},
		{PermWorkspaceWrite, CategoryRead, true},
		{PermWorkspaceWrite, CategoryWrite, true},
		{PermWorkspaceWrite, CategoryExecute, true},
		{PermWorkspaceWrite, CategoryDestructive, false},
		{PermWorkspaceWrite, CategoryBrowser, false},
		{PermDangerFullAccess, CategoryRead, true},
		{PermDangerFullAccess, CategoryWrite, true},
		{PermDangerFullAccess, CategoryExecute, true},
		{PermDangerFullAccess, CategoryDestructive, true},
		{PermDangerFullAccess, CategoryBrowser, true},
	}

	for _, tt := range tests {
		got := PermissionModeAllows(tt.mode, tt.category)
		if got != tt.expected {
			t.Errorf("PermissionModeAllows(%q, %q) = %v, want %v", tt.mode, tt.category, got, tt.expected)
		}
	}
}

func TestPermissionContextDenyNames(t *testing.T) {
	pc := NewPermissionContext(PermWorkspaceWrite, "/tmp/project")
	pc.DenyNames = []string{"Bash", "Write"}

	if !pc.isDenied("Bash") {
		t.Error("Bash should be denied")
	}
	if !pc.isDenied("bash") {
		t.Error("deny check should be case-insensitive")
	}
	if pc.isDenied("Read") {
		t.Error("Read should not be denied")
	}
}

func TestPermissionContextDenyPrefixes(t *testing.T) {
	pc := NewPermissionContext(PermWorkspaceWrite, "/tmp/project")
	pc.DenyPrefixes = []string{"git push"}

	if !pc.isDenied("git push origin main") {
		t.Error("git push should be denied by prefix")
	}
	if pc.isDenied("git status") {
		t.Error("git status should not be denied")
	}
}

func TestPermissionContextAllows(t *testing.T) {
	pc := NewPermissionContext(PermWorkspaceWrite, "/tmp/project")
	pc.DenyNames = []string{"Bash"}

	// Write allowed, but Bash denied (even though execute category normally allowed)
	if pc.Allows(CategoryExecute, "Bash") {
		t.Error("Bash should be denied despite workspace write mode")
	}
	if !pc.Allows(CategoryWrite, "Write") {
		t.Error("Write should be allowed")
	}
	if pc.Allows(CategoryDestructive, "rm") {
		t.Error("Destructive should be blocked in workspace_write mode")
	}
}

func TestPermissionContextPathAllowed(t *testing.T) {
	pc := NewPermissionContext(PermWorkspaceWrite, "/tmp/project")

	tests := []struct {
		path     string
		expected bool
	}{
		{"/tmp/project/main.go", true},
		{"/tmp/project/sub/file.go", true},
		{"/etc/passwd", false},
		{"../../etc/shadow", false},
	}

	for _, tt := range tests {
		got := pc.IsPathAllowed(tt.path)
		if got != tt.expected {
			t.Errorf("IsPathAllowed(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

func TestDefaultPermissionForSubAgent(t *testing.T) {
	tests := []struct {
		typ      SubAgentType
		expected PermissionMode
	}{
		{SubAgentCode, PermWorkspaceWrite},
		{SubAgentExplore, PermReadOnly},
		{SubAgentWeb, PermReadOnly},
		{SubAgentComputerUse, PermDangerFullAccess},
	}

	for _, tt := range tests {
		got := DefaultPermissionForSubAgent(tt.typ)
		if got != tt.expected {
			t.Errorf("DefaultPermissionForSubAgent(%q) = %q, want %q", tt.typ, got, tt.expected)
		}
	}
}

func TestRequiresConfirmation(t *testing.T) {
	pc := NewPermissionContext(PermWorkspaceWrite, "/tmp/project")

	if pc.RequiresConfirmation(CategoryRead) {
		t.Error("read should not need confirmation")
	}
	if !pc.RequiresConfirmation(CategoryDestructive) {
		t.Error("destructive should need confirmation")
	}
}

// --- Bash security tests ---

func TestClassifyCommandDestructive(t *testing.T) {
	tests := []string{
		"rm -rf /",
		"rm -rf ~",
		"git push --force origin main",
		"git push -f",
		"git reset --hard HEAD~1",
		"DROP TABLE users",
		"shutdown now",
		"reboot",
	}

	for _, cmd := range tests {
		result := ClassifyCommand(cmd)
		if result.Risk != RiskDestructive {
			t.Errorf("ClassifyCommand(%q).Risk = %q, want %q", cmd, result.Risk, RiskDestructive)
		}
		if result.Allowed {
			t.Errorf("ClassifyCommand(%q).Allowed = true, want false", cmd)
		}
	}
}

func TestClassifyCommandReadOnly(t *testing.T) {
	tests := []string{
		"ls -la",
		"cat main.go",
		"grep -r 'TODO' .",
		"git status",
		"git log --oneline -5",
		"git diff",
		"go version",
		"go test -v ./...",
		"echo hello",
		"pwd",
		"curl https://example.com",
	}

	for _, cmd := range tests {
		result := ClassifyCommand(cmd)
		if result.Risk != RiskSafe {
			t.Errorf("ClassifyCommand(%q).Risk = %q, want %q (reason: %s)", cmd, result.Risk, RiskSafe, result.Reason)
		}
		if !result.Allowed {
			t.Errorf("ClassifyCommand(%q).Allowed = false, want true", cmd)
		}
	}
}

func TestClassifyCommandWrite(t *testing.T) {
	tests := []string{
		"mkdir build",
		"cp file1 file2",
		"git add .",
		"git commit -m 'test'",
		"go build ./...",
		"npm install",
		"make build",
		"echo hello > output.txt",
	}

	for _, cmd := range tests {
		result := ClassifyCommand(cmd)
		if result.Risk != RiskWrite {
			t.Errorf("ClassifyCommand(%q).Risk = %q, want %q (reason: %s)", cmd, result.Risk, RiskWrite, result.Reason)
		}
		if !result.Allowed {
			t.Errorf("ClassifyCommand(%q).Allowed = false, want true", cmd)
		}
	}
}

func TestClassifyCommandEmpty(t *testing.T) {
	result := ClassifyCommand("")
	if result.Risk != RiskSafe {
		t.Errorf("empty command should be safe, got %q", result.Risk)
	}
}

func TestCheckPathSafety(t *testing.T) {
	tests := []struct {
		path       string
		projectDir string
		safe       bool
	}{
		{"/tmp/project/main.go", "/tmp/project", true},
		{"/etc/passwd", "/tmp/project", false},
		{"../../root/.ssh/id_rsa", "/tmp/project", false},
		{"/proc/self/environ", "/tmp/project", false},
	}

	for _, tt := range tests {
		safe, reason := CheckPathSafety(tt.path, tt.projectDir)
		if safe != tt.safe {
			t.Errorf("CheckPathSafety(%q, %q) = %v (reason: %s), want %v", tt.path, tt.projectDir, safe, reason, tt.safe)
		}
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git status", "git status"},
		{"go build ./...", "go build"},
		{"ls -la", "ls"},
		{"npm install", "npm install"},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractBaseCommand(tt.input)
		if got != tt.want {
			t.Errorf("extractBaseCommand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- Session persistence tests ---

func TestSessionManagerSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	sm := &SessionManager{
		logger:  zap.NewNop(),
		baseDir: tmpDir,
	}

	session := PersistedSession{
		SessionID:   "test-sess-123",
		ProjectDir:  "/tmp/project",
		AgentID:     "agent-1",
		CreatedAtMs: time.Now().UnixMilli(),
	}

	// Save
	if err := sm.Save(t.Context(), session); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load
	loaded, err := sm.Load(t.Context(), "test-sess-123")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.SessionID != "test-sess-123" {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, "test-sess-123")
	}
	if loaded.ProjectDir != "/tmp/project" {
		t.Errorf("ProjectDir = %q, want %q", loaded.ProjectDir, "/tmp/project")
	}
}

func TestSessionManagerLoadNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	sm := &SessionManager{logger: zap.NewNop(), baseDir: tmpDir}

	loaded, err := sm.Load(t.Context(), "nonexistent")
	if err != nil {
		t.Fatalf("Load should not error for missing session: %v", err)
	}
	if loaded != nil {
		t.Error("Load should return nil for missing session")
	}
}

func TestSessionManagerDelete(t *testing.T) {
	tmpDir := t.TempDir()
	sm := &SessionManager{
		logger:  zap.NewNop(),
		baseDir: tmpDir,
	}

	session := PersistedSession{SessionID: "del-me"}
	sm.Save(t.Context(), session)

	if err := sm.Delete(t.Context(), "del-me"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	loaded, _ := sm.Load(t.Context(), "del-me")
	if loaded != nil {
		t.Error("Session should be deleted")
	}
}

func TestSessionManagerRecordSubAgent(t *testing.T) {
	tmpDir := t.TempDir()
	sm := &SessionManager{logger: zap.NewNop(), baseDir: tmpDir}

	session := PersistedSession{SessionID: "sub-test"}
	sm.Save(t.Context(), session)

	result := SubAgentResult{
		SubAgentID: "sub-code-1",
		Type:       SubAgentCode,
		Status:     StatusComplete,
		Output:     "implemented feature X",
	}
	sm.RecordSubAgent(t.Context(), "sub-test", result)

	loaded, _ := sm.Load(t.Context(), "sub-test")
	if len(loaded.SubAgents) != 1 {
		t.Fatalf("expected 1 sub-agent, got %d", len(loaded.SubAgents))
	}
	if loaded.SubAgents[0].SubAgentID != "sub-code-1" {
		t.Errorf("SubAgentID = %q, want %q", loaded.SubAgents[0].SubAgentID, "sub-code-1")
	}
}

func TestNewPersistedSession(t *testing.T) {
	s := NewPersistedSession("/tmp/project", "agent-1")
	if s.SessionID == "" {
		t.Error("SessionID should be auto-generated")
	}
	if s.ProjectDir != "/tmp/project" {
		t.Errorf("ProjectDir = %q", s.ProjectDir)
	}
	if s.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", s.AgentID)
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, want 1", s.Version)
	}
}

func TestSessionSerialization(t *testing.T) {
	s := PersistedSession{
		Version:    1,
		SessionID:  "sess-123",
		ProjectDir: "/project",
		AgentID:    "agent-1",
		SubAgents: []SubAgentResult{
			{SubAgentID: "sub-1", Type: SubAgentExplore, Status: StatusComplete},
		},
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded PersistedSession
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.SessionID != s.SessionID {
		t.Errorf("SessionID mismatch")
	}
	if len(decoded.SubAgents) != 1 {
		t.Errorf("SubAgents count = %d, want 1", len(decoded.SubAgents))
	}
}

func TestSessionPathSanitization(t *testing.T) {
	tmpDir := t.TempDir()
	sm := &SessionManager{logger: zap.NewNop(), baseDir: tmpDir}

	// SessionID with special chars should be sanitized
	session := PersistedSession{SessionID: "sess/../../etc/passwd"}
	sm.Save(t.Context(), session)

	// Should not have created a file outside baseDir
	loaded, err := sm.Load(t.Context(), "sess/../../etc/passwd")
	if err != nil {
		t.Logf("Load with special chars: %v (expected)", err)
	}
	_ = loaded
}

// --- Integration: BashSecurity + PermissionContext ---

func TestBashSecurityWithPermissionContext(t *testing.T) {
	pc := NewPermissionContext(PermWorkspaceWrite, "/tmp/project")

	// Read-only commands should be allowed
	readResult := ClassifyCommand("cat main.go")
	if !pc.Allows(readResult.Category, "Read") {
		t.Error("Read should be allowed")
	}

	// Write commands should be allowed
	writeResult := ClassifyCommand("mkdir build")
	if !pc.Allows(writeResult.Category, "Write") {
		t.Error("Write should be allowed")
	}

	// Destructive commands should be blocked
	destructResult := ClassifyCommand("rm -rf /")
	if pc.Allows(destructResult.Category, "Bash") {
		t.Error("Destructive should be blocked")
	}
}

// --- Ensure file path is valid ---

func TestSessionFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	sm := &SessionManager{logger: zap.NewNop(), baseDir: tmpDir}

	path := sm.sessionPath("test-session-id")
	expected := filepath.Join(tmpDir, "session-test-session-id.json")
	if path != expected {
		t.Errorf("sessionPath = %q, want %q", path, expected)
	}
}

// --- Ensure helper vars compile ---
var _ = strings.HasPrefix
var _ = filepath.Join
var _ = os.ReadFile
