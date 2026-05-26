package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/autoconfig"
	"github.com/auto-developer-orchestrator/backend/internal/hooks"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// kernelWorkerNames is populated at startup from the kernel workers directory.
// These names are immutable (Contract 3.5) — REST endpoints reject modifications.
var kernelWorkerNames map[string]bool

// orgRoleDefault holds the original files for a single org role.
type orgRoleDefault struct {
	configYAML []byte // original config.yaml
	promptMD   []byte // original prompt.md
	rolesDir   string // absolute path to the org's roles/ directory
}

// WorkerHandler handles worker config HTTP endpoints.
type WorkerHandler struct {
	store        *autoconfig.WorkerStore
	log          *zap.Logger
	defaults     map[string][]byte          // original YAML content by kernel worker name
	orgDefaults  map[string]orgRoleDefault  // "orgName/roleName" → original files
}

// NewWorkerHandler creates a new worker handler.
// Snapshots existing worker files as defaults for revert.
func NewWorkerHandler(store *autoconfig.WorkerStore, logger *zap.Logger) *WorkerHandler {
	h := &WorkerHandler{
		store:       store,
		log:         logger,
		defaults:    make(map[string][]byte),
		orgDefaults: make(map[string]orgRoleDefault),
	}

	// Snapshot kernel worker names (Contract 3.5: immutable)
	kernelWorkerNames = common.KernelWorkerNames()

	// Snapshot current kernel worker files as defaults
	if dir := store.Dir(); dir != "" {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			name := e.Name()[:len(e.Name())-5]
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil {
				h.defaults[name] = data
			}
		}
	}

	// Snapshot org role files as defaults
	for _, org := range common.DiscoverOrgs() {
		if org.RolesDir == "" {
			continue
		}
		for _, roleName := range org.Roles {
			cfgData, _ := os.ReadFile(filepath.Join(org.RolesDir, roleName, "config.yaml"))
			promptData, _ := os.ReadFile(filepath.Join(org.RolesDir, roleName, "prompt.md"))
			if cfgData != nil {
				h.orgDefaults[org.Name+"/"+roleName] = orgRoleDefault{
					configYAML: cfgData,
					promptMD:   promptData,
					rolesDir:   org.RolesDir,
				}
			}
		}
	}

	return h
}

// RegisterRoutes registers all worker routes on the given router.
func (h *WorkerHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.ListWorkers)
	r.Get("/capabilities", h.ListCapabilities)
	r.Get("/hooks", h.ListHooks)
	r.Get("/orgs", h.ListOrgs)
	r.Post("/", h.CreateWorker)
	r.Put("/{name}", h.UpdateWorker)
	r.Delete("/{name}", h.DeleteWorker)
	r.Post("/{name}/revert", h.RevertWorker)
}

// ListOrgs returns all discovered organizations from ~/.pux/orgs/.
func (h *WorkerHandler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs := common.DiscoverOrgs()
	writeJSON(w, http.StatusOK, map[string]any{"orgs": orgs})
}

// ListWorkers returns all workers — kernel workers plus org workers.
// Each worker includes a "source" field: "kernel" for base workers, or the org name.
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

	// Track names to deduplicate — org workers shadow kernel workers with the same name
	seen := make(map[string]bool)
	workers := make([]map[string]any, 0, len(names))
	for _, name := range names {
		detail, err := h.store.Get(r.Context(), name)
		if err != nil {
			continue
		}
		if m, ok := detail.(map[string]any); ok {
			m["isDefault"] = h.defaults[name] != nil
			m["isModified"] = h.isKernelModified(name)
			m["source"] = "kernel"
			workers = append(workers, m)
			seen[name] = true
		}
	}

	// Append org workers (skip if name already present from kernel — org overlays kernel)
	orgs := common.DiscoverOrgs()
	for _, org := range orgs {
		for _, name := range org.Roles {
			if seen[name] {
				continue // org role shadows kernel worker — skip kernel duplicate
			}
			detail, ok := org.RoleDetails[name]
			if !ok {
				continue
			}
			m, ok := detail.(map[string]any)
			if !ok {
				continue
			}
			key := org.Name + "/" + name
			m["source"] = org.Name
			m["sourceDescription"] = org.Description
			m["sourcePath"] = org.Path
			m["isDefault"] = true
			m["isModified"] = h.isOrgModified(key, org.RolesDir, name)
			m["isOrg"] = true
			workers = append(workers, m)
			seen[name] = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"workers": workers})
}

// ListHooks returns all available hook names.
func (h *WorkerHandler) ListHooks(w http.ResponseWriter, r *http.Request) {
	names := hooks.AvailableHookNames()
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string]any{"hooks": names})
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
		Hint         string   `json:"hint"`
		Persona      string   `json:"persona"`
		Capabilities []string `json:"capabilities"`
		Model        string   `json:"model"`
		MaxRounds    int      `json:"maxRounds"`
		Temperature  float64  `json:"temperature"`
		Sandbox      string   `json:"sandbox"`
		DelegatesTo  []string `json:"delegatesTo"`
		Hooks        []string `json:"hooks"`
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
	if rejectKernel(w, req.Name) {
		return
	}

	spec := map[string]any{
		"hint":         req.Hint,
		"persona":      req.Persona,
		"capabilities": req.Capabilities,
		"model":        req.Model,
		"max_rounds":   req.MaxRounds,
		"temperature":  req.Temperature,
		"sandbox":      req.Sandbox,
		"delegates_to": req.DelegatesTo,
		"hooks":        req.Hooks,
	}

	_, err := h.store.Put(r.Context(), req.Name, spec)
	if err != nil {
		h.log.Error("worker create", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Return the created worker detail
	detail, _ := h.store.Get(r.Context(), req.Name)

	// Invalidate prompt builder cache — worker roster changed
	common.ResetGlobalBuilder()

	writeJSON(w, http.StatusCreated, detail)
}

// UpdateWorker updates an existing worker (kernel or org).
func (h *WorkerHandler) UpdateWorker(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if rejectKernel(w, name) {
		return
	}

	var req struct {
		Hint         string   `json:"hint"`
		Persona      string   `json:"persona"`
		Capabilities []string `json:"capabilities"`
		Model        string   `json:"model"`
		MaxRounds    int      `json:"maxRounds"`
		Temperature  float64  `json:"temperature"`
		Sandbox      string   `json:"sandbox"`
		DelegatesTo  []string `json:"delegatesTo"`
		Hooks        []string `json:"hooks"`
		Source       string   `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	// Route to org update if source is an org name
	if req.Source != "" && req.Source != "kernel" {
		h.updateOrgWorker(w, r, name, req.Source, &req)
		return
	}

	spec := map[string]any{
		"hint":         req.Hint,
		"persona":      req.Persona,
		"capabilities": req.Capabilities,
		"model":        req.Model,
		"max_rounds":   req.MaxRounds,
		"temperature":  req.Temperature,
		"sandbox":      req.Sandbox,
		"delegates_to": req.DelegatesTo,
		"hooks":        req.Hooks,
	}

	if _, err := h.store.Put(r.Context(), name, spec); err != nil {
		h.log.Error("worker update", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	detail, _ := h.store.Get(r.Context(), name)
	common.ResetGlobalBuilder()
	writeJSON(w, http.StatusOK, detail)
}

// updateOrgWorker writes updated config.yaml + prompt.md for an org role.
func (h *WorkerHandler) updateOrgWorker(w http.ResponseWriter, r *http.Request, name, source string, req *struct {
	Hint         string   `json:"hint"`
	Persona      string   `json:"persona"`
	Capabilities []string `json:"capabilities"`
	Model        string   `json:"model"`
	MaxRounds    int      `json:"maxRounds"`
	Temperature  float64  `json:"temperature"`
	Sandbox      string   `json:"sandbox"`
	DelegatesTo  []string `json:"delegatesTo"`
	Hooks        []string `json:"hooks"`
	Source       string   `json:"source"`
}) {
	// Find the org's roles dir from defaults (fast, no re-discovery)
	key := source + "/" + name
	rolesDir := ""
	if def, ok := h.orgDefaults[key]; ok {
		rolesDir = def.rolesDir
	} else {
		// Fallback: discover
		for _, org := range common.DiscoverOrgs() {
			if org.Name == source {
				rolesDir = org.RolesDir
				break
			}
		}
	}
	if rolesDir == "" {
		http.Error(w, fmt.Sprintf(`{"error":"org %q not found"}`, source), http.StatusNotFound)
		return
	}

	roleDir := filepath.Join(rolesDir, name)
	if _, err := os.Stat(roleDir); os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf(`{"error":"role %q not found in org %q"}`, name, source), http.StatusNotFound)
		return
	}

	// Build config.yaml — merge with existing to preserve fields we don't edit
	existingCfg, _ := os.ReadFile(filepath.Join(roleDir, "config.yaml"))
	cfgMap := make(map[string]any)
	if existingCfg != nil {
		yaml.Unmarshal(existingCfg, &cfgMap)
	}

	// Update fields from request
	cfgMap["description"] = req.Persona
	if len(req.Capabilities) > 0 {
		cfgMap["imports"] = req.Capabilities
	}
	if req.Model != "" {
		cfgMap["model"] = req.Model
	}
	if req.MaxRounds > 0 {
		cfgMap["max_rounds"] = req.MaxRounds
	}
	if req.Temperature > 0 {
		cfgMap["temperature"] = req.Temperature
	}
	if req.Sandbox != "" {
		cfgMap["sandbox"] = req.Sandbox
	}
	if len(req.Hooks) > 0 {
		cfgMap["hooks"] = req.Hooks
	}

	cfgData, err := yaml.Marshal(cfgMap)
	if err != nil {
		http.Error(w, `{"error":"marshal config failed"}`, http.StatusInternalServerError)
		return
	}

	// Write config.yaml
	if err := os.WriteFile(filepath.Join(roleDir, "config.yaml"), cfgData, 0644); err != nil {
		h.log.Error("org worker config write", zap.Error(err))
		http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
		return
	}

	// Write prompt.md if persona is provided
	if req.Persona != "" {
		if err := os.WriteFile(filepath.Join(roleDir, "prompt.md"), []byte(req.Persona), 0644); err != nil {
			h.log.Error("org worker prompt write", zap.Error(err))
			http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
			return
		}
	}

	common.ResetGlobalBuilder()

	// Return updated worker detail
	key = source + "/" + name
	detail := map[string]any{
		"name":         name,
		"persona":      req.Persona,
		"capabilities": req.Capabilities,
		"model":        req.Model,
		"max_rounds":   req.MaxRounds,
		"temperature":  req.Temperature,
		"sandbox":      req.Sandbox,
		"hooks":        req.Hooks,
		"source":       source,
		"isDefault":    true,
		"isModified":   h.isOrgModified(key, rolesDir, name),
		"isOrg":        true,
	}
	writeJSON(w, http.StatusOK, detail)
}

// DeleteWorker removes a worker.
func (h *WorkerHandler) DeleteWorker(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if rejectKernel(w, name) {
		return
	}

	if err := h.store.Delete(r.Context(), name); err != nil {
		h.log.Error("worker delete", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	common.ResetGlobalBuilder()
	writeJSON(w, http.StatusOK, map[string]any{"message": "worker deleted"})
}

// RevertWorker restores a worker to its default content.
func (h *WorkerHandler) RevertWorker(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if rejectKernel(w, name) {
		return
	}

	// Check for org revert via query param
	source := r.URL.Query().Get("source")
	if source != "" && source != "kernel" {
		h.revertOrgWorker(w, r, name, source)
		return
	}

	// Kernel worker revert
	original, ok := h.defaults[name]
	if !ok {
		http.Error(w, `{"error":"no default for this worker"}`, http.StatusNotFound)
		return
	}

	path := filepath.Join(h.store.Dir(), name+".yaml")
	if err := os.WriteFile(path, original, 0644); err != nil {
		h.log.Error("worker revert", zap.Error(err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	detail, _ := h.store.Get(r.Context(), name)
	common.ResetGlobalBuilder()
	writeJSON(w, http.StatusOK, detail)
}

// revertOrgWorker restores an org role's config.yaml + prompt.md from startup snapshot.
func (h *WorkerHandler) revertOrgWorker(w http.ResponseWriter, r *http.Request, name, source string) {
	key := source + "/" + name
	def, ok := h.orgDefaults[key]
	if !ok {
		http.Error(w, `{"error":"no default for this org role"}`, http.StatusNotFound)
		return
	}

	rolesDir := def.rolesDir
	roleDir := filepath.Join(rolesDir, name)

	// Restore config.yaml
	if err := os.WriteFile(filepath.Join(roleDir, "config.yaml"), def.configYAML, 0644); err != nil {
		h.log.Error("org worker revert config", zap.Error(err))
		http.Error(w, `{"error":"revert failed"}`, http.StatusInternalServerError)
		return
	}

	// Restore prompt.md
	if def.promptMD != nil {
		if err := os.WriteFile(filepath.Join(roleDir, "prompt.md"), def.promptMD, 0644); err != nil {
			h.log.Error("org worker revert prompt", zap.Error(err))
			http.Error(w, `{"error":"revert failed"}`, http.StatusInternalServerError)
			return
		}
	}

	common.ResetGlobalBuilder()

	// Parse the restored config to return details
	var cfg struct {
		Description string   `yaml:"description"`
		Imports     []string `yaml:"imports"`
		Tools       []string `yaml:"tools"`
		MaxRounds   int      `yaml:"max_rounds"`
		Temperature float64  `yaml:"temperature"`
		Model       string   `yaml:"model"`
		Sandbox     string   `yaml:"sandbox"`
		Hooks       []string `yaml:"hooks"`
	}
	yaml.Unmarshal(def.configYAML, &cfg)

	detail := map[string]any{
		"name":         name,
		"persona":      cfg.Description,
		"capabilities": cfg.Imports,
		"model":        cfg.Model,
		"max_rounds":   cfg.MaxRounds,
		"temperature":  cfg.Temperature,
		"sandbox":      cfg.Sandbox,
		"hooks":        cfg.Hooks,
		"source":       source,
		"isDefault":    true,
		"isModified":   false,
		"isOrg":        true,
	}
	writeJSON(w, http.StatusOK, detail)
}

// isKernelModified checks if a kernel worker file differs from its default.
func (h *WorkerHandler) isKernelModified(name string) bool {
	original, ok := h.defaults[name]
	if !ok {
		return false
	}
	current, err := os.ReadFile(filepath.Join(h.store.Dir(), name+".yaml"))
	if err != nil {
		return true
	}
	return string(current) != string(original)
}

// isOrgModified checks if an org role's config.yaml or prompt.md differs from default.
func (h *WorkerHandler) isOrgModified(key, rolesDir, roleName string) bool {
	def, ok := h.orgDefaults[key]
	if !ok {
		return false
	}
	roleDir := filepath.Join(rolesDir, roleName)

	currentCfg, err := os.ReadFile(filepath.Join(roleDir, "config.yaml"))
	if err != nil || string(currentCfg) != string(def.configYAML) {
		return true
	}

	if def.promptMD != nil {
		currentPrompt, err := os.ReadFile(filepath.Join(roleDir, "prompt.md"))
		if err != nil || string(currentPrompt) != string(def.promptMD) {
			return true
		}
	}

	return false
}

// rejectKernel returns true and writes a 403 if name is a kernel worker.
func rejectKernel(w http.ResponseWriter, name string) bool {
	if kernelWorkerNames[name] {
		http.Error(w, `{"error":"kernel workers are immutable (Contract 3.5)"}`, http.StatusForbidden)
		return true
	}
	return false
}
