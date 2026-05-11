package context

import (
	"testing"
)

func TestScratchStore_WriteAndRead(t *testing.T) {
	store := NewScratchStore()

	store.Write("plan", "Step 1: design\nStep 2: implement", nil)
	store.Write("findings", "API returns JSON", []string{"api", "json"})

	note, ok := store.Read("plan")
	if !ok {
		t.Fatal("expected to find plan note")
	}
	if note.Content != "Step 1: design\nStep 2: implement" {
		t.Fatalf("unexpected content: %q", note.Content)
	}

	note2, ok := store.Read("findings")
	if !ok {
		t.Fatal("expected to find findings note")
	}
	if len(note2.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(note2.Tags))
	}
}

func TestScratchStore_UpdateExisting(t *testing.T) {
	store := NewScratchStore()

	store.Write("plan", "v1", nil)
	store.Write("plan", "v2", []string{"updated"})

	note, ok := store.Read("plan")
	if !ok {
		t.Fatal("expected to find plan note")
	}
	if note.Content != "v2" {
		t.Fatalf("expected v2, got %q", note.Content)
	}
	if len(note.Tags) != 1 || note.Tags[0] != "updated" {
		t.Fatalf("unexpected tags: %v", note.Tags)
	}
}

func TestScratchStore_Delete(t *testing.T) {
	store := NewScratchStore()

	store.Write("temp", "data", nil)
	deleted := store.Delete("temp")
	if !deleted {
		t.Fatal("expected delete to return true")
	}

	_, ok := store.Read("temp")
	if ok {
		t.Fatal("expected note to be gone after delete")
	}

	deleted = store.Delete("nonexistent")
	if deleted {
		t.Fatal("expected delete of nonexistent to return false")
	}
}

func TestScratchStore_List(t *testing.T) {
	store := NewScratchStore()

	store.Write("a", "first", nil)
	store.Write("b", "second", nil)
	store.Write("c", "third", nil)

	notes := store.List()
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
	// Order preserved
	if notes[0].ID != "a" || notes[1].ID != "b" || notes[2].ID != "c" {
		t.Fatalf("unexpected order: %v", notes)
	}
}

func TestScratchStore_FormatForContext(t *testing.T) {
	store := NewScratchStore()

	// Empty store returns empty string
	if s := store.FormatForContext(); s != "" {
		t.Fatalf("expected empty, got %q", s)
	}

	store.Write("plan", "Step 1: design", nil)
	store.Write("findings", "works", nil)

	s := store.FormatForContext()
	if s == "" {
		t.Fatal("expected non-empty formatted output")
	}
	if !contains(s, "<scratchpad>") || !contains(s, "</scratchpad>") {
		t.Fatalf("expected scratchpad tags in: %q", s)
	}
	if !contains(s, "[plan]") || !contains(s, "[findings]") {
		t.Fatalf("expected note IDs in: %q", s)
	}
}

func TestScratchStore_FormatForContext_Truncation(t *testing.T) {
	store := NewScratchStore()

	// Note longer than 500 chars should be truncated
	longContent := make([]byte, 600)
	for i := range longContent {
		longContent[i] = 'x'
	}
	store.Write("big", string(longContent), nil)

	s := store.FormatForContext()
	if contains(s, string(longContent)) {
		t.Fatal("expected long content to be truncated in format")
	}
	if !contains(s, "...") {
		t.Fatal("expected truncation indicator")
	}
}

func TestScratchWriteTool_Execute(t *testing.T) {
	store := NewScratchStore()
	tool := NewScratchWriteTool(store)

	result, err := tool.Execute(nil, map[string]any{
		"id":      "plan",
		"content": "do the thing",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if m["written"] != true {
		t.Fatal("expected written=true")
	}

	note, ok := store.Read("plan")
	if !ok {
		t.Fatal("expected note in store")
	}
	if note.Content != "do the thing" {
		t.Fatalf("unexpected content: %q", note.Content)
	}
}

func TestScratchReadTool_Execute(t *testing.T) {
	store := NewScratchStore()
	store.Write("plan", "test content", []string{"tag1"})

	tool := NewScratchReadTool(store)
	result, err := tool.Execute(nil, map[string]any{"id": "plan"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if m["content"] != "test content" {
		t.Fatalf("unexpected content: %v", m["content"])
	}
}

func TestScratchClearTool_Execute(t *testing.T) {
	store := NewScratchStore()
	store.Write("a", "1", nil)
	store.Write("b", "2", nil)

	tool := NewScratchClearTool(store)
	result, err := tool.Execute(nil, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if m["cleared"] != 2 {
		t.Fatalf("expected cleared=2, got %v", m["cleared"])
	}

	if len(store.List()) != 0 {
		t.Fatal("expected empty store after clear")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || searchSubstring(s, sub))
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
