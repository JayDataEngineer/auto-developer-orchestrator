package nlp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// mockProvider implements LLMProvider for testing.
type mockProvider struct {
	response string
	events   []core.ChatEvent
}

func (m *mockProvider) StreamChat(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
	ch := make(chan core.ChatEvent, len(m.events)+1)
	for _, evt := range m.events {
		ch <- evt
	}
	ch <- core.ChatEvent{Finish: "stop"}
	close(ch)
	return ch, nil
}

func (m *mockProvider) ModelName() string    { return "test-model" }
func (m *mockProvider) ContextSize() int      { return 4096 }

func TestRegisterAll_NilProvider(t *testing.T) {
	tools := []core.Tool{}
	result := RegisterAll(tools, nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 tools with nil provider, got %d", len(result))
	}
}

func TestRegisterAll_ValidProvider(t *testing.T) {
	provider := &mockProvider{}
	tools := []core.Tool{}
	result := RegisterAll(tools, provider)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}

	expected := []string{"extract_entities", "cluster_content"}
	for i, name := range expected {
		if result[i].Name() != name {
			t.Errorf("tool %d: expected %q, got %q", i, name, result[i].Name())
		}
	}
}

func TestRegisterAll_PreservesExisting(t *testing.T) {
	provider := &mockProvider{}
	existing := []core.Tool{&stubTool{name: "bash"}}
	result := RegisterAll(existing, provider)
	if len(result) != 3 {
		t.Fatalf("expected 3 tools (1 existing + 2 nlp), got %d", len(result))
	}
}

func TestExtractEntities_MissingText(t *testing.T) {
	provider := &mockProvider{}
	tool := NewExtractEntitiesTool(provider)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing text parameter")
	}
	if err.Error() != "missing required parameter 'text'" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractEntities_BasicExtraction(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: `[{"name": "Alice", "type": "PERSON"}, {"name": "Google", "type": "ORG"}]`},
		},
	}
	tool := NewExtractEntitiesTool(provider)

	result, err := tool.Execute(context.Background(), map[string]any{
		"text": "Alice works at Google",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	entities := m["entities"].([]map[string]any)
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}
	if entities[0]["name"] != "Alice" {
		t.Errorf("first entity: expected Alice, got %v", entities[0]["name"])
	}
	if entities[1]["name"] != "Google" {
		t.Errorf("second entity: expected Google, got %v", entities[1]["name"])
	}
}

func TestExtractEntities_SaveToFile(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: `[{"name": "Test", "type": "OTHER"}]`},
		},
	}
	tool := NewExtractEntitiesTool(provider)

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "entities.json")

	result, err := tool.Execute(context.Background(), map[string]any{
		"text":        "test text",
		"output_path": outputPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	if m["saved_to"] != outputPath {
		t.Errorf("expected saved_to=%q, got %v", outputPath, m["saved_to"])
	}

	// Verify file was written
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}

	var entities []map[string]any
	if err := json.Unmarshal(data, &entities); err != nil {
		t.Fatalf("output file is not valid JSON: %v", err)
	}
}

func TestExtractEntities_NonJSONResponse(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: "The entities are Alice and Bob."},
		},
	}
	tool := NewExtractEntitiesTool(provider)

	result, err := tool.Execute(context.Background(), map[string]any{
		"text": "Alice and Bob",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	entities := m["entities"].([]map[string]any)
	if len(entities) != 1 {
		t.Fatalf("expected 1 raw entity for non-JSON response, got %d", len(entities))
	}
	if entities[0]["raw"] != "The entities are Alice and Bob." {
		t.Errorf("unexpected raw content: %v", entities[0]["raw"])
	}
}

func TestClusterContent_MissingPath(t *testing.T) {
	provider := &mockProvider{}
	tool := NewClusterContentTool(provider)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing items_path parameter")
	}
}

func TestClusterContent_NonexistentPath(t *testing.T) {
	provider := &mockProvider{}
	tool := NewClusterContentTool(provider)
	_, err := tool.Execute(context.Background(), map[string]any{
		"items_path": "/nonexistent/path.json",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestClusterContent_StringArray(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: `[{"cluster_id": 0, "label": "Tech", "items": ["AI", "ML"]}]`},
		},
	}
	tool := NewClusterContentTool(provider)

	tmpDir := t.TempDir()
	itemsPath := filepath.Join(tmpDir, "items.json")
	items := []string{"AI", "ML", "deep learning"}
	data, _ := json.Marshal(items)
	os.WriteFile(itemsPath, data, 0644)

	result, err := tool.Execute(context.Background(), map[string]any{
		"items_path": itemsPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	clusters := m["clusters"].([]map[string]any)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
}

func TestClusterContent_ObjectArray(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: `[{"cluster_id": 0, "label": "News", "items": ["article1"]}]`},
		},
	}
	tool := NewClusterContentTool(provider)

	tmpDir := t.TempDir()
	itemsPath := filepath.Join(tmpDir, "items.json")
	items := []map[string]any{{"text": "article1 about news"}}
	data, _ := json.Marshal(items)
	os.WriteFile(itemsPath, data, 0644)

	_, err := tool.Execute(context.Background(), map[string]any{
		"items_path": itemsPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClusterContent_SaveToFile(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: `[{"cluster_id": 0, "label": "Group1"}]`},
		},
	}
	tool := NewClusterContentTool(provider)

	tmpDir := t.TempDir()
	itemsPath := filepath.Join(tmpDir, "items.json")
	os.WriteFile(itemsPath, []byte(`["a", "b"]`), 0644)
	outputPath := filepath.Join(tmpDir, "clusters.json")

	result, err := tool.Execute(context.Background(), map[string]any{
		"items_path":  itemsPath,
		"output_path": outputPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	if m["saved_to"] != outputPath {
		t.Errorf("expected saved_to=%q, got %v", outputPath, m["saved_to"])
	}

	// Verify file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("output file not created")
	}
}

func TestToolSchemas(t *testing.T) {
	provider := &mockProvider{}
	tools := []core.Tool{
		NewExtractEntitiesTool(provider),
		NewClusterContentTool(provider),
	}

	for _, tool := range tools {
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("tool %q has invalid schema: %v", tool.Name(), err)
		}
		if typ, ok := parsed["type"].(string); !ok || typ != "object" {
			t.Fatalf("tool %q schema missing type=object", tool.Name())
		}
		props, _ := parsed["properties"].(map[string]any)
		if len(props) == 0 {
			t.Fatalf("tool %q schema has no properties", tool.Name())
		}
	}
}

func TestReplaceTemplate(t *testing.T) {
	result := replaceTemplate("Hello {{.Name}}, welcome!", "Name", "World")
	if result != "Hello World, welcome!" {
		t.Errorf("unexpected result: %q", result)
	}
}

type stubTool struct {
	name string
}

func (s *stubTool) Name() string                                          { return s.name }
func (s *stubTool) Description() string                                    { return "stub" }
func (s *stubTool) Schema() json.RawMessage                                { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(ctx context.Context, args map[string]any) (any, error) { return nil, nil }
