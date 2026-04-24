package llama

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ArtifactType defines the kind of artifact produced.
type ArtifactType string

const (
	ArtifactCode    ArtifactType = "code"
	ArtifactSummary ArtifactType = "summary"
	ArtifactPlan    ArtifactType = "plan"
)

// Artifact represents a typed output from a sub-agent or orchestrator.
// Artifacts are the "distilled essence" of a completed task — the orchestrator
// only sees artifacts, never raw tool output.
type Artifact struct {
	ID        string            `json:"id"`
	ParentID  string            `json:"parentId"`            // orchestrator agent ID
	SourceID  string            `json:"sourceId"`            // sub-agent ID that produced it
	Source    string            `json:"source"`              // which source created it ("orchestrator" or task slug)
	Type      ArtifactType      `json:"type"`                // data, code, summary, file, plan
	Title     string            `json:"title"`               // human-readable title
	Content   string            `json:"content"`             // text content or serialized data
	Metadata  map[string]string `json:"metadata,omitempty"`  // extra fields (filepath, language, etc.)
	CreatedAt time.Time         `json:"createdAt"`
}

// ArtifactRegistry tracks artifacts in memory during an orchestrator session.
// Thread-safe. Artifacts can also be persisted to DB via the existing storage layer.
type ArtifactRegistry struct {
	mu        sync.RWMutex
	artifacts map[string]*Artifact // artifact ID → artifact
	order     []string            // insertion order for listing
	counter   int                 // auto-increment for IDs
}

// NewArtifactRegistry creates a new empty registry.
func NewArtifactRegistry() *ArtifactRegistry {
	return &ArtifactRegistry{
		artifacts: make(map[string]*Artifact),
	}
}

// Create adds a new artifact and returns its ID.
func (r *ArtifactRegistry) Create(artifact *Artifact) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.counter++
	if artifact.ID == "" {
		artifact.ID = fmt.Sprintf("art-%d-%d", r.counter, time.Now().UnixMilli())
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now()
	}

	r.artifacts[artifact.ID] = artifact
	r.order = append(r.order, artifact.ID)
	return artifact.ID
}

// Get retrieves a single artifact by ID.
func (r *ArtifactRegistry) Get(id string) (*Artifact, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.artifacts[id]
	return a, ok
}

// ListByType returns all artifacts of a given type.
func (r *ArtifactRegistry) ListByType(t ArtifactType) []*Artifact {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*Artifact
	for _, id := range r.order {
		a := r.artifacts[id]
		if a.Type == t {
			result = append(result, a)
		}
	}
	return result
}

// All returns all artifacts in creation order.
func (r *ArtifactRegistry) All() []*Artifact {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Artifact, 0, len(r.order))
	for _, id := range r.order {
		result = append(result, r.artifacts[id])
	}
	return result
}

// ── Plan Types ───────────────────────────────────────────────────────

// PlanStep represents a single step in an execution plan.
type PlanStep struct {
	Index    int    `json:"index"`
	Desc     string `json:"desc"`
	Status   string `json:"status"` // "pending", "in_progress", "done", "failed", "skipped"
	Note     string `json:"note"`  // result or error summary
	Artifact string `json:"artifactId,omitempty"`
}

// Plan represents the orchestrator's step-by-step execution plan.
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// ToContent serializes the plan to JSON for storage in Artifact.Content.
func (p *Plan) ToContent() string {
	b, _ := json.Marshal(p)
	return string(b)
}

