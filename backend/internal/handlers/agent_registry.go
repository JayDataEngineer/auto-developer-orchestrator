package handlers

import (
	"sync"
	"time"
)

// AgentStatus represents the current state of an agent.
type AgentStatus string

const (
	AgentStatusRunning AgentStatus = "running"
	AgentStatusIdle    AgentStatus = "idle"
)

// AgentEntry tracks a single running agent.
type AgentEntry struct {
	Project     string    `json:"project"`
	AgentID     string    `json:"agentId"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"startedAt"`
	LastEventAt time.Time `json:"lastEventAt"`
}

// AgentRegistry is a thread-safe in-memory registry of running agents.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*AgentEntry
}

// NewAgentRegistry creates a new AgentRegistry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*AgentEntry),
	}
}

// Start registers an agent as running.
func (r *AgentRegistry) Start(project, agentID string) {
	key := compositeAgentKey(project, agentID)
	now := time.Now()
	r.mu.Lock()
	r.agents[key] = &AgentEntry{
		Project:     project,
		AgentID:     agentID,
		Status:      string(AgentStatusRunning),
		StartedAt:   now,
		LastEventAt: now,
	}
	r.mu.Unlock()
}

// Stop removes an agent from the registry.
func (r *AgentRegistry) Stop(project, agentID string) {
	key := compositeAgentKey(project, agentID)
	r.mu.Lock()
	delete(r.agents, key)
	r.mu.Unlock()
}

// Bump updates the LastEventAt timestamp for a running agent.
func (r *AgentRegistry) Bump(project, agentID string) {
	key := compositeAgentKey(project, agentID)
	r.mu.Lock()
	if e, ok := r.agents[key]; ok {
		e.LastEventAt = time.Now()
	}
	r.mu.Unlock()
}

// IsRunning returns true if an agent is currently registered as running.
func (r *AgentRegistry) IsRunning(project, agentID string) bool {
	key := compositeAgentKey(project, agentID)
	r.mu.RLock()
	_, ok := r.agents[key]
	r.mu.RUnlock()
	return ok
}

// GetAllRunning returns all currently running agents.
func (r *AgentRegistry) GetAllRunning() []AgentEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]AgentEntry, 0, len(r.agents))
	for _, e := range r.agents {
		result = append(result, *e)
	}
	return result
}
