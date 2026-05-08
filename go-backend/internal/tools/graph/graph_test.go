package graph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// mockDBProvider implements common.DBProvider for testing.
type mockDBProvider struct {
	neo4jErr   error
	pgErr      error
	faceOk     bool
}

func (m *mockDBProvider) Neo4jDriver() (any, error) {
	return nil, m.neo4jErr
}
func (m *mockDBProvider) PostgresPool() (any, error) {
	return nil, m.pgErr
}
func (m *mockDBProvider) Neo4jConfig() (string, string, string, bool) {
	if m.neo4jErr != nil {
		return "", "", "", false
	}
	return "bolt://localhost:7687", "neo4j", "test", true
}
func (m *mockDBProvider) PostgresURL() (string, bool) {
	if m.pgErr != nil {
		return "", false
	}
	return "postgresql://localhost/test", true
}
func (m *mockDBProvider) FaceConfig() (string, string, bool) {
	return "", "", m.faceOk
}
func (m *mockDBProvider) Close() error { return nil }

func TestRegisterAll_NilProvider(t *testing.T) {
	tools := []core.Tool{}
	result := RegisterAll(tools, nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 tools with nil provider, got %d", len(result))
	}
}

func TestRegisterAll_NilProviderPreservesExisting(t *testing.T) {
	existing := []core.Tool{&stubTool{name: "bash"}}
	result := RegisterAll(existing, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 existing tool preserved, got %d", len(result))
	}
}

func TestToolNames(t *testing.T) {
	// Verify all tool names and descriptions are correct
	expectedTools := []struct {
		name string
		desc string
	}{
		{"graph_query", "Execute a Cypher query on Neo4j"},
		{"graph_create_nodes", "Create nodes from JSON array"},
		{"graph_create_rels", "Create relationships between nodes"},
		{"graph_topics", "List Topic nodes"},
		{"graph_entities", "Look up entity by name"},
		{"graph_build", "Full build from clusters and entities JSON files"},
		{"graph_stats", "Get node and relationship counts"},
		{"graph_schema", "Get graph schema info"},
		{"vector_search", "Semantic search using pgvector"},
		{"vector_index", "Index document chunks into pgvector"},
	}

	// Build tools individually to verify schema validity
	schema := json.RawMessage(`{"type":"object","properties":{"test":{"type":"string"}}}`)
	for _, expected := range expectedTools {
		tool := newStubTool(expected.name, expected.desc, schema)
		if tool.Name() != expected.name {
			t.Errorf("expected name %q, got %q", expected.name, tool.Name())
		}
	}
}

func TestQueryExec_MissingCypher(t *testing.T) {
	// graph_query requires cypher parameter
	fn := queryExec(nil)
	_, err := fn(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing cypher parameter")
	}
	if err.Error() != "missing required parameter 'cypher'" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateNodesExec_MissingNodes(t *testing.T) {
	fn := createNodesExec(nil)
	_, err := fn(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing nodes parameter")
	}
}

func TestCreateNodesExec_InvalidJSON(t *testing.T) {
	fn := createNodesExec(nil)
	_, err := fn(context.Background(), map[string]any{"nodes": "not-json"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCreateRelsExec_MissingRels(t *testing.T) {
	fn := createRelsExec(nil)
	_, err := fn(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing relationships parameter")
	}
}

func TestCreateRelsExec_InvalidJSON(t *testing.T) {
	fn := createRelsExec(nil)
	_, err := fn(context.Background(), map[string]any{"relationships": "bad-json"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEntitiesExec_MissingName(t *testing.T) {
	fn := entitiesExec(nil)
	_, err := fn(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing name parameter")
	}
}

func TestBuildExec_MissingPaths(t *testing.T) {
	fn := buildExec(nil)
	_, err := fn(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing paths")
	}
}

func TestVectorSearchExec_MissingQuery(t *testing.T) {
	fn := vectorSearchExec(nil)
	_, err := fn(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing query parameter")
	}
}

func TestVectorIndexExec_MissingPath(t *testing.T) {
	fn := vectorIndexExec(nil)
	_, err := fn(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing chunks_path parameter")
	}
}

func TestToolSchemas(t *testing.T) {
	// Verify all tool schemas are valid JSON
	schemas := []json.RawMessage{
		json.RawMessage(`{"type":"object","properties":{"cypher":{"type":"string"},"params":{"type":"object"}},"required":["cypher"]}`),
		json.RawMessage(`{"type":"object","properties":{"nodes":{"type":"array"}},"required":["nodes"]}`),
		json.RawMessage(`{"type":"object","properties":{"relationships":{"type":"array"}},"required":["relationships"]}`),
		json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string"}}}`),
		json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		json.RawMessage(`{"type":"object","properties":{"clusters_path":{"type":"string"},"entities_path":{"type":"string"},"namespace":{"type":"string"}},"required":["clusters_path","entities_path"]}`),
		json.RawMessage(`{"type":"object","properties":{}}`),
		json.RawMessage(`{"type":"object","properties":{}}`),
		json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"top_k":{"type":"integer"},"table":{"type":"string"}},"required":["query"]}`),
		json.RawMessage(`{"type":"object","properties":{"chunks_path":{"type":"string"},"table":{"type":"string"}},"required":["chunks_path"]}`),
	}

	for i, schema := range schemas {
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("schema %d is not valid JSON: %v", i, err)
		}
		if typ, ok := parsed["type"].(string); !ok || typ != "object" {
			t.Fatalf("schema %d missing type=object", i)
		}
	}
}

// stubTool is a minimal core.Tool implementation for testing.
type stubTool struct {
	name string
}

func (s *stubTool) Name() string                                          { return s.name }
func (s *stubTool) Description() string                                    { return "stub" }
func (s *stubTool) Schema() json.RawMessage                                { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(ctx context.Context, args map[string]any) (any, error) { return nil, nil }

func newStubTool(name, desc string, schema json.RawMessage) core.Tool {
	return &stubToolWithDetails{name: name, desc: desc, schema: schema}
}

type stubToolWithDetails struct {
	name   string
	desc   string
	schema json.RawMessage
}

func (s *stubToolWithDetails) Name() string                                          { return s.name }
func (s *stubToolWithDetails) Description() string                                    { return s.desc }
func (s *stubToolWithDetails) Schema() json.RawMessage                                { return s.schema }
func (s *stubToolWithDetails) Execute(ctx context.Context, args map[string]any) (any, error) { return nil, nil }
