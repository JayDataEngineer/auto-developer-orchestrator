package file

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

// FileReadTracker records content hashes on file_read and validates them on file_edit.
// This detects concurrent modifications between read and edit (GAP-D).
type FileReadTracker struct {
	mu    sync.RWMutex
	hashes map[string]string // path → SHA-256 of last-read content
}

func NewFileReadTracker() *FileReadTracker {
	return &FileReadTracker{hashes: make(map[string]string)}
}

func (t *FileReadTracker) Record(path, content string) {
	h := sha256.Sum256([]byte(content))
	t.mu.Lock()
	t.hashes[path] = hex.EncodeToString(h[:])
	t.mu.Unlock()
}

// CheckAndInvalidate returns nil if the file hasn't changed since last read.
// Returns an error if the content differs. Removes the entry after check.
func (t *FileReadTracker) CheckAndInvalidate(path, currentContent string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	expected, ok := t.hashes[path]
	if !ok {
		return nil // no prior read recorded — allow edit
	}
	delete(t.hashes, path) // one-shot: invalidate after check
	h := sha256.Sum256([]byte(currentContent))
	actual := hex.EncodeToString(h[:])
	if actual != expected {
		return fmt.Errorf("file %s was modified after last read — re-read before editing", path)
	}
	return nil
}

// SandboxFileOps provides file operation capabilities inside a sandbox.
type SandboxFileOps interface {
	ReadFile(ctx context.Context, path string) (string, error)
	WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error)
	EditFile(ctx context.Context, path string, oldStr, newStr string, replaceAll bool) (string, error)
	Grep(ctx context.Context, path string, pattern string) (string, error)
	Glob(ctx context.Context, path string, pattern string) (string, error)
	// AbsPath resolves a (possibly sandbox-relative) path to an absolute host path.
	AbsPath(p string) string
}

// SimpleSandboxOps implements SandboxFileOps using the local filesystem.
type SimpleSandboxOps struct {
	BasePath string
}

func (s *SimpleSandboxOps) ReadFile(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(s.AbsPath(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *SimpleSandboxOps) WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error) {
	fullPath := s.AbsPath(path)
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

func (s *SimpleSandboxOps) EditFile(ctx context.Context, path string, oldStr, newStr string, replaceAll bool) (string, error) {
	data, err := os.ReadFile(s.AbsPath(path))
	if err != nil {
		return "", err
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		return "", fmt.Errorf("oldString not found in file")
	}

	count := strings.Count(content, oldStr)
	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		if count > 1 {
			return "", fmt.Errorf("oldString found %d times in file. Use replace_all=true to replace all occurrences, or provide a more specific string", count)
		}
		newContent = strings.Replace(content, oldStr, newStr, 1)
		count = 1
	}

	if err := os.WriteFile(s.AbsPath(path), []byte(newContent), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Replaced %d occurrence(s) in %s", count, path), nil
}

// skipDirs are directories always excluded from local file search,
// matching the ripgrep exclude list used in adapters.FileOps.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, ".git": true, ".idea": true, ".vscode": true,
	"__pycache__": true, "bin": true, "obj": true, "out": true,
	"coverage": true, "tmp": true, "temp": true, "logs": true,
	"generated": true, "bower_components": true, "jspm_packages": true,
	".cache": true, ".next": true, ".nuxt": true, ".turbo": true,
	".venv": true, "venv": true, "env": true,
	".tox": true, ".mypy_cache": true, ".pytest_cache": true,
}

func (s *SimpleSandboxOps) Grep(ctx context.Context, path string, pattern string) (string, error) {
	root := s.AbsPath(path)

	// Try ripgrep first (respects .gitignore, fast)
	if rg, _ := exec.LookPath("rg"); rg != "" {
		out, err := exec.CommandContext(ctx, rg,
			"--max-count=200", "--max-depth=6",
			"--glob", "!node_modules", "--glob", "!vendor",
			"--glob", "!.git", "--glob", "!__pycache__",
			"--glob", "!.cache", "--glob", "!dist",
			pattern, root,
		).CombinedOutput()
		if err == nil && len(out) > 0 {
			return string(out), nil
		}
	}

	// Fallback: walk filesystem and search with regexp
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	var results []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(results) >= 200 {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && name != ".." && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		// Read file and search
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		lineno := 0
		for scanner.Scan() {
			lineno++
			if re.MatchString(scanner.Text()) {
				results = append(results, fmt.Sprintf("%s:%d:%s", p, lineno, scanner.Text()))
				if len(results) >= 200 {
					break
				}
			}
		}
		return nil
	})
	if len(results) == 0 {
		return "", nil
	}
	return strings.Join(results, "\n"), nil
}

func (s *SimpleSandboxOps) Glob(ctx context.Context, path string, pattern string) (string, error) {
	root := s.AbsPath(path)

	// Try ripgrep first (respects .gitignore, fast)
	if rg, _ := exec.LookPath("rg"); rg != "" {
		out, err := exec.CommandContext(ctx, rg,
			"--files", "--glob", pattern,
			"--glob", "!node_modules", "--glob", "!vendor",
			"--glob", "!.git", "--glob", "!__pycache__",
			"--glob", "!.cache", "--glob", "!dist",
			"--max-depth", "6", "--sort=path", root,
		).CombinedOutput()
		if err == nil && len(out) > 0 {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 500 {
				lines = lines[:500]
			}
			return strings.Join(lines, "\n"), nil
		}
	}

	// Fallback: filepath.WalkDir with pattern matching
	var results []string
	// filepath.Match does NOT support `**` (recursive glob) — it treats `**` as
	// a single `*`. Strip leading `**/` so we match against the basename only,
	// which is what WalkDir gives us via d.Name().
	basePattern := pattern
	if strings.HasPrefix(basePattern, "**/") {
		basePattern = basePattern[3:]
	} else if strings.Contains(basePattern, "/") {
		// Pattern like "src/*.go" — match against the full path relative to root.
		// WalkDir gives us absolute paths; we'll match on the basename as a fallback.
		basePattern = basePattern[strings.LastIndex(basePattern, "/")+1:]
	}

	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(results) >= 500 {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && name != ".." && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		matched, _ := filepath.Match(basePattern, d.Name())
		if matched {
			results = append(results, p)
		}
		return nil
	})
	if len(results) == 0 {
		return "", nil
	}
	return strings.Join(results, "\n"), nil
}

func (s *SimpleSandboxOps) AbsPath(p string) string {
	if strings.HasPrefix(p, "/") {
		// Remap sandbox paths to the host project directory.
		// When using the native executor (no Docker), /sandbox/workspace/ doesn't
		// exist on the host — it needs to be mapped to the actual project dir.
		if s.BasePath != "" {
			if strings.HasPrefix(p, "/sandbox/workspace/") {
				remapped := s.BasePath + p[len("/sandbox/workspace"):]

				// Fix double-nesting: if the project basename appears right after
				// the BasePath, strip it. E.g., if BasePath=.../go-backend and
				// path=/sandbox/workspace/go-backend/internal/..., the remap would
				// produce .../go-backend/go-backend/internal/... — fix it to
				// .../go-backend/internal/...
				baseName := filepath.Base(s.BasePath)
				if baseName != "" && baseName != "." {
					doublePath := s.BasePath + "/" + baseName + "/"
					if strings.HasPrefix(remapped, doublePath) {
						remapped = s.BasePath + remapped[len(doublePath)-1:]
					}
				}
				return remapped
			}
			if strings.HasPrefix(p, "/sandbox/tmp/") {
				return os.TempDir() + p[len("/sandbox/tmp"):]
			}
			if p == "/sandbox/workspace" {
				return s.BasePath
			}
			if p == "/sandbox/tmp" {
				return os.TempDir()
			}
		}
		return p
	}
	return s.BasePath + "/" + p
}

// ReadTool implements core.Tool for reading files.
type ReadTool struct {
	ops     SandboxFileOps
	tracker *FileReadTracker // optional: records content hash for edit validation
}

func NewReadTool(ops SandboxFileOps) *ReadTool {
	return &ReadTool{ops: ops}
}

func NewReadToolWithTracker(ops SandboxFileOps, tracker *FileReadTracker) *ReadTool {
	return &ReadTool{ops: ops, tracker: tracker}
}

func (t *ReadTool) Name() string        { return "file_read" }
func (t *ReadTool) Description() string {
	return fmt.Sprintf(
		"Read file contents. Truncated to %d lines or %s. "+
			"Use offset/limit for large files. "+
			"ALWAYS prefer this over bash cat/head/tail — file_read is faster, handles truncation, and returns structured output.",
		truncate.FileMaxLines, truncate.FormatSize(truncate.FileMaxBytes),
	)
}

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

	// Parse optional offset (1-indexed) and limit
	offset := intArg(args, "offset", 1)
	limit := intArg(args, "limit", 0)
	if offset < 1 {
		offset = 1
	}

	content, err := t.ops.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}

	// Record content hash for concurrent modification detection
	if t.tracker != nil {
		t.tracker.Record(path, content)
	}

	// Apply offset: skip to the requested line (1-indexed → 0-indexed slice)
	lines := strings.Split(content, "\n")
	// Remove trailing empty from trailing newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)

	startIdx := offset - 1 // convert to 0-indexed
	if startIdx >= totalLines {
		return nil, core.NewToolError("file_read",
			fmt.Sprintf("offset %d is beyond end of file (%d lines total)", offset, totalLines))
	}

	// Apply user-specified limit
	var userLimit int
	var selected []string
	if limit > 0 {
		end := startIdx + limit
		if end > totalLines {
			end = totalLines
		}
		selected = lines[startIdx:end]
		userLimit = limit
	} else {
		selected = lines[startIdx:]
	}

	selectedContent := strings.Join(selected, "\n")

	// Apply truncation (respects both line and byte limits)
	tr := truncate.Head(selectedContent, truncate.FileMaxLines, truncate.FileMaxBytes)

	// Build output with line numbers and continuation message
	output := truncate.AddLineNumbers(tr.Content, offset)
	contMsg := truncate.FormatFileContinuation(tr, offset, userLimit, totalLines)
	if contMsg != "" {
		output += contMsg
	}

	return map[string]any{
		"content":     output,
		"path":        path,
		"total_lines": totalLines,
		"start_line":  offset,
		"shown_lines": tr.OutputLines,
	}, nil
}

// intArg extracts an integer argument from the args map, returning def if missing/invalid.
func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return def
		}
		return int(i)
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return def
		}
		return i
	}
	return def
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
	ops     SandboxFileOps
	tracker *FileReadTracker // optional: validates content hasn't changed since read
}

func NewEditTool(ops SandboxFileOps) *EditTool {
	return &EditTool{ops: ops}
}

func NewEditToolWithTracker(ops SandboxFileOps, tracker *FileReadTracker) *EditTool {
	return &EditTool{ops: ops, tracker: tracker}
}

func (t *EditTool) Name() string        { return "file_edit" }
func (t *EditTool) Description() string {
	return "Replace a string in a file. Set replace_all=true to replace all occurrences."
}

func (t *EditTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to edit"},
			"old_string": {"type": "string", "description": "The text to replace"},
			"new_string": {"type": "string", "description": "The text to replace it with"},
			"replace_all": {"type": "boolean", "description": "If true, replace all occurrences. Default is false (first occurrence only)."}
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
	replaceAll := false
	if v, ok := args["replace_all"].(bool); ok {
		replaceAll = v
	}
	// Normalize smart quotes to ASCII — LLMs sometimes generate curly quotes
	oldStr = normalizeQuotes(oldStr)
	newStr = normalizeQuotes(newStr)
	// Validate concurrent modification: if tracker is set and the file was
	// previously read, check the current content matches the recorded hash.
	if t.tracker != nil {
		currentContent, err := t.ops.ReadFile(ctx, path)
		if err != nil {
			return nil, core.NewToolError("file_edit", fmt.Sprintf("cannot read file for validation: %v", err))
		}
		if err := t.tracker.CheckAndInvalidate(path, currentContent); err != nil {
			return nil, core.NewToolError("file_edit", err.Error())
		}
	}
	result, err := t.ops.EditFile(ctx, path, oldStr, newStr, replaceAll)
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

// normalizeQuotes converts smart/curly quotes to ASCII equivalents.
// LLMs sometimes generate these, which breaks exact string matching.
func normalizeQuotes(s string) string {
	s = strings.ReplaceAll(s, "\u201c", `"`)  // left double quote
	s = strings.ReplaceAll(s, "\u201d", `"`)  // right double quote
	s = strings.ReplaceAll(s, "\u2018", "'")  // left single quote
	s = strings.ReplaceAll(s, "\u2019", "'")  // right single quote
	s = strings.ReplaceAll(s, "\u00ab", "<<") // left guillemet
	s = strings.ReplaceAll(s, "\u00bb", ">>") // right guillemet
	return s
}
