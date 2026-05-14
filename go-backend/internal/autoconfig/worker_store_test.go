package autoconfig

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
)

func setupTestEnv(t *testing.T) {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	os.Setenv("PROJECT_ROOT", repoRoot)
	common.ReloadPromptTemplate()
}

func TestWorkerStoreCreateAndList(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)

	// Create a worker
	ctx := context.Background()
	result, err := s.Put(ctx, "test-worker", map[string]any{
		"persona":      "Test worker",
		"capabilities": []any{"shell"},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if result == nil {
		t.Fatal("Put returned nil result")
	}

	// Verify file exists on disk
	if _, err := os.Stat(filepath.Join(dir, "test-worker.yaml")); err != nil {
		t.Fatalf("worker file not created: %v", err)
	}

	// List should include it
	listResult, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	listMap := listResult.(map[string]any)
	items := listMap["items"].([]string)
	if len(items) != 1 || items[0] != "test-worker" {
		t.Errorf("List = %v, want [test-worker]", items)
	}
}

func TestWorkerStoreGet(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	ctx := context.Background()

	_, err := s.Put(ctx, "my-worker", map[string]any{
		"persona":      "My test worker",
		"capabilities": []any{"shell"},
		"max_rounds":   float64(20),
		"temperature":  float64(0.3),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	result, err := s.Get(ctx, "my-worker")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	m := result.(map[string]any)
	if m["persona"] != "My test worker" {
		t.Errorf("persona = %q, want %q", m["persona"], "My test worker")
	}
	if m["max_rounds"] != 20 {
		t.Errorf("max_rounds = %v, want 20", m["max_rounds"])
	}
	if m["temperature"] != 0.3 {
		t.Errorf("temperature = %v, want 0.3", m["temperature"])
	}
}

func TestWorkerStoreDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	ctx := context.Background()

	_, _ = s.Put(ctx, "to-delete", map[string]any{
		"persona":      "Delete me",
		"capabilities": []any{"shell"},
	})

	if err := s.Delete(ctx, "to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Should not be listed
	listResult, _ := s.List(ctx)
	listMap := listResult.(map[string]any)
	items := listMap["items"].([]string)
	if len(items) != 0 {
		t.Errorf("List after delete = %v, want empty", items)
	}
}

func TestWorkerStoreCapabilityValidation(t *testing.T) {
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	ctx := context.Background()

	// Unknown capability should fail
	_, err := s.Put(ctx, "bad-worker", map[string]any{
		"persona":      "Bad worker",
		"capabilities": []any{"nonexistent_capability"},
	})
	if err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestWorkerStoreNameValidation(t *testing.T) {
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	ctx := context.Background()

	// Path traversal
	_, err := s.Put(ctx, "../../etc/evil", map[string]any{
		"persona": "Evil",
	})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	// Empty name
	_, err = s.Put(ctx, "", map[string]any{
		"persona": "Empty",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}

	// Uppercase
	_, err = s.Put(ctx, "BadName", map[string]any{
		"persona": "Uppercase",
	})
	if err == nil {
		t.Fatal("expected error for uppercase name")
	}
}

func TestWorkerStoreSandboxValidation(t *testing.T) {
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	ctx := context.Background()

	_, err := s.Put(ctx, "bad-sandbox", map[string]any{
		"persona": "Bad sandbox",
		"sandbox": "invalid_tier",
	})
	if err == nil {
		t.Fatal("expected error for invalid sandbox")
	}
}

func TestWorkerStorePersonaRequired(t *testing.T) {
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	ctx := context.Background()

	_, err := s.Put(ctx, "no-persona", map[string]any{
		"capabilities": []any{"shell"},
	})
	if err == nil {
		t.Fatal("expected error for missing persona")
	}
}

func TestJITWorkerStoreCleanup(t *testing.T) {
	setupTestEnv(t)
	sessionDir := t.TempDir()
	s := NewJITWorkerStore(sessionDir)
	ctx := context.Background()

	if !s.IsJIT() {
		t.Error("expected JIT store to report IsJIT=true")
	}

	// Create a JIT worker
	_, err := s.Put(ctx, "temp-worker", map[string]any{
		"persona":      "Temporary",
		"capabilities": []any{"shell"},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Dir should exist
	workersDir := s.Dir()
	if _, err := os.Stat(workersDir); err != nil {
		t.Fatalf("JIT workers dir not created: %v", err)
	}

	// Cleanup
	if err := s.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	// Dir should be gone
	if _, err := os.Stat(workersDir); !os.IsNotExist(err) {
		t.Error("JIT workers dir should be removed after cleanup")
	}
}

func TestWorkerStoreYAMLCompatibleWithLoader(t *testing.T) {
	setupTestEnv(t)
	dir := t.TempDir()
	s := NewWorkerStore(dir)
	ctx := context.Background()

	// Create a worker via the store
	_, err := s.Put(ctx, "roundtrip-worker", map[string]any{
		"persona":      "Roundtrip test worker",
		"capabilities": []any{"shell"},
		"max_rounds":   float64(25),
		"temperature":  float64(0.2),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Load it with the common loader — proves YAML format is compatible
	roles := common.LoadWorkersFrom(dir)
	role, ok := roles["roundtrip-worker"]
	if !ok {
		t.Fatal("LoadWorkersFrom did not find roundtrip-worker")
	}
	if role.Description != "Roundtrip test worker" {
		t.Errorf("description = %q, want %q", role.Description, "Roundtrip test worker")
	}
	if role.MaxRounds != 25 {
		t.Errorf("max_rounds = %d, want 25", role.MaxRounds)
	}
	if role.Temperature != 0.2 {
		t.Errorf("temperature = %f, want 0.2", role.Temperature)
	}
}
