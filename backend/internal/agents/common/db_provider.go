package common

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBProvider is an interface for providing database configurations.
type DBProvider interface {
	Neo4jDriver() (neo4j.Driver, error)
	PostgresPool() (*pgxpool.Pool, error)
	Neo4jConfig() (uri, username, password string, ok bool)
	PostgresURL() (url string, ok bool)
	Close() error
}

// OrgDBProvider holds database connections for an organization.
type OrgDBProvider struct {
	dbs          map[string]DatabaseConfig
	mu           sync.RWMutex
	neo4jDriver  neo4j.Driver
	pgPool       *pgxpool.Pool
}

func resolveEnv(password, passwordEnv string) string {
	if password != "" {
		return password
	}
	if passwordEnv != "" {
		return os.Getenv(passwordEnv)
	}
	return ""
}

// NewOrgDBProvider creates a new OrgDBProvider from database configs.
func NewOrgDBProvider(dbs map[string]DatabaseConfig) *OrgDBProvider {
	return &OrgDBProvider{
		dbs: dbs,
	}
}

// Neo4jConfig returns the Neo4j connection configuration.
func (p *OrgDBProvider) Neo4jConfig() (uri, username, password string, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cfg, exists := p.dbs["neo4j"]
	if !exists {
		return "", "", "", false
	}
	return cfg.URI, cfg.Username, resolveEnv(cfg.Password, cfg.PasswordEnv), true
}

// PostgresURL returns the Postgres connection URL.
func (p *OrgDBProvider) PostgresURL() (url string, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cfg, exists := p.dbs["postgres"]
	if !exists {
		return "", false
	}
	return resolveEnv(cfg.URL, cfg.PasswordEnv), true
}

// Neo4jDriver returns a lazy-initialized Neo4j driver.
func (p *OrgDBProvider) Neo4jDriver() (neo4j.Driver, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.neo4jDriver != nil {
		return p.neo4jDriver, nil
	}

	uri, username, password, ok := p.neo4jConfigLocked()
	if !ok {
		return nil, fmt.Errorf("neo4j not configured")
	}

	driver, err := neo4j.NewDriver(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to create neo4j driver: %w", err)
	}

	p.neo4jDriver = driver
	return driver, nil
}

func (p *OrgDBProvider) neo4jConfigLocked() (uri, username, password string, ok bool) {
	cfg, exists := p.dbs["neo4j"]
	if !exists {
		return "", "", "", false
	}
	return cfg.URI, cfg.Username, resolveEnv(cfg.Password, cfg.PasswordEnv), true
}

// PostgresPool returns a lazy-initialized Postgres connection pool.
func (p *OrgDBProvider) PostgresPool() (*pgxpool.Pool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pgPool != nil {
		return p.pgPool, nil
	}

	url, ok := p.postgresURLLocked()
	if !ok {
		return nil, fmt.Errorf("postgres not configured")
	}

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres pool: %w", err)
	}

	p.pgPool = pool
	return pool, nil
}

func (p *OrgDBProvider) postgresURLLocked() (url string, ok bool) {
	cfg, exists := p.dbs["postgres"]
	if !exists {
		return "", false
	}
	return resolveEnv(cfg.URL, cfg.PasswordEnv), true
}

// Close closes all database connections.
func (p *OrgDBProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error

	if p.neo4jDriver != nil {
		if err := p.neo4jDriver.Close(); err != nil {
			errs = append(errs, fmt.Errorf("neo4j driver close: %w", err))
		}
	}

	if p.pgPool != nil {
		p.pgPool.Close()
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing connections: %v", errs)
	}
	return nil
}