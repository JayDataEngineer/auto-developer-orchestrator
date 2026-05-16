package autoconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerToolListCapabilities(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	tool := NewWorkerTool(s, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"operation": "list_capabilities",
	})
	if err != nil {
		t.Fatalf("list_capabilities: %v", err)
	}

	m := result.(map[string]any)
	caps, ok := m["capabilities"].([]map[string]string)
	if !ok {
		t.Fatalf("capabilities not []map[string]string: %T", m["capabilities"])
	}
	if len(caps) == 0 {
		t.Fatal("no capabilities returned")
	}

	// Should include known capabilities
	names := make(map[string]bool)
	for _, c := range caps {
		names[c["name"]] = true
	}
	for _, expected := range []string{"browser", "code", "desktop", "research", "shell", "vision"} {
		if !names[expected] {
			t.Errorf("missing capability: %s", expected)
		}
	}
}

func TestWorkerToolCreateAndList(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	tool := NewWorkerTool(s, nil)
	ctx := context.Background()

	// Create
	result, err := tool.Execute(ctx, map[string]any{
		"operation":    "create",
		"name":         "my-worker",
		"persona":      "Test worker",
		"capabilities": []any{"shell"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m := result.(map[string]any)
	if m["name"] != "my-worker" {
		t.Errorf("name = %q, want %q", m["name"], "my-worker")
	}

	// List
	result, err = tool.Execute(ctx, map[string]any{
		"operation": "list",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m = result.(map[string]any)
	workers := m["workers"]
	if workers == nil {
		t.Fatal("workers is nil")
	}
}

func TestWorkerToolCreateJIT(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	sessionDir := t.TempDir()
	s := NewWorkerStore(dir)
	jit := NewJITWorkerStore(sessionDir)
	tool := NewWorkerTool(s, jit)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"operation":    "create_jit",
		"name":         "temp-worker",
		"persona":      "Temporary worker",
		"capabilities": []any{"shell"},
	})
	if err != nil {
		t.Fatalf("create_jit: %v", err)
	}
	m := result.(map[string]any)
	if m["jit"] != true {
		t.Error("expected jit=true")
	}

	// Verify JIT file exists
	if _, err := os.Stat(filepath.Join(sessionDir, "workers", "temp-worker.yaml")); err != nil {
		t.Fatalf("JIT worker file not created: %v", err)
	}
}

func TestWorkerToolShow(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	tool := NewWorkerTool(s, nil)
	ctx := context.Background()

	_, _ = tool.Execute(ctx, map[string]any{
		"operation":    "create",
		"name":         "show-me",
		"persona":      "Show test",
		"capabilities": []any{"shell"},
		"max_rounds":   float64(10),
	})

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "show",
		"name":      "show-me",
	})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	m := result.(map[string]any)
	worker := m["worker"].(map[string]any)
	if worker["persona"] != "Show test" {
		t.Errorf("persona = %q, want %q", worker["persona"], "Show test")
	}
}

func TestWorkerToolDelete(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	tool := NewWorkerTool(s, nil)
	ctx := context.Background()

	_, _ = tool.Execute(ctx, map[string]any{
		"operation":    "create",
		"name":         "delete-me",
		"persona":      "Delete test",
		"capabilities": []any{"shell"},
	})

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "delete",
		"name":      "delete-me",
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	m := result.(map[string]any)
	if m["message"] == nil {
		t.Error("expected message in delete result")
	}
}

func TestWorkerToolUnknownCapability(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	tool := NewWorkerTool(s, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"operation":    "create",
		"name":         "bad-worker",
		"persona":      "Bad",
		"capabilities": []any{"nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestWorkerToolMissingName(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	tool := NewWorkerTool(s, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"operation": "show",
	})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestWorkerToolInvalidOperation(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	tool := NewWorkerTool(s, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"operation": "bogus",
	})
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}
}
