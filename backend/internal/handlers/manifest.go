package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/auto-developer-orchestrator/backend/internal/manifest"
)

// GetManifest returns the parsed pux.yaml manifest for a project.
// Returns 404 if the project has no manifest (not all projects need one).
func (h *ProjectHandler) GetManifest(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "name")
	if projectName == "" {
		projectName = r.URL.Query().Get("project")
	}
	if projectName == "" {
		JSONError(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectDir := resolveProjectPath(projectName, h.db)
	if projectDir == "" {
		JSONError(w, "Project not found", http.StatusNotFound)
		return
	}

	mf, err := manifest.LoadManifest(projectDir)
	if err != nil {
		JSONError(w, "Failed to parse manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if mf == nil {
		JSONError(w, "Project has no pux.yaml manifest", http.StatusNotFound)
		return
	}

	// Resolve all prompts to include text in response
	resolved, errors := mf.ResolveAllPrompts(projectDir)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"manifest":         mf,
		"resolved_prompts": resolved,
		"prompt_errors":    errors,
	})
}
