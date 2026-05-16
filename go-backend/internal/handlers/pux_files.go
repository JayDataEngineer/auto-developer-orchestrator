package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GetProjectFiles handles GET /api/pux/files — returns a recursive file tree
// for the given project, read from the local filesystem.
// Query params: project (required), depth (optional, default 4)
func (h *PuxHandler) GetProjectFiles(w http.ResponseWriter, r *http.Request) {
	projectPath := requireProject(w, r, h.db)
	if projectPath == "" {
		return
	}

	tree, err := buildFileTree(projectPath, projectPath, 4)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, tree)
}

// GetProjectFile handles GET /api/pux/file — returns the content of a single file.
// Query params: project (required), path (required, relative to project root)
func (h *PuxHandler) GetProjectFile(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	projectPath := requireProject(w, r, h.db)
	if projectPath == "" {
		return
	}
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query param required"})
		return
	}

	// Security: ensure the resolved path stays within the project directory
	absPath := filepath.Join(projectPath, relPath)
	absPath, err := filepath.Abs(absPath)
	if err != nil || !strings.HasPrefix(absPath, projectPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "path escapes project directory"})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// SaveProjectFile handles PUT /api/pux/file — writes content to a file.
// Body: JSON { "project": "...", "path": "...", "content": "..." }
// Also handles POST /api/pux/file with "create" mode — creates an empty file.
func (h *PuxHandler) SaveProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}](w, r)
	if !ok { return }
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	projectPath := requireProjectBody(w, req.Project, h.db)
	if projectPath == "" {
		return
	}

	absPath := filepath.Join(projectPath, req.Path)
	absPath, err := filepath.Abs(absPath)
	if err != nil || !strings.HasPrefix(absPath, projectPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "path escapes project directory"})
		return
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := os.WriteFile(absPath, []byte(req.Content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateProjectFile handles POST /api/pux/file/create — creates a new empty file.
// Body: JSON { "project": "...", "path": "..." }
func (h *PuxHandler) CreateProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		Path    string `json:"path"`
	}](w, r)
	if !ok { return }
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	projectPath := requireProjectBody(w, req.Project, h.db)
	if projectPath == "" {
		return
	}

	absPath := filepath.Join(projectPath, req.Path)
	absPath, err := filepath.Abs(absPath)
	if err != nil || !strings.HasPrefix(absPath, projectPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "path escapes project directory"})
		return
	}

	// Check if already exists
	if _, err := os.Stat(absPath); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "file already exists"})
		return
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := os.WriteFile(absPath, []byte{}, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "path": req.Path})
}

// MoveProjectFile handles POST /api/pux/file/move — moves/renames a file.
// Body: JSON { "project": "...", "from": "...", "to": "..." }
func (h *PuxHandler) MoveProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		From    string `json:"from"`
		To      string `json:"to"`
	}](w, r)
	if !ok { return }
	if req.From == "" || req.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from and to are required"})
		return
	}

	projectPath := requireProjectBody(w, req.Project, h.db)
	if projectPath == "" {
		return
	}

	fromAbs := filepath.Join(projectPath, req.From)
	fromAbs, err := filepath.Abs(fromAbs)
	if err != nil || !strings.HasPrefix(fromAbs, projectPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "source path escapes project directory"})
		return
	}

	toAbs := filepath.Join(projectPath, req.To)
	toAbs, err = filepath.Abs(toAbs)
	if err != nil || !strings.HasPrefix(toAbs, projectPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "destination path escapes project directory"})
		return
	}

	if _, err := os.Stat(fromAbs); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source file not found"})
		return
	}

	if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := os.Rename(fromAbs, toAbs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "from": req.From, "to": req.To})
}

// DeleteProjectFile handles DELETE /api/pux/file — moves file to .pux/trash/ for undo.
// Query params: project (required), path (required)
func (h *PuxHandler) DeleteProjectFile(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	projectPath := requireProject(w, r, h.db)
	if projectPath == "" {
		return
	}
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query param required"})
		return
	}

	absPath := filepath.Join(projectPath, relPath)
	absPath, err := filepath.Abs(absPath)
	if err != nil || !strings.HasPrefix(absPath, projectPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "path escapes project directory"})
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	// Move to .pux/trash/<timestamp>_<filename> instead of deleting
	trashDir := filepath.Join(projectPath, ".pux", "trash")
	os.MkdirAll(trashDir, 0o755)

	name := filepath.Base(absPath)
	trashPath := filepath.Join(trashDir, fmt.Sprintf("%d_%s", time.Now().UnixMilli(), name))

	if err := os.Rename(absPath, trashPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "ok",
		"trashPath": trashPath,
	})
}

// RestoreProjectFile handles POST /api/pux/file/restore — restores from .pux/trash/.
// Body: JSON { "project": "...", "trashPath": "..." }
func (h *PuxHandler) RestoreProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project   string `json:"project"`
		TrashPath string `json:"trashPath"`
	}](w, r)
	if !ok { return }
	if req.TrashPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trashPath is required"})
		return
	}

	projectPath := requireProjectBody(w, req.Project, h.db)
	if projectPath == "" {
		return
	}

	// Validate trash path is within project's .pux/trash/
	absTrash, err := filepath.Abs(req.TrashPath)
	if err != nil || !strings.HasPrefix(absTrash, filepath.Join(projectPath, ".pux", "trash")) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid trash path"})
		return
	}

	if _, err := os.Stat(absTrash); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trash file not found"})
		return
	}

	// Extract original filename from trash name (<timestamp>_<original>)
	trashName := filepath.Base(absTrash)
	underscoreIdx := strings.Index(trashName, "_")
	originalName := trashName
	if underscoreIdx > 0 {
		originalName = trashName[underscoreIdx+1:]
	}

	// Restore to project root (best we can do without tracking original path)
	restorePath := filepath.Join(projectPath, originalName)
	if err := os.Rename(absTrash, restorePath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":      "ok",
		"restoredTo":  restorePath,
	})
}

// ── File tree builder ──

type FileNode struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"` // "file" or "dir"
	Path     string      `json:"path"`
	Children []FileNode  `json:"children,omitempty"`
}

// Directories and files to skip when listing.
var skipNames = map[string]bool{
	"node_modules": true,
	".git":         true,
	"__pycache__":  true,
	".next":        true,
	"dist":         true,
	".cache":       true,
	"vendor":       true,
	".pux":         true,
	".DS_Store":    true,
}

func buildFileTree(root, currentPath string, maxDepth int) ([]FileNode, error) {
	if maxDepth <= 0 {
		return nil, nil
	}

	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", currentPath, err)
	}

	var nodes []FileNode
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files and known noise
		if strings.HasPrefix(name, ".") || skipNames[name] {
			continue
		}

		fullPath := filepath.Join(currentPath, name)
		relPath, _ := filepath.Rel(root, fullPath)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.IsDir() {
			children, _ := buildFileTree(root, fullPath, maxDepth-1)
			nodes = append(nodes, FileNode{
				Name:     name,
				Type:     "dir",
				Path:     relPath,
				Children: children,
			})
		} else {
			// Skip files larger than 1MB
			if info.Size() > 1_000_000 {
				continue
			}
			nodes = append(nodes, FileNode{
				Name: name,
				Type: "file",
				Path: relPath,
			})
		}
	}

	return nodes, nil
}
