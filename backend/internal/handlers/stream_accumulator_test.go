package handlers

import (
	"encoding/json"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestBasicDelegation(t *testing.T) {
	a := NewStreamAccumulator()

	// CTO calls delegate_to
	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "tc1", ToolName: "delegate_to",
		ToolArgs: map[string]any{"role": "explorer", "task": "find files"},
	}})

	// Sub-agent starts
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentStartData{
		AgentName: "explorer",
		Task:      "find files",
	}})

	// Sub-agent calls bash
	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "tc2", ToolName: "bash",
		ToolArgs: map[string]any{"command": "ls"},
		AgentName: "explorer",
	}})

	// Sub-agent bash result
	a.ProcessEvent(core.AgentEvent{Data: core.ToolEnd{
		ToolID: "tc2", ToolName: "bash",
		Result: "file1.go\nfile2.go",
		AgentName: "explorer",
	}})

	// Sub-agent thinking
	a.ProcessEvent(core.AgentEvent{Data: core.ThinkingDelta{
		Text: "I need to look at the Go files",
		AgentName: "explorer",
	}})

	// Sub-agent text output
	a.ProcessEvent(core.AgentEvent{Data: core.TextDelta{
		Text: "Found 2 Go files",
		AgentName: "explorer",
	}})

	// Sub-agent ends
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentEndData{
		AgentName: "explorer",
		Status:    "completed",
		Result:    "Found 2 Go files: file1.go, file2.go",
	}})

	// CTO gets delegate_to result
	a.ProcessEvent(core.AgentEvent{Data: core.ToolEnd{
		ToolID: "tc1", ToolName: "delegate_to",
		Result: map[string]any{"result": "Found 2 Go files", "status": "completed"},
	}})

	// Parse JSON output
	raw := a.ToolCallsJSON()
	var calls []jsonToolCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		t.Fatalf("failed to parse ToolCallsJSON: %v\nraw: %s", err, raw)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}

	c := calls[0]
	if c.Name != "delegate_to" {
		t.Errorf("expected name=delegate_to, got %s", c.Name)
	}
	if c.ID != "tc1" {
		t.Errorf("expected id=tc1, got %s", c.ID)
	}

	if c.SubAgent == nil {
		t.Fatal("expected subAgent to be non-nil")
	}
	sa := c.SubAgent

	if sa.Name != "explorer" {
		t.Errorf("expected subAgent.name=explorer, got %s", sa.Name)
	}
	if sa.Status != "completed" {
		t.Errorf("expected subAgent.status=completed, got %s", sa.Status)
	}
	if sa.Result != "Found 2 Go files: file1.go, file2.go" {
		t.Errorf("unexpected subAgent.result: %s", sa.Result)
	}
	if sa.Thinking != "I need to look at the Go files" {
		t.Errorf("unexpected subAgent.thinking: %s", sa.Thinking)
	}
	if sa.Text != "Found 2 Go files" {
		t.Errorf("unexpected subAgent.text: %s", sa.Text)
	}

	if len(sa.ToolCalls) != 1 {
		t.Fatalf("expected 1 sub-agent tool call, got %d", len(sa.ToolCalls))
	}
	if sa.ToolCalls[0].Name != "bash" {
		t.Errorf("expected sub-agent tool name=bash, got %s", sa.ToolCalls[0].Name)
	}
	// Verify the sub-agent tool result was captured in the JSON
	if sa.ToolCalls[0].Result == "" {
		t.Error("expected sub-agent tool result to be populated")
	}
}

func TestNoSubAgent(t *testing.T) {
	a := NewStreamAccumulator()

	// CTO calls a non-delegate tool
	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "tc1", ToolName: "bash",
		ToolArgs: map[string]any{"command": "ls"},
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.ToolEnd{
		ToolID: "tc1", ToolName: "bash",
		Result: "file1.go",
	}})

	raw := a.ToolCallsJSON()
	var calls []jsonToolCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "bash" {
		t.Errorf("expected bash, got %s", calls[0].Name)
	}
	if calls[0].SubAgent != nil {
		t.Error("non-delegate tool should not have subAgent")
	}
}

func TestMultipleDelegations(t *testing.T) {
	a := NewStreamAccumulator()

	// First delegation
	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "tc1", ToolName: "delegate_to",
		ToolArgs: map[string]any{"role": "explorer"},
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentStartData{AgentName: "explorer"}})
	a.ProcessEvent(core.AgentEvent{Data: core.TextDelta{Text: "mapping codebase", AgentName: "explorer"}})
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentEndData{
		AgentName: "explorer", Status: "completed", Result: "mapped",
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.ToolEnd{
		ToolID: "tc1", ToolName: "delegate_to",
		Result: "mapped",
	}})

	// Second delegation
	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "tc2", ToolName: "delegate_to",
		ToolArgs: map[string]any{"role": "code_ops"},
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentStartData{AgentName: "code_ops"}})
	a.ProcessEvent(core.AgentEvent{Data: core.TextDelta{Text: "writing code", AgentName: "code_ops"}})
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentEndData{
		AgentName: "code_ops", Status: "completed", Result: "done",
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.ToolEnd{
		ToolID: "tc2", ToolName: "delegate_to",
		Result: "done",
	}})

	raw := a.ToolCallsJSON()
	var calls []jsonToolCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	if calls[0].SubAgent == nil || calls[0].SubAgent.Name != "explorer" {
		t.Error("first call should have explorer subAgent")
	}
	if calls[1].SubAgent == nil || calls[1].SubAgent.Name != "code_ops" {
		t.Error("second call should have code_ops subAgent")
	}
}

func TestCTOTextUnaffected(t *testing.T) {
	a := NewStreamAccumulator()

	// CTO text
	a.ProcessEvent(core.AgentEvent{Data: core.TextDelta{Text: "Hello "}})
	a.ProcessEvent(core.AgentEvent{Data: core.TextDelta{Text: "CTO"}})

	// Sub-agent text (should NOT appear in CTO text)
	a.ProcessEvent(core.AgentEvent{Data: core.TextDelta{Text: "sub output", AgentName: "explorer"}})

	text := a.Text()
	if text != "Hello CTO" {
		t.Errorf("expected CTO text='Hello CTO', got %q", text)
	}
}

func TestCTOThinkingUnaffected(t *testing.T) {
	a := NewStreamAccumulator()

	a.ProcessEvent(core.AgentEvent{Data: core.ThinkingDelta{Text: "CTO thinking "}})
	a.ProcessEvent(core.AgentEvent{Data: core.ThinkingDelta{Text: "sub think", AgentName: "explorer"}})

	thinking := a.Thinking()
	if thinking != "CTO thinking " {
		t.Errorf("expected 'CTO thinking ', got %q", thinking)
	}
}

func TestSubAgentError(t *testing.T) {
	a := NewStreamAccumulator()

	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "tc1", ToolName: "delegate_to",
		ToolArgs: map[string]any{"role": "explorer"},
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentStartData{AgentName: "explorer"}})
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentEndData{
		AgentName: "explorer",
		Status:    "error",
		Error:     "timeout",
	}})

	raw := a.ToolCallsJSON()
	var calls []jsonToolCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1, got %d", len(calls))
	}
	sa := calls[0].SubAgent
	if sa == nil {
		t.Fatal("subAgent nil")
	}
	if sa.Status != "error" {
		t.Errorf("expected error status, got %s", sa.Status)
	}
	if sa.Error != "timeout" {
		t.Errorf("expected timeout, got %s", sa.Error)
	}
}

func TestToolResultsPreserved(t *testing.T) {
	a := NewStreamAccumulator()

	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "tc1", ToolName: "bash",
		ToolArgs: map[string]any{"command": "echo hi"},
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.ToolEnd{
		ToolID: "tc1", ToolName: "bash",
		Result: "hi",
	}})

	results := a.ToolResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ToolCallID != "tc1" {
		t.Errorf("expected tc1, got %s", results[0].ToolCallID)
	}
}

func TestEmptyAccumulator(t *testing.T) {
	a := NewStreamAccumulator()
	if a.Text() != "" {
		t.Error("expected empty text")
	}
	if a.Thinking() != "" {
		t.Error("expected empty thinking")
	}
	if a.ToolCallsJSON() != "[]" {
		t.Errorf("expected [], got %s", a.ToolCallsJSON())
	}
	if len(a.ToolResults()) != 0 {
		t.Error("expected empty results")
	}
}

func TestSubAgentToolEndLinksToSubAgent(t *testing.T) {
	a := NewStreamAccumulator()

	// Delegate
	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "d1", ToolName: "delegate_to",
		ToolArgs: map[string]any{"role": "explorer"},
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentStartData{AgentName: "explorer"}})

	// Sub-agent tool
	a.ProcessEvent(core.AgentEvent{Data: core.ToolStart{
		ToolID: "st1", ToolName: "file_read",
		ToolArgs: map[string]any{"path": "/tmp/test.go"},
		AgentName: "explorer",
	}})
	a.ProcessEvent(core.AgentEvent{Data: core.ToolEnd{
		ToolID: "st1", ToolName: "file_read",
		Result: "package main",
		AgentName: "explorer",
	}})

	a.ProcessEvent(core.AgentEvent{Data: core.SubAgentEndData{
		AgentName: "explorer", Status: "completed",
	}})

	// Verify the sub-agent tool call was captured
	raw := a.ToolCallsJSON()
	var calls []jsonToolCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		t.Fatalf("parse: %v", err)
	}

	sa := calls[0].SubAgent
	if sa == nil {
		t.Fatal("subAgent nil")
	}
	if len(sa.ToolCalls) != 1 {
		t.Fatalf("expected 1 sub-agent tool call, got %d", len(sa.ToolCalls))
	}
	if sa.ToolCalls[0].Name != "file_read" {
		t.Errorf("expected file_read, got %s", sa.ToolCalls[0].Name)
	}

	// Sub-agent tool end should still appear in ToolResults for legacy compat
	results := a.ToolResults()
	if len(results) < 1 {
		t.Fatal("expected at least 1 tool result")
	}
	found := false
	for _, r := range results {
		if r.ToolCallID == "st1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("sub-agent tool end (st1) not in ToolResults")
	}
}
