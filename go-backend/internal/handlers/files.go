package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
)

// FileHandler handles file transfer between host and sandbox containers.
type FileHandler struct {
	manager *sandbox.Manager
	logger  *zap.Logger
}

// NewFileHandler creates a new file transfer handler.
func NewFileHandler(manager *sandbox.Manager, logger *zap.Logger) *FileHandler {
	return &FileHandler{
		manager: manager,
		logger:  logger,
	}
}

// RegisterRoutes registers file transfer routes.
func (h *FileHandler) RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
}) {
	r.Post("/upload", h.Upload)
	r.Get("/download", h.Download)
	r.Get("/list", h.List)
	r.Get("/stat", h.Stat)
}

// UploadRequest is the JSON body for uploading a file into the sandbox.
type UploadRequest struct {
	// Path inside the container (absolute). Must be under /sandbox/.
	Path string `json:"path"`
	// Base64-encoded file contents.
	Content string `json:"content"`
	// If true, decode Content as base64. If false, Content is written as plain text.
	Base64 bool `json:"base64"`
}

// POST /api/sandbox/{id}/files/upload
func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")

	var req UploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		JSONError(w, "path is required", http.StatusBadRequest)
		return
	}
	if err := sandbox.ValidatePath(req.Path); err != nil {
		JSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Ensure parent directory exists
	dir := filepath.Dir(req.Path)
	if _, err := h.manager.ExecInSandbox(r.Context(), sandboxID, []string{"mkdir", "-p", dir}); err != nil {
		JSONError(w, fmt.Sprintf("failed to create parent dir: %v", err), http.StatusInternalServerError)
		return
	}

	var data []byte
	if req.Base64 {
		decoded, err := base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			JSONError(w, fmt.Sprintf("base64 decode failed: %v", err), http.StatusBadRequest)
			return
		}
		data = decoded
	} else {
		data = []byte(req.Content)
	}

	// Use base64 pipe to transfer binary data safely through Docker exec:
	// echo BASE64 | base64 -d > /path
	encoded := base64.StdEncoding.EncodeToString(data)
	// Shell-escape: the encoded string is ASCII-safe (A-Za-z0-9+/=) so no escaping needed.
	cmd := fmt.Sprintf("echo '%s' | base64 -d > '%s'", encoded, sandbox.ShellEscape(req.Path))
	if _, err := h.manager.ExecInSandbox(r.Context(), sandboxID, []string{"bash", "-c", cmd}); err != nil {
		JSONError(w, fmt.Sprintf("write failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path": req.Path,
		"size": len(data),
	})
}

// GET /api/sandbox/{id}/files/download?path=/sandbox/workspace/file.txt
func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	path := r.URL.Query().Get("path")

	if path == "" {
		JSONError(w, "path query parameter is required", http.StatusBadRequest)
		return
	}
	if err := sandbox.ValidatePath(path); err != nil {
		JSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Read file as base64 to safely transfer binary data through Docker exec.
	output, err := h.manager.ExecInSandbox(r.Context(), sandboxID, []string{
		"bash", "-c",
		fmt.Sprintf("if [ -f '%s' ]; then base64 -w0 '%s'; else echo 'FILE_NOT_FOUND'; fi", sandbox.ShellEscape(path), sandbox.ShellEscape(path)),
	})
	if err != nil {
		JSONError(w, fmt.Sprintf("read failed: %v", err), http.StatusInternalServerError)
		return
	}

	output = strings.TrimSpace(output)
	if output == "FILE_NOT_FOUND" {
		JSONError(w, "file not found", http.StatusNotFound)
		return
	}

	// Check if client wants raw bytes or JSON
	format := r.URL.Query().Get("format")
	if format == "raw" {
		decoded, err := base64.StdEncoding.DecodeString(output)
		if err != nil {
			JSONError(w, "base64 decode failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
		w.Write(decoded)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"path":    path,
		"content": output,
		"encoding": "base64",
	})
}

// GET /api/sandbox/{id}/files/list?path=/sandbox/workspace
func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/sandbox/workspace"
	}
	if err := sandbox.ValidatePath(path); err != nil {
		JSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Use stat to get structured output: name\tsize\tis_dir
	output, err := h.manager.ExecInSandbox(r.Context(), sandboxID, []string{
		"bash", "-c",
		fmt.Sprintf("find '%s' -maxdepth 1 -printf '%%f\\t%%s\\t%%y\\n' 2>/dev/null | tail -n +2", sandbox.ShellEscape(path)),
	})
	if err != nil {
		JSONError(w, fmt.Sprintf("list failed: %v", err), http.StatusInternalServerError)
		return
	}

	type FileEntry struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		IsDir bool   `json:"isDir"`
	}

	var entries []FileEntry
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		var size int64
		fmt.Sscanf(parts[1], "%d", &size)
		entries = append(entries, FileEntry{
			Name:  parts[0],
			Size:  size,
			IsDir: strings.HasPrefix(parts[2], "d"),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    path,
		"entries": entries,
	})
}

// GET /api/sandbox/{id}/files/stat?path=/sandbox/workspace/file.txt
func (h *FileHandler) Stat(w http.ResponseWriter, r *http.Request) {
	sandboxID := r.PathValue("id")
	path := r.URL.Query().Get("path")

	if path == "" {
		JSONError(w, "path query parameter is required", http.StatusBadRequest)
		return
	}
	if err := sandbox.ValidatePath(path); err != nil {
		JSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	output, err := h.manager.ExecInSandbox(r.Context(), sandboxID, []string{
		"stat", "-c", "%s\t%Y\t%F", path,
	})
	if err != nil {
		JSONError(w, "file not found", http.StatusNotFound)
		return
	}

	parts := strings.SplitN(strings.TrimSpace(output), "\t", 3)
	if len(parts) < 3 {
		JSONError(w, "unexpected stat output", http.StatusInternalServerError)
		return
	}

	var size int64
	fmt.Sscanf(parts[0], "%d", &size)
	var modTime int64
	fmt.Sscanf(parts[1], "%d", &modTime)

	writeJSON(w, http.StatusOK, map[string]any{
		"path":     path,
		"size":     size,
		"modTime":  modTime,
		"type":     strings.TrimSpace(parts[2]),
		"exists":   true,
	})
}


