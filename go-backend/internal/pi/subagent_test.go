package pi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSubAgentTypeValidation(t *testing.T) {
	tests := []struct {
		typ     SubAgentType
		isValid bool
	}{
		{SubAgentCode, true},
		{SubAgentExplore, true},
		{SubAgentWeb, true},
		{SubAgentComputerUse, true},
		{SubAgentType("invalid"), false},
		{SubAgentType(""), false},
	}

	for _, tt := range tests {
		got := ValidSubAgentTypes[tt.typ]
		if got != tt.isValid {
			t.Errorf("ValidSubAgentTypes[%q] = %v, want %v", tt.typ, got, tt.isValid)
		}
	}
}

func TestSubAgentConfigInitDefaults(t *testing.T) {
	cfg := SubAgentConfig{
		ProjectDir: "/app/projects/test",
		ParentID:   "agent-123",
		Type:       SubAgentExplore,
		Task:       "find all Go files",
	}
	cfg.InitDefaults()

	if cfg.AgentID == "" {
		t.Error("InitDefaults should auto-generate AgentID")
	}
	if !strings.HasPrefix(cfg.AgentID, "sub-explore-") {
		t.Errorf("AgentID should start with 'sub-explore-', got %q", cfg.AgentID)
	}
}

func TestSubAgentConfigInitDefaultsPreservesExisting(t *testing.T) {
	cfg := SubAgentConfig{
		AgentID: "custom-id",
	}
	cfg.InitDefaults()

	if cfg.AgentID != "custom-id" {
		t.Errorf("InitDefaults should preserve existing AgentID, got %q", cfg.AgentID)
	}
}

func TestSubAgentConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SubAgentConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: SubAgentConfig{
				ProjectDir: "/app/projects/test",
				ParentID:   "agent-1",
				Type:       SubAgentCode,
				Task:       "implement feature",
			},
			wantErr: false,
		},
		{
			name: "missing project dir",
			cfg: SubAgentConfig{
				ParentID: "agent-1",
				Type:     SubAgentCode,
				Task:     "implement feature",
			},
			wantErr: true,
			errMsg:  "projectDir is required",
		},
		{
			name: "missing parent id",
			cfg: SubAgentConfig{
				ProjectDir: "/app/projects/test",
				Type:       SubAgentCode,
				Task:       "implement feature",
			},
			wantErr: true,
			errMsg:  "parentId is required",
		},
		{
			name: "missing task",
			cfg: SubAgentConfig{
				ProjectDir: "/app/projects/test",
				ParentID:   "agent-1",
				Type:       SubAgentCode,
			},
			wantErr: true,
			errMsg:  "task is required",
		},
		{
			name: "invalid type",
			cfg: SubAgentConfig{
				ProjectDir: "/app/projects/test",
				ParentID:   "agent-1",
				Type:       SubAgentType("invalid"),
				Task:       "do thing",
			},
			wantErr: true,
			errMsg:  "invalid sub-agent type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestSubAgentStatusIsTerminal(t *testing.T) {
	tests := []struct {
		status   SubAgentStatus
		terminal bool
	}{
		{StatusPending, false},
		{StatusRunning, false},
		{StatusComplete, true},
		{StatusFailed, true},
		{StatusAborted, true},
	}

	for _, tt := range tests {
		got := tt.status.IsTerminal()
		if got != tt.terminal {
			t.Errorf("SubAgentStatus(%q).IsTerminal() = %v, want %v", tt.status, got, tt.terminal)
		}
	}
}

func TestSubAgentResultSerialization(t *testing.T) {
	result := SubAgentResult{
		SubAgentID:   "sub-code-123",
		Type:         SubAgentCode,
		Status:       StatusComplete,
		Output:       "Implemented UserService.Create",
		InputTokens:  1500.0,
		OutputTokens: 800.0,
		CacheTokens:  500.0,
		DurationMs:   5000,
		ToolCalls:    3,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal SubAgentResult: %v", err)
	}

	var decoded SubAgentResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal SubAgentResult: %v", err)
	}

	if decoded.SubAgentID != result.SubAgentID {
		t.Errorf("SubAgentID mismatch: got %q, want %q", decoded.SubAgentID, result.SubAgentID)
	}
	if decoded.Type != result.Type {
		t.Errorf("Type mismatch: got %q, want %q", decoded.Type, result.Type)
	}
	if decoded.Status != result.Status {
		t.Errorf("Status mismatch: got %q, want %q", decoded.Status, result.Status)
	}
	if decoded.DurationMs != result.DurationMs {
		t.Errorf("DurationMs mismatch: got %d, want %d", decoded.DurationMs, result.DurationMs)
	}
	if decoded.ToolCalls != result.ToolCalls {
		t.Errorf("ToolCalls mismatch: got %d, want %d", decoded.ToolCalls, result.ToolCalls)
	}
}

func TestSubAgentResultSerializationWithEmptyError(t *testing.T) {
	result := SubAgentResult{
		SubAgentID: "sub-explore-1",
		Type:       SubAgentExplore,
		Status:     StatusComplete,
		Output:     "Found 5 files",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Error field should be omitted when empty (omitempty)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to raw map: %v", err)
	}

	if _, hasError := raw["error"]; hasError {
		t.Error("error field should be omitted when empty")
	}
}

func TestBuildSubAgentPromptCode(t *testing.T) {
	prompt := BuildSubAgentPrompt(SubAgentPromptConfig{
		ProjectDir: t.TempDir(),
		Type:       SubAgentCode,
	})

	if !strings.Contains(prompt, "Code Implementation") {
		t.Error("code prompt should contain 'Code Implementation'")
	}
	if !strings.Contains(prompt, "DO NOT modify") {
		// Code agent CAN modify files
	}
	if !strings.Contains(prompt, "summary of all changes") {
		t.Error("code prompt should mention summary of changes")
	}
}

func TestBuildSubAgentPromptExplore(t *testing.T) {
	prompt := BuildSubAgentPrompt(SubAgentPromptConfig{
		ProjectDir: t.TempDir(),
		Type:       SubAgentExplore,
	})

	if !strings.Contains(prompt, "Code Exploration") {
		t.Error("explore prompt should contain 'Code Exploration'")
	}
	if !strings.Contains(prompt, "DO NOT modify any files") {
		t.Error("explore prompt should say DO NOT modify files")
	}
	if !strings.Contains(prompt, "structured report") {
		t.Error("explore prompt should mention structured report")
	}
}

func TestBuildSubAgentPromptWeb(t *testing.T) {
	browserURL := "http://localhost:3847/api/pi/web"
	prompt := BuildSubAgentPrompt(SubAgentPromptConfig{
		ProjectDir:     t.TempDir(),
		Type:           SubAgentWeb,
		BrowserBaseURL: browserURL,
	})

	if !strings.Contains(prompt, "Web Research") {
		t.Error("web prompt should contain 'Web Research'")
	}
	if !strings.Contains(prompt, browserURL) {
		t.Error("web prompt should contain browser base URL")
	}
	if !strings.Contains(prompt, "POST "+browserURL+"/navigate") {
		t.Error("web prompt should contain navigate endpoint")
	}
	if !strings.Contains(prompt, "POST "+browserURL+"/click") {
		t.Error("web prompt should contain click endpoint")
	}
	if !strings.Contains(prompt, "POST "+browserURL+"/type") {
		t.Error("web prompt should contain type endpoint")
	}
	if !strings.Contains(prompt, "POST "+browserURL+"/scroll") {
		t.Error("web prompt should contain scroll endpoint")
	}
	if !strings.Contains(prompt, browserURL+"/state") {
		t.Error("web prompt should contain state endpoint")
	}
	if !strings.Contains(prompt, browserURL+"/screenshot") {
		t.Error("web prompt should contain screenshot endpoint")
	}
	if !strings.Contains(prompt, "DELETE "+browserURL+"/session") {
		t.Error("web prompt should contain close session endpoint")
	}
}

func TestBuildSubAgentPromptComputerUse(t *testing.T) {
	prompt := BuildSubAgentPrompt(SubAgentPromptConfig{
		ProjectDir: t.TempDir(),
		Type:       SubAgentComputerUse,
	})

	if !strings.Contains(prompt, "Desktop Automation") {
		t.Error("computer_use prompt should contain 'Desktop Automation'")
	}
	if !strings.Contains(prompt, "screenshot") {
		t.Error("computer_use prompt should mention screenshots")
	}
}

func TestBuildSubAgentPromptUnknownType(t *testing.T) {
	prompt := BuildSubAgentPrompt(SubAgentPromptConfig{
		ProjectDir: t.TempDir(),
		Type:       SubAgentType("unknown"),
	})

	// Should return just the base prompt without type-specific section
	// (base prompt still contains "Pi Agent" intro)
	if !strings.Contains(prompt, "Pi Agent") {
		t.Error("base prompt should still contain intro section")
	}
}

func TestBuildSubAgentPromptIncludesBaseSections(t *testing.T) {
	prompt := BuildSubAgentPrompt(SubAgentPromptConfig{
		ProjectDir: t.TempDir(),
		Type:       SubAgentCode,
	})

	// Should include base SystemPromptBuilder sections
	if !strings.Contains(prompt, "# Pi Agent") {
		t.Error("sub-agent prompt should include Pi Agent intro from base builder")
	}
	if !strings.Contains(prompt, "# System Rules") {
		t.Error("sub-agent prompt should include System Rules from base builder")
	}
	if !strings.Contains(prompt, "# Environment") {
		t.Error("sub-agent prompt should include Environment section from base builder")
	}
}

func TestExtractUsageFromMessages(t *testing.T) {
	msgs := []struct {
		Role  string `json:"role"`
		Usage struct {
			Input     float64 `json:"input"`
			Output    float64 `json:"output"`
			CacheRead float64 `json:"cacheRead"`
		} `json:"usage"`
	}{
		{Role: "user", Usage: struct {
			Input     float64 `json:"input"`
			Output    float64 `json:"output"`
			CacheRead float64 `json:"cacheRead"`
		}{Input: 100, Output: 0, CacheRead: 0}},
		{Role: "assistant", Usage: struct {
			Input     float64 `json:"input"`
			Output    float64 `json:"output"`
			CacheRead float64 `json:"cacheRead"`
		}{Input: 1500, Output: 800, CacheRead: 500}},
	}

	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("Failed to marshal test messages: %v", err)
	}

	input, output, cache := extractUsageFromMessages(raw)
	if input != 1500 {
		t.Errorf("input tokens = %v, want 1500", input)
	}
	if output != 800 {
		t.Errorf("output tokens = %v, want 800", output)
	}
	if cache != 500 {
		t.Errorf("cache tokens = %v, want 500", cache)
	}
}

func TestExtractUsageFromMessagesEmpty(t *testing.T) {
	input, output, cache := extractUsageFromMessages(json.RawMessage(`[]`))
	if input != 0 || output != 0 || cache != 0 {
		t.Errorf("expected all zeros for empty messages, got input=%v output=%v cache=%v", input, output, cache)
	}
}

func TestExtractUsageFromMessagesInvalid(t *testing.T) {
	input, output, cache := extractUsageFromMessages(json.RawMessage(`invalid json`))
	if input != 0 || output != 0 || cache != 0 {
		t.Errorf("expected all zeros for invalid json, got input=%v output=%v cache=%v", input, output, cache)
	}
}

func TestSubAgentInstanceAccessors(t *testing.T) {
	inst := &SubAgentInstance{
		ID:     "sub-test-1",
		Status: StatusRunning,
		Done:   make(chan struct{}),
	}

	// Test Output and ToolCount initial state
	if inst.Output() != "" {
		t.Error("initial output should be empty")
	}
	if inst.ToolCount() != 0 {
		t.Error("initial tool count should be 0")
	}

	// Test CollectOutput
	inst.CollectOutput("hello ")
	inst.CollectOutput("world")
	if inst.Output() != "hello world" {
		t.Errorf("output = %q, want 'hello world'", inst.Output())
	}

	// Test IncrementToolCount
	inst.IncrementToolCount()
	inst.IncrementToolCount()
	if inst.ToolCount() != 2 {
		t.Errorf("tool count = %d, want 2", inst.ToolCount())
	}

	// Test IsTerminalState
	if inst.IsTerminalState() {
		t.Error("running status should not be terminal")
	}
}

func TestSubAgentInstanceGetResult(t *testing.T) {
	inst := &SubAgentInstance{
		ID:     "sub-test-1",
		Status: StatusComplete,
		Result: &SubAgentResult{
			SubAgentID: "sub-test-1",
			Status:     StatusComplete,
			Output:     "task done",
		},
	}

	result := inst.GetResult()
	if result == nil {
		t.Fatal("GetResult should return result when set")
	}
	if result.Output != "task done" {
		t.Errorf("result output = %q, want 'task done'", result.Output)
	}
}

func TestSubAgentInstanceGetResultNil(t *testing.T) {
	inst := &SubAgentInstance{
		ID:     "sub-test-1",
		Status: StatusRunning,
	}

	result := inst.GetResult()
	if result != nil {
		t.Error("GetResult should return nil when not set")
	}
}

func TestSubAgentTypeStringer(t *testing.T) {
	tests := []struct {
		typ  SubAgentType
		want string
	}{
		{SubAgentCode, "code"},
		{SubAgentExplore, "explore"},
		{SubAgentWeb, "web"},
		{SubAgentComputerUse, "computer_use"},
	}

	for _, tt := range tests {
		got := tt.typ.String()
		if got != tt.want {
			t.Errorf("SubAgentType(%q).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestSubAgentStatusStringer(t *testing.T) {
	tests := []struct {
		status SubAgentStatus
		want   string
	}{
		{StatusPending, "pending"},
		{StatusRunning, "running"},
		{StatusComplete, "complete"},
		{StatusFailed, "failed"},
		{StatusAborted, "aborted"},
	}

	for _, tt := range tests {
		got := tt.status.String()
		if got != tt.want {
			t.Errorf("SubAgentStatus(%q).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestSubAgentManagerOptions(t *testing.T) {
	mgr := NewSubAgentManager(nil, nil,
		WithMaxPerParent(5),
		WithMaxTotal(20),
		WithBrowserBaseURL("http://test:8080/api/pi/web"),
	)

	if mgr.maxPerParent != 5 {
		t.Errorf("maxPerParent = %d, want 5", mgr.maxPerParent)
	}
	if mgr.maxTotal != 20 {
		t.Errorf("maxTotal = %d, want 20", mgr.maxTotal)
	}
	if mgr.browserBaseURL != "http://test:8080/api/pi/web" {
		t.Errorf("browserBaseURL = %q, want http://test:8080/api/pi/web", mgr.browserBaseURL)
	}
}

func TestSubAgentManagerDefaults(t *testing.T) {
	mgr := NewSubAgentManager(nil, nil)

	if mgr.maxPerParent != defaultMaxPerParent {
		t.Errorf("default maxPerParent = %d, want %d", mgr.maxPerParent, defaultMaxPerParent)
	}
	if mgr.maxTotal != defaultMaxTotal {
		t.Errorf("default maxTotal = %d, want %d", mgr.maxTotal, defaultMaxTotal)
	}
}

func TestSubAgentInstanceDoneChannel(t *testing.T) {
	inst := &SubAgentInstance{
		ID:     "sub-test-1",
		Status: StatusRunning,
		Done:   make(chan struct{}),
	}

	// Should not be done yet
	select {
	case <-inst.Done:
		t.Error("Done channel should not be closed yet")
	default:
		// expected
	}

	// Close it
	close(inst.Done)

	// Now should be done
	select {
	case <-inst.Done:
		// expected
	case <-time.After(time.Second):
		t.Error("Done channel should be closed")
	}
}
