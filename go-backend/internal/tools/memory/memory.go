package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Store persists project memory (MEMORY.md file).
type Store struct {
	mu         sync.Mutex
	projectDir string
	cache      string
	loaded     bool
}

// NewStore creates a memory store for a project directory.
func NewStore(projectDir string) *Store {
	return &Store{projectDir: projectDir}
}

// NewProjectMemory is an alias for NewStore (backward compat with old API).
func NewProjectMemory(projectDir string) *Store {
	s := NewStore(projectDir)
	s.Read() // Load eagerly
	return s
}

// InjectPrefix returns the memory content formatted for injection into prompts.
func (s *Store) InjectPrefix() string {
	c := s.Read()
	if c == "" {
		return ""
	}
	return "<memory>\n" + c + "\n</memory>\n\n"
}

// ReadMemoryFile reads a MEMORY.md file from disk (convenience function).
func ReadMemoryFile(projectDir string) string {
	path := filepath.Join(projectDir, "MEMORY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s *Store) Read() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.cache
	}
	s.loaded = true
	path := filepath.Join(s.projectDir, "MEMORY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s.cache = string(data)
	return s.cache
}

func (s *Store) Write(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cap at 200 lines / 25KB
	lines := strings.Split(content, "\n")
	if len(lines) > 200 {
		lines = lines[:200]
		content = strings.Join(lines, "\n")
	}
	if len(content) > 25000 {
		content = content[:25000]
	}

	path := filepath.Join(s.projectDir, "MEMORY.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// Atomic write via temp file
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	s.cache = content
	return nil
}

// Tool implements core.Tool for updating project memory.
type Tool struct {
	store *Store
}

func NewTool(store *Store) *Tool {
	return &Tool{store: store}
}

func (t *Tool) Name() string        { return "update_memory" }
func (t *Tool) Description() string { return "Persist information to project memory across sessions" }

func (t *Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"key": {"type": "string", "description": "The fact or preference to remember"},
			"section": {"type": "string", "description": "Section to store under (User Preferences, Project Facts, Feedback, Reference)"}
		},
		"required": ["key"]
	}`)
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	key, _ := args["key"].(string)
	section, _ := args["section"].(string)
	if key == "" {
		return nil, core.NewToolError("update_memory", "missing required parameter 'key'")
	}

	if section == "" {
		section = "Project Facts"
	}

	existing := t.store.Read()
	var newContent string
	if existing == "" {
		newContent = fmt.Sprintf("# MEMORY\n\n## %s\n- %s\n", section, key)
	} else {
		// Append to the appropriate section
		sectionHeader := "## " + section
		newLines := []string{existing}
		if idx := strings.Index(existing, sectionHeader); idx >= 0 {
			// Insert after section header
			endOfHeader := idx + len(sectionHeader)
			newLines = []string{
				existing[:endOfHeader],
				"\n- " + key,
				existing[endOfHeader:],
			}
		} else {
			newLines = []string{existing, "\n\n" + sectionHeader + "\n- " + key}
		}
		newContent = strings.Join(newLines, "")
	}

	if err := t.store.Write(newContent); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "section": section, "key": key}, nil
}
