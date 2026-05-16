package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FsBrowseHandler handles GET /api/fs/browse — single-level directory listing
// for the "Open Folder" file picker. Uses an allowlist of permitted root paths.
type FsBrowseHandler struct {
	allowedRoots []string
}

// browseEntry is one item in the directory listing response.
type browseEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size,omitempty"`
}

// browseResponse is the JSON response for GET /api/fs/browse.
type browseResponse struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent"`
	Entries []browseEntry `json:"entries"`
}

// NewFsBrowseHandler creates a handler with an allowlist of permitted roots.
// Default roots: $HOME, $PROJECT_ROOT, /home, /tmp.
// Override/add with PUX_FS_ROOTS env (comma-separated; set to "/" for full access).
func NewFsBrowseHandler() *FsBrowseHandler {
	roots := []string{}

	// Home directory
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}

	// Project root
	if pr := os.Getenv("PROJECT_ROOT"); pr != "" {
		roots = append(roots, pr)
	}

	// Common dev paths
	roots = append(roots, "/home", "/tmp")

	// User-configured additional roots
	if extra := os.Getenv("PUX_FS_ROOTS"); extra != "" {
		for _, p := range strings.Split(extra, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				roots = append(roots, p)
			}
		}
	}

	// Clean all roots
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		abs, err := filepath.Abs(filepath.Clean(r))
		if err != nil {
			continue
		}
		cleaned = append(cleaned, abs)
	}

	return &FsBrowseHandler{allowedRoots: cleaned}
}

// isAllowed checks if the given path falls under one of the allowed roots.
func (h *FsBrowseHandler) isAllowed(path string) bool {
	for _, root := range h.allowedRoots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// Browse handles GET /api/fs/browse?path=...
func (h *FsBrowseHandler) Browse(w http.ResponseWriter, r *http.Request) {
	queryPath := r.URL.Query().Get("path")

	// Default to HOME
	if queryPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			queryPath = home
		} else {
			queryPath = "/"
		}
	}

	// Clean and resolve
	absPath, err := filepath.Abs(filepath.Clean(queryPath))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	// Resolve symlinks
	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path does not exist"})
		return
	}

	// Security: must be under an allowed root
	if !h.isAllowed(evalPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	// Read directory
	entries, err := os.ReadDir(evalPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read directory"})
		return
	}

	// Filter and build response
	var result []browseEntry
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files and known noise
		if strings.HasPrefix(name, ".") || skipNames[name] {
			continue
		}

		isDir := entry.IsDir()
		var size int64
		if !isDir {
			if info, err := entry.Info(); err == nil {
				size = info.Size()
			}
		}

		result = append(result, browseEntry{Name: name, IsDir: isDir, Size: size})

		// Cap at 200 entries
		if len(result) >= 200 {
			break
		}
	}

	// Sort: directories first, then alphabetical
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})

	// Compute parent
	parent := filepath.Dir(evalPath)
	if parent == evalPath {
		parent = ""
	}

	writeJSON(w, http.StatusOK, browseResponse{
		Path:    evalPath,
		Parent:  parent,
		Entries: result,
	})
}
