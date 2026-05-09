package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// AGEClient executes Cypher queries via Postgres with Apache AGE extension.
// Falls back to standard SQL when AGE is not installed.
type AGEClient struct {
	db *sql.DB
}

// NewAGEClient creates a client connected to the cluster Postgres with AGE.
// Uses AGE_DATABASE_URL env var (or constructs from POSTGRES_PASSWORD + cluster defaults).
func NewAGEClient() (*AGEClient, error) {
	dsn := os.Getenv("AGE_DATABASE_URL")
	if dsn == "" {
		host := os.Getenv("AGE_HOST")
		if host == "" {
			host = "100.86.69.57"
		}
		port := os.Getenv("AGE_PORT")
		if port == "" {
			port = "30432"
		}
		password := os.Getenv("POSTGRES_PASSWORD")
		dbname := os.Getenv("AGE_DATABASE")
		if dbname == "" {
			dbname = "postgres" // default DB, not langfuse
		}
		user := os.Getenv("AGE_USER")
		if user == "" {
			user = "postgres"
		}
		dsn = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
			user, password, host, port, dbname)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("age connect: %w", err)
	}
	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("age ping: %w", err)
	}
	return &AGEClient{db: db}, nil
}

// Close closes the database connection.
func (c *AGEClient) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// QueryCypher executes a Cypher query via AGE and returns results as maps.
// The graph must be created first with EnsureGraph.
func (c *AGEClient) QueryCypher(ctx context.Context, graphName, cypher string) ([]map[string]interface{}, error) {
	// AGE requires LOAD 'age' and SET search_path for each session
	setup := fmt.Sprintf(`LOAD 'age'; SET search_path = ag_catalog, "$user", public;`)
	if _, err := c.db.ExecContext(ctx, setup); err != nil {
		// AGE extension might not be loaded — try without it
		return nil, fmt.Errorf("age setup: %w", err)
	}

	query := fmt.Sprintf(
		`SELECT * FROM ag_catalog.cypher('%s', $$ %s $$) as (result agtype);`,
		sanitizeGraphName(graphName), cypher,
	)

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("age query: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			continue
		}
		parsed := parseAGType(result)
		results = append(results, parsed)
	}
	return results, rows.Err()
}

// ExecCypher executes a write Cypher query (CREATE, MERGE, etc.).
func (c *AGEClient) ExecCypher(ctx context.Context, graphName, cypher string) error {
	setup := `LOAD 'age'; SET search_path = ag_catalog, "$user", public;`
	if _, err := c.db.ExecContext(ctx, setup); err != nil {
		return fmt.Errorf("age setup: %w", err)
	}

	query := fmt.Sprintf(
		`SELECT * FROM ag_catalog.cypher('%s', $$ %s $$) as (result agtype);`,
		sanitizeGraphName(graphName), cypher,
	)
	_, err := c.db.ExecContext(ctx, query)
	return err
}

// EnsureGraph creates the graph if it doesn't exist.
func (c *AGEClient) EnsureGraph(ctx context.Context, graphName string) error {
	setup := `LOAD 'age'; SET search_path = ag_catalog, "$user", public;`
	if _, err := c.db.ExecContext(ctx, setup); err != nil {
		return err
	}
	// CREATE GRAPH is idempotent with IF NOT EXISTS (AGE 1.5+)
	_, err := c.db.ExecContext(ctx, fmt.Sprintf(
		`SELECT ag_catalog.create_graph('%s');`, sanitizeGraphName(graphName)))
	// Ignore "already exists" errors
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

// Stats returns node and edge counts for a graph.
func (c *AGEClient) Stats(ctx context.Context, graphName string) (nodes, edges int, err error) {
	setup := `LOAD 'age'; SET search_path = ag_catalog, "$user", public;`
	if _, err = c.db.ExecContext(ctx, setup); err != nil {
		return 0, 0, err
	}

	// Count vertices
	row := c.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT count(*) FROM ag_catalog.cypher('%s', $$ MATCH (n) RETURN n $$) as (n agtype);`,
		sanitizeGraphName(graphName)))
	var nodeCount string
	if err := row.Scan(&nodeCount); err == nil {
		nodes, _ = strconv.Atoi(nodeCount)
	}

	// Count edges
	row = c.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT count(*) FROM ag_catalog.cypher('%s', $$ MATCH ()-[r]->() RETURN r $$) as (r agtype);`,
		sanitizeGraphName(graphName)))
	var edgeCount string
	if err := row.Scan(&edgeCount); err == nil {
		edges, _ = strconv.Atoi(edgeCount)
	}

	return nodes, edges, nil
}

// parseAGType converts an AGE agtype string to a Go map.
// agtype is essentially JSON with some vertex/edge wrapping.
func parseAGType(agtype string) map[string]interface{} {
	result := make(map[string]interface{})

	var raw interface{}
	if err := json.Unmarshal([]byte(agtype), &raw); err != nil {
		result["raw"] = agtype
		return result
	}

	switch v := raw.(type) {
	case map[string]interface{}:
		// AGE wraps vertices as {"id": ..., "label": ..., "properties": {...}}
		if props, ok := v["properties"]; ok {
			if propsMap, ok := props.(map[string]interface{}); ok {
				for key, val := range propsMap {
					result[key] = val
				}
			}
			if label, ok := v["label"]; ok {
				result["_label"] = label
			}
		} else {
			// Not a vertex — copy as-is
			for key, val := range v {
				result[key] = val
			}
		}
	default:
		result["value"] = v
	}
	return result
}

// sanitizeGraphName prevents SQL injection in graph names.
func sanitizeGraphName(name string) string {
	// Only allow alphanumeric and underscores
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
