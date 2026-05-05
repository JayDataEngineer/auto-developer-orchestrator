package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SandboxFileOps provides file operation capabilities inside a sandbox.
type SandboxFileOps interface {
	ReadFile(ctx context.Context, path string) (string, error)
	WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error)
	EditFile(ctx context.Context, path string, oldStr, newStr string) (string, error)
	Grep(ctx context.Context, path string, pattern string) (string, error)
	Glob(ctx context.Context, path string, pattern string) (string, error)
}

// SimpleSandboxOps implements SandboxFileOps using the local filesystem.
type SimpleSandboxOps struct {
	BasePath string
}

func (s *SimpleSandboxOps) ReadFile(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(s.absPath(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *SimpleSandboxOps) WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error) {
	fullPath := s.absPath(path)
	if !overwrite {
		if _, err := os.Stat(fullPath); err == nil {
			return "", fmt.Errorf("file already exists, use overwrite=true")
		}
	}
	if err := os.MkdirAll(fullPath[:max(strings.LastIndex(fullPath, "/"), 0)], 0755); err != nil {
		return "", err
	}
	err := os.WriteFile(fullPath, []byte(content), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path), nil
}

func (s *SimpleSandboxOps) EditFile(ctx context.Context, path string, oldStr, newStr string) (string, error) {
	data, err := os.ReadFile(s.absPath(path))
	if err != nil {
		return "", err
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		return "", fmt.Errorf("oldString not found in file")
	}
	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(s.absPath(path), []byte(newContent), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Replaced 1 occurrence in %s", path), nil
}

func (s *SimpleSandboxOps) Grep(ctx context.Context, path string, pattern string) (string, error) {
	return "", fmt.Errorf("grep not implemented")
}

func (s *SimpleSandboxOps) Glob(ctx context.Context, path string, pattern string) (string, error) {
	return "", fmt.Errorf("glob not implemented")
}

func (s *SimpleSandboxOps) absPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return s.BasePath + "/" + p
}

// ReadTool implements core.Tool for reading files.
type ReadTool struct {
	ops SandboxFileOps
}

func NewReadTool(ops SandboxFileOps) *ReadTool {
	return &ReadTool{ops: ops}
}

func (t *ReadTool) Name() string        { return "file_read" }
func (t *ReadTool) Description() string { return "Read a file from the filesystem" }

func (t *ReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to read"},
			"offset": {"type": "integer", "description": "Line number to start reading from (1-indexed)"},
			"limit": {"type": "integer", "description": "Maximum number of lines to read"}
		},
		"required": ["file_path"]
	}`)
}

func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["file_path"].(string)
	if path == "" {
		return nil, core.NewToolError("file_read", "missing required parameter 'file_path'")
	}
	content, err := t.ops.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": content, "path": path}, nil
}

// WriteTool implements core.Tool for writing files.
type WriteTool struct {
	ops SandboxFileOps
}

func NewWriteTool(ops SandboxFileOps) *WriteTool {
	return &WriteTool{ops: ops}
}

func (t *WriteTool) Name() string        { return "file_write" }
func (t *WriteTool) Description() string { return "Create or overwrite a file" }

func (t *WriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to write"},
			"content": {"type": "string", "description": "Content to write to the file"},
			"overwrite": {"type": "boolean", "description": "If true, overwrite existing file"}
		},
		"required": ["file_path", "content"]
	}`)
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["file_path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return nil, core.NewToolError("file_write", "missing required parameter 'file_path'")
	}
	if content == "" {
		return nil, core.NewToolError("file_write", "missing required parameter 'content'")
	}
	overwrite := false
	if v, ok := args["overwrite"].(bool); ok {
		overwrite = v
	}
	result, err := t.ops.WriteFile(ctx, path, content, overwrite)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result, "path": path}, nil
}

// EditTool implements core.Tool for editing files.
type EditTool struct {
	ops SandboxFileOps
}

func NewEditTool(ops SandboxFileOps) *EditTool {
	return &EditTool{ops: ops}
}

func (t *EditTool) Name() string        { return "file_edit" }
func (t *EditTool) Description() string { return "Replace a string in a file" }

func (t *EditTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to edit"},
			"old_string": {"type": "string", "description": "The text to replace"},
			"new_string": {"type": "string", "description": "The text to replace it with"}
		},
		"required": ["file_path", "old_string", "new_string"]
	}`)
}

func (t *EditTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["file_path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	if path == "" || oldStr == "" {
		return nil, core.NewToolError("file_edit", "missing required parameters")
	}
	result, err := t.ops.EditFile(ctx, path, oldStr, newStr)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result, "path": path}, nil
}

// GrepTool implements core.Tool for searching file contents.
type GrepTool struct {
	ops SandboxFileOps
}

func NewGrepTool(ops SandboxFileOps) *GrepTool {
	return &GrepTool{ops: ops}
}

func (t *GrepTool) Name() string        { return "file_grep" }
func (t *GrepTool) Description() string { return "Search file contents using regex" }

func (t *GrepTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Regex pattern to search for"},
			"path": {"type": "string", "description": "Directory or file to search in"}
		},
		"required": ["pattern"]
	}`)
}

func (t *GrepTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pattern, _ := args["pattern"].(string)
	path, _ := args["path"].(string)
	if pattern == "" {
		return nil, core.NewToolError("file_grep", "missing required parameter 'pattern'")
	}
	if path == "" {
		path = "."
	}
	result, err := t.ops.Grep(ctx, path, pattern)
	if err != nil {
		return nil, err
	}
	return map[string]any{"matches": result, "pattern": pattern}, nil
}

// GlobTool implements core.Tool for file pattern matching.
type GlobTool struct {
	ops SandboxFileOps
}

func NewGlobTool(ops SandboxFileOps) *GlobTool {
	return &GlobTool{ops: ops}
}

func (t *GlobTool) Name() string        { return "file_glob" }
func (t *GlobTool) Description() string { return "Find files matching a glob pattern" }

func (t *GlobTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern (e.g. *.go, src/**/*.ts)"},
			"path": {"type": "string", "description": "Directory to search in"}
		},
		"required": ["pattern"]
	}`)
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pattern, _ := args["pattern"].(string)
	path, _ := args["path"].(string)
	if pattern == "" {
		return nil, core.NewToolError("file_glob", "missing required parameter 'pattern'")
	}
	if path == "" {
		path = "."
	}
	result, err := t.ops.Glob(ctx, path, pattern)
	if err != nil {
		return nil, err
	}
	return map[string]any{"files": result, "pattern": pattern}, nil
}
