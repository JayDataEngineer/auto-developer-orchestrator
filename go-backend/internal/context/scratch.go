package context

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ScratchStore is an in-memory scratch pad for externalizing working memory.
// Notes survive compaction because they live outside the session tree.
type ScratchStore struct {
	mu    sync.Mutex
	notes map[string]ScratchNote
	order []string
}

// ScratchNote is a single scratch pad entry.
type ScratchNote struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewScratchStore() *ScratchStore {
	return &ScratchStore{notes: make(map[string]ScratchNote)}
}

func (s *ScratchStore) Write(id, content string, tags []string) ScratchNote {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if existing, ok := s.notes[id]; ok {
		existing.Content = content
		existing.Tags = tags
		existing.UpdatedAt = now
		s.notes[id] = existing
		return existing
	}

	note := ScratchNote{
		ID:        id,
		Content:   content,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.notes[id] = note
	s.order = append(s.order, id)
	return note
}

func (s *ScratchStore) Read(id string) (ScratchNote, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	return n, ok
}

func (s *ScratchStore) List() []ScratchNote {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]ScratchNote, 0, len(s.order))
	for _, id := range s.order {
		result = append(result, s.notes[id])
	}
	return result
}

func (s *ScratchStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[id]; !ok {
		return false
	}
	delete(s.notes, id)
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return true
}

// FormatForContext returns scratch notes formatted for injection into context.
func (s *ScratchStore) FormatForContext() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<scratchpad>\n")
	for _, id := range s.order {
		n := s.notes[id]
		content := n.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		b.WriteString(fmt.Sprintf("  [%s] %s\n", n.ID, content))
	}
	b.WriteString("</scratchpad>")
	return b.String()
}

// ── Tools ──

// ScratchWriteTool writes a note to the scratch pad.
type ScratchWriteTool struct {
	store *ScratchStore
}

func NewScratchWriteTool(store *ScratchStore) *ScratchWriteTool {
	return &ScratchWriteTool{store: store}
}

func (t *ScratchWriteTool) Name() string { return "scratch_write" }

func (t *ScratchWriteTool) Description() string {
	return "Write a note to your scratch pad for externalizing working memory. Notes survive context compaction. Use short IDs like 'plan', 'findings', 'api-design'."
}

func (t *ScratchWriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "Note identifier (e.g., 'plan', 'findings', 'api-design')"},
			"content": {"type": "string", "description": "The content to store"},
			"tags": {"type": "array", "items": {"type": "string"}, "description": "Optional tags for categorization"}
		},
		"required": ["id", "content"]
	}`)
}

func (t *ScratchWriteTool) Execute(_ context.Context, args map[string]any) (any, error) {
	id, _ := args["id"].(string)
	content, _ := args["content"].(string)
	var tags []string
	if raw, ok := args["tags"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
	}
	if id == "" || content == "" {
		return nil, fmt.Errorf("scratch_write: missing required parameters 'id' and 'content'")
	}
	note := t.store.Write(id, content, tags)
	return map[string]any{"id": note.ID, "written": true}, nil
}

// ScratchReadTool reads a scratch note by ID.
type ScratchReadTool struct {
	store *ScratchStore
}

func NewScratchReadTool(store *ScratchStore) *ScratchReadTool {
	return &ScratchReadTool{store: store}
}

func (t *ScratchReadTool) Name() string { return "scratch_read" }

func (t *ScratchReadTool) Description() string {
	return "Read a scratch pad note by ID. Returns full content."
}

func (t *ScratchReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "Note identifier to read"}
		},
		"required": ["id"]
	}`)
}

func (t *ScratchReadTool) Execute(_ context.Context, args map[string]any) (any, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("scratch_read: missing required parameter 'id'")
	}
	note, ok := t.store.Read(id)
	if !ok {
		return nil, fmt.Errorf("scratch_read: note %q not found", id)
	}
	return map[string]any{"id": note.ID, "content": note.Content, "tags": note.Tags}, nil
}

// ScratchClearTool clears all scratch notes.
type ScratchClearTool struct {
	store *ScratchStore
}

func NewScratchClearTool(store *ScratchStore) *ScratchClearTool {
	return &ScratchClearTool{store: store}
}

func (t *ScratchClearTool) Name() string { return "scratch_clear" }

func (t *ScratchClearTool) Description() string {
	return "Clear all scratch pad notes."
}

func (t *ScratchClearTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *ScratchClearTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	notes := t.store.List()
	for _, n := range notes {
		t.store.Delete(n.ID)
	}
	return map[string]any{"cleared": len(notes)}, nil
}

// LoadSpilledTool retrieves the full content of a previously offloaded tool result.
type LoadSpilledTool struct {
	mgr ContextManager
}

func NewLoadSpilledTool(mgr ContextManager) *LoadSpilledTool {
	return &LoadSpilledTool{mgr: mgr}
}

func (t *LoadSpilledTool) Name() string { return "load_spilled" }

func (t *LoadSpilledTool) Description() string {
	return "Retrieve the full content of a previously offloaded tool result by its spill reference (e.g., 'spill-a3f2b1'). Use this when you need the complete output that was too large for context."
}

func (t *LoadSpilledTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"ref": {"type": "string", "description": "The spill reference ID (e.g., 'spill-a3f2b1')"}
		},
		"required": ["ref"]
	}`)
}

func (t *LoadSpilledTool) Execute(_ context.Context, args map[string]any) (any, error) {
	ref, _ := args["ref"].(string)
	if ref == "" {
		return nil, fmt.Errorf("load_spilled: missing required parameter 'ref'")
	}
	content, err := t.mgr.LoadSpilledContent(ref)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ref": ref, "content": content}, nil
}
