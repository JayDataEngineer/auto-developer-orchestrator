package nlp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

const entityExtractionPrompt = `Extract named entities from the following text. Return a JSON array of entities, each with "name", "type" (PERSON, ORG, LOCATION, DATE, or OTHER), and optional "context".

Text:
{{.Text}}

Output JSON:`

const clusteringPrompt = `Cluster the following items into groups based on semantic similarity. Return a JSON array of clusters, each with "cluster_id", "label", and "items".

Items:
{{.Items}}

Output JSON:`

// LLMProvider wraps the core LLMProvider interface.
type LLMProvider interface {
	StreamChat(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error)
	ModelName() string
	ContextSize() int
}

// ExtractEntitiesTool extracts named entities using LLM.
type ExtractEntitiesTool struct {
	provider LLMProvider
}

// NewExtractEntitiesTool creates a new entity extraction tool.
func NewExtractEntitiesTool(provider LLMProvider) *ExtractEntitiesTool {
	return &ExtractEntitiesTool{provider: provider}
}

func (t *ExtractEntitiesTool) Name() string        { return "extract_entities" }
func (t *ExtractEntitiesTool) Description() string { return "Extract named entities from text using LLM" }

func (t *ExtractEntitiesTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"text": {"type": "string", "description": "Text to extract entities from"},
			"output_path": {"type": "string", "description": "Optional path to save JSON output"}
		},
		"required": ["text"]
	}`)
}

func (t *ExtractEntitiesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return nil, fmt.Errorf("missing required parameter 'text'")
	}

	outputPath, _ := args["output_path"].(string)

	prompt := entityExtractionPrompt
	prompt = replaceTemplate(prompt, "Text", text)

	messages := []core.Message{
		{Role: string(core.RoleUser), Content: prompt},
	}

	opts := core.GenerateOptions{
		MaxTokens:   4096,
		Temperature: 0.2,
	}

	events, err := t.provider.StreamChat(ctx, messages, nil, opts)
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var content strings.Builder
	for event := range events {
		if event.Content != "" {
			content.WriteString(event.Content)
		}
		if event.Finish != "" {
			break
		}
	}

	entities := t.parseEntities(content.String())

	if outputPath != "" {
		data, _ := json.MarshalIndent(entities, "", "  ")
		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			return nil, fmt.Errorf("write output: %w", err)
		}
		return map[string]any{"entities": entities, "saved_to": outputPath}, nil
	}

	return map[string]any{"entities": entities, "count": len(entities)}, nil
}

func (t *ExtractEntitiesTool) parseEntities(content string) []map[string]any {
	var result []map[string]any
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return []map[string]any{{"raw": content}}
	}
	return result
}

// ClusterContentTool clusters content items using LLM.
type ClusterContentTool struct {
	provider LLMProvider
}

// NewClusterContentTool creates a new content clustering tool.
func NewClusterContentTool(provider LLMProvider) *ClusterContentTool {
	return &ClusterContentTool{provider: provider}
}

func (t *ClusterContentTool) Name() string        { return "cluster_content" }
func (t *ClusterContentTool) Description() string { return "Cluster content items using LLM" }

func (t *ClusterContentTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"items_path": {"type": "string", "description": "Path to JSON array of items"},
			"output_path": {"type": "string", "description": "Optional path to save JSON output"}
		},
		"required": ["items_path"]
	}`)
}

func (t *ClusterContentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	itemsPath, _ := args["items_path"].(string)
	if itemsPath == "" {
		return nil, fmt.Errorf("missing required parameter 'items_path'")
	}

	outputPath, _ := args["output_path"].(string)

	data, err := os.ReadFile(itemsPath)
	if err != nil {
		return nil, fmt.Errorf("read items: %w", err)
	}

	var items []string
	if err := json.Unmarshal(data, &items); err != nil {
		var itemsMap []map[string]any
		if err := json.Unmarshal(data, &itemsMap); err != nil {
			return nil, fmt.Errorf("parse items: %w", err)
		}
		for _, item := range itemsMap {
			if s, ok := item["text"].(string); ok {
				items = append(items, s)
			}
		}
	}

	itemsJSON, _ := json.Marshal(items)

	prompt := clusteringPrompt
	prompt = replaceTemplate(prompt, "Items", string(itemsJSON))

	messages := []core.Message{
		{Role: string(core.RoleUser), Content: prompt},
	}

	opts := core.GenerateOptions{
		MaxTokens:   4096,
		Temperature: 0.3,
	}

	events, err := t.provider.StreamChat(ctx, messages, nil, opts)
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var content strings.Builder
	for event := range events {
		if event.Content != "" {
			content.WriteString(event.Content)
		}
		if event.Finish != "" {
			break
		}
	}

	clusters := t.parseClusters(content.String())

	if outputPath != "" {
		data, _ := json.MarshalIndent(clusters, "", "  ")
		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			return nil, fmt.Errorf("write output: %w", err)
		}
		return map[string]any{"clusters": clusters, "count": len(clusters), "saved_to": outputPath}, nil
	}

	return map[string]any{"clusters": clusters, "count": len(clusters)}, nil
}

func (t *ClusterContentTool) parseClusters(content string) []map[string]any {
	var result []map[string]any
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return []map[string]any{{"raw": content}}
	}
	return result
}

func replaceTemplate(template, key, value string) string {
	return strings.Replace(template, "{{."+key+"}}", value, 1)
}

func RegisterAll(tools []core.Tool, provider LLMProvider) []core.Tool {
	if provider == nil {
		return tools
	}
	return append(tools,
		NewExtractEntitiesTool(provider),
		NewClusterContentTool(provider),
	)
}