package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/checkpoint"
	"github.com/go-chi/chi/v5"
)

// checkpointBaseDir returns the base directory for checkpoints.
func checkpointBaseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent", "checkpoints")
}

// ListCheckpoints handles GET /api/pux/checkpoints
// Returns all snapshots for a given session.
func (h *PuxHandler) ListCheckpoints(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		// Try project-based resolution: scan for sessions matching project
		project := r.URL.Query().Get("project")
		if project == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session or project query param required"})
			return
		}
		sessionID = project // session IDs often match project names
	}

	man, err := loadCheckpointManifest(sessionID)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, man.Snapshots)
}

// GetCheckpointFiles handles GET /api/pux/checkpoints/{id}/files
// Returns the file versions in a specific snapshot.
func (h *PuxHandler) GetCheckpointFiles(w http.ResponseWriter, r *http.Request) {
	snapID := chi.URLParam(r, "id")
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("project")
	}
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session or project query param required"})
		return
	}

	man, err := loadCheckpointManifest(sessionID)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no checkpoints found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for _, snap := range man.Snapshots {
		if snap.ID == snapID {
			writeJSON(w, http.StatusOK, snap.Files)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "snapshot not found"})
}

// RestoreCheckpoint handles POST /api/pux/checkpoints/{id}/restore
// Restores all files to the state captured by a snapshot.
func (h *PuxHandler) RestoreCheckpoint(w http.ResponseWriter, r *http.Request) {
	snapID := chi.URLParam(r, "id")

	var body struct {
		Session string `json:"session"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	sessionID := body.Session
	if sessionID == "" {
		sessionID = body.Project
	}
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session or project required"})
		return
	}

	man, err := loadCheckpointManifest(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Find snapshot
	var targetSnap *checkpoint.Snapshot
	for _, snap := range man.Snapshots {
		if snap.ID == snapID {
			s := snap // copy
			targetSnap = s
			break
		}
	}
	if targetSnap == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "snapshot not found"})
		return
	}

	// Create a manager, load the manifest, and restore
	mgr := checkpoint.NewManager(sessionID, man.Project, filepath.Join(checkpointBaseDir(), sessionID))
	mgr.Load()
	restored, err := mgr.RestoreSnapshot(r.Context(), snapID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"restored": restored,
		"count":    len(restored),
	})
}

// GetFileHistory handles GET /api/pux/checkpoints/file-history?path=...&session=...
// Returns all versions of a specific file.
func (h *PuxHandler) GetFileHistory(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("project")
	}
	filePath := r.URL.Query().Get("path")
	if sessionID == "" || filePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session and path required"})
		return
	}

	mgr := checkpoint.NewManager(sessionID, "", filepath.Join(checkpointBaseDir(), sessionID))
	if err := mgr.Load(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	versions := mgr.ListFileVersions(filePath)
	writeJSON(w, http.StatusOK, versions)
}

// RestoreFileVersion handles POST /api/pux/checkpoints/file-restore
// Restores a single file to a specific version.
func (h *PuxHandler) RestoreFileVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
		Project string `json:"project"`
		Path    string `json:"path"`
		Version int    `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	sessionID := body.Session
	if sessionID == "" {
		sessionID = body.Project
	}
	if sessionID == "" || body.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session/project and path required"})
		return
	}

	mgr := checkpoint.NewManager(sessionID, "", filepath.Join(checkpointBaseDir(), sessionID))
	if err := mgr.Load(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	content, err := mgr.LoadFileVersion(body.Path, body.Version)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"path":    body.Path,
		"version": body.Version,
		"size":    len(content),
	})
}

// loadCheckpointManifest loads a manifest from disk for a given session.
func loadCheckpointManifest(sessionID string) (*checkpoint.Manifest, error) {
	manifestPath := filepath.Join(checkpointBaseDir(), sessionID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var man checkpoint.Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, err
	}
	return &man, nil
}
