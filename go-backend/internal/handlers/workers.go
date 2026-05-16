package handlers

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/autoconfig"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// WorkerHandler handles worker config HTTP endpoints.
type WorkerHandler struct {
	store *autoconfig.WorkerStore
	log   *zap.Logger
}

// NewWorkerHandler creates a new worker handler.
func NewWorkerHandler(store *autoconfig.WorkerStore, logger *zap.Logger) *WorkerHandler {
	return &WorkerHandler{store: store, log: logger}
}

// RegisterRoutes registers all worker routes on the given router.
func (h *WorkerHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.ListWorkers)
	r.Get("/capabilities", h.ListCapabilities)
	r.Post("/", h.CreateWorker)
	r.Put("/{name}", h.UpdateWorker)
	r.Delete("/{name}", h.DeleteWorker)
}

// ListWorkers returns all workers.
func (h *WorkerHandler) ListWorkers(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.List(r.Context())
	if err != nil {
		h.log.Error("workers list", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// List returns names — fetch details for each
	listResult, ok := result.(map[string]any)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"workers": []any{}})
		return
	}
	names, _ := listResult["items"].([]string)

	workers := make([]map[string]any, 0, len(names))
	for _, name := range names {
		detail, err := h.store.Get(r.Context(), name)
		if err != nil {
			continue
		}
		if m, ok := detail.(map[string]any); ok {
			workers = append(workers, m)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"workers": workers})
}

// ListCapabilities returns all available capability names.
func (h *WorkerHandler) ListCapabilities(w http.ResponseWriter, r *http.Request) {
	pkgs := common.LoadToolPackages()
	var caps []map[string]string
	for name := range pkgs {
		caps = append(caps, map[string]string{"name": name})
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i]["name"] < caps[j]["name"] })
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": caps})
}

// CreateWorker creates a new worker.
func (h *WorkerHandler) CreateWorker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string   `json:"name"`
		Persona      string   `json:"persona"`
		Capabilities []string `json:"capabilities"`
		Model        string   `json:"model"`
		MaxRounds    int      `json:"maxRounds"`
		Temperature  float64  `json:"temperature"`
		Sandbox      string   `json:"sandbox"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if req.Persona == "" {
		http.Error(w, `{"error":"persona is required"}`, http.StatusBadRequest)
		return
	}

	spec := map[string]any{
		"persona":      req.Persona,
		"capabilities": req.Capabilities,
		"model":        req.Model,
		"max_rounds":   req.MaxRounds,
		"temperature":  req.Temperature,
		"sandbox":      req.Sandbox,
	}

	_, err := h.store.Put(r.Context(), req.Name, spec)
	if err != nil {
		h.log.Error("worker create", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Return the created worker detail
	detail, _ := h.store.Get(r.Context(), req.Name)
	writeJSON(w, http.StatusCreated, detail)
}

// UpdateWorker updates an existing worker.
func (h *WorkerHandler) UpdateWorker(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Persona      string   `json:"persona"`
		Capabilities []string `json:"capabilities"`
		Model        string   `json:"model"`
		MaxRounds    int      `json:"maxRounds"`
		Temperature  float64  `json:"temperature"`
		Sandbox      string   `json:"sandbox"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	spec := map[string]any{
		"persona":      req.Persona,
		"capabilities": req.Capabilities,
		"model":        req.Model,
		"max_rounds":   req.MaxRounds,
		"temperature":  req.Temperature,
		"sandbox":      req.Sandbox,
	}

	if _, err := h.store.Put(r.Context(), name, spec); err != nil {
		h.log.Error("worker update", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	detail, _ := h.store.Get(r.Context(), name)
	writeJSON(w, http.StatusOK, detail)
}

// DeleteWorker removes a worker.
func (h *WorkerHandler) DeleteWorker(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	if err := h.store.Delete(r.Context(), name); err != nil {
		h.log.Error("worker delete", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"message": "worker deleted"})
}
