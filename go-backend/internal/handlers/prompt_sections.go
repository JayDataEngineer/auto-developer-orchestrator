package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// PromptSectionHandler handles CTO prompt section CRUD.
type PromptSectionHandler struct {
	configDir string
	log       *zap.Logger
}

// NewPromptSectionHandler creates a handler for prompt section files.
func NewPromptSectionHandler(configDir string, logger *zap.Logger) *PromptSectionHandler {
	return &PromptSectionHandler{configDir: configDir, log: logger}
}

// RegisterRoutes registers prompt section routes.
func (h *PromptSectionHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.ListSections)
	r.Get("/{name}", h.GetSection)
	r.Put("/{name}", h.UpdateSection)
}

// ListSections returns all prompt section names and their content.
func (h *PromptSectionHandler) ListSections(w http.ResponseWriter, r *http.Request) {
	sectionsDir := filepath.Join(h.configDir, "prompt_sections")
	entries, err := os.ReadDir(sectionsDir)
	if err != nil {
		// Return empty list — sections might not exist yet
		writeJSON(w, http.StatusOK, map[string]any{"sections": []any{}})
		return
	}

	type section struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}

	var sections []section
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(sectionsDir, e.Name()))
		if err != nil {
			continue
		}
		sections = append(sections, section{Name: name, Content: string(data)})
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].Name < sections[j].Name })

	writeJSON(w, http.StatusOK, map[string]any{"sections": sections})
}

// GetSection returns a single prompt section.
func (h *PromptSectionHandler) GetSection(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, `{"error":"invalid section name"}`, http.StatusBadRequest)
		return
	}

	path := filepath.Join(h.configDir, "prompt_sections", name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, `{"error":"section not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"read failed"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"name": name, "content": string(data)})
}

// UpdateSection saves a prompt section and invalidates the prompt cache.
func (h *PromptSectionHandler) UpdateSection(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, `{"error":"invalid section name"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	sectionsDir := filepath.Join(h.configDir, "prompt_sections")
	if err := os.MkdirAll(sectionsDir, 0755); err != nil {
		h.log.Error("prompt sections mkdir", zap.Error(err))
		http.Error(w, `{"error":"create dir failed"}`, http.StatusInternalServerError)
		return
	}

	path := filepath.Join(sectionsDir, name+".md")
	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		h.log.Error("prompt section write", zap.Error(err))
		http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
		return
	}

	// Invalidate prompt builder cache — section content changed
	common.ResetGlobalBuilder()

	writeJSON(w, http.StatusOK, map[string]any{"name": name, "content": req.Content})
}
