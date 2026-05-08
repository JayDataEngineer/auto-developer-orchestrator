package graph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
	"github.com/auto-developer-orchestrator/backend/internal/tools/base"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// errDBProvider returns errors for all database operations.
// Satisfies common.DBProvider using concrete neo4j.Driver and pgxpool.Pool return types.
type errDBProvider struct{}

func (e *errDBProvider) Neo4jDriver() (neo4j.Driver, error)    { return nil, errors.New("no neo4j") }
func (e *errDBProvider) PostgresPool() (*pgxpool.Pool, error)  { return nil, errors.New("no pg") }
func (e *errDBProvider) Close() error                           { return nil }
func (e *errDBProvider) Neo4jConfig() (string, string, string, bool) { return "", "", "", false }
func (e *errDBProvider) PostgresURL() (string, bool)                 { return "", false }
func (e *errDBProvider) FaceConfig() (string, string, bool)          { return "", "", false }

func TestRegisterAll_NilProvider(t *testing.T) {
	result := RegisterAll(nil, nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 tools with nil provider, got %d", len(result))
	}
}

func TestRegisterAll_PreservesExisting(t *testing.T) {
	existing := []core.Tool{testutil.NewStubTool("bash")}
	result := RegisterAll(existing, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 existing tool preserved, got %d", len(result))
	}
}

func TestRegisterAll_WithProvider(t *testing.T) {
	db := &errDBProvider{}
	tools := RegisterAll(nil, db)
	expected := []string{
		"graph_query", "graph_create_nodes", "graph_create_rels",
		"graph_topics", "graph_entities", "graph_build",
		"graph_stats", "graph_schema",
		"vector_search", "vector_index",
	}
	testutil.AssertToolNames(t, tools, expected)
	testutil.AssertValidSchemas(t, tools)
}

func TestExecFunctions_MissingParams(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(common.DBProvider) base.ToolFunc
		errMsg string
	}{
		{name: "graph_query", fn: queryExec, errMsg: "missing required parameter 'cypher'"},
		{name: "graph_create_nodes", fn: createNodesExec, errMsg: "missing required parameter 'nodes'"},
		{name: "graph_create_rels", fn: createRelsExec, errMsg: "missing required parameter 'relationships'"},
		{name: "graph_entities", fn: entitiesExec, errMsg: "missing required parameter 'name'"},
		{name: "graph_build", fn: buildExec, errMsg: "missing required parameters"},
		{name: "vector_search", fn: vectorSearchExec, errMsg: "missing required parameter 'query'"},
		{name: "vector_index", fn: vectorIndexExec, errMsg: "missing required parameter 'chunks_path'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := tt.fn(nil)
			_, err := fn(context.Background(), map[string]any{})
			testutil.AssertErrorContains(t, err, tt.errMsg)
		})
	}
}

func TestCreateNodes_InvalidJSON(t *testing.T) {
	fn := createNodesExec(nil)
	_, err := fn(context.Background(), map[string]any{"nodes": "not-json"})
	testutil.AssertErrorContains(t, err, "parse nodes")
}

func TestCreateRels_InvalidJSON(t *testing.T) {
	fn := createRelsExec(nil)
	_, err := fn(context.Background(), map[string]any{"relationships": "bad-json"})
	testutil.AssertErrorContains(t, err, "parse relationships")
}

func TestBuildExec_PartialPaths(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{name: "missing entities_path", args: map[string]any{"clusters_path": "/tmp/c.json"}},
		{name: "missing clusters_path", args: map[string]any{"entities_path": "/tmp/e.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := buildExec(nil)
			_, err := fn(context.Background(), tt.args)
			testutil.AssertErrorContains(t, err, "missing required parameters")
		})
	}
}

func TestBuildExec_WithFiles(t *testing.T) {
	db := &errDBProvider{}
	fn := buildExec(db)

	tmpDir := t.TempDir()
	clustersPath := filepath.Join(tmpDir, "clusters.json")
	entitiesPath := filepath.Join(tmpDir, "entities.json")
	os.WriteFile(clustersPath, []byte(`[{"name": "topic1"}]`), 0644)
	os.WriteFile(entitiesPath, []byte(`[{"name": "entity1", "type": "PERSON"}]`), 0644)

	_, err := fn(context.Background(), map[string]any{
		"clusters_path": clustersPath,
		"entities_path": entitiesPath,
	})
	// Should fail with DB error after reading files successfully
	testutil.AssertErrorContains(t, err, "no neo4j")
}

func TestVectorSearch_DefaultTopK(t *testing.T) {
	db := &errDBProvider{}
	fn := vectorSearchExec(db)
	_, err := fn(context.Background(), map[string]any{"query": "test"})
	// Should fail with DB error (not missing param), proving top_k defaults
	testutil.AssertErrorContains(t, err, "no pg")
}

func TestVectorSearch_MissingQuery(t *testing.T) {
	db := &errDBProvider{}
	fn := vectorSearchExec(db)
	_, err := fn(context.Background(), map[string]any{"top_k": 10})
	testutil.AssertErrorContains(t, err, "missing required parameter 'query'")
}

func TestVectorIndex_DefaultTable(t *testing.T) {
	db := &errDBProvider{}
	fn := vectorIndexExec(db)

	tmpDir := t.TempDir()
	chunksPath := filepath.Join(tmpDir, "chunks.json")
	os.WriteFile(chunksPath, []byte(`[{"content": "hello"}]`), 0644)

	_, err := fn(context.Background(), map[string]any{"chunks_path": chunksPath})
	// Should fail with DB error (not missing param), proving default table name is used
	testutil.AssertErrorContains(t, err, "no pg")
}

func TestTopicsExec_WithNamespace(t *testing.T) {
	db := &errDBProvider{}
	fn := topicsExec(db)
	_, err := fn(context.Background(), map[string]any{"namespace": "test"})
	testutil.AssertErrorContains(t, err, "no neo4j")
}

func TestTopicsExec_WithoutNamespace(t *testing.T) {
	db := &errDBProvider{}
	fn := topicsExec(db)
	_, err := fn(context.Background(), map[string]any{})
	testutil.AssertErrorContains(t, err, "no neo4j")
}
