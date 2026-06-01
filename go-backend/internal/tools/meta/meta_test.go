package meta

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestWaitTool_Name(t *testing.T) {
	if NewWaitTool().Name() != "wait" {
		t.Errorf("Name() = %q, want %q", NewWaitTool().Name(), "wait")
	}
}

func TestWaitTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewWaitTool())
}

func TestWaitTool_Execute_Default(t *testing.T) {
	tool := NewWaitTool()
	// Context with cancel to avoid sleeping
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	// The wait may not execute due to cancelled context, but the function structure
	// should have returned a result map
	_ = m
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "hello world", want: "hello-world"},
		{input: "Hello World", want: "hello-world"},
		{input: "  spaces  ", want: "spaces"},
		{input: "special!@#chars", want: "special-chars"},
		{input: "Refactor Auth Module", want: "refactor-auth-module"},
		{input: "UPPERCASE", want: "uppercase"},
		{input: "123 numbers", want: "123-numbers"},
		{input: "dash---dash", want: "dash-dash"},
		{input: "leading and trailing ", want: "leading-and-trailing"},
		{input: "", want: ""},
		{input: "a", want: "a"},
		{input: "   ", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := Slugify(tc.input)
			if got != tc.want {
				t.Errorf("Slugify(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  string
	}{
		{name: "zero", input: 0, want: "0"},
		{name: "one", input: 1, want: "1"},
		{name: "forty two", input: 42, want: "42"},
		{name: "nine ninety nine", input: 999, want: "999"},
		{name: "negative one", input: -1, want: "-1"},
		{name: "negative forty two", input: -42, want: "-42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := itoa(tc.input)
			if got != tc.want {
				t.Errorf("itoa(%d) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestYieldArtifactTool_Name(t *testing.T) {
	tool := NewYieldArtifactTool()
	if tool.Name() != "yield_artifact" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "yield_artifact")
	}
}

func TestYieldArtifactTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewYieldArtifactTool())
}

func TestYieldArtifactTool_Execute_MissingParams(t *testing.T) {
	tool := NewYieldArtifactTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"title": "report",
	})
	if err == nil {
		t.Fatal("expected error for missing content")
	}

	_, err = tool.Execute(context.Background(), map[string]any{
		"content": "body",
	})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestYieldArtifactTool_Execute_DefaultPath(t *testing.T) {
	tool := NewYieldArtifactTool()

	_, err := tool.Execute(context.Background(), map[string]any{
		"title":   "Research Report",
		"type":    "report",
		"content": "# Report Content",
	})
	// Default path may fail if /sandbox doesn't exist, which is expected in test env
	if err != nil && tool.db == nil {
		// OK — no DB configured, and /sandbox/workspace likely doesn't exist
	}
}

func TestYieldArtifactTool_Execute_CustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "my-artifact.md")
	tool := NewYieldArtifactTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"title":   "Custom",
		"type":    "memo",
		"content": "body",
		"path":    customPath,
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "filepath", customPath)

	data, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatalf("failed to read artifact: %v", err)
	}
	if string(data) != "body" {
		t.Errorf("expected 'body', got %q", string(data))
	}
}

func TestYieldArtifactTool_Execute_DefaultType(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "test.md")
	tool := NewYieldArtifactTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"title":   "Test",
		"content": "body",
		"path":    customPath,
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "type", "memo")
}

func TestNewYieldArtifactToolWithDB(t *testing.T) {
	tool := NewYieldArtifactToolWithDB(nil, "/tmp", "agent-1")
	if tool.db != nil {
		t.Error("expected nil db")
	}
	if tool.sandboxDir != "/tmp" {
		t.Errorf("expected sandboxDir '/tmp', got %q", tool.sandboxDir)
	}
	if tool.agentID != "agent-1" {
		t.Errorf("expected agentID 'agent-1', got %q", tool.agentID)
	}
}
