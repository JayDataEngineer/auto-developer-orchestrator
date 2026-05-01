package llama

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
)

// ensureFileOps lazy-initializes the SandboxFileOps for this executor.
func (e *SandboxToolExecutor) ensureFileOps(sandboxID string) *SandboxFileOps {
	if e.fileOps == nil {
		e.fileOps = NewSandboxFileOps(e.Manager, sandboxID, e.Logger)
	}
	return e.fileOps
}

func (e *SandboxToolExecutor) executeFileRead(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	path, _ := args["file_path"].(string)
	if path == "" {
		return nil, fmt.Errorf("missing 'file_path' argument")
	}
	var offset, limit int
	if v, ok := args["offset"].(float64); ok {
		offset = int(v)
	}
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	content, lineCount, err := e.ensureFileOps(sandboxID).ReadFile(ctx, path, offset, limit)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"content": content, "lineCount": lineCount, "file_path": path}, nil
}

func (e *SandboxToolExecutor) executeFileWrite(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	path, _ := args["file_path"].(string)
	content, _ := args["content"].(string)
	if path == "" || content == "" {
		return nil, fmt.Errorf("missing 'file_path' or 'content' argument")
	}
	force := false
	if v, ok := args["overwrite"].(bool); ok {
		force = v
	}
	result, err := e.ensureFileOps(sandboxID).WriteFile(ctx, path, content, force)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (e *SandboxToolExecutor) executeFileEdit(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	path, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll := false
	if v, ok := args["replace_all"].(bool); ok {
		replaceAll = v
	}
	if path == "" || oldString == "" {
		return nil, fmt.Errorf("missing 'file_path' or 'old_string' argument")
	}
	count, diff, err := e.ensureFileOps(sandboxID).EditFile(ctx, path, oldString, newString, replaceAll)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "file_path": path, "replacements": count, "diff": diff}, nil
}

func (e *SandboxToolExecutor) executeFileGrep(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("missing 'pattern' argument")
	}
	path, _ := args["path"].(string)
	glob, _ := args["glob"].(string)
	outputMode, _ := args["output_mode"].(string)
	if outputMode == "" {
		outputMode = "content"
	}
	var contextLines int
	if v, ok := args["context_lines"].(float64); ok {
		contextLines = int(v)
	}
	caseInsensitive := false
	if v, ok := args["case_insensitive"].(bool); ok {
		caseInsensitive = v
	}
	var headLimit int
	if v, ok := args["head_limit"].(float64); ok {
		headLimit = int(v)
	}
	result, err := e.ensureFileOps(sandboxID).Grep(ctx, pattern, path, glob, outputMode, contextLines, caseInsensitive, headLimit)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "pattern": pattern, "results": result}, nil
}

func (e *SandboxToolExecutor) executeFileGlob(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("missing 'pattern' argument")
	}
	path, _ := args["path"].(string)
	files, err := e.ensureFileOps(sandboxID).Glob(ctx, pattern, path)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "pattern": pattern, "files": files, "count": len(files)}, nil
}

func (e *SandboxToolExecutor) executeUndoEdit(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	path, _ := args["file_path"].(string)
	if path == "" {
		return nil, fmt.Errorf("missing 'file_path' argument")
	}
	content, err := e.ensureFileOps(sandboxID).UndoEdit(ctx, path)
	if err != nil {
		return nil, err
	}
	lines := strings.Count(content, "\n") + 1
	return map[string]interface{}{"success": true, "file_path": path, "restored_lines": lines, "message": fmt.Sprintf("Restored %s to previous version (%d lines)", path, lines)}, nil
}

func (e *SandboxToolExecutor) executeCodeSearch(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	operation, _ := args["operation"].(string)
	if operation == "" {
		return nil, fmt.Errorf("missing 'operation' argument (use: find_references, find_definition, list_symbols, hover)")
	}
	symbol, _ := args["symbol"].(string)
	path, _ := args["path"].(string)
	fileType, _ := args["file_type"].(string)
	ops := NewCodeSearchOps(e.ensureFileOps(sandboxID))
	result, err := ops.Search(ctx, operation, symbol, path, fileType)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "operation": operation, "symbol": symbol, "results": result}, nil
}

func (e *SandboxToolExecutor) executeImageRead(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return nil, fmt.Errorf("missing 'file_path' argument")
	}
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Describe this image in detail. Include any text, UI elements, data, diagrams, or code visible."
	}
	if e.Vision == nil {
		return nil, fmt.Errorf("image_read requires vision model (no VisionClient configured)")
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	imageExts := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
	mimeType, ok := imageExts[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported image format (use png, jpg, gif, or webp): %s", filePath)
	}
	ops := e.ensureFileOps(sandboxID)
	output, err := ops.exec(ctx, fmt.Sprintf("base64 '%s'", sandbox.ShellEscape(filePath)))
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}
	b64 := strings.TrimSpace(output)
	if b64 == "" {
		return nil, fmt.Errorf("image file is empty: %s", filePath)
	}
	imageBytes, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	if len(imageBytes) > 10*1024*1024 {
		return nil, fmt.Errorf("image too large (%d bytes, max 10MB)", len(imageBytes))
	}
	desc, err := e.Vision.DescribeImage(ctx, imageBytes, prompt, mimeType)
	if err != nil {
		return nil, fmt.Errorf("vision failed: %w", err)
	}
	return map[string]interface{}{"description": desc, "file": filePath, "size_bytes": len(imageBytes)}, nil
}
