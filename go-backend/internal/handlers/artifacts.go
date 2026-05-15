package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// Artifact represents an agent-generated artifact (plan, todo, notes)
type Artifact struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agentId"`
	Type      string    `json:"type"` // plan, todo, notes
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ArtifactHandler handles HTTP requests for agent artifacts
type ArtifactHandler struct {
	artifacts map[string]map[string]*Artifact // agentId -> artifactId -> Artifact (in-memory cache)
	mu        sync.RWMutex
	db        *storage.Database
	logger    *zap.Logger
}

// NewArtifactHandler creates a new artifact handler
func NewArtifactHandler(db *storage.Database, logger *zap.Logger) *ArtifactHandler {
	return &ArtifactHandler{
		artifacts: make(map[string]map[string]*Artifact),
		db:        db,
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
	Type    string `json:"type"` // plan, todo, notes
	Title   string `json:"title"`
	Content string `json:"content"`
}

// CreateOrUpdate creates or updates an artifact
// POST /api/pux/artifacts
func (h *ArtifactHandler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[CreateOrUpdateArtifactRequest](w, r)
	if !ok { return }

	if req.AgentID == "" || req.Type == "" {
		JSONError(w, "agentId and type are required", http.StatusBadRequest)
		return
	}

	artifactID := req.AgentID + ":" + req.Type

	artifact := &Artifact{
		ID:        artifactID,
		AgentID:   req.AgentID,
		Type:      req.Type,
		Title:     req.Title,
		Content:   req.Content,
		UpdatedAt: time.Now(),
	}

	// Persist to database
	if h.db != nil {
		if err := h.db.SaveArtifact(r.Context(), &storage.DBArtifact{
			ID:      artifactID,
			AgentID: req.AgentID,
			Type:    req.Type,
			Title:   req.Title,
			Content: req.Content,
		}); err != nil {
			h.logger.Warn("failed to persist artifact to DB", zap.Error(err))
		}
	}

	// Update in-memory cache
	h.mu.Lock()
	if h.artifacts[req.AgentID] == nil {
		h.artifacts[req.AgentID] = make(map[string]*Artifact)
	}
	h.artifacts[req.AgentID][artifactID] = artifact
	h.mu.Unlock()

	h.logger.Debug("artifact updated",
		zap.String("agent_id", req.AgentID),
		zap.String("type", req.Type),
		zap.String("title", req.Title),
	)

	writeJSON(w, http.StatusOK, artifact)
}

// List returns all artifacts for an agent
// GET /api/pux/artifacts?agentId=...
func (h *ArtifactHandler) List(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agentId")
	if agentID == "" {
		JSONError(w, "agentId query parameter required", http.StatusBadRequest)
		return
	}

	// Try database first
	if h.db != nil {
		dbArtifacts, err := h.db.GetArtifactsByAgent(r.Context(), agentID)
		if err == nil && len(dbArtifacts) > 0 {
			result := make([]*Artifact, 0, len(dbArtifacts))
			for _, a := range dbArtifacts {
				result = append(result, &Artifact{
					ID:        a.ID,
					AgentID:   a.AgentID,
					Type:      a.Type,
					Title:     a.Title,
					Content:   a.Content,
					UpdatedAt: time.Time{}, // close enough for listing
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"artifacts": result,
			})
			return
		}
	}

	// Fallback to in-memory cache
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

	writeJSON(w, http.StatusOK, map[string]any{
		"artifacts": result,
	})
}
