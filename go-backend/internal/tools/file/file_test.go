package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestSimpleSandboxOps_ReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0644)

	ops := &SimpleSandboxOps{BasePath: dir}
	content, err := ops.ReadFile(context.Background(), "test.txt")
	testutil.AssertNoError(t, err)
	if content != "hello" {
		t.Errorf("expected 'hello', got %q", content)
	}
}

func TestSimpleSandboxOps_ReadFile_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("absolute"), 0644)

	ops := &SimpleSandboxOps{BasePath: "/nonexistent"}
	content, err := ops.ReadFile(context.Background(), path)
	testutil.AssertNoError(t, err)
	if content != "absolute" {
		t.Errorf("expected 'absolute', got %q", content)
	}
}

func TestSimpleSandboxOps_ReadFile_NotFound(t *testing.T) {
	ops := &SimpleSandboxOps{BasePath: t.TempDir()}
	_, err := ops.ReadFile(context.Background(), "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestSimpleSandboxOps_WriteFile(t *testing.T) {
	dir := t.TempDir()
	ops := &SimpleSandboxOps{BasePath: dir}

	result, err := ops.WriteFile(context.Background(), "new.txt", "content", false)
	testutil.AssertNoError(t, err)
	if result == "" {
		t.Error("expected non-empty result")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "new.txt"))
	if string(data) != "content" {
		t.Errorf("expected 'content', got %q", string(data))
	}
}

func TestSimpleSandboxOps_WriteFile_NoOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("original"), 0644)

	ops := &SimpleSandboxOps{BasePath: dir}
	_, err := ops.WriteFile(context.Background(), "existing.txt", "new", false)
	if err == nil {
		t.Fatal("expected error for overwrite without flag")
	}
}

func TestSimpleSandboxOps_WriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("original"), 0644)

	ops := &SimpleSandboxOps{BasePath: dir}
	result, err := ops.WriteFile(context.Background(), "existing.txt", "modified", true)
	testutil.AssertNoError(t, err)
	if result == "" {
		t.Error("expected non-empty result")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "modified" {
		t.Errorf("expected 'modified', got %q", string(data))
	}
}

func TestSimpleSandboxOps_WriteFile_CreatesDirs(t *testing.T) {
	dir := t.TempDir()
	ops := &SimpleSandboxOps{BasePath: dir}

	_, err := ops.WriteFile(context.Background(), "subdir/nested/file.txt", "content", false)
	testutil.AssertNoError(t, err)

	data, _ := os.ReadFile(filepath.Join(dir, "subdir/nested/file.txt"))
	if string(data) != "content" {
		t.Errorf("expected 'content', got %q", string(data))
	}
}

func TestSimpleSandboxOps_EditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("hello world foo"), 0644)

	ops := &SimpleSandboxOps{BasePath: dir}
	result, err := ops.EditFile(context.Background(), "edit.txt", "world", "there")
	testutil.AssertNoError(t, err)
	if result == "" {
		t.Error("expected non-empty result")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello there foo" {
		t.Errorf("expected 'hello there foo', got %q", string(data))
	}
}

func TestSimpleSandboxOps_EditFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	ops := &SimpleSandboxOps{BasePath: dir}
	_, err := ops.EditFile(context.Background(), "edit.txt", "nonexistent", "replacement")
	if err == nil {
		t.Fatal("expected error for oldString not found")
	}
}

func TestSimpleSandboxOps_EditFile_FileNotFound(t *testing.T) {
	ops := &SimpleSandboxOps{BasePath: t.TempDir()}
	_, err := ops.EditFile(context.Background(), "nope.txt", "old", "new")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestSimpleSandboxOps_Grep(t *testing.T) {
	ops := &SimpleSandboxOps{BasePath: t.TempDir()}
	result, err := ops.Grep(context.Background(), ".", "pattern")
	if err != nil {
		// Grep is not implemented, expect it to fail but return empty string
	}
	if result != "" {
		t.Errorf("expected empty result for unimplemented Grep, got %q", result)
	}
}

func TestSimpleSandboxOps_Glob(t *testing.T) {
	ops := &SimpleSandboxOps{BasePath: t.TempDir()}
	result, err := ops.Glob(context.Background(), ".", "*.go")
	if err != nil {
		// Glob is not implemented
	}
	if result != "" {
		t.Errorf("expected empty result for unimplemented Glob, got %q", result)
	}
}

func TestReadTool_Name(t *testing.T) {
	ops := &SimpleSandboxOps{BasePath: t.TempDir()}
	tool := NewReadTool(ops)
	if tool.Name() != "file_read" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "file_read")
	}
}

func TestReadTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewReadTool(&SimpleSandboxOps{}))
}

func TestReadTool_Execute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "read.txt")
	os.WriteFile(path, []byte("file content"), 0644)

	tool := NewReadTool(&SimpleSandboxOps{BasePath: dir})
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "read.txt",
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	// Content now includes line numbers
	content, _ := m["content"].(string)
	if !strings.Contains(content, "file content") {
		t.Errorf("content should contain file body, got %q", content)
	}
	if !strings.Contains(content, "1|") {
		t.Errorf("content should have line numbers, got %q", content)
	}
	testutil.AssertStringField(t, m, "path", "read.txt")
}

func TestReadTool_Execute_MissingPath(t *testing.T) {
	tool := NewReadTool(&SimpleSandboxOps{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing file_path")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestReadTool_Execute_WithOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	lines := []string{"line1", "line2", "line3", "line4", "line5"}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)

	tool := NewReadTool(&SimpleSandboxOps{BasePath: dir})
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "lines.txt",
		"offset":    float64(3), // JSON numbers come as float64
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	content := m["content"].(string)
	// Should start at line 3
	if !strings.Contains(content, "3|line3") {
		t.Errorf("should show line 3, got %q", content)
	}
	if strings.Contains(content, "line1") {
		t.Errorf("should not contain line1 when offset=3, got %q", content)
	}
	if !strings.Contains(content, "5|line5") {
		t.Errorf("should show line 5, got %q", content)
	}
}

func TestReadTool_Execute_WithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	lines := []string{"line1", "line2", "line3", "line4", "line5"}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)

	tool := NewReadTool(&SimpleSandboxOps{BasePath: dir})
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "lines.txt",
		"limit":     float64(2),
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	content := m["content"].(string)
	// Should show lines 1-2 with continuation message
	if !strings.Contains(content, "1|line1") {
		t.Errorf("should show line 1, got %q", content)
	}
	if !strings.Contains(content, "2|line2") {
		t.Errorf("should show line 2, got %q", content)
	}
	if strings.Contains(content, "line3") {
		t.Errorf("should not contain line3 with limit=2, got %q", content)
	}
	if !strings.Contains(content, "more lines") {
		t.Errorf("should have continuation message, got %q", content)
	}
}

func TestReadTool_Execute_Metadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	lines := []string{"line1", "line2", "line3"}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)

	tool := NewReadTool(&SimpleSandboxOps{BasePath: dir})
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "lines.txt",
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	// Check metadata fields
	totalLines, _ := m["total_lines"].(int)
	if totalLines != 3 {
		t.Errorf("total_lines should be 3, got %d", totalLines)
	}
	startLine, _ := m["start_line"].(int)
	if startLine != 1 {
		t.Errorf("start_line should be 1, got %d", startLine)
	}
}

func TestWriteTool_Name(t *testing.T) {
	tool := NewWriteTool(&SimpleSandboxOps{})
	if tool.Name() != "file_write" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "file_write")
	}
}

func TestWriteTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewWriteTool(&SimpleSandboxOps{}))
}

func TestWriteTool_Execute(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(&SimpleSandboxOps{BasePath: dir})

	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "write.txt",
		"content":   "hello",
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "path", "write.txt")

	data, _ := os.ReadFile(filepath.Join(dir, "write.txt"))
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestWriteTool_Execute_MissingParams(t *testing.T) {
	tool := NewWriteTool(&SimpleSandboxOps{})

	_, err := tool.Execute(context.Background(), map[string]any{"content": "x"})
	if err == nil {
		t.Fatal("expected error for missing file_path")
	}

	_, err = tool.Execute(context.Background(), map[string]any{"file_path": "x"})
	if err == nil {
		t.Fatal("expected error for missing content")
	}
}

func TestEditTool_Name(t *testing.T) {
	tool := NewEditTool(&SimpleSandboxOps{})
	if tool.Name() != "file_edit" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "file_edit")
	}
}

func TestEditTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewEditTool(&SimpleSandboxOps{}))
}

func TestEditTool_Execute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("old content"), 0644)

	tool := NewEditTool(&SimpleSandboxOps{BasePath: dir})
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path":  "edit.txt",
		"old_string": "old",
		"new_string": "new",
	})
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "path", "edit.txt")

	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Errorf("expected 'new content', got %q", string(data))
	}
}

func TestEditTool_Execute_MissingParams(t *testing.T) {
	tool := NewEditTool(&SimpleSandboxOps{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "x",
		"old_string": "",
		"new_string": "y",
	})
	if err == nil {
		t.Fatal("expected error for missing old_string")
	}
}

func TestGrepTool_Name(t *testing.T) {
	if NewGrepTool(nil).Name() != "file_grep" {
		t.Errorf("Name() = %q, want %q", NewGrepTool(nil).Name(), "file_grep")
	}
}

func TestGrepTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewGrepTool(nil))
}

func TestGrepTool_Execute_MissingPattern(t *testing.T) {
	tool := NewGrepTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

type mockFileOps struct {
	readFileFn  func(ctx context.Context, path string) (string, error)
	writeFileFn func(ctx context.Context, path string, content string, overwrite bool) (string, error)
	editFileFn  func(ctx context.Context, path string, oldStr, newStr string) (string, error)
	grepFn      func(ctx context.Context, path string, pattern string) (string, error)
	globFn      func(ctx context.Context, path string, pattern string) (string, error)
}

func (m *mockFileOps) ReadFile(ctx context.Context, path string) (string, error) {
	if m.readFileFn != nil {
		return m.readFileFn(ctx, path)
	}
	return "", nil
}
func (m *mockFileOps) WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error) {
	if m.writeFileFn != nil {
		return m.writeFileFn(ctx, path, content, overwrite)
	}
	return "", nil
}
func (m *mockFileOps) EditFile(ctx context.Context, path string, oldStr, newStr string) (string, error) {
	if m.editFileFn != nil {
		return m.editFileFn(ctx, path, oldStr, newStr)
	}
	return "", nil
}
func (m *mockFileOps) Grep(ctx context.Context, path string, pattern string) (string, error) {
	if m.grepFn != nil {
		return m.grepFn(ctx, path, pattern)
	}
	return "", nil
}
func (m *mockFileOps) Glob(ctx context.Context, path string, pattern string) (string, error) {
	if m.globFn != nil {
		return m.globFn(ctx, path, pattern)
	}
	return "", nil
}

func TestGrepTool_Execute_DefaultPath(t *testing.T) {
	ops := &mockFileOps{
		grepFn: func(ctx context.Context, path, pattern string) (string, error) {
			if path == "." {
				return "default path works", nil
			}
			return "", nil
		},
	}
	tool := NewGrepTool(ops)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "search",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "pattern", "search")
}

func TestGlobTool_Name(t *testing.T) {
	if NewGlobTool(nil).Name() != "file_glob" {
		t.Errorf("Name() = %q, want %q", NewGlobTool(nil).Name(), "file_glob")
	}
}

func TestGlobTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewGlobTool(nil))
}

func TestGlobTool_Execute_MissingPattern(t *testing.T) {
	tool := NewGlobTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

func TestAbsPath(t *testing.T) {
	ops := &SimpleSandboxOps{BasePath: "/base"}
	if p := ops.absPath("/absolute/path"); p != "/absolute/path" {
		t.Errorf("expected '/absolute/path', got %q", p)
	}
	if p := ops.absPath("relative/path"); p != "/base/relative/path" {
		t.Errorf("expected '/base/relative/path', got %q", p)
	}
}

// Helper types for mocking Grep/Glob
type grepFunc func(ctx context.Context, path, pattern string) (string, error)

func (f grepFunc) Grep(ctx context.Context, path, pattern string) (string, error) {
	return f(ctx, path, pattern)
}

func (f grepFunc) Glob(ctx context.Context, path, pattern string) (string, error) {
	return "", nil
}

func (f grepFunc) ReadFile(ctx context.Context, path string) (string, error) {
	return "", nil
}

func (f grepFunc) WriteFile(ctx context.Context, path string, content string, overwrite bool) (string, error) {
	return "", nil
}

func (f grepFunc) EditFile(ctx context.Context, path string, oldStr, newStr string) (string, error) {
	return "", nil
}
