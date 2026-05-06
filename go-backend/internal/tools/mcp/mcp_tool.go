package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpclient "github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// MCPTool wraps an MCP server tool as a core.Tool for the agent registry.
type MCPTool struct {
	name        string
	description string
	schema      json.RawMessage
	client      *mcpclient.MultiClient
}

// NewMCPTool creates a core.Tool wrapper for an MCP tool.
func NewMCPTool(tool mcpclient.MCPTool, client *mcpclient.MultiClient) *MCPTool {
	return &MCPTool{
		name:        tool.Name,
		description: tool.Description,
		schema:      tool.InputSchema,
		client:      client,
	}
}

func (t *MCPTool) Name() string        { return t.name }
func (t *MCPTool) Description() string { return t.description }
func (t *MCPTool) Schema() json.RawMessage { return t.schema }

func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	result, err := t.client.CallTool(ctx, t.name, args)
	if err != nil {
		return nil, fmt.Errorf("MCP %s: %w", t.name, err)
	}
	return result, nil
}

// RegisterAll creates core.Tool wrappers for all tools discovered by the MultiClient
// and appends them to the tools slice. Returns the updated slice.
func RegisterAll(tools []core.Tool, client *mcpclient.MultiClient) []core.Tool {
	if client == nil {
		return tools
	}

	mcpTools := client.AllTools()
	for _, t := range mcpTools {
		tools = append(tools, NewMCPTool(t, client))
	}

	return tools
}
