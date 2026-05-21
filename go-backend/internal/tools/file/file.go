package file

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

// SandboxFileOps provides file operation capabilities inside a sandbox.
type SandboxFileOps interface {
	ReadFile(ctx context.Context, path string) (string, error)
	WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error)
	EditFile(ctx context.Context, path string, oldStr, newStr string, replaceAll bool) (string, error)
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

func (s *SimpleSandboxOps) EditFile(ctx context.Context, path string, oldStr, newStr string, replaceAll bool) (string, error) {
	data, err := os.ReadFile(s.absPath(path))
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

	if err := os.WriteFile(s.absPath(path), []byte(newContent), 0644); err != nil {
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
	root := s.absPath(path)

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
	root := s.absPath(path)

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
	globPattern := pattern
	if !strings.Contains(globPattern, "/") {
		globPattern = "**/" + globPattern
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
		// Match the filename against the original pattern
		matched, _ := filepath.Match(pattern, d.Name())
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

func (s *SimpleSandboxOps) absPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return s.BasePath + "/" + p
}

// ReadTool implements core.Tool for reading files.
type ReadTool struct {
	ops   SandboxFileOps
	media MediaDescriber
}

func NewReadTool(ops SandboxFileOps, media MediaDescriber) *ReadTool {
	return &ReadTool{ops: ops, media: media}
}

func (t *ReadTool) Name() string        { return "file_read" }
func (t *ReadTool) Description() string {
	return fmt.Sprintf(
		"Read file contents. Supports text files (truncated to %d lines or %s), "+
			"images (png/jpg/gif/webp), documents (pdf/ppt/pptx), and audio (wav/mp3/flac). "+
			"Use offset/limit for large text files.",
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

	// Check if this is a multimodal file type (image, audio, document)
	ext := filepath.Ext(path)
	if toolName, ok := isMultimodalExt(ext); ok {
		return t.readMultimodal(ctx, path, toolName)
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
	output := addLineNumbers(tr.Content, offset)
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

// readMultimodal handles non-text files (images, audio, documents) via the MediaDescriber.
func (t *ReadTool) readMultimodal(ctx context.Context, path string, toolName string) (any, error) {
	if t.media == nil {
		ext := filepath.Ext(path)
		return nil, core.NewToolError("file_read",
			fmt.Sprintf("cannot read %s file: media analysis not available", ext))
	}

	// Resolve to absolute path for the media describer
	absPath := path
	if !filepath.IsAbs(path) {
		if baseOps, ok := t.ops.(*SimpleSandboxOps); ok {
			absPath = baseOps.absPath(path)
		}
	}

	desc, err := t.media.Describe(ctx, absPath, toolName)
	if err != nil {
		return nil, core.NewToolError("file_read",
			fmt.Sprintf("failed to analyze %s file: %v", filepath.Ext(path), err))
	}

	return map[string]any{
		"content": desc,
		"path":    path,
		"media":   true,
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

// addLineNumbers prepends line numbers to each line, starting at startLine.
func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	// Determine width for alignment
	maxLine := startLine + len(lines) - 1
	width := len(strconv.Itoa(maxLine))
	if width < 4 {
		width = 4
	}

	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%*d|%s", width, startLine+i, line)
	}
	return b.String()
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
