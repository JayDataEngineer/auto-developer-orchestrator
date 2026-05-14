package autoconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
)

func TestMemoryStoreList(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	// Empty
	result, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 0 {
		t.Errorf("count = %d, want 0", m["count"])
	}

	// With content
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Hello"), 0644)
	result, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	m = result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %d, want 1", m["count"])
	}
	items := m["items"].([]string)
	if len(items) != 1 || items[0] != "memory" {
		t.Errorf("items = %v, want [memory]", items)
	}
}

func TestMemoryStoreGet(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	inner.Write("# My Memory\n\n## Facts\n- fact 1\n")

	result, err := s.Get(ctx, "memory")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := result.(map[string]any)
	if m["name"] != "memory" {
		t.Errorf("name = %q, want memory", m["name"])
	}
	if m["content"] != "# My Memory\n\n## Facts\n- fact 1\n" {
		t.Errorf("content = %q", m["content"])
	}
}

func TestMemoryStoreGetWrongName(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	_, err := s.Get(ctx, "not-memory")
	if err == nil {
		t.Error("expected error for wrong name")
	}
}

func TestMemoryStorePut(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	result, err := s.Put(ctx, "memory", map[string]any{"content": "# Replaced\n\nNew content."})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	_ = result

	content := inner.Read()
	if content != "# Replaced\n\nNew content." {
		t.Errorf("content = %q", content)
	}
}

func TestMemoryStorePutWrongName(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	_, err := s.Put(ctx, "wrong", map[string]any{"content": "x"})
	if err == nil {
		t.Error("expected error for wrong name")
	}
}

func TestMemoryStorePutMissingContent(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	_, err := s.Put(ctx, "memory", map[string]any{})
	if err == nil {
		t.Error("expected error for missing content")
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	inner.Write("# To delete")

	if err := s.Delete(ctx, "memory"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	content := inner.Read()
	if content != "" {
		t.Errorf("expected empty after delete, got %q", content)
	}
}

func TestMemoryStoreDeleteWrongName(t *testing.T) {
	dir := t.TempDir()
	inner := memory.NewStore(dir)
	s := NewMemoryStore(inner, dir)
	ctx := context.Background()

	if err := s.Delete(ctx, "wrong"); err == nil {
		t.Error("expected error for wrong name")
	}
}
