package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
)

// FolderStore manages a directory of markdown memory docs at <project>/.pux/memory/.
type FolderStore struct {
	mu         sync.Mutex
	projectDir string
	bashExec   bash.Executor
}

// NewFolderStore creates a folder-based memory store.
func NewFolderStore(projectDir string, bashExec bash.Executor) *FolderStore {
	return &FolderStore{
		projectDir: projectDir,
		bashExec:   bashExec,
	}
}

// memoryDir returns the absolute path to the memory folder.
func (s *FolderStore) memoryDir() string {
	return filepath.Join(s.projectDir, ".pux", "memory")
}

// InjectIndex returns a summary of all memory docs for prompt injection.
// Returns empty string if no docs exist.
func (s *FolderStore) InjectIndex() string {
	docs, err := s.listDocs()
	if err != nil || len(docs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<memory-index>\n")
	for _, d := range docs {
		sb.WriteString(fmt.Sprintf("- %s (updated %s, commit %s)\n", d.Name, d.Updated.Format("2006-01-02"), d.Commit))
	}
	sb.WriteString("</memory-index>\n")
	return sb.String()
}

// InjectPrefix returns either the folder index (new system) or the old MEMORY.md content.
// Used for backwards compatibility.
func (s *FolderStore) InjectPrefix() string {
	// Try new folder-based memory first
	if idx := s.InjectIndex(); idx != "" {
		return idx + "\n"
	}
	return ""
}

// docMeta is parsed from YAML frontmatter.
type docMeta struct {
	Updated time.Time
	Commit  string
}

// docEntry is a listed doc with metadata.
type docEntry struct {
	Name    string
	Updated time.Time
	Commit  string
}

// save writes a memory doc with auto-stamped frontmatter.
func (s *FolderStore) save(ctx context.Context, relPath, content string) error {
	if relPath == "" {
		return fmt.Errorf("path is required")
	}
	if err := validatePath(relPath); err != nil {
		return err
	}

	// Per-file size cap
	if len(content) > 50*1024 {
		content = content[:50*1024]
	}

	// Total folder size cap
	if err := s.checkFolderSize(); err != nil {
		return err
	}

	// Get short commit hash
	commit := s.getCommit(ctx)

	// Build doc with frontmatter
	now := time.Now().UTC()
	doc := fmt.Sprintf("---\nupdated: %s\ncommit: %s\n---\n%s",
		now.Format(time.RFC3339), commit, content)

	// Write atomically
	fullPath := filepath.Join(s.memoryDir(), relPath+".md")
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	tmpPath := fullPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(doc), 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, fullPath)
}

// recall reads a specific doc or lists all docs.
func (s *FolderStore) recall(relPath string) (any, error) {
	if relPath == "" {
		// List all docs
		docs, err := s.listDocs()
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return map[string]any{"docs": []docEntry{}, "count": 0}, nil
		}
		return map[string]any{"docs": docs, "count": len(docs)}, nil
	}

	if err := validatePath(relPath); err != nil {
		return nil, err
	}

	fullPath := filepath.Join(s.memoryDir(), relPath+".md")
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, core.NewToolError("memory", fmt.Sprintf("doc not found: %s", relPath))
		}
		return nil, err
	}

	body := string(data)
	// Strip frontmatter for the response
	content := stripFrontmatter(body)
	meta := parseFrontmatter(body)

	return map[string]any{
		"path":    relPath,
		"content": content,
		"updated": meta.Updated.Format(time.RFC3339),
		"commit":  meta.Commit,
	}, nil
}

// delete removes a memory doc.
func (s *FolderStore) deleteDoc(relPath string) error {
	if relPath == "" {
		return fmt.Errorf("path is required")
	}
	if err := validatePath(relPath); err != nil {
		return err
	}

	fullPath := filepath.Join(s.memoryDir(), relPath+".md")
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// listDocs reads the memory directory and returns all .md files with metadata.
func (s *FolderStore) listDocs() ([]docEntry, error) {
	dir := s.memoryDir()
	var entries []docEntry

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		// Strip .md extension for display
		name := strings.TrimSuffix(rel, ".md")

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		meta := parseFrontmatter(string(data))

		entries = append(entries, docEntry{
			Name:    name,
			Updated: meta.Updated,
			Commit:  meta.Commit,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by name for deterministic order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// getCommit returns the short git hash for the project.
func (s *FolderStore) getCommit(ctx context.Context) string {
	if s.bashExec == nil {
		return "unknown"
	}
	out, err := s.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git rev-parse --short HEAD 2>/dev/null", s.projectDir))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out)
}

// checkFolderSize returns error if total memory exceeds 500KB.
func (s *FolderStore) checkFolderSize() error {
	dir := s.memoryDir()
	var total int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	if total > 500*1024 {
		return fmt.Errorf("memory folder exceeds 500KB limit (current: %d bytes). Delete old docs first.", total)
	}
	return nil
}

// validatePath rejects path traversal and absolute paths.
func validatePath(p string) error {
	if strings.Contains(p, "..") {
		return fmt.Errorf("path must not contain '..': %s", p)
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("path must be relative: %s", p)
	}
	// Only allow alphanumeric, slashes, hyphens, underscores
	for _, ch := range p {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '/' || ch == '-' || ch == '_' || ch == ' ') {
			return fmt.Errorf("path contains invalid character '%c': %s", ch, p)
		}
	}
	return nil
}

// parseFrontmatter extracts updated and commit from YAML frontmatter.
func parseFrontmatter(content string) docMeta {
	meta := docMeta{}
	if !strings.HasPrefix(content, "---\n") {
		return meta
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return meta
	}
	fm := content[4 : 4+end]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "updated:") {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(line, "updated:")))
			if err == nil {
				meta.Updated = t
			}
		}
		if strings.HasPrefix(line, "commit:") {
			meta.Commit = strings.TrimSpace(strings.TrimPrefix(line, "commit:"))
		}
	}
	return meta
}

// stripFrontmatter removes the YAML frontmatter block.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return content
	}
	return content[4+end+5:] // skip "---\n...\n---\n"
}

// FolderTool implements core.Tool for folder-based memory.
type FolderTool struct {
	store *FolderStore
}

// NewFolderTool creates a new memory tool backed by a folder store.
func NewFolderTool(store *FolderStore) *FolderTool {
	return &FolderTool{store: store}
}

func (t *FolderTool) Name() string { return "memory" }

func (t *FolderTool) Description() string {
	return "Save, recall, or delete memory docs. Memory persists across sessions at .pux/memory/. Use 'recall' without a path to list all docs."
}

func (t *FolderTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["save", "recall", "delete"],
				"description": "Action: save (create/update doc), recall (read doc or list all), delete (remove doc)"
			},
			"path": {
				"type": "string",
				"description": "Doc path relative to .pux/memory/ (no .md extension). Required for save/delete. Omit for recall to list all docs."
			},
			"content": {
				"type": "string",
				"description": "Markdown content for the doc. Only used with save action."
			}
		},
		"required": ["action"]
	}`)
}

func (t *FolderTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	t.store.mu.Lock()
	defer t.store.mu.Unlock()

	action, _ := args["action"].(string)
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	switch action {
	case "save":
		if path == "" {
			return nil, core.NewToolError("memory", "path is required for save")
		}
		if content == "" {
			return nil, core.NewToolError("memory", "content is required for save")
		}
		if err := t.store.save(ctx, path, content); err != nil {
			return nil, err
		}
		return tools.QuarantineResult(map[string]any{"saved": true, "path": path}), nil

	case "recall":
		result, err := t.store.recall(path)
		if err != nil {
			return nil, err
		}
		// Recall returns agent-stored memory docs verbatim. Memory content
		// is the highest-risk untrusted input in the system — agents store
		// transcripts, web research, MCP results that may contain injection
		// patterns. QuarantineResult wraps suspicious lines in
		// <suspicious_input> tags so the model sees them as data.
		return tools.QuarantineResult(result), nil

	case "delete":
		if path == "" {
			return nil, core.NewToolError("memory", "path is required for delete")
		}
		if err := t.store.deleteDoc(path); err != nil {
			return nil, err
		}
		return tools.QuarantineResult(map[string]any{"deleted": true, "path": path}), nil

	default:
		return nil, core.NewToolError("memory", fmt.Sprintf("unknown action: %s (use save, recall, or delete)", action))
	}
}
