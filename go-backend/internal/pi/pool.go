package pi

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const maxAgentsPerProject = 5

// compositeKey builds a pool map key from projectPath and agentId.
func compositeKey(projectPath, agentId string) string {
	return projectPath + "\x00" + agentId
}

// splitCompositeKey splits a composite key back into projectPath and agentId.
func splitCompositeKey(key string) (projectPath, agentId string) {
	idx := strings.Index(key, "\x00")
	if idx < 0 {
		return key, "default"
	}
	return key[:idx], key[idx+1:]
}

// PiPool manages Pi subprocesses per project, supporting multiple agents per project.
type PiPool struct {
	mu          sync.Mutex
	clients     map[string]*PiClient // key: compositeKey(projectPath, agentId)
	logger      *zap.Logger
	idleTimeout time.Duration
	sandboxMgr  interface{} // *sandbox.Manager (using interface to avoid import cycle)
}

// NewPiPool creates a new Pi process pool.
func NewPiPool(logger *zap.Logger, idleTimeout time.Duration) *PiPool {
	return NewPiPoolWithSandbox(logger, idleTimeout, nil)
}

// NewPiPoolWithSandbox creates a new Pi process pool with sandbox manager.
func NewPiPoolWithSandbox(logger *zap.Logger, idleTimeout time.Duration, sandboxMgr interface{}) *PiPool {
	if idleTimeout == 0 {
		idleTimeout = 5 * time.Minute
	}

	p := &PiPool{
		clients:     make(map[string]*PiClient),
		logger:      logger,
		idleTimeout: idleTimeout,
		sandboxMgr:  sandboxMgr,
	}

	// Start idle cleanup goroutine
	go p.cleanupIdle()

	return p
}

// GetOrCreate returns a PiClient for the given project path with agentId "default".
// Backward-compatible wrapper.
func (p *PiPool) GetOrCreate(projectPath string) (*PiClient, error) {
	return p.GetOrCreateWithID(projectPath, "default")
}

// GetOrCreateWithID returns a PiClient for the given project path and agentId.
// If no client exists, a new Pi subprocess is spawned.
// Session resume is NOT used for new agents to avoid loading stale 90K+ token contexts.
func (p *PiPool) GetOrCreateWithID(projectPath, agentId string) (*PiClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := compositeKey(projectPath, agentId)

	if client, ok := p.clients[key]; ok {
		if client.IsRunning() {
			return client, nil
		}
		// Stale client, clean it up
		p.logger.Warn("Evicting stale Pi client",
			zap.String("project", projectPath),
			zap.String("agentId", agentId),
		)
		client.Close()
		delete(p.clients, key)
	}

	// Enforce max agents per project — evict oldest idle agent if at limit
	if p.countForProjectLocked(projectPath) >= maxAgentsPerProject {
		evicted := p.evictOldestLocked(projectPath)
		if !evicted {
			return nil, fmt.Errorf("max agents (%d) reached for project %s (all active, cannot evict)", maxAgentsPerProject, filepath.Base(projectPath))
		}
		p.logger.Warn("Evicted oldest agent to make room",
			zap.String("project", projectPath),
			zap.String("newAgentId", agentId),
		)
	}

	// Type assertion for sandbox manager
	var sandboxMgrTyped interface{}
	if p.sandboxMgr != nil {
		sandboxMgrTyped = p.sandboxMgr
	}

	// Start FRESH — no session resume. Old sessions accumulate 90K+ tokens
	// of stale context that makes every request hang for 30+ seconds.
	client, err := NewPiClientWithSession(projectPath, agentId, p.logger, sandboxMgrTyped, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to spawn pi for %s: %w", projectPath, err)
	}

	p.clients[key] = client
	p.logger.Info("Spawned new Pi client",
		zap.String("project", projectPath),
		zap.String("agentId", agentId),
	)

	return client, nil
}

// Get returns an existing PiClient for projectPath with agentId "default", or nil.
func (p *PiPool) Get(projectPath string) *PiClient {
	return p.GetWithID(projectPath, "default")
}

// GetWithID returns an existing PiClient for the given composite key, or nil.
func (p *PiPool) GetWithID(projectPath, agentId string) *PiClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := compositeKey(projectPath, agentId)
	client, ok := p.clients[key]
	if !ok || !client.IsRunning() {
		return nil
	}
	return client
}

// Remove shuts down and removes the PiClient for a project with agentId "default".
func (p *PiPool) Remove(projectPath string) {
	p.RemoveAgent(projectPath, "default")
}

// RemoveAgent shuts down and removes a specific agent.
func (p *PiPool) RemoveAgent(projectPath, agentId string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := compositeKey(projectPath, agentId)
	if client, ok := p.clients[key]; ok {
		client.Close()
		delete(p.clients, key)
	}
}

// Shutdown closes all Pi subprocesses.
func (p *PiPool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for key, client := range p.clients {
		client.Close()
		delete(p.clients, key)
	}

	p.logger.Info("PiPool shutdown complete")
}

// cleanupIdle periodically removes dead and idle Pi processes.
// Dead: process not running. Idle: running but no activity for idleTimeout.
func (p *PiPool) cleanupIdle() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		for key, client := range p.clients {
			if !client.IsRunning() {
				client.Close()
				delete(p.clients, key)
				pPath, aId := splitCompositeKey(key)
				p.logger.Warn("GC: evicted dead Pi client",
					zap.String("project", filepath.Base(pPath)),
					zap.String("agentId", aId),
				)
				continue
			}
			// Evict idle agents that haven't been used
			age := time.Since(client.startTime)
			state := client.GetState()
			if age > p.idleTimeout && !state.Streaming {
				client.Close()
				delete(p.clients, key)
				pPath, aId := splitCompositeKey(key)
				p.logger.Warn("GC: evicted idle Pi client",
					zap.String("project", filepath.Base(pPath)),
					zap.String("agentId", aId),
					zap.Duration("age", age),
				)
			}
		}
		p.mu.Unlock()
	}
}

// evictOldestLocked evicts the oldest non-streaming agent for the given project.
// Must be called with p.mu held. Returns true if an agent was evicted.
func (p *PiPool) evictOldestLocked(projectPath string) bool {
	var oldestKey string
	var oldestTime time.Time

	for key, client := range p.clients {
		pPath, _ := splitCompositeKey(key)
		if pPath != projectPath {
			continue
		}
		// Don't evict agents that are actively streaming
		if client.GetState().Streaming {
			continue
		}
		if oldestKey == "" || client.startTime.Before(oldestTime) {
			oldestKey = key
			oldestTime = client.startTime
		}
	}

	if oldestKey == "" {
		return false
	}

	client := p.clients[oldestKey]
	client.Close()
	delete(p.clients, oldestKey)
	pPath, aId := splitCompositeKey(oldestKey)
	p.logger.Warn("Evicted oldest agent",
		zap.String("project", filepath.Base(pPath)),
		zap.String("agentId", aId),
		zap.Duration("age", time.Since(oldestTime)),
	)
	return true
}

// RegisterIfAbsent registers an existing PiClient in the pool if not already present.
// This allows sub-agents (created outside the pool) to be reachable via /api/pi/prompt
// for multi-turn conversations.
func (p *PiPool) RegisterIfAbsent(projectPath, agentId string, client *PiClient) (*PiClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := compositeKey(projectPath, agentId)

	if existing, ok := p.clients[key]; ok {
		if existing.IsRunning() {
			return existing, nil
		}
		existing.Close()
		delete(p.clients, key)
	}

	if p.countForProjectLocked(projectPath) >= maxAgentsPerProject {
		return nil, fmt.Errorf("max agents (%d) reached for project %s", maxAgentsPerProject, filepath.Base(projectPath))
	}

	p.clients[key] = client
	p.logger.Info("Registered external PiClient in pool",
		zap.String("project", projectPath),
		zap.String("agentId", agentId),
	)
	return client, nil
}

// ListActive returns paths of all projects with active Pi processes.
func (p *PiPool) ListActive() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	seen := make(map[string]bool)
	for key, client := range p.clients {
		if client.IsRunning() {
			projectPath, _ := splitCompositeKey(key)
			seen[projectPath] = true
		}
	}
	active := make([]string, 0, len(seen))
	for path := range seen {
		active = append(active, path)
	}
	return active
}

// ListActiveByProject returns all agents for a single project.
func (p *PiPool) ListActiveByProject(projectPath string) []AgentEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	var entries []AgentEntry
	for key, client := range p.clients {
		pPath, aId := splitCompositeKey(key)
		if pPath != projectPath || !client.IsRunning() {
			continue
		}
		entries = append(entries, AgentEntry{
			AgentId:     aId,
			Project:     filepath.Base(pPath),
			ProjectPath: pPath,
			Namespace:   client.Namespace(), // Per-project OpenShell namespace
			State:       client.GetState(),
		})
	}
	return entries
}

// ListAllActive returns all agents grouped by project path.
func (p *PiPool) ListAllActive() map[string][]AgentEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make(map[string][]AgentEntry)
	for key, client := range p.clients {
		if !client.IsRunning() {
			continue
		}
		pPath, aId := splitCompositeKey(key)
		entry := AgentEntry{
			AgentId:     aId,
			Project:     filepath.Base(pPath),
			ProjectPath: pPath,
			Namespace:   client.Namespace(), // Per-project OpenShell namespace
			State:       client.GetState(),
		}
		result[pPath] = append(result[pPath], entry)
	}
	return result
}

// CountForProject returns the number of active agents for a project.
func (p *PiPool) CountForProject(projectPath string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.countForProjectLocked(projectPath)
}

// countForProjectLocked is the lock-held version of CountForProject.
func (p *PiPool) countForProjectLocked(projectPath string) int {
	count := 0
	for key, client := range p.clients {
		pPath, _ := splitCompositeKey(key)
		if pPath == projectPath && client.IsRunning() {
			count++
		}
	}
	return count
}
