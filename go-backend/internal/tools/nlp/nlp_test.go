package nlp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
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

func (m *mockProvider) ModelName() string   { return "test-model" }
func (m *mockProvider) ContextSize() int     { return 4096 }

func TestRegisterAll_NilProvider(t *testing.T) {
	result := RegisterAll(nil, nil)
	if result != nil {
		t.Fatalf("expected nil with nil provider, got %d tools", len(result))
	}
}

func TestRegisterAll_ValidProvider(t *testing.T) {
	provider := &mockProvider{}
	tools := RegisterAll(nil, provider)
	expected := []string{"extract_entities", "cluster_content"}
	testutil.AssertToolNames(t, tools, expected)
	testutil.AssertValidSchemas(t, tools)
}

func TestRegisterAll_PreservesExisting(t *testing.T) {
	existing := []core.Tool{testutil.NewStubTool("bash")}
	result := RegisterAll(existing, &mockProvider{})
	if len(result) != 3 {
		t.Fatalf("expected 3 tools (1 existing + 2 nlp), got %d", len(result))
	}
}

func TestExtractEntities_MissingText(t *testing.T) {
	tool := NewExtractEntitiesTool(&mockProvider{})
	testutil.AssertMissingParam(t, tool, "text")
}

func TestExtractEntities_BasicExtraction(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: `[{"name": "Alice", "type": "PERSON"}, {"name": "Google", "type": "ORG"}]`},
		},
	}
	tool := NewExtractEntitiesTool(provider)

	result, err := tool.Execute(context.Background(), map[string]any{"text": "Alice works at Google"})
	testutil.AssertNoError(t, err)

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
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "saved_to", outputPath)

	data, _ := os.ReadFile(outputPath)
	testutil.AssertJSONValid(t, data)
}

func TestExtractEntities_NonJSONResponse(t *testing.T) {
	provider := &mockProvider{
		events: []core.ChatEvent{
			{Content: "The entities are Alice and Bob."},
		},
	}
	tool := NewExtractEntitiesTool(provider)

	result, err := tool.Execute(context.Background(), map[string]any{"text": "Alice and Bob"})
	testutil.AssertNoError(t, err)

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
	tool := NewClusterContentTool(&mockProvider{})
	testutil.AssertMissingParam(t, tool, "items_path")
}

func TestClusterContent_NonexistentPath(t *testing.T) {
	tool := NewClusterContentTool(&mockProvider{})
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

	result, err := tool.Execute(context.Background(), map[string]any{"items_path": itemsPath})
	testutil.AssertNoError(t, err)

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

	_, err := tool.Execute(context.Background(), map[string]any{"items_path": itemsPath})
	testutil.AssertNoError(t, err)
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
	testutil.AssertNoError(t, err)

	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "saved_to", outputPath)

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
	testutil.AssertValidSchemas(t, tools)
}

func TestReplaceTemplate(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		key      string
		value    string
		expected string
	}{
		{name: "single placeholder", tmpl: "Hello {{.Name}}, welcome!", key: "Name", value: "World", expected: "Hello World, welcome!"},
		{name: "multiple same placeholders", tmpl: "{{.X}} and {{.X}}", key: "X", value: "same", expected: "same and {{.X}}"},
		{name: "empty value", tmpl: "Hello {{.Name}}!", key: "Name", value: "", expected: "Hello !"},
		{name: "no placeholder", tmpl: "Hello World!", key: "Missing", value: "X", expected: "Hello World!"},
		{name: "JSON in template", tmpl: `Items: {{.Items}}`, key: "Items", value: `["a","b"]`, expected: `Items: ["a","b"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceTemplate(tt.tmpl, tt.key, tt.value)
			if got != tt.expected {
				t.Errorf("replaceTemplate() = %q, want %q", got, tt.expected)
			}
		})
	}
}
