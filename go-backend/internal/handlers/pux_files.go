package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// GetProjectFiles handles GET /api/pux/files — returns a recursive file tree
// for the given project, read from the local filesystem.
// Query params: project (required), depth (optional, default 4)
func (h *PuxHandler) GetProjectFiles(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project query param required"})
		return
	}

	projectPath := resolveProjectPath(project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
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
	project := r.URL.Query().Get("project")
	relPath := r.URL.Query().Get("path")
	if project == "" || relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project and path query params required"})
		return
	}

	projectPath := resolveProjectPath(project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
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
