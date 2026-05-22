package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// GetProjectInfo handles GET /api/pux/project-info — returns project metadata
// including whether it's local or SSH. Frontend uses this to route file ops.
func (h *PuxHandler) GetProjectInfo(w http.ResponseWriter, r *http.Request) {
	fs := requireProjectFS(w, r, h.db, h.sshManager)
	if fs == nil {
		return
	}

	resp := map[string]any{
		"type": fs.Type(),
		"root": fs.Root(),
	}
	if ssh := fs.SSHInfo(); ssh != nil {
		resp["host"] = ssh.Host
		resp["user"] = ssh.User
		resp["port"] = ssh.Port
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetProjectFiles handles GET /api/pux/files — returns a recursive file tree.
// Supports both local and SSH-backed projects via ProjectFS.
func (h *PuxHandler) GetProjectFiles(w http.ResponseWriter, r *http.Request) {
	fs := requireProjectFS(w, r, h.db, h.sshManager)
	if fs == nil {
		return
	}

	tree, err := fs.BuildTree(4)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "SSH not connected") {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, tree)
}

// GetProjectFile handles GET /api/pux/file — returns the content of a single file.
func (h *PuxHandler) GetProjectFile(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	fs := requireProjectFS(w, r, h.db, h.sshManager)
	if fs == nil {
		return
	}
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query param required"})
		return
	}

	data, err := fs.ReadFile(relPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Serve image files with correct MIME type
	if ct := imageContentType(relPath); ct != "" {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// SaveProjectFile handles PUT /api/pux/file — writes content to a file.
func (h *PuxHandler) SaveProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}](w, r)
	if !ok {
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	fs := requireProjectFSBody(w, req.Project, h.db, h.sshManager)
	if fs == nil {
		return
	}

	if err := fs.WriteFile(req.Path, []byte(req.Content)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateProjectFile handles POST /api/pux/file/create — creates a new empty file.
func (h *PuxHandler) CreateProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		Path    string `json:"path"`
	}](w, r)
	if !ok {
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	fs := requireProjectFSBody(w, req.Project, h.db, h.sshManager)
	if fs == nil {
		return
	}

	if err := fs.CreateFile(req.Path); err != nil {
		if os.IsExist(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "file already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "path": req.Path})
}

// MoveProjectFile handles POST /api/pux/file/move — moves/renames a file.
func (h *PuxHandler) MoveProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		From    string `json:"from"`
		To      string `json:"to"`
	}](w, r)
	if !ok {
		return
	}
	if req.From == "" || req.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from and to are required"})
		return
	}

	fs := requireProjectFSBody(w, req.Project, h.db, h.sshManager)
	if fs == nil {
		return
	}

	if err := fs.MoveFile(req.From, req.To); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "source file not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "from": req.From, "to": req.To})
}

// DeleteProjectFile handles DELETE /api/pux/file — deletes a file.
// For local projects: moves to .pux/trash/ for undo.
// For SSH projects: permanent delete (no trash on remote).
func (h *PuxHandler) DeleteProjectFile(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	fs := requireProjectFS(w, r, h.db, h.sshManager)
	if fs == nil {
		return
	}
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query param required"})
		return
	}

	trashPath, err := fs.DeleteFile(relPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp := map[string]string{"status": "ok"}
	if trashPath != "" {
		resp["trashPath"] = trashPath
	}
	writeJSON(w, http.StatusOK, resp)
}

// RestoreProjectFile handles POST /api/pux/file/restore — restores from .pux/trash/.
// Only supported for local projects.
func (h *PuxHandler) RestoreProjectFile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project   string `json:"project"`
		TrashPath string `json:"trashPath"`
	}](w, r)
	if !ok {
		return
	}
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

	// Restore to project root
	restorePath := filepath.Join(projectPath, originalName)
	if err := os.Rename(absTrash, restorePath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "ok",
		"restoredTo": restorePath,
	})
}

// ── Helpers ──

// imageContentType returns the MIME type for known image extensions, or "".
func imageContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	default:
		return ""
	}
}
