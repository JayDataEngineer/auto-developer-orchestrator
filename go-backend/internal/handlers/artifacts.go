package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Artifact represents an agent-generated artifact (plan, todo, notes)
type Artifact struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agentId"`
	Type      string    `json:"type"`  // plan, todo, notes
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ArtifactHandler handles HTTP requests for agent artifacts
type ArtifactHandler struct {
	artifacts map[string]map[string]*Artifact // agentId -> artifactId -> Artifact
	mu        sync.RWMutex
	logger    *zap.Logger
}

// NewArtifactHandler creates a new artifact handler
func NewArtifactHandler(logger *zap.Logger) *ArtifactHandler {
	return &ArtifactHandler{
		artifacts: make(map[string]map[string]*Artifact),
		logger:    logger,
	}
}

// RegisterRoutes registers artifact routes
func (h *ArtifactHandler) RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
}) {
	r.Post("/", h.CreateOrUpdate)
	r.Get("/", h.List)
}

// CreateOrUpdateArtifactRequest is the request body for creating/updating an artifact
type CreateOrUpdateArtifactRequest struct {
	AgentID string `json:"agentId"`
	Type    string `json:"type"`    // plan, todo, notes
	Title   string `json:"title"`
	Content string `json:"content"`
}

// CreateOrUpdate creates or updates an artifact
// POST /api/pi/artifacts
func (h *ArtifactHandler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	var req CreateOrUpdateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.AgentID == "" || req.Type == "" {
		http.Error(w, "agentId and type are required", http.StatusBadRequest)
		return
	}

	artifactID := req.AgentID + ":" + req.Type

	h.mu.Lock()
	if h.artifacts[req.AgentID] == nil {
		h.artifacts[req.AgentID] = make(map[string]*Artifact)
	}

	artifact := &Artifact{
		ID:        artifactID,
		AgentID:   req.AgentID,
		Type:      req.Type,
		Title:     req.Title,
		Content:   req.Content,
		UpdatedAt: time.Now(),
	}
	h.artifacts[req.AgentID][artifactID] = artifact
	h.mu.Unlock()

	h.logger.Debug("artifact updated",
		zap.String("agent_id", req.AgentID),
		zap.String("type", req.Type),
		zap.String("title", req.Title),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// List returns all artifacts for an agent
// GET /api/pi/artifacts?agentId=...
func (h *ArtifactHandler) List(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agentId")
	if agentID == "" {
		http.Error(w, "agentId query parameter required", http.StatusBadRequest)
		return
	}

	h.mu.RLock()
	agentArtifacts := h.artifacts[agentID]
	h.mu.RUnlock()

	var result []*Artifact
	if agentArtifacts != nil {
		for _, a := range agentArtifacts {
			result = append(result, a)
		}
	}

	if result == nil {
		result = []*Artifact{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"artifacts": result,
	})
}
