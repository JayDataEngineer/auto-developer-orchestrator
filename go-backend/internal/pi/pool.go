package pi

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PiPool manages Pi subprocesses per project.
type PiPool struct {
	mu          sync.Mutex
	clients     map[string]*PiClient // key: project path
	logger      *zap.Logger
	idleTimeout time.Duration
}

// NewPiPool creates a new Pi process pool.
func NewPiPool(logger *zap.Logger, idleTimeout time.Duration) *PiPool {
	if idleTimeout == 0 {
		idleTimeout = 5 * time.Minute
	}

	p := &PiPool{
		clients:     make(map[string]*PiClient),
		logger:      logger,
		idleTimeout: idleTimeout,
	}

	// Start idle cleanup goroutine
	go p.cleanupIdle()

	return p
}

// GetOrCreate returns a PiClient for the given project path.
// If no client exists, a new Pi subprocess is spawned.
func (p *PiPool) GetOrCreate(projectPath string) (*PiClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.clients[projectPath]; ok {
		if client.IsRunning() {
			return client, nil
		}
		// Stale client, clean it up
		client.Close()
		delete(p.clients, projectPath)
	}

	client, err := NewPiClient(projectPath, p.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn pi for %s: %w", projectPath, err)
	}

	p.clients[projectPath] = client
	p.logger.Info("Spawned new Pi client", zap.String("project", projectPath))

	return client, nil
}

// Get returns an existing PiClient or nil if not found.
func (p *PiPool) Get(projectPath string) *PiClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	client, ok := p.clients[projectPath]
	if !ok || !client.IsRunning() {
		return nil
	}
	return client
}

// Remove shuts down and removes the PiClient for a project.
func (p *PiPool) Remove(projectPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.clients[projectPath]; ok {
		client.Close()
		delete(p.clients, projectPath)
	}
}

// Shutdown closes all Pi subprocesses.
func (p *PiPool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for path, client := range p.clients {
		client.Close()
		delete(p.clients, path)
	}

	p.logger.Info("PiPool shutdown complete")
}

// cleanupIdle periodically removes idle Pi processes.
func (p *PiPool) cleanupIdle() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		for path, client := range p.clients {
			if !client.IsRunning() {
				client.Close()
				delete(p.clients, path)
				p.logger.Info("Cleaned up dead Pi client", zap.String("project", path))
				continue
			}
		}
		p.mu.Unlock()
	}
}

// ListActive returns paths of all projects with active Pi processes.
func (p *PiPool) ListActive() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	active := make([]string, 0, len(p.clients))
	for path, client := range p.clients {
		if client.IsRunning() {
			active = append(active, path)
		}
	}
	return active
}
