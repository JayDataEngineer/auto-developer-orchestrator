package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpillStore_SpillAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewSpillStore(dir)

	content := "this is a large tool result with lots of data"
	preview := "this is a large..."

	entry, err := store.Spill("bash", "call-123", content, preview)
	if err != nil {
		t.Fatalf("Spill() error: %v", err)
	}

	if entry.Ref == "" {
		t.Fatal("expected non-empty ref")
	}
	if entry.Size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), entry.Size)
	}

	loaded, err := store.Load(entry.Ref)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded != content {
		t.Fatalf("expected %q, got %q", content, loaded)
	}

	if store.Count() != 1 {
		t.Fatalf("expected count 1, got %d", store.Count())
	}
	if store.TotalBytes() != int64(len(content)) {
		t.Fatalf("expected %d bytes, got %d", len(content), store.TotalBytes())
	}
}

func TestSpillStore_Cleanup(t *testing.T) {
	dir := t.TempDir()
	store := NewSpillStore(dir)

	_, err := store.Spill("tool", "id", "content", "pre")
	if err != nil {
		t.Fatalf("Spill() error: %v", err)
	}

	err = store.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("expected spill dir to be removed")
	}
}

func TestSpillStore_LoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	store := NewSpillStore(dir)

	_, err := store.Load("spill-nope")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestSpillStore_SameContentSameRef(t *testing.T) {
	dir := t.TempDir()
	store := NewSpillStore(dir)

	content := "identical content"
	e1, _ := store.Spill("a", "1", content, "p")
	e2, _ := store.Spill("b", "2", content, "p")

	if e1.Ref != e2.Ref {
		t.Fatalf("same content should produce same ref: %s vs %s", e1.Ref, e2.Ref)
	}

	// File should still exist and be readable
	loaded, err := store.Load(e1.Ref)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded != content {
		t.Fatalf("expected %q, got %q", content, loaded)
	}
}

func TestSpillStore_LargeContent(t *testing.T) {
	dir := t.TempDir()
	store := NewSpillStore(dir)

	// Simulate a 50KB tool result
	content := make([]byte, 50*1024)
	for i := range content {
		content[i] = 'A' + byte(i%26)
	}

	entry, err := store.Spill("bash", "big-1", string(content), "preview")
	if err != nil {
		t.Fatalf("Spill() error: %v", err)
	}

	// Verify file on disk
	filePath := filepath.Join(dir, entry.Ref+".txt")
	stat, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}
	if stat.Size() != int64(len(content)) {
		t.Fatalf("expected file size %d, got %d", len(content), stat.Size())
	}

	loaded, err := store.Load(entry.Ref)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != len(content) {
		t.Fatalf("expected %d bytes loaded, got %d", len(content), len(loaded))
	}
}
