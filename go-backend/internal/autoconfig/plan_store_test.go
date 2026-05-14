package autoconfig

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPlanStoreListEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	result, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 0 {
		t.Errorf("count = %d, want 0", m["count"])
	}
}

func TestPlanStorePutAndGet(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	spec := map[string]any{"content": "# Plan: Test\n\nDo something."}
	result, err := s.Put(ctx, "my-plan", spec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	_ = result

	got, err := s.Get(ctx, "my-plan")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := got.(map[string]any)
	if m["name"] != "my-plan" {
		t.Errorf("name = %q, want my-plan", m["name"])
	}
	if m["content"] != "# Plan: Test\n\nDo something." {
		t.Errorf("content = %q", m["content"])
	}
}

func TestPlanStorePutOverwrite(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	s.Put(ctx, "my-plan", map[string]any{"content": "v1"})
	s.Put(ctx, "my-plan", map[string]any{"content": "v2"})

	got, _ := s.Get(ctx, "my-plan")
	m := got.(map[string]any)
	if m["content"] != "v2" {
		t.Errorf("content = %q, want v2", m["content"])
	}
}

func TestPlanStoreList(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	s.Put(ctx, "plan-a", map[string]any{"content": "a"})
	s.Put(ctx, "plan-b", map[string]any{"content": "b"})
	s.Put(ctx, "plan-c", map[string]any{"content": "c"})

	result, _ := s.List(ctx)
	m := result.(map[string]any)
	if m["count"] != 3 {
		t.Errorf("count = %d, want 3", m["count"])
	}
}

func TestPlanStoreDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	s.Put(ctx, "delete-me", map[string]any{"content": "delete me"})

	if err := s.Delete(ctx, "delete-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get(ctx, "delete-me")
	if err == nil {
		t.Error("expected error for deleted plan")
	}
}

func TestPlanStoreGetNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	_, err := s.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

func TestPlanStoreDeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	if err := s.Delete(ctx, "nonexistent"); err == nil {
		t.Error("expected error for deleting nonexistent plan")
	}
}

func TestPlanStoreNameValidation(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	tests := []struct {
		name  string
		valid bool
	}{
		{"valid-plan", true},
		{"another_plan", true},
		{"a", false},          // too short
		{"../escape", false},  // path traversal
		{"UPPERCASE", false},  // not lowercase
		{"", false},           // empty
	}
	for _, tt := range tests {
		_, err := s.Put(ctx, tt.name, map[string]any{"content": "x"})
		if tt.valid && err != nil {
			t.Errorf("expected valid name %q to pass, got error: %v", tt.name, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("expected invalid name %q to fail", tt.name)
		}
	}
}

func TestPlanStorePutRequiresContent(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	_, err := s.Put(ctx, "empty-plan", map[string]any{})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestPlanStoreFileExtension(t *testing.T) {
	dir := t.TempDir()
	s := NewPlanStore(dir)
	ctx := context.Background()

	s.Put(ctx, "extension-test", map[string]any{"content": "test"})

	// Files on disk should have .md extension
	files, _ := filepath.Glob(dir + "/*")
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if filepath.Ext(files[0]) != ".md" {
		t.Errorf("expected .md extension, got %q", filepath.Ext(files[0]))
	}
}
